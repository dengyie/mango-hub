package earnings

import (
	"os"
	"testing"
	"time"
)

// 用真实抓取的整页 HTML 做端到端解析（本地文件，跳过即不影响 CI）。
func TestParseRealBalancePageE2E(t *testing.T) {
	html, err := os.ReadFile("/tmp/bal-auth.html")
	if err != nil {
		t.Skip("real page sample not available locally")
	}
	now := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	b, err := ParseBalancePage(string(html), now)
	if err != nil {
		t.Fatalf("parse real page: %v", err)
	}
	t.Logf("Total=%v Available=%v Unpaid=%v entries=%d dayEarned=%v",
		b.Total, b.Available, b.Unpaid, len(b.Entries), b.dayEarned(now))
	if b.Total <= 0 || b.Available <= 0 {
		t.Errorf("unexpected KPIs: %+v", b)
	}
	if len(b.Entries) < 10 {
		t.Errorf("entries = %d, want >= 10", len(b.Entries))
	}
}
