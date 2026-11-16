package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dengyie/mangohub/hub/cmd/flags"
	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
	v2 "github.com/dengyie/mangohub/protocol/v2"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// A guest polling /api/clients must not receive mining wallet/pool data, GPU
// device details or disk IO — the same guest-visibility policy enforced by
// publicGetClientRecentRecords and getNodesLatestStatus. Logged-in sessions
// keep the full report.
func TestGetClientsRedactsGuestReports(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:ws_guest_redact?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/clients", GetClients)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	clientUUID := "client-ws-guest"
	now := time.Now().UTC()
	if err := db.Create(&models.Client{UUID: clientUUID, Token: "token-ws-guest", Name: "guest_node", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	wallet := "0xdeadbeefwallet"
	agent_runtime.RecordReport(v2.Report{
		UUID:      clientUUID,
		UpdatedAt: now,
		CPU:       v2.CPUReport{Usage: 12.5},
		Mining:    &v2.MiningReport{Algorithm: "ethash", Wallet: wallet, Pool: "pool.example.com:3333"},
		GPU:       &v2.GPUDetailReport{AverageUsage: 88},
		DiskIO:    []v2.DiskIOReport{{ReadBytes: 1}},
		Message:   "internal note",
	})
	t.Cleanup(func() { agent_runtime.DeleteLatestReport(clientUUID) })

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/clients"
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	// Origin check is on by default; present the server's own origin exactly
	// like the embedded homepage does.
	conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{server.URL}})
	if err != nil {
		t.Fatalf("dial /api/clients: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.WriteMessage(websocket.TextMessage, []byte("get")); err != nil {
		t.Fatalf("send get: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Online []string             `json:"online"`
			Data   map[string]v2.Report `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if payload.Status != "success" {
		t.Fatalf("status = %q", payload.Status)
	}
	report, ok := payload.Data.Data[clientUUID]
	if !ok {
		t.Fatalf("guest response missing node report: %s", raw)
	}
	if report.Mining != nil {
		t.Fatalf("guest received mining payload: %+v", report.Mining)
	}
	if report.GPU != nil {
		t.Fatalf("guest received GPU payload: %+v", report.GPU)
	}
	if report.DiskIO != nil {
		t.Fatalf("guest received disk IO payload: %+v", report.DiskIO)
	}
	if report.Message != "" {
		t.Fatalf("guest received internal message: %q", report.Message)
	}
	if strings.Contains(string(raw), wallet) {
		t.Fatal("wallet address leaked to guest over /api/clients")
	}
	if report.CPU.Usage != 12.5 {
		t.Fatalf("guest report lost the public CPU metrics: %+v", report.CPU)
	}
}
