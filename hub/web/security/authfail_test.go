package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newAuthFailTestRouter(st *authFailState) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(st.middleware())
	r.GET("/rpc", func(c *gin.Context) {
		c.Status(http.StatusUnauthorized)
	})
	r.GET("/ok", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

// serve sends a request with explicit socket address and optional
// CF-Connecting-IP, as the middleware sees them. It always presents a client
// token: the limiter only counts credential-carrying requests.
func serve(r *gin.Engine, remoteAddr, cfIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/rpc?token=test-token", nil)
	req.RemoteAddr = remoteAddr
	if cfIP != "" {
		req.Header.Set("CF-Connecting-IP", cfIP)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// serveAnon sends a request with no credential at all.
func serveAnon(r *gin.Engine, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthFailLimiterBlocksAfterLimit(t *testing.T) {
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit+1; i++ {
		w := serve(r, "203.0.113.7:5555", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("req %d: want 401 while counting, got %d", i, w.Code)
		}
	}
	// limit+1 failures trip the block; the next attempt gets 429.
	w := serve(r, "203.0.113.7:5555", "")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 after limit, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 response missing Retry-After header")
	}
}

func TestAuthFailLimiterPerIP(t *testing.T) {
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit+5; i++ {
		serve(r, "203.0.113.7:5555", "")
	}
	w := serve(r, "198.51.100.9:6666", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("other IP should still get 401, got %d", w.Code)
	}
}

func TestAuthFailLimiterSuccessNeverCounts(t *testing.T) {
	st := newAuthFailStateForTest()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(st.middleware())
	r.GET("/rpc", func(c *gin.Context) { c.Status(http.StatusOK) })
	for i := 0; i < authFailLimit+10; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/rpc", nil)
		req.RemoteAddr = "203.0.113.7:5555"
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("200s must never be blocked, got %d", w.Code)
		}
	}
}

func TestAuthFailLimiterWindowExpires(t *testing.T) {
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	// limit failures (not limit+1): no block yet, entries still tracked.
	for i := 0; i < authFailLimit; i++ {
		serve(r, "203.0.113.7:5555", "")
	}
	// Simulate the window passing by rewriting timestamps directly.
	st.mu.Lock()
	st.hits["203.0.113.7"] = []time.Time{time.Now().Add(-2 * authFailWindow)}
	st.mu.Unlock()
	w := serve(r, "203.0.113.7:5555", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("after window expiry want 401 again, got %d", w.Code)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.hits["203.0.113.7"]) != 1 {
		t.Fatalf("stale entries should be dropped, got %v", st.hits)
	}
}

func TestAuthFailClientIPViaTrustedHop(t *testing.T) {
	// Tunnel hop is loopback: CF-Connecting-IP is the tracked key.
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit; i++ {
		serve(r, "127.0.0.1:9999", "2a04:4e41:c:49eb::ea92:49eb")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.hits["2a04:4e41:c:49eb::ea92:49eb"]; !ok {
		t.Fatalf("CF-Connecting-IP not used as key, hits=%v", st.hits)
	}
	if _, ok := st.hits["127.0.0.1"]; ok {
		t.Fatalf("tunnel hop itself must not be the key, hits=%v", st.hits)
	}
}

func TestAuthFailClientIPDirectIgnoresForgedHeader(t *testing.T) {
	// Direct public connection: socket address wins even with a forged
	// CF-Connecting-IP.
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit; i++ {
		serve(r, "203.0.113.7:5555", "1.2.3.4")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.hits["1.2.3.4"]; ok {
		t.Fatal("forged CF-Connecting-IP must not be trusted on direct connections")
	}
	if _, ok := st.hits["203.0.113.7"]; !ok {
		t.Fatalf("socket address must be the key, hits=%v", st.hits)
	}
}

func TestAuthFailBlockExpiry(t *testing.T) {
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit+1; i++ {
		serve(r, "203.0.113.7:5555", "")
	}
	st.mu.Lock()
	st.blockedUntil["203.0.113.7"] = time.Now().Add(-time.Second)
	st.mu.Unlock()
	w := serve(r, "203.0.113.7:5555", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("after block expiry want 401 (counting again), got %d", w.Code)
	}
}

func TestAuthFailTrustedHopWithoutCFHeader(t *testing.T) {
	// Private hop with no CF headers (internal callers) falls back to the hop
	// address instead of panicking or treating headers as authoritative.
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	w := serve(r, "10.0.0.5:1234", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestAuthFailClientIPUntrustedPrivateHopCannotSpoofHeader(t *testing.T) {
	SetTrustedProxies(nil, false) // Default: private IPs are NOT trusted proxies
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit; i++ {
		serve(r, "10.0.0.5:1234", "1.2.3.4")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.hits["1.2.3.4"]; ok {
		t.Fatal("untrusted private hop must not be allowed to spoof client IP")
	}
	if _, ok := st.hits["10.0.0.5"]; !ok {
		t.Fatalf("expected remote socket IP 10.0.0.5 to be tracked, hits=%v", st.hits)
	}
}

func TestAuthFailClientIPConfiguredTrustedProxy(t *testing.T) {
	SetTrustedProxies([]string{"10.0.0.0/8"}, false)
	defer SetTrustedProxies(nil, false)
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit; i++ {
		serve(r, "10.0.0.5:1234", "198.51.100.25")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.hits["198.51.100.25"]; !ok {
		t.Fatalf("configured trusted proxy should allow CF-Connecting-IP, hits=%v", st.hits)
	}
}

func TestAuthFailClientIPInvalidHeaderIgnored(t *testing.T) {
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit; i++ {
		serve(r, "127.0.0.1:1234", "not-a-valid-ip")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.hits["not-a-valid-ip"]; ok {
		t.Fatal("invalid IP in header must be rejected")
	}
	if _, ok := st.hits["127.0.0.1"]; !ok {
		t.Fatalf("expected loopback socket IP to be tracked, hits=%v", st.hits)
	}
}

func TestAuthFailLimiterAnonymousNeverCounted(t *testing.T) {
	// Health probes and anonymous callers must not be able to trip the limiter.
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit*5; i++ {
		if w := serveAnon(r, "203.0.113.7:5555"); w.Code != http.StatusUnauthorized {
			t.Fatalf("anon req %d: want 401, got %d", i, w.Code)
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.hits) != 0 {
		t.Fatalf("anonymous 401s must not be counted, hits=%v", st.hits)
	}
}

func TestAuthFailLimiterBlockedIPStillServesAnonymous(t *testing.T) {
	// Once an address is blocked for bad credentials, unauthenticated reads
	// (public pages, /api/version, health) must keep working.
	st := newAuthFailStateForTest()
	r := newAuthFailTestRouter(st)
	for i := 0; i < authFailLimit+1; i++ {
		serve(r, "203.0.113.7:5555", "")
	}
	if w := serve(r, "203.0.113.7:5555", ""); w.Code != http.StatusTooManyRequests {
		t.Fatalf("setup: want 429 for credentialed request, got %d", w.Code)
	}
	if w := serveAnon(r, "203.0.113.7:5555"); w.Code == http.StatusTooManyRequests {
		t.Fatal("anonymous request must not inherit another client's cooldown")
	}
}

func TestRequestCarriesCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		set  func(*http.Request)
		want bool
	}{
		{"none", func(*http.Request) {}, false},
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer x") }, true},
		{"query token", func(r *http.Request) { r.URL.RawQuery = "token=x" }, true},
		{"query authorization", func(r *http.Request) { r.URL.RawQuery = "Authorization=x" }, true},
		{"session cookie", func(r *http.Request) { r.Header.Set("Cookie", "session_token=x") }, true},
		{"blank authorization", func(r *http.Request) { r.Header.Set("Authorization", "   ") }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := httptest.NewRequest("GET", "/rpc", nil)
			tc.set(req)
			c.Request = req
			if got := requestCarriesCredentials(c); got != tc.want {
				t.Fatalf("requestCarriesCredentials=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestWarnIfNoTrustedProxiesDoesNotPanic(t *testing.T) {
	defer SetTrustedProxies(nil, false)
	SetTrustedProxies(nil, false)
	WarnIfNoTrustedProxies()
	SetTrustedProxies([]string{"172.17.0.1/32"}, false)
	WarnIfNoTrustedProxies()
}

func newAuthFailStateForTest() *authFailState {
	return &authFailState{
		hits:         make(map[string][]time.Time),
		blockedUntil: make(map[string]time.Time),
	}
}
