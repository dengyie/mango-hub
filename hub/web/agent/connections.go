package agent

import (
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	v2 "github.com/dengyie/mangohub/hub/protocol/v2"
	"github.com/dengyie/mangohub/hub/web/connection"
)

var (
	connectedClients = make(map[string]*connection.SafeConn)
	v2Clients        = make(map[string]struct{})
	latestReport     = make(map[string]*v2.Report)
	recentReports    = make(map[string][]v2.Report)
	// presenceOnly stores online state for non-WebSocket agents.
	// value keeps connectionID and a soft expiration to avoid flicker
	presenceOnly = make(map[string]struct {
		id     int64
		expire time.Time
	})
	// graceUntil stores the display-grace deadline stamped when a registered
	// WebSocket connection ends: the uuid keeps counting as online until the
	// deadline passes, so a drop + fast reconnect never flickers the UI.
	graceUntil = make(map[string]time.Time)
	// registrationLocks serializes the duplicate-connection decision per
	// uuid: two concurrent challengers must not both probe and take over.
	registrationLocks sync.Map
	mu                = sync.RWMutex{}
)

const recentReportRetention = time.Minute

// presenceGrace is the offline hysteresis window for the *displayed* online
// set (GetAllOnlineUUIDs, i.e. the homepage and every public status API).
// Losing the WebSocket does not mean losing the node: agents notice a dead
// path within ~10s (write deadline), dial again right away and are serving
// within seconds. Marking the pill offline instantly turned every one of
// those blips into a visible 假失联. Both sources of displayed presence
// therefore get the same soft landing: a WebSocket slot keeps counting until
// presenceGrace after its removal, and an expired POST presence keeps
// counting until presenceGrace past its expiry. Real outages still flip
// offline after one grace window. Notifications are unaffected — they run on
// their own connection-lifecycle state machine with per-client grace periods.
// 仅为测试可调。
var presenceGrace = 30 * time.Second

func GetConnectedClients() map[string]*connection.SafeConn {
	mu.RLock()
	defer mu.RUnlock()
	clientsCopy := make(map[string]*connection.SafeConn)
	for k, v := range connectedClients {
		clientsCopy[k] = v
	}
	return clientsCopy
}

func GetConnectedClient(uuid string) *connection.SafeConn {
	mu.RLock()
	defer mu.RUnlock()
	return connectedClients[uuid]
}

func SetConnectedClients(uuid string, conn *connection.SafeConn) {
	mu.Lock()
	defer mu.Unlock()
	connectedClients[uuid] = conn
	// 真实注册到活连接意味着节点确实回来了，宽限期立即作废。
	delete(graceUntil, uuid)
}

func MarkV2Client(uuid string) {
	mu.Lock()
	defer mu.Unlock()
	v2Clients[uuid] = struct{}{}
}

func IsV2Client(uuid string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := v2Clients[uuid]
	return ok
}

func DeleteClientConditionally(uuid string, connToRemove *connection.SafeConn) {
	mu.Lock()
	defer mu.Unlock()

	// 检查当前 map 里的 conn 是否就是要删除的这一个
	if currentConn, exists := connectedClients[uuid]; exists && currentConn == connToRemove {
		delete(connectedClients, uuid)
		delete(v2Clients, uuid)
		// 连接正常退出时盖上展示宽限期：agent 数秒内重连回来期间 UI 不闪离线。
		graceUntil[uuid] = time.Now().Add(presenceGrace)
	}
}
func DeleteConnectedClients(uuid string) {
	mu.Lock()
	defer mu.Unlock()
	// 只从 map 中删除，不再负责关闭连接。管理员强制下线不走宽限期。
	delete(connectedClients, uuid)
	delete(v2Clients, uuid)
	delete(graceUntil, uuid)
}

// SetPresence sets or clears presence for non-WebSocket agents.
// When present=false, it only clears if the connectionID matches current one.
// KeepAlivePresence sets presence with TTL for non-WebSocket agents.
func KeepAlivePresence(uuid string, connectionID int64, ttl time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	presenceOnly[uuid] = struct {
		id     int64
		expire time.Time
	}{id: connectionID, expire: time.Now().Add(ttl)}
	delete(graceUntil, uuid)
}

var defaultPresenceTTL = 20 * time.Second

// SetPresence keeps compatibility with existing callers.
func SetPresence(uuid string, connectionID int64, present bool) {
	mu.Lock()
	defer mu.Unlock()
	if present {
		presenceOnly[uuid] = struct {
			id     int64
			expire time.Time
		}{id: connectionID, expire: time.Now().Add(defaultPresenceTTL)}
		return
	}
	if cur, ok := presenceOnly[uuid]; ok && cur.id == connectionID {
		delete(presenceOnly, uuid)
	}
}

// GetAllOnlineUUIDs returns a de-duplicated list of online UUIDs from both WebSocket and non-WebSocket agents.
// WebSocket 槽位在删除后仍计入 presenceGrace（展示滞回），POST 上报的
// presence 过期后同样宽限 presenceGrace：短抖动期间 UI 保持在线，
// 只有真正的持续失联才会翻转成离线。
func GetAllOnlineUUIDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	set := make(map[string]struct{})
	for k := range connectedClients {
		set[k] = struct{}{}
	}
	now := time.Now()
	for k, v := range presenceOnly {
		if v.expire.Add(presenceGrace).After(now) {
			set[k] = struct{}{}
		}
	}
	for k, until := range graceUntil {
		if until.After(now) {
			set[k] = struct{}{}
		}
	}
	res := make([]string, 0, len(set))
	for k := range set {
		res = append(res, k)
	}
	return res
}

// ProbeAlive actively checks whether conn can still exchange WebSocket
// control frames. The duplicate-connection decision used to rely purely on
// LastActive staleness, so a half-open connection whose peer died silently
// (NAT rebuild, CDN edge recycle, sleep) occupied the registry slot until
// zombieConnThreshold passed — and every reconnect attempt in between was
// rejected as a duplicate, which production logs show produced recurring
// ~40-70s 假失联 windows per incident. A single ping answers the question
// in one round trip: a live peer's pong handler refreshes LastActive, a dead
// path stays silent (or WriteControl fails outright).
func ProbeAlive(conn *connection.SafeConn, wait time.Duration) bool {
	bornAt := conn.LastActive()
	if err := conn.GetConn().WriteControl(websocket.PingMessage, []byte("liveness-probe"), time.Now().Add(time.Second)); err != nil {
		return false
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if conn.LastActive().After(bornAt) {
			return true
		}
	}
	return false
}

// WithRegistrationLock serializes the duplicate-connection decision and the
// registration that follows it per uuid. Without it, two challengers dialing
// at once (old agent binaries retry every ~2s) could both probe the same
// zombie and both take over, leaving two live connections racing over one
// registry slot.
func WithRegistrationLock(uuid string, fn func()) {
	v, _ := registrationLocks.LoadOrStore(uuid, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	defer m.Unlock()
	fn()
}
func GetLatestReport() map[string]*v2.Report {
	mu.RLock()
	defer mu.RUnlock()
	reportCopy := make(map[string]*v2.Report)
	for k, v := range latestReport {
		if v == nil {
			continue
		}
		item := *v
		reportCopy[k] = &item
	}
	return reportCopy
}

// RecordReport updates the latest runtime state and keeps only the short raw
// window used by recent-status compatibility endpoints.
func RecordReport(report v2.Report) {
	if report.UUID == "" {
		return
	}
	if report.UpdatedAt.IsZero() {
		report.UpdatedAt = time.Now().UTC()
	} else {
		report.UpdatedAt = report.UpdatedAt.UTC()
	}
	mu.Lock()
	defer mu.Unlock()
	if latest := latestReport[report.UUID]; latest == nil || !report.UpdatedAt.Before(latest.UpdatedAt) {
		item := report
		latestReport[report.UUID] = &item
	}
	cutoff := time.Now().UTC().Add(-recentReportRetention)
	reports := reportsAfter(recentReports[report.UUID], cutoff)
	if report.UpdatedAt.Before(cutoff) {
		recentReports[report.UUID] = reports
		return
	}
	insertAt := sort.Search(len(reports), func(i int) bool {
		return reports[i].UpdatedAt.After(report.UpdatedAt)
	})
	reports = append(reports, v2.Report{})
	copy(reports[insertAt+1:], reports[insertAt:])
	reports[insertAt] = report
	recentReports[report.UUID] = reports
}

func GetRecentReports(uuid string) []v2.Report {
	mu.Lock()
	defer mu.Unlock()
	reports := reportsAfter(recentReports[uuid], time.Now().UTC().Add(-recentReportRetention))
	if len(reports) == 0 {
		delete(recentReports, uuid)
		return []v2.Report{}
	}
	recentReports[uuid] = reports
	return append([]v2.Report(nil), reports...)
}

func reportsAfter(reports []v2.Report, cutoff time.Time) []v2.Report {
	first := 0
	for first < len(reports) && reports[first].UpdatedAt.Before(cutoff) {
		first++
	}
	out := make([]v2.Report, len(reports)-first)
	copy(out, reports[first:])
	return out
}

func DeleteLatestReport(uuid string) {
	mu.Lock()
	defer mu.Unlock()
	delete(latestReport, uuid)
	delete(recentReports, uuid)
}
