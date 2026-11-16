package client

import (
	"net"
	"testing"
	"time"

	"github.com/dengyie/mangohub/hub/cmd/flags"
	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/internal/config"
	v2 "github.com/dengyie/mangohub/hub/protocol/v2"
	"github.com/dengyie/mangohub/hub/utils/geoip"
)

type staticGeoIPProvider struct {
	name string
	iso  string
}

func (p staticGeoIPProvider) Name() string {
	return p.name
}

func (p staticGeoIPProvider) GetGeoInfo(ip net.IP) (*geoip.GeoInfo, error) {
	return &geoip.GeoInfo{ISOCode: p.iso, Name: p.iso}, nil
}

func (p staticGeoIPProvider) UpdateDatabase() error {
	return nil
}

func (p staticGeoIPProvider) Close() error {
	return nil
}

func TestV2BasicInfoFillsRegionFromGeoIP(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:v2_basic_info_geoip?mode=memory&cache=shared"

	db := dbcore.GetDBInstance()
	if err := config.Set(config.GeoIpEnabledKey, true); err != nil {
		t.Fatalf("enable geoip: %v", err)
	}

	oldProvider := geoip.CurrentProvider
	geoip.CurrentProvider = staticGeoIPProvider{name: t.Name(), iso: "SG"}
	t.Cleanup(func() {
		geoip.CurrentProvider = oldProvider
	})

	clientUUID := "client-v2-geoip"
	now := time.Now().UTC()
	if err := db.Create(&models.Client{
		UUID:      clientUUID,
		Token:     "token-v2-geoip",
		Name:      "client_v2_geoip",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	resp := handleV2RPC(clientUUID, v2.Request{
		JSONRPC: v2.Version,
		Method:  v2.MethodAgentBasicInfo,
		Params: map[string]interface{}{
			"info": map[string]interface{}{
				"ipv4": "8.8.8.8",
			},
		},
		ID: "basic-info",
	}, false)
	if resp.Error != nil {
		t.Fatalf("v2 basic info failed: %+v", resp.Error)
	}

	var got models.Client
	if err := db.First(&got, "uuid = ?", clientUUID).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}
	want := geoip.GetRegionUnicodeEmoji("SG")
	if got.Region != want {
		t.Fatalf("expected GeoIP region to be saved, got %q", got.Region)
	}
}

func TestV2SSHTicketResultHandling(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:v2_ssh_ticket_result?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()

	ticketID := "ticket-ack-test-1"
	now := time.Now().UTC()
	ticket := models.SSHTicket{
		TicketID:     ticketID,
		SourceClient: "source-1",
		TargetClient: "target-1",
		PublicKey:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI mock",
		Status:       "active",
		TargetUser:   "ubuntu",
		TargetHost:   "10.0.0.1",
		ExpiresAt:    now.Add(time.Hour),
		CreatedAt:    now,
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	// 模拟 Agent 报告授权失败（如 user not found）
	respFail := handleV2RPC("target-1", v2.Request{
		JSONRPC: v2.Version,
		Method:  v2.MethodAgentSSHTicketResult,
		Params: v2.SSHTicketResultParams{
			TicketID: ticketID,
			Success:  false,
			Error:    "user 'ubuntu' not found",
		},
		ID: "ticket-ack-1",
	}, false)
	if respFail.Error != nil {
		t.Fatalf("handleV2RPC ticket result fail returned error: %+v", respFail.Error)
	}

	var updatedTicket models.SSHTicket
	if err := db.First(&updatedTicket, "ticket_id = ?", ticketID).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if updatedTicket.Status != "failed" || updatedTicket.ErrorMessage != "user 'ubuntu' not found" {
		t.Fatalf("expected status=failed and error recorded, got %+v", updatedTicket)
	}

	// 模拟 Agent 报告授权成功
	respSuccess := handleV2RPC("target-1", v2.Request{
		JSONRPC: v2.Version,
		Method:  v2.MethodAgentSSHTicketResult,
		Params: v2.SSHTicketResultParams{
			TicketID: ticketID,
			Success:  true,
		},
		ID: "ticket-ack-2",
	}, false)
	if respSuccess.Error != nil {
		t.Fatalf("handleV2RPC ticket result success returned error: %+v", respSuccess.Error)
	}

	if err := db.First(&updatedTicket, "ticket_id = ?", ticketID).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if updatedTicket.ErrorMessage != "" {
		t.Fatalf("expected empty error_message on success, got %q", updatedTicket.ErrorMessage)
	}

	// 模拟非目标节点伪造上报结果（鉴权拦截）
	respSpoof := handleV2RPC("rogue-client-2", v2.Request{
		JSONRPC: v2.Version,
		Method:  v2.MethodAgentSSHTicketResult,
		Params: v2.SSHTicketResultParams{
			TicketID: ticketID,
			Success:  false,
			Error:    "spoofed report",
		},
		ID: "ticket-ack-spoof",
	}, false)
	if respSpoof.Error == nil || respSpoof.Error.Code != -32003 {
		t.Fatalf("expected error code -32003 for mismatched target client, got: %+v", respSpoof.Error)
	}
}

// An agent token must not be able to mass-assign admin-owned columns through
// its basic-info report: hidden, token, name, group, price etc. stay under
// admin control, and unknown keys are dropped instead of failing the write.
func TestV2BasicInfoWhitelistsAgentColumns(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:v2_basic_info_whitelist?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()

	clientUUID := "client-v2-whitelist"
	now := time.Now().UTC()
	if err := db.Create(&models.Client{
		UUID:      clientUUID,
		Token:     "token-v2-whitelist",
		Name:      "admin_name",
		Hidden:    true,
		Price:     9.9,
		Weight:    3,
		Group:     "admin_group",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	payload := map[string]interface{}{
		// Legitimate hardware/OS fields — must be applied.
		"cpu_name":  "Intel i9",
		"arch":      "amd64",
		"os":        "Ubuntu 24.04",
		"mem_total": int64(8 << 30),
		"version":   "test-agent",
		// Admin-owned / dangerous columns — must be dropped.
		"hidden":       false,
		"token":        "attacker-token",
		"name":         "spoofed_name",
		"group":        "spoofed_group",
		"price":        0.0,
		"weight":       0,
		"region":       "XX",
		"not_a_column": "junk",
	}

	if err := saveClientBasicInfo(payload, clientUUID, "203.0.113.10"); err != nil {
		t.Fatalf("save basic info: %v", err)
	}

	var client models.Client
	if err := db.Where("uuid = ?", clientUUID).First(&client).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}
	if client.CpuName != "Intel i9" || client.Arch != "amd64" || client.OS != "Ubuntu 24.04" {
		t.Fatalf("legitimate basic-info fields were not applied: %+v", client)
	}
	if !client.Hidden {
		t.Fatal("agent un-hid its own node via basic-info mass assignment")
	}
	if client.Token != "token-v2-whitelist" {
		t.Fatalf("agent rotated its own token: %q", client.Token)
	}
	if client.Name != "admin_name" || client.Group != "admin_group" {
		t.Fatalf("agent spoofed identity fields: name=%q group=%q", client.Name, client.Group)
	}
	if client.Price != 9.9 || client.Weight != 3 {
		t.Fatalf("agent overwrote price/weight: price=%v weight=%v", client.Price, client.Weight)
	}
	if client.Region == "XX" {
		t.Fatal("agent forged its own region flag")
	}
}
