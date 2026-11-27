package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var testTargets = []struct {
	target string
}{
	{"v6-sh-cm.oojj.de"},
	{"2409:8c1e:8f80:2:6a::"},
	{"[2409:8c1e:8f80:2:6a::]"},
	{"[2409:8c1e:8f80:2:6a::]:80"},
	{"v4-sh-cm.oojj.de"},
	{"117.185.125.154"},
	{"117.185.125.154:80"},
}

func TestUsePrivilegedICMP(t *testing.T) {
	tests := []struct {
		name string
		goos string
		raw  bool
		want bool
	}{
		{name: "linux defaults to UDP", goos: "linux", want: false},
		{name: "linux raw opt-in", goos: "linux", raw: true, want: true},
		{name: "windows remains privileged", goos: "windows", want: true},
		{name: "darwin uses system ping", goos: "darwin", raw: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usePrivilegedICMP(tt.goos, tt.raw); got != tt.want {
				t.Fatalf("usePrivilegedICMP(%q, %t) = %t, want %t", tt.goos, tt.raw, got, tt.want)
			}
		})
	}
}

func TestICMPPing(t *testing.T) {
	timeout := 3 * time.Second
	for _, tt := range testTargets {
		t.Run(tt.target, func(t *testing.T) {
			latency, err := icmpPing(tt.target, timeout)
			if latency < -1 {
				t.Errorf("ICMP ping %s: invalid latency %d", tt.target, latency)
			}
			if err != nil {
				t.Errorf("ICMP ping %s error: %v", tt.target, err)
			}
		})
	}
}

func TestTCPPing(t *testing.T) {
	timeout := 3 * time.Second
	for _, tt := range testTargets {
		t.Run(tt.target, func(t *testing.T) {
			latency, err := tcpPing(tt.target, timeout)
			if latency < -1 {
				t.Errorf("TCP ping %s: invalid latency %d", tt.target, latency)
			}
			if err != nil {
				t.Errorf("TCP ping %s error: %v", tt.target, err)
			}
		})
	}
}

func TestHTTPPing(t *testing.T) {
	// First verify local server ping
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	localLatency, err := httpPing(ts.URL, 2*time.Second)
	if err != nil || localLatency < 0 {
		t.Fatalf("local httpPing failed: latency=%d, err=%v", localLatency, err)
	}

	timeout := 2 * time.Second
	for _, tt := range testTargets {
		t.Run(tt.target, func(t *testing.T) {
			latency, err := httpPing(tt.target, timeout)
			if latency < -1 {
				t.Errorf("HTTP ping %s: invalid latency %d", tt.target, latency)
			}
			if err != nil {
				t.Logf("External HTTP ping %s unreachable (network dependency): %v", tt.target, err)
			}
		})
	}
}

func TestUploadTaskResultRetriesOnTransientFailure(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att < 2 {
			http.Error(w, "temporary error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"status":"success"}}`))
	}))
	defer server.Close()

	oldEndpoint := flags.Endpoint
	oldToken := flags.Token
	flags.Endpoint = server.URL
	flags.Token = "test-token"
	defer func() {
		flags.Endpoint = oldEndpoint
		flags.Token = oldToken
	}()

	uploadTaskResult("task-123", "success", 0, time.Now())

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 attempts due to retry, got %d", got)
	}
}
