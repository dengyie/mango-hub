package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	"github.com/komari-monitor/komari/database/auditlog"
	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/database/tasks"
	"github.com/komari-monitor/komari/pkg/rpc"
	v2 "github.com/komari-monitor/komari/protocol/v2"
	"github.com/komari-monitor/komari/utils"
	agent_runtime "github.com/komari-monitor/komari/web/agent"
)

// admin.mining.go
// 挖矿管控 RPC2 方法（admin 命名空间）：中心端启动/暂停子节点挖矿任务。
// 命令在节点侧由 agent 的 AGENT_MINER_CONTROL_CMD 模板定义（含 {action} 占位符），
// hub 只下发 action，执行走 agent 的远程任务链路，结果用 task_id 经 getTaskResultsByTaskId 查询。

func init() {
	reg("miningControl", adminMiningControl, "Start or stop mining on clients (requires agent AGENT_MINER_CONTROL_CMD)")
}

// partitionTargets 将目标节点按「在线可即时下发 / 在线排队补发 / 离线」分区。
// 查询以函数注入隔离 agent_runtime 全局态，表驱动测试见 admin.mining_test.go；
// 顺序保持调用方传入顺序，便于结果呈现与排查。
func partitionTargets(clients []string, isConnected, isOnline func(string) bool) (online, queued, offline []string) {
	for _, uuid := range clients {
		switch {
		case isConnected(uuid):
			online = append(online, uuid)
		case isOnline(uuid):
			queued = append(queued, uuid)
		default:
			offline = append(offline, uuid)
		}
	}
	return
}

// splitByCapability 把连通性分区后的节点再按 miner_controllable 切开。
// 不可控的节点不能下发：agent 会立刻 -1 拒绝，Hub 却显示 RPC 成功。
func splitByCapability(uuids []string, isControllable func(string) bool) (supported, unsupported []string) {
	for _, uuid := range uuids {
		if isControllable(uuid) {
			supported = append(supported, uuid)
			continue
		}
		unsupported = append(unsupported, uuid)
	}
	return
}

func clientSupportsMiningControl(uuid string) bool {
	client, err := clients.GetClientByUUID(uuid)
	return err == nil && client.MinerControllable
}

// miningControlRejectReason 保留原因文案分类（全员离线 ≠ 没配 CONTROL_CMD）。
// RPC 本身不再因此返回 InvalidParams：抽屉是单 uuid，必须带回 task_id
// 和 unsupported_clients，否则前端 catch 只能显示笼统「下发失败」。
func miningControlRejectReason(online, queued, unsupported, offline []string) string {
	if len(online) > 0 || len(queued) > 0 {
		return ""
	}
	if len(unsupported) > 0 && len(offline) == 0 {
		return "No online client supports mining control (agent must set AGENT_MINER_CONTROL_CMD and leave remote control enabled)"
	}
	if len(unsupported) == 0 {
		return "No online clients"
	}
	return "No online client supports mining control (targets are offline or missing AGENT_MINER_CONTROL_CMD)"
}

const miningControlUnsupportedResult = "Client does not support mining control (agent must set AGENT_MINER_CONTROL_CMD and leave remote control enabled)"

// dispatchToConnected 对在线节点逐个下发 payload；单点失败记录并继续，
// 绝不因一个连接中断整批（否则会出现「部分节点已执行但 RPC 整体报错」的误导）。
// 返回 (成功清单, 失败清单)，失败结果由调用方落 tasks.SaveTaskResult。
func dispatchToConnected(payload []byte, uuids []string, send func(uuid string) error) (sent, failed []string) {
	for _, uuid := range uuids {
		if err := send(uuid); err != nil {
			failed = append(failed, uuid)
			continue
		}
		sent = append(sent, uuid)
	}
	return
}

func adminMiningControl(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		Clients []string `json:"clients"`
		Action  string   `json:"action"` // "start" | "stop"
	}
	req.BindParams(&params)
	if params.Action != "start" && params.Action != "stop" {
		return nil, rpc.MakeError(rpc.InvalidParams, "action must be \"start\" or \"stop\"", params.Action)
	}
	if len(params.Clients) == 0 {
		return nil, rpc.MakeError(rpc.InvalidParams, "clients is required", nil)
	}

	online, queued, offline := partitionTargets(params.Clients,
		func(uuid string) bool { return agent_runtime.GetConnectedClient(uuid) != nil },
		agent_runtime.IsAgentOnline,
	)
	online, unsupportedOnline := splitByCapability(online, clientSupportsMiningControl)
	queued, unsupportedQueued := splitByCapability(queued, clientSupportsMiningControl)
	unsupported := append(append([]string{}, unsupportedOnline...), unsupportedQueued...)
	// 即使没有可下发目标也建任务：抽屉是单 uuid，必须带回 task_id +
	// unsupported_clients，否则前端 catch 只能显示笼统「下发失败」。

	taskId := utils.GenerateRandomString(16)
	taskClients := make([]string, 0, len(online)+len(queued)+len(offline)+len(unsupported))
	taskClients = append(taskClients, online...)
	taskClients = append(taskClients, queued...)
	taskClients = append(taskClients, offline...)
	taskClients = append(taskClients, unsupported...)
	if err := tasks.CreateTask(taskId, taskClients, "mining "+params.Action); err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "Failed to create task: "+err.Error(), nil)
	}

	payload, _ := json.Marshal(v2.Request{
		JSONRPC: v2.Version,
		Method:  v2.MethodAgentMiningControl,
		Params:  v2.MiningControlParams{TaskID: taskId, Action: params.Action},
	})
	now := time.Now().UTC()
	sent, failed := dispatchToConnected(payload, online, func(uuid string) error {
		client := agent_runtime.GetConnectedClient(uuid)
		if client == nil {
			return fmt.Errorf("connection lost")
		}
		return client.WriteMessage(websocket.TextMessage, payload)
	})
	for _, uuid := range failed {
		tasks.SaveTaskResult(taskId, uuid, "Dispatch failed: client connection broke during send", -1, now)
	}
	for _, uuid := range queued {
		agent_runtime.DispatchV2Event(uuid, v2.MethodAgentMiningControl,
			v2.MiningControlParams{TaskID: taskId, Action: params.Action})
	}
	for _, uuid := range offline {
		tasks.SaveTaskResult(taskId, uuid, "Client offline!", -1, now)
	}
	for _, uuid := range unsupported {
		tasks.SaveTaskResult(taskId, uuid, miningControlUnsupportedResult, -1, now)
	}

	actor, ip := auditActor(ctx)
	auditlog.Log(ip, actor, fmt.Sprintf("Mining control: %s, task id: %s", params.Action, taskId), "warn")

	return map[string]any{
		"task_id":             taskId,
		"action":              params.Action,
		"clients":             online,
		"sent_clients":        sent,
		"queued_clients":      queued,
		"offline_clients":     offline,
		"failed_clients":      failed,
		"unsupported_clients": unsupported,
	}, nil
}
