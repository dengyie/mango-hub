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

func waitV2ClientGone(t *testing.T, uuid string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if agent_runtime.GetConnectedClient(uuid) == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client %s stayed registered after its connection closed", uuid)
}

// roundTripV2Pull sends one pull request and verifies the JSON-RPC response,
// proving the connection is registered and serving.
func roundTripV2Pull(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	request := v2.Request{JSONRPC: v2.Version, ID: "pull-1", Method: v2.MethodAgentPull, Params: map[string]interface{}{}}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatalf("write pull request: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read pull response: %v", err)
	}
	var resp v2.Response
	if err := json.Unmarshal(message, &resp); err != nil {
		t.Fatalf("parse pull response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("pull failed: %+v", resp.Error)
	}
	if got, ok := resp.ID.(string); !ok || got != "pull-1" {
		t.Fatalf("pull response id mismatch: got %#v", resp.ID)
	}
}

func setupV2WSTest(t *testing.T, clientUUID, token string) string {
	t.Helper()
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:v2_ws_" + clientUUID + "?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v2/rpc", WebSocketV2RPC)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	now := time.Now().UTC()
	if err := db.Create(&models.Client{
		UUID:      clientUUID,
		Token:     token,
		Name:      "client_" + clientUUID,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/v2/rpc?token=" + token
}

func TestWebSocketV2RPCRejectsDuplicateKeepsOldConnection(t *testing.T) {
	clientUUID := "client-v2-ws-dup"
	token := "token-v2-ws-dup"
	wsURL := setupV2WSTest(t, clientUUID, token)

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
	roundTripV2Pull(t, first)
}

// A client that disconnects cleanly must free its registry slot at once so
// the next reconnect is accepted instead of being treated as a duplicate.
func TestWebSocketV2RPCAcceptsReconnectAfterCleanDisconnect(t *testing.T) {
	clientUUID := "client-v2-ws-reconnect"
	token := "token-v2-ws-reconnect"
	wsURL := setupV2WSTest(t, clientUUID, token)

	first := dialV2WS(t, wsURL)
	waitRegisteredV2Client(t, clientUUID)
	if err := first.Close(); err != nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("close first connection: %v", err)
	}
	waitV2ClientGone(t, clientUUID)

	second := dialV2WS(t, wsURL)
	waitRegisteredV2Client(t, clientUUID)
	roundTripV2Pull(t, second)
}

// A registered connection that stopped delivering frames (half-open socket
// after sleep or a NAT rebuild) must be taken over by a new connection
// instead of locking the reconnecting agent out until the read deadline.
func TestWebSocketV2RPCTakesOverStaleConnection(t *testing.T) {
	oldThreshold := zombieConnThreshold
	zombieConnThreshold = 300 * time.Millisecond
	t.Cleanup(func() { zombieConnThreshold = oldThreshold })

	clientUUID := "client-v2-ws-stale"
	token := "token-v2-ws-stale"
	wsURL := setupV2WSTest(t, clientUUID, token)

	stale := dialV2WS(t, wsURL)
	waitRegisteredV2Client(t, clientUUID)
	// The stale client stays silent; its LastActive stays at upgrade time.
	time.Sleep(zombieConnThreshold + 300*time.Millisecond)

	fresh := dialV2WS(t, wsURL)
	registered := waitRegisteredV2Client(t, clientUUID)
	roundTripV2Pull(t, fresh)

	// The takeover must have torn the stale connection down.
	_ = stale.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := stale.ReadMessage(); err == nil {
		t.Fatalf("stale connection was not closed by the takeover")
	}
	_ = registered
}
