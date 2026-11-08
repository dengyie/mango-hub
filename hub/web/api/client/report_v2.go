package client

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	logger "github.com/dengyie/mangohub/hub/utils/log"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dengyie/mangohub/hub/database/clients"
	"github.com/dengyie/mangohub/hub/database/ssh"
	"github.com/dengyie/mangohub/hub/database/tasks"
	v2 "github.com/dengyie/mangohub/hub/protocol/v2"
	"github.com/dengyie/mangohub/hub/utils/notifier"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
	"github.com/dengyie/mangohub/hub/web/api"
	"github.com/dengyie/mangohub/hub/web/connection"
	"github.com/dengyie/mangohub/hub/web/filemanager"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func readMaybeCompressedBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	}
	return io.ReadAll(r.Body)
}

func bindV2Params[T any](raw any, target *T) error {
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

func handleV2RPC(uuid string, req v2.Request, allowWait bool) v2.Response {
	if req.JSONRPC != v2.Version {
		return v2.Error(req.ID, -32600, "invalid jsonrpc version", nil)
	}
	switch req.Method {
	case v2.MethodAgentReport:
		var params v2.ReportParams
		if err := bindV2Params(req.Params, &params); err != nil {
			return v2.Error(req.ID, -32602, "invalid report params", err.Error())
		}
		if err := ingestReport(uuid, params.Report, true); err != nil {
			return v2.Error(req.ID, -32000, "failed to save report", err.Error())
		}
		return v2.Success(req.ID, gin.H{
			"status": "success",
			"events": agent_runtime.TakeV2Events(uuid, params.AckEventIDs, 8),
		})
	case v2.MethodAgentBasicInfo:
		var params v2.BasicInfoParams
		if err := bindV2Params(req.Params, &params); err != nil {
			return v2.Error(req.ID, -32602, "invalid basic info params", err.Error())
		}
		if err := ingestBasicInfo(uuid, params.Info, ""); err != nil {
			return v2.Error(req.ID, -32000, "failed to save basic info", err.Error())
		}
		return v2.Success(req.ID, gin.H{"status": "success"})
	case v2.MethodAgentPingResult:
		var params v2.PingResultParams
		if err := bindV2Params(req.Params, &params); err != nil {
			return v2.Error(req.ID, -32602, "invalid ping result params", err.Error())
		}
		if err := ingestPingResult(uuid, params.TaskID, params.Value); err != nil {
			return v2.Error(req.ID, -32000, "failed to save ping result", err.Error())
		}
		return v2.Success(req.ID, gin.H{"status": "success"})
	case v2.MethodAgentTaskResult:
		var params v2.TaskResultParams
		if err := bindV2Params(req.Params, &params); err != nil {
			return v2.Error(req.ID, -32602, "invalid task result params", err.Error())
		}
		finishedAt := params.FinishedAt
		if finishedAt.IsZero() {
			finishedAt = time.Now().UTC()
		}
		if err := tasks.SaveTaskResult(params.TaskID, uuid, params.Result, params.ExitCode, finishedAt); err != nil {
			return v2.Error(req.ID, -32000, "failed to save task result", err.Error())
		}
		return v2.Success(req.ID, gin.H{"status": "success"})
	case v2.MethodAgentPull:
		var params v2.PullParams
		if err := bindV2Params(req.Params, &params); err != nil {
			return v2.Error(req.ID, -32602, "invalid pull params", err.Error())
		}
		refreshPostPresence(uuid)
		agent_runtime.MarkV2Client(uuid)
		timeout := 0 * time.Second
		if allowWait {
			timeout = 25 * time.Second
		}
		return v2.Success(req.ID, gin.H{
			"events": agent_runtime.WaitV2Events(uuid, params.AckEventIDs, timeout),
		})
	case v2.MethodAgentFileResult:
		var params v2.FileResult
		if err := bindV2Params(req.Params, &params); err != nil {
			return v2.Error(req.ID, -32602, "invalid file result params", err.Error())
		}
		params.UUID = uuid
		if !filemanager.Resolve(params) {
			return v2.Error(req.ID, -32004, "unknown or expired file operation", nil)
		}
		return v2.Success(req.ID, gin.H{"status": "success"})
	case v2.MethodAgentSSHTicketResult:
		var params v2.SSHTicketResultParams
		if err := bindV2Params(req.Params, &params); err != nil {
			return v2.Error(req.ID, -32602, "invalid ssh ticket result params", err.Error())
		}
		if params.TicketID == "" {
			return v2.Error(req.ID, -32602, "ticket_id is required", nil)
		}
		if err := ssh.UpdateSSHTicketResult(params.TicketID, params.Success, params.Error); err != nil {
			return v2.Error(req.ID, -32000, "failed to update ticket result", err.Error())
		}
		return v2.Success(req.ID, gin.H{"status": "success"})
	default:
		return v2.Error(req.ID, -32601, "method not found", req.Method)
	}
}

func UploadV2RPC(c *gin.Context) {
	bytesBody, err := readMaybeCompressedBody(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, v2.Error(nil, -32700, "invalid compressed body", err.Error()))
		return
	}
	var req v2.Request
	if err := json.Unmarshal(bytesBody, &req); err != nil {
		c.JSON(http.StatusBadRequest, v2.Error(nil, -32700, "parse error", err.Error()))
		return
	}
	uuid, ok := clientUUIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, v2.Error(req.ID, -32001, "invalid token", nil))
		return
	}
	resp := handleV2RPC(uuid, req, true)
	status := http.StatusOK
	if resp.Error != nil {
		status = http.StatusBadRequest
	}
	c.JSON(status, resp)
}

func WebSocketV2RPC(c *gin.Context) {
	if !api.IsWebSocketUpgrade(c) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "Require WebSocket upgrade"})
		return
	}
	conn, err := api.UpgradeSafeConn(c, api.EnableWebSocketCompression)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "Failed to upgrade to WebSocket." + err.Error()})
		return
	}
	defer conn.Close()

	uuid, ok := clientUUIDFromContext(c)
	if !ok {
		conn.WriteJSON(v2.Error(nil, -32001, "invalid token", nil))
		return
	}
	// The uuid already has an active connection. If that connection is alive
	// (frames seen within zombieConnThreshold — healthy agents report every
	// few seconds), the new one is the duplicate: reject it and keep the old
	// one, otherwise two agents sharing one token kick each other in a
	// reconnect storm. A silent registered connection is a dead half-open
	// socket (agent slept, NAT rebuilt): take over so the reconnecting agent
	// is not locked out until the read deadline expires.
	if oldConn := agent_runtime.GetConnectedClient(uuid); oldConn != nil {
		if time.Since(oldConn.LastActive()) < zombieConnThreshold {
			duplicateCloseReason := fmt.Sprintf("duplicate connection rejected: client %s already has an active connection", uuid)
			if len(duplicateCloseReason) > maxCloseReasonLength {
				// gorilla does not truncate; an oversized reason makes
				// WriteControl reject the whole close frame.
				duplicateCloseReason = duplicateCloseReason[:maxCloseReasonLength]
			}
			_ = conn.GetConn().WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, duplicateCloseReason),
				time.Now().Add(time.Second),
			)
			// Give the peer a moment to complete the close handshake before
			// the deferred conn.Close() tears the TCP connection down, so the
			// client actually observes close code 1008 instead of a bare 1006.
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					break
				}
			}
			logDuplicateReject(uuid, c.ClientIP(), oldConn.ID)
			return
		}
		logger.Warnf("client-api", "replacing stale v2 connection %d for client %s (no frames for %s): serving new connection from %s", oldConn.ID, uuid, time.Since(oldConn.LastActive()).Round(time.Second), c.ClientIP())
		// Closing wakes the old handler, whose conditional registry cleanup
		// only removes a connection that is still the registered one, so the
		// registration below survives it.
		go oldConn.Close()
	}
	agent_runtime.SetConnectedClients(uuid, conn)
	agent_runtime.MarkV2Client(uuid)
	go notifierOnline(uuid, conn.ID)
	defer func() {
		agent_runtime.DeleteClientConditionally(uuid, conn)
		notifierOffline(uuid, conn.ID)
	}()
	if !pushQueuedV2Events(conn, uuid) {
		return
	}

	for {
		conn.SetReadDeadline(time.Now().Add(readWait))
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("client-api", "Client %s v2 connection error: %v", uuid, err)
			}
			return
		}
		message = bytes.TrimSpace(message)
		var req v2.Request
		if err := json.Unmarshal(message, &req); err != nil {
			conn.WriteJSON(v2.Error(nil, -32700, "parse error", err.Error()))
			continue
		}
		resp := handleV2RPC(uuid, req, false)
		if req.ID != nil {
			if err := conn.WriteJSON(resp); err != nil {
				logger.Errorf("client-api", "failed to write v2 rpc response: %v", err)
				return
			}
		}
	}
}

func pushQueuedV2Events(conn *connection.SafeConn, uuid string) bool {
	events := agent_runtime.TakeV2Events(uuid, nil, 0)
	if len(events) == 0 {
		return true
	}
	ackIDs := make([]string, 0, len(events))
	for _, event := range events {
		payload := v2.Request{JSONRPC: v2.Version, Method: event.Method, Params: event.Params}
		if err := conn.WriteJSON(payload); err != nil {
			agent_runtime.AckV2Events(uuid, ackIDs)
			logger.Errorf("client-api", "failed to push queued v2 event %s to client %s: %v", event.ID, uuid, err)
			return false
		}
		ackIDs = append(ackIDs, event.ID)
	}
	agent_runtime.AckV2Events(uuid, ackIDs)
	return true
}

func clientUUIDFromContext(c *gin.Context) (string, bool) {
	if v, ok := c.Get("client_uuid"); ok {
		if uuid, ok := v.(string); ok && uuid != "" {
			return uuid, true
		}
	}
	token := c.Query("token")
	if token == "" {
		return "", false
	}
	uuid, err := clients.GetClientUUIDByToken(token)
	return uuid, err == nil && uuid != ""
}

// duplicateRejectLogInterval throttles the duplicate-rejection warning per
// client: agent binaries without reconnect backoff (pre-2026-11-03
// snapshots) retry every couple of seconds and would otherwise flood the log
// for as long as a token misconfiguration persists.
var duplicateRejectLogInterval = time.Minute

type duplicateRejectLogState struct {
	mu         sync.Mutex
	lastWarn   time.Time
	suppressed int
}

var duplicateRejectLogs sync.Map // uuid -> *duplicateRejectLogState

// duplicateRejectTakeTicket decides whether one duplicate-rejection attempt
// emits the warning line: at most one per interval per client, with the
// suppressed attempts folded into the next line as a "+N" suffix.
func duplicateRejectTakeTicket(uuid string) (shouldLog bool, suppressed int) {
	v, _ := duplicateRejectLogs.LoadOrStore(uuid, &duplicateRejectLogState{})
	st := v.(*duplicateRejectLogState)
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	if st.lastWarn.IsZero() || now.Sub(st.lastWarn) >= duplicateRejectLogInterval {
		suppressed = st.suppressed
		st.lastWarn = now
		st.suppressed = 0
		return true, suppressed
	}
	st.suppressed++
	return false, 0
}

// logDuplicateReject emits the duplicate-rejection warning through the
// interval throttle in duplicateRejectTakeTicket.
func logDuplicateReject(uuid, ip string, oldConnID int64) {
	shouldLog, suppressed := duplicateRejectTakeTicket(uuid)
	if !shouldLog {
		return
	}
	if suppressed > 0 {
		logger.Warnf("client-api", "rejected duplicate v2 connection for client %s from %s: keeping active connection %d (+%d more attempts suppressed within %s)", uuid, ip, oldConnID, suppressed, duplicateRejectLogInterval)
		return
	}
	logger.Warnf("client-api", "rejected duplicate v2 connection for client %s from %s: keeping active connection %d", uuid, ip, oldConnID)
}

func notifierOnline(uuid string, connID int64) {
	go func() {
		defer func() { _ = recover() }()
		notifier.OnlineNotification(uuid, connID)
	}()
}

func notifierOffline(uuid string, connID int64) {
	defer func() { _ = recover() }()
	notifier.OfflineNotification(uuid, connID)
}
