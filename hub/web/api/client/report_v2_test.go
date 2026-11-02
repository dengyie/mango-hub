package client

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dengyie/mangohub/hub/cmd/flags"
	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
	v2 "github.com/dengyie/mangohub/hub/protocol/v2"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
	"github.com/dengyie/mangohub/hub/web/connection"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func dialV2WS(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dialer.Dial(url, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial %s: %v (status %d)", url, err, status)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func waitRegisteredV2Client(t *testing.T, uuid string) *connection.SafeConn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn := agent_runtime.GetConnectedClient(uuid); conn != nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client %s was never registered as connected", uuid)
	return nil
}

func TestWebSocketV2RPCRejectsDuplicateKeepsOldConnection(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:v2_ws_duplicate?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v2/rpc", WebSocketV2RPC)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	clientUUID := "client-v2-ws-dup"
	token := "token-v2-ws-dup"
	now := time.Now().UTC()
	if err := db.Create(&models.Client{
		UUID:      clientUUID,
		Token:     token,
		Name:      "client_v2_ws_dup",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v2/rpc?token=" + token

	// First connection: must be registered and keep serving.
	first := dialV2WS(t, wsURL)
	oldConn := waitRegisteredV2Client(t, clientUUID)

	// Second connection with the same token: must be rejected with close
	// code 1008 instead of kicking the first connection.
	second := dialV2WS(t, wsURL)
	_ = second.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err := second.ReadMessage()
	if err == nil {
		t.Fatalf("expected duplicate connection to be closed by the hub, got a message")
	}
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("expected close code 1008 (policy violation), got %v", err)
	}
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("expected *websocket.CloseError, got %T: %v", err, err)
	}
	if !strings.Contains(closeErr.Text, "duplicate connection rejected") ||
		!strings.Contains(closeErr.Text, clientUUID) {
		t.Fatalf("close reason should mention the duplicate rejection and the uuid, got %q", closeErr.Text)
	}

	// The old connection must still be the registered one.
	if current := agent_runtime.GetConnectedClient(clientUUID); current != oldConn {
		t.Fatalf("old connection was replaced: got %v, want the original %v", current, oldConn)
	}

	// The old connection must still work: a pull round-trip succeeds.
	request := v2.Request{JSONRPC: v2.Version, ID: "pull-1", Method: v2.MethodAgentPull, Params: map[string]interface{}{}}
	if err := first.WriteJSON(request); err != nil {
		t.Fatalf("write pull request on old connection: %v", err)
	}
	_ = first.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := first.ReadMessage()
	if err != nil {
		t.Fatalf("old connection died after duplicate rejection: %v", err)
	}
	var resp v2.Response
	if err := json.Unmarshal(message, &resp); err != nil {
		t.Fatalf("parse pull response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("pull on old connection failed: %+v", resp.Error)
	}
	if got, ok := resp.ID.(string); !ok || got != "pull-1" {
		t.Fatalf("pull response id mismatch: got %#v", resp.ID)
	}
}
