// Package reconnect holds the WebSocket reconnection policy: exponential
// backoff after abnormal disconnects, with jitter so a fleet does not
// reconnect in lockstep.
package reconnect

import (
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// A connection that stayed up at least this long counts as stable: the
	// next disconnect restarts the backoff sequence from the base instead of
	// escalating further. Shorter-lived connections are treated as part of a
	// connect/disconnect storm.
	stableConnectionDuration = 60 * time.Second

	// MaxBackoff is the upper bound for the exponential reconnect backoff.
	// Fast retries cannot fix a hub that is refusing us, and they turn a
	// hub-side rejection storm into a full outage (see the 2026-11-02
	// duplicate-token incident). Exported so the connection manager can warn
	// at startup when a configured base interval exceeds the cap.
	MaxBackoff = 5 * time.Minute

	// Uniform jitter fraction applied to every computed backoff so a fleet of
	// agents that lose the hub at the same moment does not reconnect in lockstep.
	jitterFraction = 0.2

	// duplicateRetryWait replaces the former "pin at MaxBackoff" behavior. A
	// duplicate rejection has two causes with very different recovery times:
	// (a) the hub still holds a half-open zombie connection — its takeover
	// path clears the registration once that connection has been silent for
	// zombieConnThreshold (40s), so a retry at 60s±20% jitter (48–72s) always
	// lands past the threshold and succeeds on the first retry; (b) a
	// genuinely duplicated token — retrying cannot succeed, but a cheap
	// rejection about once a minute is harmless (the hub throttles its
	// rejection log to one line per minute). Waiting the full 5 minutes would
	// turn every zombie recovery into a ~5-minute outage, which production
	// logs showed is the common case: the takeover path fired within minutes
	// of the 2026-11-04 deploy. This revises the original "duplicate → pin at
	// max" criterion, which predates the hub-side zombie takeover.
	duplicateRetryWait = 60 * time.Second

	// Close code the hub sends when it rejects a second connection carrying a
	// client token that already has an active connection
	// (hub/web/api/client/report_v2.go, websocket.ClosePolicyViolation).
	closeCodeDuplicateConnection = websocket.ClosePolicyViolation

	// duplicateRejectionReasonPrefix is the reason the hub sends alongside
	// that code. Matching the reason too keeps the pin specific to the
	// duplicate-rejection contract should another policy-violation close
	// ever appear; a reason stripped by a middlebox degrades to the normal
	// escalation, which is safe.
	duplicateRejectionReasonPrefix = "duplicate connection rejected"
)

// backoffRandFloat is overridable in tests.
var backoffRandFloat = rand.Float64

// Disconnect describes why a WebSocket connection ended. CloseCode is set
// only when the peer sent a close frame; otherwise Detail carries the raw
// error text (TCP reset, EOF, write timeout, ...).
type Disconnect struct {
	CloseCode int
	CloseText string
	Detail    string
}

func (d Disconnect) String() string {
	if d.CloseCode != 0 {
		if d.CloseText == "" {
			return fmt.Sprintf("close code %d", d.CloseCode)
		}
		return fmt.Sprintf("close code %d, reason %q", d.CloseCode, d.CloseText)
	}
	if d.Detail != "" {
		return d.Detail
	}
	return "unknown cause"
}

// Describe extracts the close code and reason from a WebSocket read error so
// the reconnect log shows why the hub ended the connection instead of only
// the error string.
func Describe(err error) Disconnect {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return Disconnect{CloseCode: closeErr.Code, CloseText: closeErr.Text}
	}
	return Disconnect{Detail: err.Error()}
}

// backoff returns the wait before the next reconnect attempt after the given
// number of consecutive abnormal disconnects: base, 2*base, 4*base, ...
// capped at MaxBackoff. The first failure waits one base interval.
func backoff(failures int, base time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	wait := base
	for i := 1; i < failures && wait < MaxBackoff; i++ {
		wait *= 2
	}
	if wait > MaxBackoff {
		wait = MaxBackoff
	}
	return wait
}

// applyJitter widens d by up to ±jitterFraction.
func applyJitter(d time.Duration) time.Duration {
	jitter := (backoffRandFloat()*2 - 1) * jitterFraction
	jittered := d + time.Duration(float64(d)*jitter)
	if jittered < 0 {
		jittered = 0
	}
	return jittered
}

// State tracks consecutive abnormal disconnects across one connection
// manager lifetime and computes the next reconnect delay.
type State struct {
	consecutiveFailures int
	duplicateRejected   bool
	lastConnectedAt     time.Time
}

// MarkConnected records that a connection was established at the given time.
func (s *State) MarkConnected(at time.Time) {
	s.lastConnectedAt = at
}

// MarkDisconnected records one abnormal disconnect and returns how long to
// wait before the next reconnect attempt. A connection that was stable long
// enough resets the escalation; a duplicate-connection rejection switches to
// the fixed duplicateRetryWait cadence (see its doc: zombie recovery lands on
// the first retry, a truly duplicated token just gets cheap rejections).
func (s *State) MarkDisconnected(now time.Time, d Disconnect, base time.Duration) time.Duration {
	if !s.lastConnectedAt.IsZero() && now.Sub(s.lastConnectedAt) >= stableConnectionDuration {
		s.consecutiveFailures = 0
		s.duplicateRejected = false
	}
	s.consecutiveFailures++

	wait := backoff(s.consecutiveFailures, base)
	if d.CloseCode == closeCodeDuplicateConnection && strings.HasPrefix(d.CloseText, duplicateRejectionReasonPrefix) {
		if !s.duplicateRejected {
			log.Println("Hub rejected this connection as a duplicate (close code 1008): another connection is using this client token, or the hub is still holding a half-open connection that its zombie takeover (40s) will clear. Retrying about once a minute. If this keeps happening, check whether two nodes are misconfigured with the same token.")
		}
		s.duplicateRejected = true
		wait = duplicateRetryWait
	}

	delay := applyJitter(wait)
	log.Printf("WebSocket disconnected (%s); next reconnect in %s (consecutive failures: %d)", d, delay, s.consecutiveFailures)
	return delay
}
