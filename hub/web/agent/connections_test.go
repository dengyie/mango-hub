package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/dengyie/mangohub/hub/web/connection"
	v2 "github.com/dengyie/mangohub/protocol/v2"
)

// newWSConnPair builds one real WebSocket connection pair and returns the
// server side wrapped in a SafeConn. The client side is handed to the caller
// (or closed when clientSide is nil) because gorilla allows exactly one
// reader per connection and the tests decide who reads.
func newWSConnPair(t *testing.T) (server *connection.SafeConn, client *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverReady := make(chan *connection.SafeConn, 1)
	serverErr := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		serverReady <- connection.NewSafeConn(raw)
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	client, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial test websocket: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case server = <-serverReady:
	case err := <-serverErr:
		t.Fatalf("upgrade test websocket: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("server-side upgrade timed out")
	}
	return server, client
}

func TestPresenceGraceCoversReconnectGap(t *testing.T) {
	oldGrace := presenceGrace
	presenceGrace = 400 * time.Millisecond
	t.Cleanup(func() { presenceGrace = oldGrace })

	mu.Lock()
	previousConnected, previousPresence, previousGrace := connectedClients, presenceOnly, graceUntil
	connectedClients = make(map[string]*connection.SafeConn)
	presenceOnly = make(map[string]struct {
		id     int64
		expire time.Time
	})
	graceUntil = make(map[string]time.Time)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		connectedClients, presenceOnly, graceUntil = previousConnected, previousPresence, previousGrace
		mu.Unlock()
	})

	server, _ := newWSConnPair(t)
	const uuid = "node-grace"

	SetConnectedClients(uuid, server)
	if !uuidOnline(uuid) {
		t.Fatal("registered connection should be online")
	}

	// 连接退出：宽限期内仍算在线（UI 不闪离线）。
	DeleteClientConditionally(uuid, server)
	if !uuidOnline(uuid) {
		t.Fatal("uuid should stay online inside the presence grace window")
	}

	// 宽限期过后才真正翻转离线。
	time.Sleep(presenceGrace + 150*time.Millisecond)
	if uuidOnline(uuid) {
		t.Fatal("uuid should flip offline once the grace window passed")
	}

	// 真实注册清掉残留宽限：重新上线后宽限期过期与否都由活连接兜底。
	DeleteClientConditionally(uuid, server) // 无注册连接时仅存 map 查询，不盖章
	SetConnectedClients(uuid, server)
	time.Sleep(presenceGrace + 150*time.Millisecond)
	if !uuidOnline(uuid) {
		t.Fatal("live connection must stay online regardless of grace state")
	}
}

func TestPresenceGraceSkipsAdminPurge(t *testing.T) {
	oldGrace := presenceGrace
	presenceGrace = 5 * time.Second
	t.Cleanup(func() { presenceGrace = oldGrace })

	mu.Lock()
	graceUntil = make(map[string]time.Time)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		graceUntil = make(map[string]time.Time)
		mu.Unlock()
	})

	server, _ := newWSConnPair(t)
	const uuid = "node-admin-purge"

	SetConnectedClients(uuid, server)
	DeleteConnectedClients(uuid)
	if uuidOnline(uuid) {
		t.Fatal("admin purge must not grant a presence grace window")
	}
}

func TestExpiredPostPresenceKeepsGraceWindow(t *testing.T) {
	oldGrace := presenceGrace
	presenceGrace = 500 * time.Millisecond
	t.Cleanup(func() { presenceGrace = oldGrace })

	mu.Lock()
	previousPresence, previousGrace := presenceOnly, graceUntil
	presenceOnly = make(map[string]struct {
		id     int64
		expire time.Time
	})
	graceUntil = make(map[string]time.Time)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		presenceOnly, graceUntil = previousPresence, previousGrace
		mu.Unlock()
	})

	const uuid = "node-post-presence"
	KeepAlivePresence(uuid, 1, 100*time.Millisecond)

	// presence 已过期但仍在展示宽限内：继续算在线。
	time.Sleep(200 * time.Millisecond)
	if !uuidOnline(uuid) {
		t.Fatal("expired POST presence inside the grace window should stay online")
	}

	time.Sleep(500 * time.Millisecond)
	if uuidOnline(uuid) {
		t.Fatal("expired POST presence should flip offline after the grace window")
	}
}

func TestProbeAliveTrueForPongingPeer(t *testing.T) {
	server, client := newWSConnPair(t)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		for {
			if _, _, err := client.ReadMessage(); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = client.Close() })

	// 与生产一致：已注册连接的读循环永远在跑（WebSocketV2RPC 的 for 循环），
	// pong 帧由 gorilla 在 ReadMessage 内部经 pong handler 消费并刷新 lastActive。
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			if _, _, err := server.ReadMessage(); err != nil {
				return
			}
		}
	}()

	bornAt := server.LastActive()
	if !ProbeAlive(server, 3*time.Second) {
		t.Fatal("ProbeAlive returned false against a ponging peer")
	}
	if !server.LastActive().After(bornAt) {
		t.Fatal("pong did not refresh LastActive")
	}
	select {
	case <-clientDone:
	case <-time.After(5 * time.Second):
	}
	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
	}
}

func TestProbeAliveFalseForSilentPeer(t *testing.T) {
	server, client := newWSConnPair(t)
	// 吞掉 ping、不回 pong：模拟半开死连接（TCP 可写但应用层早已失联）。
	client.SetPingHandler(func(string) error { return nil })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := client.ReadMessage(); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = client.Close() })

	bornAt := server.LastActive()
	if ProbeAlive(server, 400*time.Millisecond) {
		t.Fatal("ProbeAlive returned true against a peer that swallows pings")
	}
	if server.LastActive() != bornAt {
		t.Fatal("LastActive advanced without a pong")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func TestProbeAliveFalseForDeadTCP(t *testing.T) {
	server, client := newWSConnPair(t)
	// 不进入读循环直接断开：对端 TCP 消失，WriteControl 应当失败。
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if ProbeAlive(server, 400*time.Millisecond) {
		t.Fatal("ProbeAlive returned true against a closed TCP peer")
	}
}

func uuidOnline(uuid string) bool {
	for _, online := range GetAllOnlineUUIDs() {
		if online == uuid {
			return true
		}
	}
	return false
}

func TestRecordReportKeepsLatestAndShortRecentWindow(t *testing.T) {
	mu.Lock()
	previousLatest := latestReport
	previousRecent := recentReports
	latestReport = make(map[string]*v2.Report)
	recentReports = make(map[string][]v2.Report)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		latestReport = previousLatest
		recentReports = previousRecent
		mu.Unlock()
	})

	now := time.Now().UTC()
	RecordReport(v2.Report{UUID: "node-a", UpdatedAt: now.Add(-2 * time.Minute), CPU: v2.CPUReport{Usage: 10}})
	RecordReport(v2.Report{UUID: "node-a", UpdatedAt: now.Add(-30 * time.Second), CPU: v2.CPUReport{Usage: 20}})
	RecordReport(v2.Report{UUID: "node-a", UpdatedAt: now.Add(-45 * time.Second), CPU: v2.CPUReport{Usage: 15}})

	recent := GetRecentReports("node-a")
	if len(recent) != 2 || recent[0].CPU.Usage != 15 || recent[1].CPU.Usage != 20 {
		t.Fatalf("recent reports = %#v", recent)
	}
	recent[0].CPU.Usage = 99
	if got := GetRecentReports("node-a"); len(got) != 2 || got[0].CPU.Usage != 15 {
		t.Fatalf("recent report cache was mutated through returned slice: %#v", got)
	}

	latest := GetLatestReport()
	if latest["node-a"] == nil || latest["node-a"].CPU.Usage != 20 {
		t.Fatalf("latest report = %#v", latest["node-a"])
	}
	latest["node-a"].CPU.Usage = 99
	if got := GetLatestReport()["node-a"]; got == nil || got.CPU.Usage != 20 {
		t.Fatalf("latest report cache was mutated through returned map: %#v", got)
	}

	DeleteLatestReport("node-a")
	if len(GetRecentReports("node-a")) != 0 || GetLatestReport()["node-a"] != nil {
		t.Fatal("deleting latest report did not clear runtime report state")
	}
}
