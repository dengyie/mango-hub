package security

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	logger "github.com/dengyie/mangohub/hub/utils/log"
	"github.com/gin-gonic/gin"
)

// Auth-failure rate limiting defaults. Tuned well above any legitimate
// traffic: agents authenticate on the first attempt and browsers only hit
// 401 on expired sessions; the observed probe pattern is 15-30 401s/min
// from a single address.
const (
	authFailWindow     = time.Minute
	authFailLimit      = 20
	authFailBlockFor   = 15 * time.Minute
	authFailBucketSize = 4096 // max tracked IPs before stale pruning kicks in
)

type authFailState struct {
	mu           sync.Mutex
	hits         map[string][]time.Time
	blockedUntil map[string]time.Time
}

// AuthFailLimiter counts 401 responses per source address over a sliding
// window and, once the limit is exceeded, answers further requests from that
// address with 429 for a cooldown period. Successful authentication is never
// counted, so a misconfigured agent can fix its token and recover after the
// window without operator action.
func AuthFailLimiter() gin.HandlerFunc {
	st := &authFailState{
		hits:         make(map[string][]time.Time),
		blockedUntil: make(map[string]time.Time),
	}
	return st.middleware()
}

func (st *authFailState) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Anonymous requests cannot brute-force a credential, so they neither
		// count nor get blocked. That keeps public pages and health probes
		// reachable even while an address is in cooldown.
		if !requestCarriesCredentials(c) {
			c.Next()
			return
		}

		ip := authFailClientIP(c)
		now := time.Now()

		st.mu.Lock()
		if until, blocked := st.blockedUntil[ip]; blocked {
			if now.Before(until) {
				st.mu.Unlock()
				retry := int(time.Until(until).Seconds()) + 1
				c.Header("Retry-After", strconv.Itoa(retry))
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"status": "error",
					"error":  "Too many failed authentication attempts. Retry later.",
				})
				return
			}
			delete(st.blockedUntil, ip)
		}
		st.mu.Unlock()

		c.Next()

		// Only failures count; the response phase sees the final status even
		// for handlers that aborted.
		if c.Writer.Status() != http.StatusUnauthorized {
			return
		}

		st.mu.Lock()
		cutoff := now.Add(-authFailWindow)
		arr := append(st.hits[ip], now)
		i := 0
		for i < len(arr) && arr[i].Before(cutoff) {
			i++
		}
		arr = arr[i:]
		if len(arr) > authFailLimit {
			st.blockedUntil[ip] = now.Add(authFailBlockFor)
			delete(st.hits, ip)
			st.mu.Unlock()
			logger.Warnf("server", "[security] auth-fail rate limit engaged for %s: >%d 401s in %s, blocking for %s",
				ip, authFailLimit, authFailWindow, authFailBlockFor)
			// A private/loopback key means the bucket is a reverse-proxy hop
			// rather than a client: every client behind that hop shares it.
			if isPrivateOrLoopback(ip) {
				logger.Warnf("server", "[security] %s looks like a proxy hop instead of a client: "+
					"set MANGOHUB_TRUSTED_PROXIES (or MANGOHUB_TRUST_PRIVATE_PROXIES=1) "+
					"so buckets key on the real client address", ip)
			}
			return
		}
		st.hits[ip] = arr
		if len(st.hits) > authFailBucketSize {
			st.pruneLocked(now)
		}
		st.mu.Unlock()
	}
}

// requestCarriesCredentials reports whether the request presents any credential
// the limiter could protect. Anonymous traffic is exempt from both counting and
// blocking: it cannot brute-force a secret, and health/monitoring probes must
// keep working during someone else's cooldown.
func requestCarriesCredentials(c *gin.Context) bool {
	if strings.TrimSpace(c.GetHeader("Authorization")) != "" {
		return true
	}
	if v, err := c.Cookie("session_token"); err == nil && v != "" {
		return true
	}
	// rpc2 agents authenticate via ?token= or ?Authorization=.
	return c.Query("token") != "" || c.Query("Authorization") != ""
}

func isPrivateOrLoopback(ipStr string) bool {
	parsed := net.ParseIP(ipStr)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast()
}

// WarnIfNoTrustedProxies surfaces the deployment mistake that makes the limiter
// dangerous: behind a reverse proxy with no trusted hop configured, every
// request is bucketed under the socket peer (nginx loopback or the docker
// gateway), so one client's 401 burst blocks every other client.
func WarnIfNoTrustedProxies() {
	trustedCIDRsMu.RLock()
	configured, private := len(trustedCIDRs), trustPrivate
	trustedCIDRsMu.RUnlock()
	if configured == 0 && !private {
		logger.Warnf("server", "[security] no trusted proxies configured (MANGOHUB_TRUSTED_PROXIES / MANGOHUB_TRUST_PRIVATE_PROXIES unset): "+
			"auth-fail buckets fall back to the socket peer. Behind nginx or docker this collapses all clients into one bucket, "+
			"so a single 401 burst blocks the whole fleet and the admin API for %s.", authFailBlockFor)
	}
}

func (st *authFailState) pruneLocked(now time.Time) {
	cutoff := now.Add(-authFailWindow)
	for ip, arr := range st.hits {
		if len(arr) == 0 || arr[len(arr)-1].Before(cutoff) {
			delete(st.hits, ip)
		}
	}
	for ip, until := range st.blockedUntil {
		if now.After(until) {
			delete(st.blockedUntil, ip)
		}
	}
}

var (
	trustedCIDRsMu sync.RWMutex
	trustedCIDRs   []*net.IPNet
	trustPrivate   bool
)

func init() {
	ReloadTrustedProxies()
}

// ReloadTrustedProxies refreshes the trusted proxies configuration from environment variables.
func ReloadTrustedProxies() {
	trustedCIDRsMu.Lock()
	defer trustedCIDRsMu.Unlock()
	trustedCIDRs = nil
	trustPrivate = false

	if v := os.Getenv("MANGOHUB_TRUST_PRIVATE_PROXIES"); v == "1" || strings.EqualFold(v, "true") {
		trustPrivate = true
	}
	raw := os.Getenv("MANGOHUB_TRUSTED_PROXIES")
	if raw == "" {
		raw = os.Getenv("TRUSTED_PROXIES")
	}
	if raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, "/") {
				if parsed := net.ParseIP(part); parsed != nil {
					if parsed.To4() != nil {
						part += "/32"
					} else {
						part += "/128"
					}
				}
			}
			_, ipNet, err := net.ParseCIDR(part)
			if err == nil && ipNet != nil {
				trustedCIDRs = append(trustedCIDRs, ipNet)
			}
		}
	}
}

// SetTrustedProxies configures trusted reverse proxy CIDRs or whether all RFC 1918 private IPs are trusted.
func SetTrustedProxies(cidrs []string, allowAllPrivate bool) {
	trustedCIDRsMu.Lock()
	defer trustedCIDRsMu.Unlock()
	trustPrivate = allowAllPrivate
	trustedCIDRs = nil
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if !strings.Contains(cidr, "/") {
			if parsed := net.ParseIP(cidr); parsed != nil {
				if parsed.To4() != nil {
					cidr += "/32"
				} else {
					cidr += "/128"
				}
			}
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil && ipNet != nil {
			trustedCIDRs = append(trustedCIDRs, ipNet)
		}
	}
}

// authFailClientIP resolves the true source address. By default only loopback
// hops or explicitly configured trusted proxies are trusted to supply CF-Connecting-IP
// or X-Real-IP. For direct connections or untrusted hops, the socket remote address wins.
// The extracted header IP is also validated to ensure it is a legal IP address.
func authFailClientIP(c *gin.Context) string {
	remote := c.Request.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if isTrustedHop(remote) {
		if v := cleanAndValidateIP(c.GetHeader("CF-Connecting-IP")); v != "" {
			return v
		}
		if v := cleanAndValidateIP(c.GetHeader("X-Real-IP")); v != "" {
			return v
		}
	}
	return remote
}

func cleanAndValidateIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ",") {
		raw = strings.TrimSpace(strings.Split(raw, ",")[0])
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	parsed := net.ParseIP(raw)
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

func isTrustedHop(ipStr string) bool {
	parsed := net.ParseIP(ipStr)
	if parsed == nil {
		return false
	}
	if parsed.IsLoopback() {
		return true
	}

	trustedCIDRsMu.RLock()
	defer trustedCIDRsMu.RUnlock()

	if trustPrivate && (parsed.IsPrivate() || parsed.IsLinkLocalUnicast()) {
		return true
	}

	for _, cidr := range trustedCIDRs {
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}
