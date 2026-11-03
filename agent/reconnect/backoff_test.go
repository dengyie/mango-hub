package reconnect

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fixedJitter pins the jitter draw so MarkDisconnected returns exact values.
func fixedJitter(t *testing.T, v float64) {
	t.Helper()
	old := backoffRandFloat
	backoffRandFloat = func() float64 { return v }
	t.Cleanup(func() { backoffRandFloat = old })
}

func TestBackoffSequence(t *testing.T) {
	base := 5 * time.Second
	want := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		80 * time.Second,
		160 * time.Second,
		5 * time.Minute, // 320s capped
		5 * time.Minute,
	}
	for i, exp := range want {
		if got := backoff(i+1, base); got != exp {
			t.Fatalf("backoff(%d failures) = %s, want %s", i+1, got, exp)
		}
	}
}

func TestBackoffClampsInvalidBase(t *testing.T) {
	if got := backoff(1, 0); got != time.Second {
		t.Fatalf("backoff with zero base = %s, want 1s", got)
	}
}

func TestApplyJitterBounds(t *testing.T) {
	d := 10 * time.Second
	cases := []struct {
		draw float64
		want time.Duration
	}{
		{0.0, 8 * time.Second},  // -20%
		{0.5, 10 * time.Second}, // no jitter
		{1.0, 12 * time.Second}, // +20%
	}
	for _, c := range cases {
		backoffRandFloat = func() float64 { return c.draw }
		if got := applyJitter(d); got != c.want {
			t.Fatalf("applyJitter(draw=%v) = %s, want %s", c.draw, got, c.want)
		}
	}
}

func TestDescribe(t *testing.T) {
	d := Describe(&websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: "duplicate connection rejected: client x"})
	if d.CloseCode != websocket.ClosePolicyViolation || d.CloseText != "duplicate connection rejected: client x" {
		t.Fatalf("close-frame error not decoded: %+v", d)
	}

	d = Describe(errors.New("read: connection reset by peer"))
	if d.CloseCode != 0 || d.Detail != "read: connection reset by peer" {
		t.Fatalf("non-close error not kept as detail: %+v", d)
	}
}

func TestStateEscalatesAndResets(t *testing.T) {
	fixedJitter(t, 0.5)
	base := 5 * time.Second
	s := &State{}
	d := Disconnect{CloseCode: websocket.CloseAbnormalClosure}

	connectedAt := time.Now()
	s.MarkConnected(connectedAt)

	// A connection killed before the stability window keeps escalating.
	now := connectedAt.Add(10 * time.Second)
	if got := s.MarkDisconnected(now, d, base); got != 5*time.Second {
		t.Fatalf("first disconnect wait = %s, want 5s", got)
	}
	now = now.Add(10 * time.Second)
	if got := s.MarkDisconnected(now, d, base); got != 10*time.Second {
		t.Fatalf("second disconnect wait = %s, want 10s", got)
	}

	// A connection that stayed up past the stability window restarts at base.
	s.MarkConnected(now)
	now = now.Add(stableConnectionDuration + time.Second)
	if got := s.MarkDisconnected(now, d, base); got != 5*time.Second {
		t.Fatalf("disconnect after stable connection wait = %s, want 5s", got)
	}
}

func TestStateDuplicateRejectionUsesFixedRetryWait(t *testing.T) {
	fixedJitter(t, 0.5)
	base := 5 * time.Second
	s := &State{}
	s.MarkConnected(time.Now())

	duplicate := Disconnect{CloseCode: websocket.ClosePolicyViolation, CloseText: "duplicate connection rejected: client x"}
	now := time.Now().Add(10 * time.Second)
	if got := s.MarkDisconnected(now, duplicate, base); got != duplicateRetryWait {
		t.Fatalf("duplicate rejection wait = %s, want %s", got, duplicateRetryWait)
	}

	// Repeated duplicate rejections keep the fixed retry cadence.
	now = now.Add(10 * time.Second)
	if got := s.MarkDisconnected(now, duplicate, base); got != duplicateRetryWait {
		t.Fatalf("repeated duplicate rejection wait = %s, want %s", got, duplicateRetryWait)
	}

	// A non-duplicate death during the episode escalates normally instead of
	// inheriting a max wait: after a zombie takeover the fresh connection can
	// die abnormally within the stability window, and that is a normal death
	// (failures=3 here → 4×base).
	other := Disconnect{Detail: "read: connection reset by peer"}
	now = now.Add(10 * time.Second)
	if got := s.MarkDisconnected(now, other, base); got != 20*time.Second {
		t.Fatalf("non-duplicate death during duplicate episode wait = %s, want 20s (normal escalation)", got)
	}

	// Once a connection is stable again, the episode clears.
	s.MarkConnected(now)
	now = now.Add(stableConnectionDuration + time.Second)
	if got := s.MarkDisconnected(now, duplicate, base); got != duplicateRetryWait {
		t.Fatalf("duplicate rejection after recovery wait = %s, want %s", got, duplicateRetryWait)
	}
}

// A 1008 without the hub's duplicate-rejection reason is not the agreed
// contract (middleboxes can strip reasons; other policy violations exist),
// so it degrades to the normal escalation instead of pinning.
func TestStatePolicyViolationWithoutDuplicateReasonDoesNotPin(t *testing.T) {
	fixedJitter(t, 0.5)
	base := 5 * time.Second
	cases := []Disconnect{
		{CloseCode: websocket.ClosePolicyViolation},
		{CloseCode: websocket.ClosePolicyViolation, CloseText: "some other policy"},
	}
	for _, d := range cases {
		s := &State{}
		s.MarkConnected(time.Now())
		now := time.Now().Add(10 * time.Second)
		if got := s.MarkDisconnected(now, d, base); got != base {
			t.Fatalf("1008 without duplicate reason (%q) wait = %s, want base %s", d.CloseText, got, base)
		}
	}
}

func TestStateSingleDisconnectAfterStable(t *testing.T) {
	fixedJitter(t, 0.5)
	base := 5 * time.Second
	s := &State{}

	// An agent that ran for hours and lost the network once must reconnect
	// within one base interval, not escalate.
	connectedAt := time.Now().Add(-6 * time.Hour)
	s.MarkConnected(connectedAt)
	d := Disconnect{CloseCode: websocket.CloseGoingAway}
	if got := s.MarkDisconnected(connectedAt.Add(time.Minute), d, base); got != base {
		t.Fatalf("single disconnect after stable connection wait = %s, want %s", got, base)
	}
}

// TestHubDuplicateReasonContract guards the cross-binary duplicate-rejection
// contract: the agent pins the backoff on close code 1008 plus the hub's
// "duplicate connection rejected" reason prefix. Both halves ship in separate
// binaries but live in the same repository, so if the hub wording changes,
// this test fails here instead of the pin silently degrading to plain
// escalation. The hub-side half of the guard is
// TestWebSocketV2RPCRejectsDuplicateKeepsOldConnection in
// hub/web/api/client/report_v2_test.go, which asserts the same prefix on a
// real close frame. Skipped when the agent module is consumed outside the
// monorepo checkout.
func TestHubDuplicateReasonContract(t *testing.T) {
	hubDir := filepath.Join("..", "..", "hub")
	if fi, err := os.Stat(hubDir); err != nil || !fi.IsDir() {
		t.Skipf("hub source tree not found next to the agent module (%s); skipping contract guard", hubDir)
	}
	reportV2, err := os.ReadFile(filepath.Join(hubDir, "web", "api", "client", "report_v2.go"))
	if err != nil {
		t.Fatalf("hub source layout changed; move this contract guard with it: %v", err)
	}
	src := string(reportV2)
	if !strings.Contains(src, duplicateRejectionReasonPrefix) {
		t.Fatalf("hub report_v2.go no longer contains the reason prefix %q the agent pins on; update duplicateRejectionReasonPrefix and the hub reason together", duplicateRejectionReasonPrefix)
	}
	if !strings.Contains(src, "ClosePolicyViolation") {
		t.Fatalf("hub report_v2.go no longer sends close code ClosePolicyViolation (1008) for duplicate rejections; update closeCodeDuplicateConnection and the hub together")
	}
}
