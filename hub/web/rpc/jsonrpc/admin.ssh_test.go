package jsonrpc

import (
	"context"
	"testing"
	"time"

	"github.com/dengyie/mangohub/hub/cmd/flags"
	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/database/ssh"
	"github.com/dengyie/mangohub/hub/pkg/rpc"
	"github.com/dengyie/mangohub/hub/utils/crypto"
)

func TestSSHManagementAndTicketFlow(t *testing.T) {
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:ssh_flow_test?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()

	// 1. 初始化目标 Client A 与 目标 Client B
	targetUUID := "target-client-uuid-1"
	targetClient := models.Client{
		UUID:      targetUUID,
		Token:     "target-token-1",
		Name:      "Tianyi-Win1",
		OS:        "linux",
		Group:     "production-fleet",
		Tags:      "region:bj;allow-ssh-fleet",
		IPv4:      "10.144.144.21",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_ = db.Create(&targetClient)

	sourceUUID := "source-client-uuid-1"
	sourceClient := models.Client{
		UUID:      sourceUUID,
		Token:     "source-token-1",
		Name:      "Cue-Sandbox1",
		OS:        "linux",
		Group:     "production-fleet",
		IPv4:      "10.144.144.81",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_ = db.Create(&sourceClient)

	unauthorizedUUID := "unauthorized-uuid-1"
	unauthorizedClient := models.Client{
		UUID:      unauthorizedUUID,
		Token:     "unauthorized-token-1",
		Name:      "Untrusted-Node",
		OS:        "linux",
		Group:     "isolated-guest",
		IPv4:      "10.144.144.99",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_ = db.Create(&unauthorizedClient)

	isolatedTargetUUID := "isolated-target-uuid-1"
	isolatedTargetClient := models.Client{
		UUID:      isolatedTargetUUID,
		Token:     "isolated-token-1",
		Name:      "Core-Database",
		OS:        "linux",
		Group:     "secure-vault",
		Tags:      "isolated;no-fleet-mesh",
		IPv4:      "10.144.144.5",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_ = db.Create(&isolatedTargetClient)

	// 2. 生成/导入 SSH 密钥（Vault 根密钥）
	genReq := &rpc.JsonRpcRequest{
		Params: map[string]any{"name": "fleet-default-key", "scope": "global", "description": "integration test"},
	}
	res, rpcErr := adminGenerateSSHKey(context.Background(), genReq)
	if rpcErr != nil {
		t.Fatalf("adminGenerateSSHKey failed: %v", rpcErr)
	}
	resMap := res.(map[string]any)
	keyObj := resMap["key"].(*models.SSHKey)
	masterPrivKey := resMap["private_key"].(string)

	if keyObj.ID == "" || masterPrivKey == "" {
		t.Fatalf("Key generation returned empty fields")
	}

	// 3. 管理员申请跨节点互访 Ticket（应成功，且下发单次专属临时私钥，绝不下发集群根私钥）
	adminCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Permission: rpc.RoleAdmin,
		User:       &models.User{Username: "admin"},
	})
	reqTicket := &rpc.JsonRpcRequest{
		Params: map[string]any{
			"source_client_uuid": sourceUUID,
			"target_client_uuid": targetUUID,
			"duration_seconds":   1800,
		},
	}
	tktRes, rpcErr := clientRequestSSHTicket(adminCtx, reqTicket)
	if rpcErr != nil {
		t.Fatalf("clientRequestSSHTicket by admin failed: %v", rpcErr)
	}
	tMap := tktRes.(map[string]any)
	ticket := tMap["ticket"].(*models.SSHTicket)
	ticketPriv := tMap["private_key"].(string)
	targetInfo := tMap["target_info"].(map[string]any)

	if ticket.TicketID == "" || ticket.Status != "active" {
		t.Fatalf("Ticket not active: %+v", ticket)
	}
	// 验证核心安全点：单次临时私钥必须独立存在，绝不等于集群 masterPrivKey！
	if ticketPriv == masterPrivKey {
		t.Fatalf("Security failure: Master private key leaked in ticket response!")
	}
	if ticket.PublicKey == keyObj.PublicKey {
		t.Fatalf("Security failure: Ticket is using global master public key instead of ephemeral public key!")
	}
	// 验证可通过 crypto.DecryptText 解出临时私钥（DB 内为密文，响应内存对象带密文字段）
	decEphem, err := crypto.DecryptText(ticket.PrivateKeyEnc)
	if err != nil || decEphem != ticketPriv {
		t.Fatalf("Ticket decrypted ephemeral private key mismatch: err=%v", err)
	}
	// 无 agent 在线的测试环境：投递回执必须如实反映（不可谎报 delivered）
	if tMap["delivery"] != "queued" && tMap["delivery"] != "offline" {
		t.Fatalf("Unexpected delivery receipt: %v", tMap["delivery"])
	}
	if targetInfo["host"] != "10.144.144.21" {
		t.Fatalf("Target host expected 10.144.144.21, got %v", targetInfo["host"])
	}

	// 4. 越权阻断测试：未授权隔离节点试图申请访问受保护核心数据库（应被拒绝）
	untrustedCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Permission:  rpc.RoleClient,
		ClientToken: "unauthorized-token-1",
	})
	unauthorizedReq := &rpc.JsonRpcRequest{
		Params: map[string]any{
			"target_client_uuid": isolatedTargetUUID,
			"duration_seconds":   3600,
		},
	}
	_, blockedErr := clientRequestSSHTicket(untrustedCtx, unauthorizedReq)
	if blockedErr == nil {
		t.Fatalf("Expected cross-group unauthorized access to be blocked, but succeeded!")
	}
	if blockedErr.Code != rpc.PermissionDenied {
		t.Fatalf("Expected PermissionDenied error code, got %d: %s", blockedErr.Code, blockedErr.Message)
	}

	// 5. 合法节点（受信任同组/含 allow-ssh-fleet）申请互访（应成功）
	clientCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Permission:  rpc.RoleClient,
		ClientToken: "source-token-1",
	})
	legitReq := &rpc.JsonRpcRequest{
		Params: map[string]any{
			"target_client_uuid": targetUUID,
			"duration_seconds":   3600,
		},
	}
	legitRes, rpcErr := clientRequestSSHTicket(clientCtx, legitReq)
	if rpcErr != nil {
		t.Fatalf("Legitimate client ticket request failed: %v", rpcErr)
	}
	legitMap := legitRes.(map[string]any)
	legitTicket := legitMap["ticket"].(*models.SSHTicket)

	// 6. 吊销权限控制测试：非相关节点试图吊销他人的 Ticket（应被拒绝）
	foreignRevReq := &rpc.JsonRpcRequest{
		Params: map[string]any{"ticket_id": legitTicket.TicketID},
	}
	_, foreignRevErr := clientRevokeSSHTicket(untrustedCtx, foreignRevReq)
	if foreignRevErr == nil {
		t.Fatalf("Foreign node was able to revoke ticket belonging to another client!")
	}

	// 7. 拥有者或管理员吊销 Ticket（应成功）
	revReq := &rpc.JsonRpcRequest{
		Params: map[string]any{"ticket_id": legitTicket.TicketID},
	}
	revRes, rpcErr := clientRevokeSSHTicket(clientCtx, revReq)
	if rpcErr != nil {
		t.Fatalf("clientRevokeSSHTicket failed: %v", rpcErr)
	}
	if revMap := revRes.(map[string]any); !revMap["success"].(bool) {
		t.Fatalf("Revoke should return success: true")
	}

	dbTicket, err := ssh.GetSSHTicketByID(legitTicket.TicketID)
	if err != nil || dbTicket.Status != "revoked" {
		t.Fatalf("Ticket should be revoked in DB, got: %+v", dbTicket)
	}
}

func TestResolveTargetUser(t *testing.T) {
	tests := []struct {
		name         string
		client       *models.Client
		explicitUser string
		expected     string
	}{
		{
			name:         "Explicit user takes top priority",
			client:       &models.Client{Name: "aws-tokyo", OS: "Debian GNU/Linux 12 (bookworm)", Tags: "ssh_user:admin"},
			explicitUser: "custom-deployer",
			expected:     "custom-deployer",
		},
		{
			name:         "Tag ssh_user takes priority over OS heuristics",
			client:       &models.Client{Name: "custom-vps", OS: "Ubuntu 22.04 LTS", Tags: "region:jp;ssh_user:admin;ssh_port:2222"},
			explicitUser: "",
			expected:     "admin",
		},
		{
			name:         "Windows OS returns empty user for agent default handling",
			client:       &models.Client{Name: "win-server", OS: "Windows Server 2022"},
			explicitUser: "",
			expected:     "",
		},
		{
			name:         "Ubuntu OS returns ubuntu",
			client:       &models.Client{Name: "tokyo-ubuntu", OS: "Ubuntu 24.04 LTS"},
			explicitUser: "",
			expected:     "ubuntu",
		},
		{
			name:         "Debian on AWS/Lightsail returns admin",
			client:       &models.Client{Name: "aws-tokyo-lightsail", OS: "Debian GNU/Linux 12 (bookworm)"},
			explicitUser: "",
			expected:     "admin",
		},
		{
			name:         "Debian with aws in tags returns admin",
			client:       &models.Client{Name: "tokyo-node", OS: "Debian GNU/Linux 12", Tags: "cloud:aws"},
			explicitUser: "",
			expected:     "admin",
		},
		{
			name:         "Standard Debian VPS returns root",
			client:       &models.Client{Name: "hetzner-node", OS: "Debian GNU/Linux 12 (bookworm)"},
			explicitUser: "",
			expected:     "root",
		},
		{
			name:         "CentOS on AWS returns ec2-user",
			client:       &models.Client{Name: "aws-oregon-centos", OS: "CentOS Stream 9"},
			explicitUser: "",
			expected:     "ec2-user",
		},
		{
			name:         "Standard CentOS/Rocky/Alma returns root",
			client:       &models.Client{Name: "racknerd-rocky", OS: "Rocky Linux 9.3 (Blue Onyx)"},
			explicitUser: "",
			expected:     "root",
		},
		{
			name:         "Amazon Linux returns ec2-user",
			client:       &models.Client{Name: "my-amzn-node", OS: "Amazon Linux 2023"},
			explicitUser: "",
			expected:     "ec2-user",
		},
		{
			name:         "Alpine/Arch Linux returns root",
			client:       &models.Client{Name: "edge-gateway", OS: "Alpine Linux v3.19"},
			explicitUser: "",
			expected:     "root",
		},
		{
			name:         "Nil client or unknown OS falls back to root",
			client:       &models.Client{Name: "unknown-box", OS: "CustomOS 1.0"},
			explicitUser: "",
			expected:     "root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTargetUser(tt.client, tt.explicitUser)
			if got != tt.expected {
				t.Errorf("resolveTargetUser() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRequestSSHTicketTargetUserInference(t *testing.T) {
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:ssh_user_infer_test?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()

	// 模拟 aws-tokyo Lightsail 节点
	awsTokyo := models.Client{
		UUID:      "aws-tokyo-uuid",
		Token:     "aws-tokyo-token",
		Name:      "aws-tokyo",
		OS:        "Debian GNU/Linux 12 (bookworm)",
		Group:     "production",
		Tags:      "ssh_user:admin;ssh_port:22",
		IPv4:      "52.192.36.79",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_ = db.Create(&awsTokyo)

	adminCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Permission: rpc.RoleAdmin,
		User:       &models.User{Username: "admin"},
	})

	// 不传 target_user，应当自动解析出 tags 里的 admin
	req := &rpc.JsonRpcRequest{
		Params: map[string]any{
			"target_client_uuid": "aws-tokyo-uuid",
			"duration_seconds":   1800,
		},
	}
	res, rpcErr := clientRequestSSHTicket(adminCtx, req)
	if rpcErr != nil {
		t.Fatalf("clientRequestSSHTicket failed: %v", rpcErr)
	}
	resMap := res.(map[string]any)
	ticket := resMap["ticket"].(*models.SSHTicket)
	targetInfo := resMap["target_info"].(map[string]any)
	sshCmd := resMap["ssh_command"].(string)

	if ticket.TargetUser != "admin" {
		t.Errorf("expected ticket.TargetUser to be 'admin', got %q", ticket.TargetUser)
	}
	if targetInfo["user"] != "admin" {
		t.Errorf("expected target_info.user to be 'admin', got %q", targetInfo["user"])
	}
	expectedCmd := "ssh -p 22 admin@52.192.36.79"
	if sshCmd != expectedCmd {
		t.Errorf("expected ssh_command %q, got %q", expectedCmd, sshCmd)
	}
}

