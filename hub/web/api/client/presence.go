package client

import (
	"sync"
	"time"

	"github.com/dengyie/mangohub/hub/utils/notifier"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
)

const (
	// 如果超过这个时间没有收到任何消息，则认为连接已死。
	// 必须明显大于 agent 的上报间隔（--interval，常见 10s）加上 GenerateReport()
	// 的耗时（开 GPU 监控后要跑 nvidia-smi，1–2s）。此前取 11s 只比 10s 的间隔多
	// 1s 余量，节点每隔几轮就因读超时被断开，日志刷 close 1006。
	readWait = 45 * time.Second

	postPresenceTTL = 35 * time.Second

	// maxCloseReasonLength 是 WebSocket close frame 能携带的最长 reason：
	// 125 字节控制帧负载减去 2 字节状态码。
	maxCloseReasonLength = 123
)

// zombieConnThreshold 决定重复连接到达时，已注册连接算"活着"的时限。
// 健康连接有两种活跃证据，都计入 SafeConn.lastActive：数据帧（每
// --interval，默认 3s，常见 ≤10s）与控制帧（agent 每 30s 心跳 ping，
// hub 回 pong，ping/pong handler 各自刷新）。因此阈值必须大于心跳周期
// （30s），否则慢上报节点（--interval 超过阈值）会被误判为半开死连接
// （休眠、NAT 重建）而遭健康接管；同时必须小于 readWait——后者仍是
// 没有挑战者时的兜底清理。仅为测试可调。
var zombieConnThreshold = 40 * time.Second

// duplicateFreshWindow 是免探测直接拒绝挑战者的窗口：最后一帧距今不足
// 该值说明对端仍在持续发帧（默认上报间隔 3s + 采集耗时），无需再花一个
// 探测往返去确认。介于该窗口与 zombieConnThreshold 之间的"不确定区"
// 交给 ProbeAlive 用一个 ping/pong 往返裁决，半开死连接因此能在 ~3s
// 内被健康接管，而不是像旧逻辑那样让重连的 agent 白吃 1008 拒绝、再
// 等 40s 僵尸过期（生产日志中的常态假失联窗口）。仅为测试可调。
var duplicateFreshWindow = 4 * time.Second

// duplicateProbeWait 是 ProbeAlive 等待 pong 的时限。要大于跨 CDN 的
// 一个往返（数百毫秒量级），小于 agent 两次重连尝试的间隔（旧版二进制
// 无退避，~2s），否则上一个挑战者的探测还没出结果，下一个挑战者就要
// 排队。仅为测试可调。
var duplicateProbeWait = 3 * time.Second

type postPresenceEntry struct {
	connID     int64
	timer      *time.Timer
	generation uint64
}

var (
	postPresenceMu     sync.Mutex
	postPresenceStates = make(map[string]*postPresenceEntry)
)

// refreshPostPresence 管理 HTTP POST 上报者的在线/离线状态。
func refreshPostPresence(uuid string) {
	postPresenceMu.Lock()
	defer postPresenceMu.Unlock()

	if entry, exists := postPresenceStates[uuid]; exists {
		entry.generation++
		entry.timer.Stop()
		gen := entry.generation
		entry.timer = time.AfterFunc(postPresenceTTL, func() {
			postPresenceExpired(uuid, entry.connID, gen)
		})
		agent_runtime.KeepAlivePresence(uuid, entry.connID, postPresenceTTL)
		return
	}

	connID := time.Now().UnixNano()
	agent_runtime.KeepAlivePresence(uuid, connID, postPresenceTTL)
	agent_runtime.MarkV2Client(uuid)
	go notifier.OnlineNotification(uuid, connID)

	defaultGeneration := uint64(0)
	entry := &postPresenceEntry{connID: connID, generation: defaultGeneration}
	entry.timer = time.AfterFunc(postPresenceTTL, func() {
		postPresenceExpired(uuid, connID, defaultGeneration)
	})
	postPresenceStates[uuid] = entry
}

func postPresenceExpired(uuid string, connID int64, gen uint64) {
	postPresenceMu.Lock()
	e, ok := postPresenceStates[uuid]
	if !ok || e.connID != connID || e.generation != gen {
		postPresenceMu.Unlock()
		return
	}
	delete(postPresenceStates, uuid)
	postPresenceMu.Unlock()

	agent_runtime.SetPresence(uuid, connID, false)
	notifier.OfflineNotification(uuid, connID)
}
