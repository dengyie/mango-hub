package ssh

import (
	"testing"
	"time"

	"github.com/dengyie/mangohub/hub/cmd/flags"
	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/utils/crypto"
)

func TestSSHKeyAndTicketCRUD(t *testing.T) {
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:ssh_test_db?mode=memory&cache=shared"
	_ = dbcore.GetDBInstance()

	pub, priv, fp, err := models.GenerateEd25519Keypair("test-key")
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair failed: %v", err)
	}

	key, err := CreateSSHKey("test-fleet-key", "global", "", "test key", pub, priv)
	if err != nil {
		t.Fatalf("CreateSSHKey failed: %v", err)
	}
	if key.ID == "" || key.Fingerprint != fp {
		t.Fatalf("Invalid key returned: %+v", key)
	}

	fetched, err := GetSSHKeyByID(key.ID)
	if err != nil {
		t.Fatalf("GetSSHKeyByID failed: %v", err)
	}
	decPriv, err := crypto.DecryptText(fetched.PrivateKeyEnc)
	if err != nil {
		t.Fatalf("decrypt stored private key failed: %v", err)
	}
	if decPriv != priv {
		t.Fatalf("Decrypted private key mismatch")
	}

	keys, err := ListSSHKeys()
	if err != nil || len(keys) == 0 {
		t.Fatalf("ListSSHKeys failed or empty: %v", err)
	}

	// Test ticket with ephemeral keypair
	ticket, ephemPriv, err := CreateSSHTicket("client-a", "client-b", key.ID, "ubuntu", "10.144.144.82", 22, "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSSHTicket failed: %v", err)
	}
	if ticket.Status != "active" || ticket.PublicKey == "" || ephemPriv == "" {
		t.Fatalf("Ticket status or ephemeral keypair invalid, got %+v", ticket)
	}

	decEphemPriv, err := crypto.DecryptText(ticket.PrivateKeyEnc)
	if err != nil {
		t.Fatalf("decrypt ticket private key failed: %v", err)
	}
	if decEphemPriv != ephemPriv {
		t.Fatalf("Decrypted ephemeral private key mismatch")
	}

	if err := RevokeSSHTicket(ticket.TicketID); err != nil {
		t.Fatalf("RevokeSSHTicket failed: %v", err)
	}

	revoked, err := GetSSHTicketByID(ticket.TicketID)
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("Ticket should be revoked, got: %+v", revoked)
	}

	// 测试更新失败状态与错误信息
	failTicket, _, err := CreateSSHTicket("client-a", "client-b", key.ID, "ubuntu", "10.144.144.82", 22, "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSSHTicket failed: %v", err)
	}
	if err := UpdateSSHTicketResult(failTicket.TicketID, false, "user 'ubuntu' not found"); err != nil {
		t.Fatalf("UpdateSSHTicketResult failed: %v", err)
	}
	failedRecord, err := GetSSHTicketByID(failTicket.TicketID)
	if err != nil || failedRecord.Status != "failed" || failedRecord.ErrorMessage != "user 'ubuntu' not found" {
		t.Fatalf("Ticket status should be failed with error, got: %+v", failedRecord)
	}

	// 测试更新成功状态
	okTicket, _, err := CreateSSHTicket("client-a", "client-b", key.ID, "admin", "10.144.144.82", 22, "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSSHTicket failed: %v", err)
	}
	if err := UpdateSSHTicketResult(okTicket.TicketID, true, ""); err != nil {
		t.Fatalf("UpdateSSHTicketResult failed: %v", err)
	}
	okRecord, err := GetSSHTicketByID(okTicket.TicketID)
	if err != nil || okRecord.Status != "active" || okRecord.ErrorMessage != "" {
		t.Fatalf("Ticket status should be active with empty error, got: %+v", okRecord)
	}

	// 测试终态保护：当工单已被 revoked 时，迟到的失败 ACK 不允许覆盖为 failed
	raceTicket, _, err := CreateSSHTicket("client-a", "client-b", key.ID, "admin", "10.144.144.82", 22, "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSSHTicket failed: %v", err)
	}
	if err := RevokeSSHTicket(raceTicket.TicketID); err != nil {
		t.Fatalf("RevokeSSHTicket failed: %v", err)
	}
	// 模拟迟到的失败上报
	if err := UpdateSSHTicketResult(raceTicket.TicketID, false, "late failure report"); err != nil {
		t.Fatalf("UpdateSSHTicketResult returned error: %v", err)
	}
	raceRecord, err := GetSSHTicketByID(raceTicket.TicketID)
	if err != nil || raceRecord.Status != "revoked" {
		t.Fatalf("Expected revoked status to be preserved, got status: %s", raceRecord.Status)
	}

	// 测试终态保护：revoked 工单的迟到成功 ACK 同样不允许触碰状态与错误信息
	if err := UpdateSSHTicketResult(raceTicket.TicketID, true, ""); err != nil {
		t.Fatalf("UpdateSSHTicketResult success ACK returned error: %v", err)
	}
	raceRecord, err = GetSSHTicketByID(raceTicket.TicketID)
	if err != nil || raceRecord.Status != "revoked" {
		t.Fatalf("Expected revoked status preserved on late success ACK, got: %s", raceRecord.Status)
	}

	// 测试恢复语义：failed 工单的后续成功 ACK 应恢复为 active 并清空错误信息
	failTicket2, _, err := CreateSSHTicket("client-a", "client-b", key.ID, "admin", "10.144.144.82", 22, "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSSHTicket failed: %v", err)
	}
	if err := UpdateSSHTicketResult(failTicket2.TicketID, false, "ssh: permission denied"); err != nil {
		t.Fatalf("UpdateSSHTicketResult failed: %v", err)
	}
	if err := UpdateSSHTicketResult(failTicket2.TicketID, true, ""); err != nil {
		t.Fatalf("UpdateSSHTicketResult success ACK failed: %v", err)
	}
	recovered, err := GetSSHTicketByID(failTicket2.TicketID)
	if err != nil || recovered.Status != "active" || recovered.ErrorMessage != "" {
		t.Fatalf("Expected failed ticket recovered to active with empty error, got: %+v", recovered)
	}

	// 测试非 active 状态（如 failed）的工单也可以被吊销
	if err := RevokeSSHTicket(failTicket.TicketID); err != nil {
		t.Fatalf("RevokeSSHTicket on failed ticket failed: %v", err)
	}
	revokedFailed, err := GetSSHTicketByID(failTicket.TicketID)
	if err != nil || revokedFailed.Status != "revoked" {
		t.Fatalf("Expected failed ticket to be revoked, got: %+v", revokedFailed)
	}

	// 测试已吊销工单再次吊销（幂等成功）
	if err := RevokeSSHTicket(failTicket.TicketID); err != nil {
		t.Fatalf("Repeated RevokeSSHTicket should succeed idempotently, got: %v", err)
	}

	// 测试不存在工单吊销（返回错误）
	if err := RevokeSSHTicket("non-existent-ticket-uuid"); err == nil {
		t.Fatalf("Expected error when revoking non-existent ticket, got nil")
	}

	// 测试 ListSSHTickets 分页与过滤
	tickets, total, err := ListSSHTickets(TicketFilter{
		Status: "revoked",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListSSHTickets failed: %v", err)
	}
	if total < 3 || len(tickets) == 0 {
		t.Fatalf("Expected at least 3 revoked tickets, got total=%d, len=%d", total, len(tickets))
	}

	// 清理该测试创建的 key，避免污染后续测试
	_ = DeleteSSHKey(key.ID)
}

func TestEnsureDefaultFleetKey(t *testing.T) {
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:ssh_seed_test_db?mode=memory&cache=shared"
	_ = dbcore.GetDBInstance()

	if err := EnsureDefaultFleetKey(); err != nil {
		t.Fatalf("EnsureDefaultFleetKey failed: %v", err)
	}

	keys, err := ListSSHKeys()
	if err != nil || len(keys) == 0 {
		t.Fatalf("Expected seeded keys, got empty or error: %v", err)
	}

	found := false
	for _, k := range keys {
		if k.Name == "mangohub-fleet-master" && k.Scope == "global" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mangohub-fleet-master key not found in keys")
	}
}
