package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	pkg_flags "github.com/dengyie/mangohub/agent/cmd/flags"
	"github.com/gorilla/websocket"
)

// TestEstablishWebSocketConnectionReconnectWiring drives the real connection
// manager loop against a local WebSocket server and verifies the wiring that
// the reconnect.State unit tests cannot see: the connect gate honors the
// computed backoff, consecutive abnormal disconnects escalate, and the hub's
// duplicate-rejection close (1008 + reason) pins the backoff at the maximum.
//
// The loop has no shutdown hook, so the goroutine leaks past the test on
// purpose: after the third accept it is parked in the 5-minute duplicate pin,
// and the server is deliberately left listening so it stays quiet.
func TestEstablishWebSocketConnectionReconnectWiring(t *testing.T) {
	var (
		mu         sync.Mutex
		acceptTime []time.Time
	)
	recordAccept := func() {
		mu.Lock()
		acceptTime = append(acceptTime, time.Now())
		mu.Unlock()
	}
	acceptCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(acceptTime)
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		recordAccept()
		if acceptCount() <= 2 {
			// Plain TCP close without a close frame: an abnormal disconnect
			// that must escalate the backoff.
			_ = conn.Close()
			return
		}
		// The hub's duplicate-rejection contract (hub report_v2.go).
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation,
				"duplicate connection rejected: client wiring-test already has an active connection"),
			time.Now().Add(time.Second))
		_ = conn.Close()
	}))
	// No Close: the leaked connection manager would dial a dead server after
	// its timers fire and spam the log for the rest of the package run.

	previous := *pkg_flags.GlobalConfig
	t.Cleanup(func() { *pkg_flags.GlobalConfig = previous })
	pkg_flags.GlobalConfig.Endpoint = server.URL
	pkg_flags.GlobalConfig.Token = "wiring-test"
	pkg_flags.GlobalConfig.Interval = 1
	pkg_flags.GlobalConfig.ReconnectInterval = 1 // base 1s so the escalation fits the test budget
	pkg_flags.GlobalConfig.MaxRetries = 3
	pkg_flags.GlobalConfig.DisableCompression = true
	// Keep a direct dial even if a proxy is configured in the environment.
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")

	go EstablishWebSocketConnection()

	waitForAccepts(t, &mu, &acceptTime, 3, 25*time.Second)

	mu.Lock()
	gap1 := acceptTime[1].Sub(acceptTime[0])
	gap2 := acceptTime[2].Sub(acceptTime[1])
	mu.Unlock()

	// The outer loop ticks every second and the base is 1s ±20% jitter, so
	// the first reconnect lands within a few seconds; the second must wait
	// for the escalated 2s backoff. Bounds are generous to stay stable on a
	// loaded CI runner.
	if gap1 > 6*time.Second {
		t.Fatalf("first reconnect took %s, want <= 6s", gap1)
	}
	if gap2 < 1400*time.Millisecond {
		t.Fatalf("second reconnect took %s, backoff did not escalate (want >= 1.4s)", gap2)
	}

	// The third connection was closed with the duplicate-rejection frame:
	// the backoff must be pinned at the maximum, so nothing reconnects.
	time.Sleep(6 * time.Second)
	if n := acceptCount(); n != 3 {
		t.Fatalf("got %d accepts after the duplicate rejection, want exactly 3 (backoff not pinned)", n)
	}
}

func waitForAccepts(t *testing.T, mu *sync.Mutex, acceptTime *[]time.Time, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*acceptTime)
		mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server accepted only %d connections in %s, want %d", func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(*acceptTime)
	}(), timeout, want)
}
