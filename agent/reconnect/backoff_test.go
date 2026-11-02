package reconnect

import (
	"errors"
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

func TestStateDuplicateRejectionPinsMax(t *testing.T) {
	fixedJitter(t, 0.5)
	base := 5 * time.Second
	s := &State{}
	s.MarkConnected(time.Now())

	duplicate := Disconnect{CloseCode: websocket.ClosePolicyViolation, CloseText: "duplicate connection rejected: client x"}
	now := time.Now().Add(10 * time.Second)
	if got := s.MarkDisconnected(now, duplicate, base); got != maxBackoff {
		t.Fatalf("duplicate rejection wait = %s, want %s", got, maxBackoff)
	}

	// Subsequent non-duplicate failures stay pinned at the maximum.
	other := Disconnect{Detail: "read: connection reset by peer"}
	now = now.Add(10 * time.Second)
	if got := s.MarkDisconnected(now, other, base); got != maxBackoff {
		t.Fatalf("wait after pinned duplicate state = %s, want %s", got, maxBackoff)
	}

	// Once a connection is stable again, the pin lifts.
	s.MarkConnected(now)
	now = now.Add(stableConnectionDuration + time.Second)
	if got := s.MarkDisconnected(now, other, base); got != base {
		t.Fatalf("wait after recovery = %s, want %s", got, base)
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
