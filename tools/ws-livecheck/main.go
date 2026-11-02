// ws-livecheck verifies a running MangoHub deployment from the outside:
//
//  1. the duplicate-connection contract — a second WebSocket carrying a live
//     client's token must be rejected with close code 1008 and the reason
//     "duplicate connection rejected: client <uuid> already has an active
//     connection" while the real agent keeps reporting untouched;
//  2. the invalid-token paths (WS handshake 401, POST upload 401);
//  3. report continuity for the target node across the test.
//
// Credentials are read from the environment and never printed: every output
// string is passed through a redaction filter for the admin key and token.
//
// Usage:
//
//	MANGO_BASE=https://hub.example.com MANGO_ADMIN_KEY=<key> go run .
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var redactions []string

func redact(s string) string {
	for _, r := range redactions {
		s = strings.ReplaceAll(s, r, "<REDACTED>")
	}
	return s
}

func out(format string, args ...any) {
	fmt.Println(redact(fmt.Sprintf(format, args...)))
}

func apiGet(base, path, adminKey string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if adminKey != "" {
		req.Header.Set("Authorization", "Bearer "+adminKey)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

// wsDialAndRead dials the v2 rpc endpoint with the given token and reports
// the first frame/close the peer sends back.
func wsDialAndRead(base, token, label string) {
	wsURL := "wss" + strings.TrimPrefix(base, "https") + "/api/clients/v2/rpc?token=" + token
	dialer := &websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		out("[%s] dial rejected/failed: err=%v http_status=%d", label, err, status)
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	mt, payload, err := conn.ReadMessage()
	if err != nil {
		code := websocket.CloseNoStatusReceived
		text := ""
		var ce *websocket.CloseError
		if asCloseError(err, &ce) {
			code, text = ce.Code, ce.Text
		}
		out("[%s] closed without data frame: close_code=%d reason=%q err=%v", label, code, text, err)
		_ = conn.Close()
		return
	}
	out("[%s] got frame type=%d payload=%s", label, mt, redact(string(payload)))
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, _, err = conn.ReadMessage()
	if asCloseError(err, nil) || err != nil {
		code := websocket.CloseNoStatusReceived
		text := ""
		var ce *websocket.CloseError
		if asCloseError(err, &ce) {
			code, text = ce.Code, ce.Text
		}
		out("[%s] subsequent close: close_code=%d reason=%q", label, code, text)
	}
	_ = conn.Close()
}

func asCloseError(err error, ce **websocket.CloseError) bool {
	if err == nil {
		return false
	}
	if c, ok := err.(*websocket.CloseError); ok {
		if ce != nil {
			*ce = c
		}
		return true
	}
	return false
}

type clientEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type recentResp struct {
	Data any `json:"data"`
}

func main() {
	base := os.Getenv("MANGO_BASE")
	adminKey := os.Getenv("MANGO_ADMIN_KEY")
	if base == "" || adminKey == "" {
		fmt.Println("MANGO_BASE / MANGO_ADMIN_KEY not set")
		os.Exit(2)
	}
	redactions = append(redactions, adminKey)

	// 0. build info
	if b, code, err := apiGet(base, "/api/version", ""); err == nil && code == 200 {
		out("[version] http=%d body=%s", code, string(b))
	} else {
		out("[version] query failed: %v", err)
	}

	// 1. pick an online client: admin list, then require recent records.
	b, code, err := apiGet(base, "/api/admin/client/list", adminKey)
	if err != nil || code != 200 {
		out("[clients] FAILED http=%d err=%v body=%s", code, err, redact(string(b)))
		os.Exit(1)
	}
	var clients []clientEntry
	if err := json.Unmarshal(b, &clients); err != nil {
		// maybe wrapped in data envelope
		var env struct {
			Data []clientEntry `json:"data"`
		}
		if e2 := json.Unmarshal(b, &env); e2 != nil || len(env.Data) == 0 {
			out("[clients] parse failed: %v / %v", err, e2)
			os.Exit(1)
		}
		clients = env.Data
	}
	out("[clients] total=%d", len(clients))

	latestRecord := func(arr []any) string {
		if len(arr) == 0 {
			return "(none)"
		}
		if m, ok := arr[len(arr)-1].(map[string]any)["time"].(string); ok {
			return m
		}
		return fmt.Sprintf("%v", arr[len(arr)-1])
	}
	target := ""
	for _, c := range clients {
		b, code, err := apiGet(base, "/api/recent/"+c.UUID+"?hours=1", adminKey)
		if err != nil || code != 200 {
			continue
		}
		var rr recentResp
		if json.Unmarshal(b, &rr) != nil || rr.Data == nil {
			continue
		}
		if arr, ok := rr.Data.([]any); ok && len(arr) > 0 {
			target = c.UUID
			out("[target] uuid=%s name=%q recent_records_1h=%d", c.UUID, c.Name, len(arr))
			break
		}
	}
	if target == "" {
		out("[target] no client with recent reports found")
		os.Exit(1)
	}
	b, code, err = apiGet(base, "/api/recent/"+target+"?hours=1", adminKey)
	if err == nil && code == 200 {
		var rr recentResp
		if json.Unmarshal(b, &rr) == nil {
			if arr, ok := rr.Data.([]any); ok {
				out("[target] last record before test: %s", latestRecord(arr))
			}
		}
	}

	// 2. fetch the target's token (kept in memory only).
	b, code, err = apiGet(base, "/api/admin/client/"+target+"/token", adminKey)
	if err != nil || code != 200 {
		out("[token] FAILED http=%d err=%v body=%s", code, err, redact(string(b)))
		os.Exit(1)
	}
	var tok struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &tok); err != nil {
		out("[token] parse failed: %v", err)
		os.Exit(1)
	}
	token := tok.Data.Token
	if token == "" {
		token = tok.Token
	}
	if token == "" {
		out("[token] empty response body shape: %s", redact(string(b)))
		os.Exit(1)
	}
	redactions = append(redactions, token)
	out("[token] fetched (len=%d, redacted)", len(token))

	// 3. duplicate connection test: the live agent already holds the slot.
	wsDialAndRead(base, token, "duplicate#1")
	wsDialAndRead(base, token, "duplicate#2")

	// 4. invalid token path.
	wsDialAndRead(base, "livecheck-invalid-token-xyz", "invalid-token")

	// 5. POST upload path with invalid token.
	req, _ := http.NewRequest("POST", base+"/api/clients/v2/rpc?token=livecheck-invalid-token-xyz", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"agent.pull","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		out("[post-invalid] failed: %v", err)
	} else {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		out("[post-invalid] http=%d body=%s", resp.StatusCode, redact(string(body)))
	}

	// 6. the real agent must have kept reporting throughout.
	time.Sleep(5 * time.Second)
	b, code, err = apiGet(base, "/api/recent/"+target+"?hours=1", adminKey)
	if err != nil || code != 200 {
		out("[after] recent query failed: %v", err)
	} else {
		var rr recentResp
		if json.Unmarshal(b, &rr) == nil {
			if arr, ok := rr.Data.([]any); ok {
				out("[after] last record after test: %s (records_1h=%d)", latestRecord(arr), len(arr))
			}
		}
	}
	out("[done] see summary above")
}
