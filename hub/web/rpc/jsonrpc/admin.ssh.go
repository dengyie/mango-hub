package jsonrpc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dengyie/mangohub/hub/database/clients"
	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/database/ssh"
	"github.com/dengyie/mangohub/hub/pkg/rpc"
	v2 "github.com/dengyie/mangohub/hub/protocol/v2"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
	"gorm.io/gorm"
)

// admin.ssh.go
// SSH 密钥管理与跨节点申请 RPC2 处理函数

func init() {
	// 管理员管理 SSH 密钥列表
	RegisterWithGroupAndMeta("listSSHKeys", rpc.RoleAdmin, adminListSSHKeys, &rpc.MethodMeta{
		Name:        "admin:listSSHKeys",
		Summary:     "List all managed SSH keys",
		Description: "Returns all registered SSH keys without revealing private keys",
		Returns:     "{ keys: SSHKey[] }",
	})

	// 管理员添加/导入 SSH 密钥
	RegisterWithGroupAndMeta("createSSHKey", rpc.RoleAdmin, adminCreateSSHKey, &rpc.MethodMeta{
		Name:        "admin:createSSHKey",
		Summary:     "Create or import an SSH key",
		Description: "Add a new public key (and optional private key) to the vault",
		Params: []rpc.ParamMeta{
			{Name: "name", Type: "string", Description: "Key name"},
			{Name: "public_key", Type: "string", Description: "SSH Public key string"},
			{Name: "private_key", Type: "string", Description: "Optional OpenSSH private key"},
			{Name: "scope", Type: "string", Description: "Scope: global/group/client"},
			{Name: "target_uuid", Type: "string", Description: "Target client UUID if scope is client"},
			{Name: "description", Type: "string", Description: "Description"},
		},
		Returns: "{ key: SSHKey }",
	})

	// 管理员自动生成 Ed25519 密钥对
	RegisterWithGroupAndMeta("generateSSHKey", rpc.RoleAdmin, adminGenerateSSHKey, &rpc.MethodMeta{
		Name:        "admin:generateSSHKey",
		Summary:     "Generate an Ed25519 SSH keypair",
		Description: "Automatically generate an Ed25519 keypair and store encrypted in vault",
		Params: []rpc.ParamMeta{
			{Name: "name", Type: "string", Description: "Key name"},
			{Name: "scope", Type: "string", Description: "Scope: global/group/client"},
			{Name: "target_uuid", Type: "string", Description: "Target client UUID if scope is client"},
			{Name: "description", Type: "string", Description: "Description"},
		},
		Returns: "{ key: SSHKey, private_key: string }",
	})

	// 管理员删除 SSH 密钥
	RegisterWithGroupAndMeta("deleteSSHKey", rpc.RoleAdmin, adminDeleteSSHKey, &rpc.MethodMeta{
		Name:        "admin:deleteSSHKey",
		Summary:     "Delete an SSH key",
		Description: "Remove an SSH key from the vault",
		Params: []rpc.ParamMeta{
			{Name: "id", Type: "string", Description: "SSH key ID"},
		},
		Returns: "{ success: boolean }",
	})

	// 跨节点互访申请 SSH 凭据（允许已认证的 Client 节点或 Admin 申请）
	RegisterWithGroupAndMeta("requestSSHTicket", rpc.RoleClient, clientRequestSSHTicket, &rpc.MethodMeta{
		Name:        "client:requestSSHTicket",
		Summary:     "Request SSH access ticket to target cloud host",
		Description: "Request authorization to SSH into another client device",
		Params: []rpc.ParamMeta{
			{Name: "source_client_uuid", Type: "string", Description: "Source client UUID (optional if calling via client token)"},
			{Name: "target_client_uuid", Type: "string", Description: "Target client UUID to access"},
			{Name: "duration_seconds", Type: "number", Description: "Desired duration in seconds (default 3600, max 86400)"},
			{Name: "target_user", Type: "string", Description: "Target login username (default ubuntu/root)"},
		},
		Returns: "{ ticket: SSHTicket, private_key: string, target_info: object }",
	})
	RegisterWithGroupAndMeta("requestSSHTicket", rpc.RoleAdmin, clientRequestSSHTicket, &rpc.MethodMeta{
		Name:        "admin:requestSSHTicket",
		Summary:     "Request SSH access ticket to target cloud host by Admin",
		Description: "Admin request authorization to SSH into a client device",
		Params: []rpc.ParamMeta{
			{Name: "source_client_uuid", Type: "string", Description: "Source client UUID (optional)"},
			{Name: "target_client_uuid", Type: "string", Description: "Target client UUID to access"},
			{Name: "duration_seconds", Type: "number", Description: "Desired duration in seconds (default 3600, max 86400)"},
			{Name: "target_user", Type: "string", Description: "Target login username (default ubuntu/root)"},
		},
		Returns: "{ ticket: SSHTicket, private_key: string, target_info: object }",
	})

	// 吊销互访 Ticket
	RegisterWithGroupAndMeta("revokeSSHTicket", rpc.RoleClient, clientRevokeSSHTicket, &rpc.MethodMeta{
		Name:        "client:revokeSSHTicket",
		Summary:     "Revoke an active SSH access ticket",
		Description: "Revoke ticket and remove public key on target client immediately",
		Params: []rpc.ParamMeta{
			{Name: "ticket_id", Type: "string", Description: "Ticket ID to revoke"},
		},
		Returns: "{ success: boolean }",
	})
	RegisterWithGroupAndMeta("revokeSSHTicket", rpc.RoleAdmin, clientRevokeSSHTicket, &rpc.MethodMeta{
		Name:        "admin:revokeSSHTicket",
		Summary:     "Revoke an active SSH access ticket by Admin",
		Description: "Admin revoke ticket and remove public key on target client immediately",
		Params: []rpc.ParamMeta{
			{Name: "ticket_id", Type: "string", Description: "Ticket ID to revoke"},
		},
		Returns: "{ success: boolean }",
	})

	// 管理员查询与审计 SSH 工单列表
	RegisterWithGroupAndMeta("listSSHTickets", rpc.RoleAdmin, adminListSSHTickets, &rpc.MethodMeta{
		Name:        "admin:listSSHTickets",
		Summary:     "List SSH access tickets with filter and pagination",
		Description: "Admin query and audit all issued SSH access tickets",
		Params: []rpc.ParamMeta{
			{Name: "status", Type: "string", Description: "Filter by status: active, revoked, expired, failed"},
			{Name: "target_client", Type: "string", Description: "Filter by target client UUID"},
			{Name: "source_client", Type: "string", Description: "Filter by source client UUID"},
			{Name: "limit", Type: "number", Description: "Page size (default 50, max 200)"},
			{Name: "offset", Type: "number", Description: "Offset (default 0)"},
		},
		Returns: "{ tickets: SSHTicket[], total: number }",
	})

	// 管理员查询单个 SSH 工单详情
	RegisterWithGroupAndMeta("getSSHTicket", rpc.RoleAdmin, adminGetSSHTicket, &rpc.MethodMeta{
		Name:        "admin:getSSHTicket",
		Summary:     "Get detailed info of a specific SSH access ticket",
		Description: "Admin get details and execution error message for a ticket",
		Params: []rpc.ParamMeta{
			{Name: "ticket_id", Type: "string", Description: "Ticket ID to query"},
		},
		Returns: "{ ticket: SSHTicket }",
	})
}

func adminListSSHKeys(_ context.Context, _ *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	keys, err := ssh.ListSSHKeys()
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "Failed to list SSH keys: "+err.Error(), nil)
	}
	return map[string]any{"keys": keys}, nil
}

func adminCreateSSHKey(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		Name        string `json:"name"`
		PublicKey   string `json:"public_key"`
		PrivateKey  string `json:"private_key"`
		Scope       string `json:"scope"`
		TargetUUID  string `json:"target_uuid"`
		Description string `json:"description"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	if params.Name == "" || params.PublicKey == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "name and public_key are required", nil)
	}
	if params.Scope == "" {
		params.Scope = "global"
	}
	key, err := ssh.CreateSSHKey(params.Name, params.Scope, params.TargetUUID, params.Description, params.PublicKey, params.PrivateKey)
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return map[string]any{"key": key}, nil
}

func adminGenerateSSHKey(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		TargetUUID  string `json:"target_uuid"`
		Description string `json:"description"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	if params.Name == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "name is required", nil)
	}
	if params.Scope == "" {
		params.Scope = "global"
	}
	key, privKey, err := ssh.GenerateAndSaveSSHKey(params.Name, params.Scope, params.TargetUUID, params.Description)
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return map[string]any{
		"key":         key,
		"private_key": privKey,
	}, nil
}

func adminDeleteSSHKey(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		ID string `json:"id"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	if params.ID == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "id is required", nil)
	}
	if err := ssh.DeleteSSHKey(params.ID); err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return map[string]any{"success": true}, nil
}

func adminListSSHTickets(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		Status       string `json:"status"`
		TargetClient string `json:"target_client"`
		SourceClient string `json:"source_client"`
		Limit        int    `json:"limit"`
		Offset       int    `json:"offset"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	tickets, total, err := ssh.ListSSHTickets(ssh.TicketFilter{
		Status:       strings.TrimSpace(params.Status),
		TargetClient: strings.TrimSpace(params.TargetClient),
		SourceClient: strings.TrimSpace(params.SourceClient),
		Limit:        params.Limit,
		Offset:       params.Offset,
	})
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "Failed to list SSH tickets: "+err.Error(), nil)
	}
	return map[string]any{
		"tickets": tickets,
		"total":   total,
	}, nil
}

func adminGetSSHTicket(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		TicketID string `json:"ticket_id"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	if params.TicketID == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "ticket_id is required", nil)
	}
	ticket, err := ssh.GetSSHTicketByID(params.TicketID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, rpc.MakeError(rpc.InvalidParams, "Ticket not found", nil)
		}
		return nil, rpc.MakeError(rpc.InternalError, "Failed to get SSH ticket", nil)
	}
	return map[string]any{"ticket": ticket}, nil
}

func clientRequestSSHTicket(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		SourceClientUUID string `json:"source_client_uuid"`
		SourceClient     string `json:"source_client"`
		SourceUUID       string `json:"source_uuid"`
		TargetClientUUID string `json:"target_client_uuid"`
		TargetClient     string `json:"target_client"`
		TargetUUID       string `json:"target_uuid"`
		DurationSeconds  int    `json:"duration_seconds"`
		TargetUser       string `json:"target_user"`
		TargetHost       string `json:"target_host"`
		TargetPort       int    `json:"target_port"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}

	// 别名归一化处理
	targetClientUUID := strings.TrimSpace(params.TargetClientUUID)
	if targetClientUUID == "" {
		targetClientUUID = strings.TrimSpace(params.TargetClient)
	}
	if targetClientUUID == "" {
		targetClientUUID = strings.TrimSpace(params.TargetUUID)
	}

	sourceParamUUID := strings.TrimSpace(params.SourceClientUUID)
	if sourceParamUUID == "" {
		sourceParamUUID = strings.TrimSpace(params.SourceClient)
	}
	if sourceParamUUID == "" {
		sourceParamUUID = strings.TrimSpace(params.SourceUUID)
	}

	meta := rpc.MetaFromContext(ctx)

	// 权限与访问控制策略校验 (ACL Check):
	// 1. 如果调用方是 Admin (通过控制台/Admin Token发起)，拥有全局互访申请权限
	// 2. 如果调用方是 Client (Agent 节点之间申请)，严格校验来源身份并执行分组/标签/租户隔离
	isAdmin := meta != nil && (meta.Permission == rpc.RoleAdmin || meta.User != nil)

	sourceUUID := ""
	if meta != nil {
		if meta.ClientUUID != "" {
			sourceUUID = meta.ClientUUID
		} else if meta.ClientToken != "" {
			c, err := clients.GetClientUUIDByToken(meta.ClientToken)
			if err == nil && c != "" {
				sourceUUID = c
			}
		}
	}

	if isAdmin {
		if sourceUUID == "" {
			if sourceParamUUID != "" {
				sourceUUID = sourceParamUUID
			} else {
				sourceUUID = "admin-console"
			}
		}
	} else {
		// 普通 Client 节点调用：必须持有合法的 ClientToken，禁止伪造 source_client_uuid
		if sourceUUID == "" {
			return nil, rpc.MakeError(rpc.PermissionDenied, "Authentication required: client token missing or invalid", nil)
		}
	}

	if targetClientUUID == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "target_client_uuid is required", nil)
	}

	targetClient, err := clients.GetClientByUUID(targetClientUUID)
	if err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "Target client not found", nil)
	}

	// 执行节点隔离矩阵检验
	if !isAdmin {
		// 禁止节点向自身申请
		if sourceUUID == targetClientUUID {
			return nil, rpc.MakeError(rpc.InvalidParams, "Cannot request SSH ticket to self", nil)
		}

		sourceClient, err := clients.GetClientByUUID(sourceUUID)
		if err != nil {
			return nil, rpc.MakeError(rpc.PermissionDenied, "Source client record not found", nil)
		}

		// 默认策略：仅同组（Group 相同且非空）或拥有匹配标签的节点允许直接互访，防止跨组任意横向穿透
		sameGroup := sourceClient.Group != "" && sourceClient.Group == targetClient.Group
		hasExplicitAllow := false

		// 检查目标节点标签中是否允许所有或允许源节点组: "allow-ssh-fleet" 或 "allow-ssh:<sourceGroup>"
		targetTags := strings.Split(targetClient.Tags, ";")
		for _, tag := range targetTags {
			t := strings.TrimSpace(tag)
			if t == "allow-ssh-fleet" || t == "allow-ssh:all" {
				hasExplicitAllow = true
				break
			}
			if sourceClient.Group != "" && t == "allow-ssh-group:"+sourceClient.Group {
				hasExplicitAllow = true
				break
			}
		}

		if !sameGroup && !hasExplicitAllow {
			return nil, rpc.MakeError(rpc.PermissionDenied, fmt.Sprintf("Access denied: node %s is not authorized to access node %s (different group and no explicit allow tag)", sourceClient.Name, targetClient.Name), nil)
		}
	}

	duration := time.Duration(params.DurationSeconds) * time.Second
	if duration <= 0 {
		duration = 1 * time.Hour
	}
	if duration > 24*time.Hour {
		duration = 24 * time.Hour
	}

	targetUser := resolveTargetUser(&targetClient, params.TargetUser)

	targetHost := targetClient.IPv4
	targetPort := 22

	// 允许调用方在入参中显式指定目标 host 和 port
	if params.TargetHost != "" {
		targetHost = strings.TrimSpace(params.TargetHost)
	}
	if params.TargetPort > 0 {
		targetPort = params.TargetPort
	}

	// 若未显式传入，支持从节点 Tags 中健壮解析自定义端口或主机，如 "ssh_port: 2222; ssh_host: 10.144.144.81"
	if targetClient.Tags != "" {
		for _, tag := range strings.Split(targetClient.Tags, ";") {
			parts := strings.SplitN(tag, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.TrimSpace(parts[1])
			if params.TargetPort <= 0 && key == "ssh_port" {
				if p, err := strconv.Atoi(val); err == nil && p > 0 {
					targetPort = p
				}
			} else if params.TargetHost == "" && key == "ssh_host" {
				if val != "" {
					targetHost = val
				}
			}
		}
	}

	if targetHost == "" {
		if targetClient.IPv6 != "" {
			targetHost = targetClient.IPv6
		} else {
			targetHost = "127.0.0.1"
		}
	}

	remoteIP := ""
	if meta != nil {
		remoteIP = meta.RemoteIP
	}

	// 核心安全重构：为每个 Ticket 动态生成单次专属临时密钥对（Ephemeral Keypair）
	// ticket.PublicKey 注入目标节点，专属临时私钥 ephemPriv 返回给调用方，
	// 彻底杜绝全局主私钥泄露给客户端！
	ticket, ephemPriv, err := ssh.CreateSSHTicket(sourceUUID, targetClientUUID, "", targetUser, targetHost, targetPort, remoteIP, duration)
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "Failed to create SSH ticket: "+err.Error(), nil)
	}

	// 投递回执（advisory）：agent 在线=delivered（直接写入 WS）；离线但曾建立 v2 会话=queued
	// （入 5min TTL 队列，重连后按序回放）；从未以 v2 接入=offline。
	delivery := "offline"
	if agent_runtime.IsV2Client(targetClientUUID) {
		delivery = "queued"
	}
	if agent_runtime.GetConnectedClient(targetClientUUID) != nil {
		delivery = "delivered"
	}
	agent_runtime.DispatchV2Event(targetClientUUID, v2.MethodAgentSSHAuthorize, v2.SSHAuthorizeParams{
		TicketID:   ticket.TicketID,
		PublicKey:  ticket.PublicKey,
		TargetUser: targetUser,
		ExpiresAt:  ticket.ExpiresAt,
		SourceUUID: sourceUUID,
	})

	return map[string]any{
		"ticket":      ticket,
		"private_key": ephemPriv,
		"delivery":    delivery,
		"target_info": map[string]any{
			"uuid": targetClient.UUID,
			"name": targetClient.Name,
			"host": targetHost,
			"port": targetPort,
			"user": targetUser,
		},
		"ssh_command": formatSSHCommand(targetPort, targetUser, targetHost),
	}, nil
}

func clientRevokeSSHTicket(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		TicketID string `json:"ticket_id"`
	}
	if err := req.BindParams(&params); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	if params.TicketID == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "ticket_id is required", nil)
	}

	ticket, err := ssh.GetSSHTicketByID(params.TicketID)
	if err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "Ticket not found", nil)
	}

	meta := rpc.MetaFromContext(ctx)
	isAdmin := meta != nil && (meta.Permission == rpc.RoleAdmin || meta.User != nil)
	if !isAdmin {
		clientUUID := ""
		if meta != nil {
			if meta.ClientUUID != "" {
				clientUUID = meta.ClientUUID
			} else if meta.ClientToken != "" {
				c, err := clients.GetClientUUIDByToken(meta.ClientToken)
				if err == nil {
					clientUUID = c
				}
			}
		}
		if clientUUID == "" || (clientUUID != ticket.SourceClient && clientUUID != ticket.TargetClient) {
			return nil, rpc.MakeError(rpc.PermissionDenied, "Access denied: cannot revoke ticket owned by another node", nil)
		}
	}

	if err := ssh.RevokeSSHTicket(params.TicketID); err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}

	// 通知目标节点注销
	agent_runtime.DispatchV2Event(ticket.TargetClient, v2.MethodAgentSSHRevoke, v2.SSHRevokeParams{
		TicketID: ticket.TicketID,
	})

	return map[string]any{"success": true}, nil
}

// formatSSHCommand 格式化客户端便捷连接命令（支持 IPv6 自动括号包裹）
func formatSSHCommand(port int, user, host string) string {
	hostPart := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		hostPart = fmt.Sprintf("[%s]", host)
	}
	if user != "" {
		return fmt.Sprintf("ssh -p %d %s@%s", port, user, hostPart)
	}
	return fmt.Sprintf("ssh -p %d %s", port, hostPart)
}

// resolveTargetUser 推导目标节点的 SSH 登录账户名：
// 1. 如果调用方显式传递了 targetUser，直接采纳；
// 2. 如果目标节点 Tags 显式配置了 "ssh_user:<user>"（大小写不敏感），优先采纳；
// 3. 否则根据操作系统及云厂商环境智能推导：
//   - Windows: 返回 "Administrator"（输出标准格式的明确连接指令）
//   - macOS / Darwin: AWS 上为 "ec2-user"，常规为 "admin"（macOS 默认禁用 root ssh）
//   - FreeBSD / OpenBSD: AWS 上为 "ec2-user"，常规为 "freebsd"
//   - Azure 节点: "azureuser"（厂商规则在发行版之前，Azure 镜像默认账户与发行版无关）
//   - Oracle Cloud (OCI) 节点: "opc"（同上）
//   - Ubuntu: "ubuntu"
//   - Debian: 若节点名/Tags 表明为 AWS Lightsail/AWS 则为 "admin"，否则 "root"
//   - CentOS / Rocky / AlmaLinux / RHEL / Fedora: 若 AWS EC2 则为 "ec2-user"，否则 "root"
//   - Amazon Linux: "ec2-user"
//   - Arch / Alpine: "root"
//   - Azure 节点: "azureuser"（仅在明确 OS 规则未命中时兜底，词级匹配避免误判）
//   - Oracle Cloud (OCI) 节点: "opc"（同上）
//   - 通用回退兜底: "root"
//
// 云厂商判定用分词级匹配（按非字母数字切分）而非子串包含，
// 避免 "social" 命中 "oci" 之类误判；Azure/OCI 固定账户规则保持在发行版规则之前。
func resolveTargetUser(client *models.Client, explicitUser string) string {
	if explicitUser != "" {
		return strings.TrimSpace(explicitUser)
	}

	if client == nil {
		return "root"
	}

	// 1. 检查节点 Tags 中的显式 ssh_user 配置（支持不区分大小写及键值空格修剪）
	if client.Tags != "" {
		for _, tag := range strings.Split(client.Tags, ";") {
			parts := strings.SplitN(tag, ":", 2)
			if len(parts) == 2 {
				key := strings.ToLower(strings.TrimSpace(parts[0]))
				val := strings.TrimSpace(parts[1])
				if key == "ssh_user" && val != "" {
					return val
				}
			}
		}
	}

	// 2. 基于 OS 及云特征推导。
	// matchesWord 报告 s 的小写分词（按非字母数字切分）是否等于词 word，
	// 用于云厂商关键词匹配，防止子串误判（如 "social" 含 "oci"）。
	matchesWord := func(s, word string) bool {
		for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}) {
			if f == word {
				return true
			}
		}
		return false
	}

	osLower := strings.ToLower(client.OS)
	isAWS := matchesWord(client.Name, "aws") || matchesWord(client.Tags, "aws") ||
		matchesWord(client.Name, "ec2") || matchesWord(client.Tags, "ec2")
	isLightsail := matchesWord(client.Name, "lightsail") || matchesWord(client.Tags, "lightsail")
	isAzure := matchesWord(client.Name, "azure") || matchesWord(client.Tags, "azure")
	isOCI := matchesWord(client.Name, "oci") || matchesWord(client.Tags, "oci") ||
		matchesWord(client.Name, "oracle") || matchesWord(client.Tags, "oracle")

	if strings.Contains(osLower, "windows") {
		return "Administrator"
	}
	if strings.Contains(osLower, "darwin") || strings.Contains(osLower, "macos") || strings.Contains(osLower, "mac os") {
		if isAWS {
			return "ec2-user"
		}
		return "admin"
	}
	if strings.Contains(osLower, "freebsd") || strings.Contains(osLower, "openbsd") {
		if isAWS {
			return "ec2-user"
		}
		return "freebsd"
	}
	// 云厂商默认账户在发行版规则之前：Azure/OCI 的镜像无论 Ubuntu/Debian/CentOS，
	// 默认登录账户均为厂商固定值（azureuser/opc），发行版推导反而会给错。
	if isAzure {
		return "azureuser"
	}
	if isOCI {
		return "opc"
	}
	if strings.Contains(osLower, "ubuntu") {
		return "ubuntu"
	}
	if strings.Contains(osLower, "debian") {
		if isLightsail || isAWS {
			return "admin"
		}
		return "root"
	}
	if strings.Contains(osLower, "centos") || strings.Contains(osLower, "rocky") ||
		strings.Contains(osLower, "almalinux") || strings.Contains(osLower, "redhat") ||
		strings.Contains(osLower, "fedora") {
		if isAWS {
			return "ec2-user"
		}
		return "root"
	}
	if strings.Contains(osLower, "amazon") {
		return "ec2-user"
	}
	if strings.Contains(osLower, "arch") || strings.Contains(osLower, "alpine") {
		return "root"
	}
	return "root"
}
