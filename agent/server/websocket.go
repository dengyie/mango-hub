package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dengyie/mangohub/agent/dnsresolver"
	"github.com/dengyie/mangohub/agent/monitoring"
	v2 "github.com/dengyie/mangohub/agent/protocol/v2"
	"github.com/dengyie/mangohub/agent/reconnect"
	"github.com/dengyie/mangohub/agent/sshauth"
	"github.com/dengyie/mangohub/agent/terminal"
	"github.com/dengyie/mangohub/agent/utils"
	"github.com/dengyie/mangohub/agent/ws"
	"github.com/gorilla/websocket"
)

var (
	v2AckMu       sync.Mutex
	v2AckEventIDs []string
	v2SeenEvents  = make(map[string]time.Time)
)

const (
	v2SeenEventTTL   = 10 * time.Minute
	v2SeenEventLimit = 4096

	// A report is written every reportInterval (default 3s). A write still
	// blocked after wsWriteTimeout therefore means the peer or the path is gone.
	wsWriteTimeout = 10 * time.Second
)

// readDrainGrace bounds how long the write-failure path waits for the read
// loop to finish recording the disconnect. A close frame already in flight
// (e.g. the hub's duplicate rejection 1008) lands within one RTT; this grace
// lets mergeReadDisconnect report the real close code instead of the write
// symptom, so the duplicate-rejection pin still engages when the failure is
// observed from the write side first. Bounded so a blackholed read — which
// only unblocks once conn.Close() runs — cannot stall the reconnect path.
var readDrainGrace = 500 * time.Millisecond

func EstablishWebSocketConnection() {
	var conn *ws.SafeConn
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()
	var err error
	interval := math.Max(1, flags.Interval)

	// Connection recovery must not wait for the (possibly much longer) report
	// interval. Poll the connection frequently and gate reports separately.
	dataTicker := time.NewTicker(time.Second)
	defer dataTicker.Stop()
	reportInterval := time.Duration(interval * float64(time.Second))
	nextReportAt := time.Now()

	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	var readDone <-chan struct{}
	var readDisconn *reconnect.Disconnect

	// Exponential backoff for reconnects after abnormal disconnects. Without
	// it, a hub that keeps killing the connection (e.g. duplicate token
	// rejection) turns every disconnect into a reconnect storm.
	backoff := &reconnect.State{}
	reconnectBase := time.Duration(flags.ReconnectInterval) * time.Second
	if reconnectBase < time.Second {
		reconnectBase = time.Second
	}
	if reconnectBase > reconnect.MaxBackoff {
		log.Printf("reconnect-interval %ds exceeds the %s reconnect backoff cap; reconnect delays are capped at %s", flags.ReconnectInterval, reconnect.MaxBackoff, reconnect.MaxBackoff)
	}
	var nextConnectAt time.Time

	// mergeReadDisconnect records one disconnect from the write path, but
	// prefers the read loop's account when it already finished: the read
	// error carries the close code (e.g. the hub's duplicate rejection 1008),
	// while the write failure is only a symptom. Consuming the closed
	// channel here keeps MarkDisconnected to exactly one call per death.
	mergeReadDisconnect := func(writeErr error) reconnect.Disconnect {
		d := reconnect.Disconnect{Detail: writeErr.Error()}
		if readDone != nil {
			select {
			case <-readDone:
				if readDisconn != nil {
					d = *readDisconn
				}
				readDone = nil
				readDisconn = nil
			case <-time.After(readDrainGrace):
				// The read loop is still unwinding; conn.Close() below ends it.
			}
		}
		return d
	}

	for {
		select {
		case <-dataTicker.C:
			if conn == nil {
				if time.Now().Before(nextConnectAt) {
					continue
				}
				log.Println("Attempting to connect to WebSocket...")
				retry := 0
				for retry <= flags.MaxRetries {
					if retry > 0 {
						log.Println("Retrying websocket connection, attempt:", retry)
					}
					websocketEndpoint := buildWebSocketEndpoint()
					conn, err = connectWebSocket(websocketEndpoint)
					if err == nil {
						log.Println("WebSocket connected using v2 protocol")
						done := make(chan struct{})
						readDone = done
						readDisconn = &reconnect.Disconnect{}
						backoff.MarkConnected(time.Now())
						go handleWebSocketMessages(conn, done, readDisconn)
						break
					} else {
						log.Println("Failed to connect to WebSocket:", err)
					}
					retry++
					time.Sleep(time.Duration(flags.ReconnectInterval) * time.Second)
				}

				if retry > flags.MaxRetries {
					log.Println("Max retries reached.")
					conn, err = runPostFallback(buildWebSocketEndpoint(), interval)
					if err != nil {
						log.Println("POST fallback stopped:", err)
						return
					}
					log.Println("WebSocket recovered from POST fallback")
					done := make(chan struct{})
					readDone = done
					readDisconn = &reconnect.Disconnect{}
					backoff.MarkConnected(time.Now())
					go handleWebSocketMessages(conn, done, readDisconn)
				}
			}
			if conn == nil || time.Now().Before(nextReportAt) {
				continue
			}
			nextReportAt = time.Now().Add(reportInterval)

			data := v2.BuildReportPayload(monitoring.GenerateReport())
			err = conn.WriteMessageWithDeadline(websocket.TextMessage, data, wsWriteTimeout)
			if err != nil {
				log.Println("Failed to send WebSocket message:", err)
				nextConnectAt = time.Now().Add(backoff.MarkDisconnected(time.Now(), mergeReadDisconnect(err), reconnectBase))
				conn.Close()
				conn = nil // Mark connection as dead
				readDone = nil
				readDisconn = nil
				continue
			}
		case <-heartbeatTicker.C:
			if conn != nil {
				err := conn.WriteMessageWithDeadline(websocket.PingMessage, nil, wsWriteTimeout)
				if err != nil {
					log.Println("Failed to send heartbeat:", err)
					nextConnectAt = time.Now().Add(backoff.MarkDisconnected(time.Now(), mergeReadDisconnect(err), reconnectBase))
					conn.Close()
					conn = nil // Mark connection as dead
					readDone = nil
					readDisconn = nil
				}
			}
		case <-readDone:
			disconn := reconnect.Disconnect{}
			if readDisconn != nil {
				disconn = *readDisconn
			}
			nextConnectAt = time.Now().Add(backoff.MarkDisconnected(time.Now(), disconn, reconnectBase))
			if conn != nil {
				conn.Close()
				conn = nil
			}
			readDone = nil
			readDisconn = nil
		}
	}
}

func buildWebSocketEndpoint() string {
	websocketEndpoint := strings.TrimSuffix(flags.Endpoint, "/") + "/api/clients/v2/rpc?token=" + flags.Token
	websocketEndpoint = "ws" + strings.TrimPrefix(websocketEndpoint, "http")
	if convertedEndpoint, err := utils.ConvertIDNToASCII(websocketEndpoint); err == nil {
		return convertedEndpoint
	} else {
		log.Printf("Warning: Failed to convert WebSocket IDN to ASCII: %v", err)
	}
	return websocketEndpoint
}

func runPostFallback(websocketEndpoint string, interval float64) (*ws.SafeConn, error) {
	log.Println("Entering v2 POST fallback mode")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runV2PullLoop(ctx)

	reportTicker := time.NewTicker(time.Duration(interval * float64(time.Second)))
	defer reportTicker.Stop()
	reconnectTicker := time.NewTicker(time.Duration(flags.ReconnectInterval) * time.Second)
	defer reconnectTicker.Stop()

	for {
		select {
		case <-reportTicker.C:
			reportID := fmt.Sprintf("report-%d", time.Now().UnixNano())
			ackIDs := snapshotV2AckEventIDs()
			resp, err := postV2Request(v2.BuildReportRequest(reportID, monitoring.GenerateReport(), ackIDs))
			if err != nil {
				log.Println("Failed to POST v2 report:", err)
				continue
			}
			clearV2AckEventIDs(ackIDs)
			processV2ResponseEvents(resp)
		case <-reconnectTicker.C:
			conn, err := connectWebSocket(websocketEndpoint)
			if err == nil {
				return conn, nil
			}
			log.Println("POST fallback WebSocket recovery failed:", err)
		}
	}
}

func runV2PullLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		pullStart := time.Now()
		pullID := fmt.Sprintf("pull-%d", time.Now().UnixNano())
		ackIDs := snapshotV2AckEventIDs()
		payload := v2.NewRequest(pullID, v2.MethodAgentPull, map[string]interface{}{
			"capabilities":  []string{"exec", "ping", "message", "event", "terminal", "file"},
			"ack_event_ids": ackIDs,
		})
		resp, err := postV2RequestContext(ctx, payload)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Println("Failed to POST v2 pull:", err)
			timer := time.NewTimer(time.Duration(flags.ReconnectInterval) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		clearV2AckEventIDs(ackIDs)
		processV2ResponseEvents(resp)

		// 防空转保护：当服务端或反向代理立即返回响应（如无挂起或快速返回）时，
		// 施加保底节流延时，防止在 HTTP 降级长轮询下进入死循环打满 CPU。
		if elapsed := time.Since(pullStart); elapsed < 200*time.Millisecond {
			backoffTimer := time.NewTimer(200*time.Millisecond - elapsed)
			select {
			case <-ctx.Done():
				backoffTimer.Stop()
				return
			case <-backoffTimer.C:
			}
		}
	}
}

func postV2Request(payload []byte) (*v2.Response, error) {
	return postV2RequestContext(context.Background(), payload)
}

func postV2RequestContext(ctx context.Context, payload []byte) (*v2.Response, error) {
	endpoint := strings.TrimSuffix(flags.Endpoint, "/") + "/api/clients/v2/rpc?token=" + flags.Token
	body := payload
	compressed := false
	if !flags.DisableCompression {
		if gz, err := gzipBytes(payload); err == nil {
			body = gz
			compressed = true
		}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if compressed {
		req.Header.Set("Content-Encoding", "gzip")
	}
	client := dnsresolver.GetHTTPClientWithPreference(35*time.Second, flags.PreferIPVersion)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bytesBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{StatusCode: resp.StatusCode, Status: resp.Status, Body: string(bytesBody)}
	}
	rpcResp, err := parseV2Response(bytesBody)
	if err != nil {
		return nil, err
	}
	return rpcResp, nil
}

func processV2ResponseEvents(resp *v2.Response) {
	if resp == nil || resp.Result == nil {
		return
	}
	var result v2.EventResult
	if err := v2.BindResult(resp.Result, &result); err != nil {
		log.Println("Failed to bind v2 event result:", err)
		return
	}
	for _, event := range result.Events {
		if processV2Event(nil, event.Method, event.Params, event.ID) {
			addV2AckEventID(event.ID)
		}
	}
}

func snapshotV2AckEventIDs() []string {
	v2AckMu.Lock()
	defer v2AckMu.Unlock()
	return append([]string{}, v2AckEventIDs...)
}

func clearV2AckEventIDs(sent []string) {
	if len(sent) == 0 {
		return
	}
	sentSet := make(map[string]struct{}, len(sent))
	for _, id := range sent {
		sentSet[id] = struct{}{}
	}
	v2AckMu.Lock()
	defer v2AckMu.Unlock()
	remaining := v2AckEventIDs[:0]
	for _, id := range v2AckEventIDs {
		if _, ok := sentSet[id]; !ok {
			remaining = append(remaining, id)
		}
	}
	v2AckEventIDs = remaining
}

func addV2AckEventID(id string) {
	if id == "" {
		return
	}
	v2AckMu.Lock()
	defer v2AckMu.Unlock()
	v2AckEventIDs = append(v2AckEventIDs, id)
}

func markV2EventSeen(id string) bool {
	if id == "" {
		return true
	}
	v2AckMu.Lock()
	defer v2AckMu.Unlock()
	now := time.Now()
	for eventID, seenAt := range v2SeenEvents {
		if now.Sub(seenAt) > v2SeenEventTTL {
			delete(v2SeenEvents, eventID)
		}
	}
	if _, ok := v2SeenEvents[id]; ok {
		return false
	}
	if len(v2SeenEvents) >= v2SeenEventLimit {
		var oldestID string
		var oldest time.Time
		for eventID, seenAt := range v2SeenEvents {
			if oldestID == "" || seenAt.Before(oldest) {
				oldestID, oldest = eventID, seenAt
			}
		}
		if oldestID != "" {
			delete(v2SeenEvents, oldestID)
		}
	}
	v2SeenEvents[id] = now
	return true
}

func connectWebSocket(websocketEndpoint string) (*ws.SafeConn, error) {
	dialer := newWSDialer()

	conn, resp, err := dialer.Dial(websocketEndpoint, nil)
	if err != nil {
		if resp != nil && resp.StatusCode != 101 {
			return nil, &httpStatusError{StatusCode: resp.StatusCode, Status: resp.Status}
		}
		return nil, err
	}

	return ws.NewSafeConn(conn), nil
}

func handleWebSocketMessages(conn *ws.SafeConn, done chan<- struct{}, disconn *reconnect.Disconnect) {
	defer close(done)
	for {
		_, message_raw, err := conn.ReadMessage()
		if err != nil {
			// Recorded before close(done): the main loop reads *disconn after
			// receiving from the closed channel, which orders the access.
			if disconn != nil {
				*disconn = reconnect.Describe(err)
			}
			log.Println("WebSocket read error:", err)
			return
		}
		var message v2.Request
		err = json.Unmarshal(message_raw, &message)
		if err != nil {
			log.Println("Bad ws message:", err)
			continue
		}
		if message.JSONRPC != v2.Version {
			log.Printf("Bad v2 ws message version %q", message.JSONRPC)
			continue
		}
		processV2Event(conn, message.Method, message.Params, "")
	}
}

func processV2Event(conn *ws.SafeConn, method string, params interface{}, eventID string) bool {
	if !markV2EventSeen(eventID) {
		return true
	}
	switch method {
	case v2.MethodAgentExec:
		var p struct {
			TaskID  string `json:"task_id"`
			Command string `json:"command"`
		}
		if err := v2.BindParams(params, &p); err == nil {
			go NewTask(p.TaskID, p.Command)
			return true
		} else {
			log.Printf("bad v2 exec params: %v", err)
		}
	case v2.MethodAgentMiningControl:
		var p struct {
			TaskID string `json:"task_id"`
			Action string `json:"action"`
		}
		if err := v2.BindParams(params, &p); err == nil {
			go NewMiningControlTask(p.TaskID, p.Action)
			return true
		} else {
			log.Printf("bad v2 mining control params: %v", err)
		}
	case v2.MethodAgentPing:
		var p struct {
			TaskID uint   `json:"ping_task_id"`
			Type   string `json:"ping_type"`
			Target string `json:"ping_target"`
		}
		if err := v2.BindParams(params, &p); err == nil {
			go NewPingTask(conn, p.TaskID, p.Type, p.Target)
			return true
		} else {
			log.Printf("bad v2 ping params: %v", err)
		}
	case v2.MethodAgentTerminal:
		var p struct {
			RequestID string `json:"request_id"`
		}
		if err := v2.BindParams(params, &p); err == nil {
			go establishTerminalConnection(flags.Token, p.RequestID, flags.Endpoint)
			return true
		} else {
			log.Printf("bad v2 terminal params: %v", err)
		}
	case v2.MethodAgentMessage, v2.MethodAgentEvent:
		log.Printf("received v2 %s: %+v", method, params)
		return true
	case v2.MethodAgentFile:
		var operation v2.FileOperation
		if err := v2.BindParams(params, &operation); err == nil {
			go handleFileOperation(operation)
			return true
		} else {
			log.Printf("bad v2 file params: %v", err)
		}
	case v2.MethodAgentSSHAuthorize:
		var p v2.SSHAuthorizeParams
		if err := v2.BindParams(params, &p); err == nil {
			go func() {
				if err := sshauth.AuthorizeKey(p.TicketID, p.PublicKey, p.TargetUser, p.ExpiresAt); err != nil {
					log.Printf("[sshauth] Failed to authorize ticket %s: %v", p.TicketID, err)
				}
			}()
			return true
		} else {
			log.Printf("bad v2 ssh authorize params: %v", err)
		}
	case v2.MethodAgentSSHRevoke:
		var p v2.SSHRevokeParams
		if err := v2.BindParams(params, &p); err == nil {
			go func() {
				if err := sshauth.RevokeKey(p.TicketID, ""); err != nil {
					log.Printf("[sshauth] Failed to revoke ticket %s: %v", p.TicketID, err)
				}
			}()
			return true
		} else {
			log.Printf("bad v2 ssh revoke params: %v", err)
		}
	default:
		log.Printf("unknown v2 event method %s", method)
	}
	return false
}

// connectWebSocket attempts to establish a WebSocket connection and upload basic info

// establishTerminalConnection 建立终端连接并使用terminal包处理终端操作
func establishTerminalConnection(token, id, endpoint string) {
	endpoint = strings.TrimSuffix(endpoint, "/") + "/api/clients/terminal?token=" + token + "&id=" + id
	endpoint = "ws" + strings.TrimPrefix(endpoint, "http")

	// 转换中文域名为 ASCII 兼容编码
	if convertedEndpoint, err := utils.ConvertIDNToASCII(endpoint); err == nil {
		endpoint = convertedEndpoint
	} else {
		log.Printf("Warning: Failed to convert Terminal WebSocket IDN to ASCII: %v", err)
	}

	// 使用与主 WS 相同的拨号策略
	dialer := newWSDialer()

	conn, _, err := dialer.Dial(endpoint, nil)
	if err != nil {
		log.Println("Failed to establish terminal connection:", err)
		return
	}

	// 启动终端
	terminal.StartTerminal(conn, id)
	if conn != nil {
		conn.Close()
	}
}

// newWSDialer 构造统一的 WebSocket 拨号器（自定义解析、IPv4/IPv6 动态排序、可选 TLS 忽略）
func newWSDialer() *websocket.Dialer {
	d := &websocket.Dialer{
		HandshakeTimeout:  15 * time.Second,
		NetDialContext:    dnsresolver.GetDialContextWithPreference(15*time.Second, flags.PreferIPVersion),
		Proxy:             http.ProxyFromEnvironment,
		EnableCompression: !flags.DisableCompression,
	}
	if flags.IgnoreUnsafeCert {
		d.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return d
}
