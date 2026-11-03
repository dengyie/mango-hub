package connection

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// recordingInterceptor captures calls and applies simple rewrites.
type recordingInterceptor struct {
	mu             sync.Mutex
	connects       []*ConnInfo
	lastInfo       *ConnInfo
	messages       []string
	sends          []string
	closes         int
	dropMessage    bool
	dropSend       bool
	rewriteMessage bool
}

func (r *recordingInterceptor) OnConnect(info *ConnInfo) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connects = append(r.connects, info)
	return false, ""
}

func (r *recordingInterceptor) OnMessage(info *ConnInfo, frameType int, data []byte) (int, []byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastInfo = info
	r.messages = append(r.messages, string(data))
	if r.dropMessage {
		return frameType, nil, true
	}
	if r.rewriteMessage {
		return frameType, []byte("rewritten:" + string(data)), false
	}
	return frameType, data, false
}

func (r *recordingInterceptor) OnSend(info *ConnInfo, frameType int, data []byte) (int, []byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastInfo = info
	r.sends = append(r.sends, string(data))
	if r.dropSend {
		return frameType, nil, true
	}
	return frameType, data, false
}

func (r *recordingInterceptor) OnClose(info *ConnInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
}

func (r *recordingInterceptor) calls() (info *ConnInfo, messages []string, sends []string, closes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastInfo, append([]string(nil), r.messages...), append([]string(nil), r.sends...), r.closes
}

// wsEchoServer upgrades every request and echoes each frame back.
func wsEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	var upgrader websocket.Upgrader
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, data); err != nil {
				return
			}
		}
	}))
}

func dialEcho(t *testing.T, server *httptest.Server) *SafeConn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewSafeConn(conn)
}

// TestSafeConnInterceptorWired 验证读写帧都经过拦截器，且连接信息与 ID 一致。
func TestSafeConnInterceptorWired(t *testing.T) {
	server := wsEchoServer(t)
	defer server.Close()
	sc := dialEcho(t, server)
	defer sc.Close()

	rec := &recordingInterceptor{}
	info := &ConnInfo{ID: sc.ID, Path: "/api/clients/v2/rpc", RemoteIP: "203.0.113.9"}
	sc.SetInterceptor(info, rec)

	if err := sc.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	mt, data, err := sc.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.TextMessage || string(data) != "ping" {
		t.Fatalf("echo = type %d data %q", mt, data)
	}
	info, messages, sends, _ := rec.calls()
	if len(messages) != 1 || len(sends) != 1 || messages[0] != "ping" || sends[0] != "ping" {
		t.Fatalf("interceptor calls = messages %v sends %v", messages, sends)
	}
	// 帧回调收到的连接信息与附加时一致（OnConnect 由升级层调用，这里不做断言）。
	if info == nil || info.ID != sc.ID || info.Path != "/api/clients/v2/rpc" {
		t.Fatalf("frame info = %+v", info)
	}
}

// TestSafeConnRewriteAndWriteJSON 验证帧改写（读侧）与 WriteJSON 走同一拦截通道。
func TestSafeConnRewriteAndWriteJSON(t *testing.T) {
	server := wsEchoServer(t)
	defer server.Close()
	sc := dialEcho(t, server)
	defer sc.Close()

	rec := &recordingInterceptor{rewriteMessage: true}
	sc.SetInterceptor(&ConnInfo{ID: sc.ID, Path: "/x"}, rec)

	if err := sc.WriteJSON(map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	mt, data, err := sc.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.TextMessage || !strings.HasPrefix(string(data), "rewritten:") {
		t.Fatalf("read = type %d data %q", mt, data)
	}
	// WriteJSON 编码后的 JSON 文本也经过 wsSend 钩子。
	_, sends, _, _ := rec.calls()
	if len(sends) != 1 || !strings.Contains(sends[0], "\"a\"") {
		t.Fatalf("sends = %v", sends)
	}
}

// TestSafeConnDropSkipsFrame 验证被丢弃的帧被透明跳过，读侧只看到后续帧。
func TestSafeConnDropSkipsFrame(t *testing.T) {
	server := wsEchoServer(t)
	defer server.Close()
	sc := dialEcho(t, server)
	defer sc.Close()

	rec := &recordingInterceptor{dropMessage: true}
	sc.SetInterceptor(&ConnInfo{ID: sc.ID, Path: "/x"}, rec)

	// 发三帧：全部被丢弃；读侧应在连续丢弃达到上限后收到错误，而不是空数据。
	if err := sc.WriteMessage(websocket.TextMessage, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := sc.WriteMessage(websocket.TextMessage, []byte("b")); err != nil {
		t.Fatal(err)
	}
	if err := sc.WriteMessage(websocket.TextMessage, []byte("c")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxConsecutiveDroppedFrames-3; i++ {
		if err := sc.WriteMessage(websocket.TextMessage, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := sc.ReadMessage(); !errors.Is(err, errTooManyDroppedFrames) {
		t.Fatalf("expected errTooManyDroppedFrames, got %v", err)
	}
}

// TestSafeConnDropSend 验证写侧丢弃：被丢弃的帧不会发到对端，后续帧不受影响。
func TestSafeConnDropSend(t *testing.T) {
	server := wsEchoServer(t)
	defer server.Close()
	sc := dialEcho(t, server)
	defer sc.Close()

	rec := &recordingInterceptor{dropSend: true}
	sc.SetInterceptor(&ConnInfo{ID: sc.ID, Path: "/x"}, rec)

	if err := sc.WriteMessage(websocket.TextMessage, []byte("hidden")); err != nil {
		t.Fatal(err)
	}
	// 关闭丢弃后发送的帧应该正常到达服务端并回显。
	rec.mu.Lock()
	rec.dropSend = false
	rec.mu.Unlock()
	if err := sc.WriteMessage(websocket.TextMessage, []byte("visible")); err != nil {
		t.Fatal(err)
	}
	mt, data, err := sc.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "visible" || mt != websocket.TextMessage {
		t.Fatalf("echo = %q (dropped frame must not reach the peer)", data)
	}
}

// TestSafeConnNoInterceptorZeroCost 验证未挂拦截器时读写路径原样工作。
func TestSafeConnNoInterceptorZeroCost(t *testing.T) {
	server := wsEchoServer(t)
	defer server.Close()
	sc := dialEcho(t, server)
	defer sc.Close()

	if err := sc.WriteMessage(websocket.TextMessage, []byte("plain")); err != nil {
		t.Fatal(err)
	}
	_, data, err := sc.ReadMessage()
	if err != nil || string(data) != "plain" {
		t.Fatalf("read = %q err %v", data, err)
	}
}

// TestSafeConnOnCloseOnce 验证 Close 只触发一次 OnClose。
func TestSafeConnOnCloseOnce(t *testing.T) {
	server := wsEchoServer(t)
	defer server.Close()
	sc := dialEcho(t, server)

	rec := &recordingInterceptor{}
	sc.SetInterceptor(&ConnInfo{ID: sc.ID, Path: "/x"}, rec)

	if err := sc.Close(); err != nil {
		t.Fatal(err)
	}
	// 第二次关闭连接已关闭：返回错误是预期行为，OnClose 只应触发一次。
	_ = sc.Close()
	_, _, _, closes := rec.calls()
	if closes != 1 {
		t.Fatalf("OnClose fired %d times", closes)
	}
}

// TestSafeConnLastActiveTracksControlFrames pins the liveness contract behind
// the v2 duplicate-connection check: control frames — an inbound ping and the
// pong to an outbound ping — must refresh LastActive just like data frames,
// so a healthy agent whose report interval exceeds zombieConnThreshold (its
// only traffic being 30s heartbeat pings) is never treated as a zombie.
func TestSafeConnLastActiveTracksControlFrames(t *testing.T) {
	serverReady := make(chan *SafeConn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sc := NewSafeConn(ws)
		serverReady <- sc
		// Keep reading so inbound control frames reach the handlers.
		for {
			if _, _, err := sc.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	client, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var sc *SafeConn
	select {
	case sc = <-serverReady:
	case <-time.After(5 * time.Second):
		t.Fatalf("server never upgraded the connection")
	}

	// Phase 1: the client pings; the hub must record the frame and answer
	// with a pong (gorilla's default reply behavior preserved).
	bornAt := sc.LastActive()
	pongSeen := make(chan struct{}, 1)
	client.SetPongHandler(func(string) error {
		select {
		case pongSeen <- struct{}{}:
		default:
		}
		return nil
	})
	clientReaderDone := make(chan struct{})
	go func() {
		defer close(clientReaderDone)
		for {
			if _, _, err := client.ReadMessage(); err != nil {
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	if err := client.WriteControl(websocket.PingMessage, []byte("hb"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("client send ping: %v", err)
	}
	select {
	case <-pongSeen:
	case <-time.After(5 * time.Second):
		t.Fatalf("hub never answered the heartbeat ping")
	}
	if !sc.LastActive().After(bornAt.Add(30 * time.Millisecond)) {
		t.Fatalf("inbound ping did not refresh LastActive: born=%v now=%v", bornAt, sc.LastActive())
	}

	// Phase 2: the hub pings; the client's default handler pongs back and the
	// hub's pong handler must record it too.
	afterPing := sc.LastActive()
	time.Sleep(50 * time.Millisecond)
	if err := sc.GetConn().WriteControl(websocket.PingMessage, []byte("probe"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("server send ping: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for sc.LastActive().Before(afterPing.Add(30 * time.Millisecond)) {
		if time.Now().After(deadline) {
			t.Fatalf("inbound pong did not refresh LastActive: before=%v now=%v", afterPing, sc.LastActive())
		}
		time.Sleep(10 * time.Millisecond)
	}

	_ = sc.Close()
	<-clientReaderDone
}
