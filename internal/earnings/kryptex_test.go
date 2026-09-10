package earnings

import (
	"math"
	"testing"
	"time"
)

// 从线上 /site/balance 抓取的真实 SSR 结构（数值已保留真实形态，
// 类名/布局为解析器的稳定锚点）。
const sampleHTML = `
<div class="col-12 col-l-6" style="padding:0 10px;">
  <div class="bg-white rounded border border-light-100 h-100 d-flex flex-column" style="padding:1.1rem;">
    <div class="d-flex align-items-start mb-4">
      <span class="bal-kpi-title" style="font-size:14px;font-weight:500;color:#1a1a2e;line-height:1.2;">Total balance</span>
    </div>
    <div class="h2 line-normal mb-0" style="font-size:23px;font-weight:500;letter-spacing:-.02em;">0.00003880 BTC</div>
    <div class="text-middle mb-4" style="font-size:14px;">≈ ￥20.09</div>
    <div class="mt-auto d-flex flex-column flex-l-row" style="gap:8px;">
      <div class="bal-kpi-avail d-flex flex-column">Available</div>
    </div>
  </div>
</div>
<div class="col-12 col-l-6" style="padding:0 10px;">
  <div class="bg-white rounded border border-light-100 h-100 d-flex flex-column" style="padding:1.1rem;">
    <div class="d-flex align-items-start mb-4">
      <span class="bal-kpi-title" style="font-size:14px;font-weight:500;color:#1a1a2e;line-height:1.2;">Bitcoin Rate</span>
    </div>
    <div class="h2 line-normal mb-0" style="font-size:23px;font-weight:500;letter-spacing:-.02em;">517802.01 BTC</div>
  </div>
</div>
<div class="bal-breakdown" style="flex:1;">
  <div class="bal-breakdown__label" style="display:flex;align-items:center;gap:5px;">
    <span style="width:7px;height:7px;border-radius:50%;background:#22c55e;flex-shrink:0;"></span>
    Available
  </div>
  <div class="bal-breakdown__value" style="font-size:12px;">0.00003305 BTC</div>
</div>
<div class="bal-breakdown bal-breakdown--expandable" style="flex:1;">
  <div class="bal-breakdown__label" style="display:flex;align-items:center;gap:5px;">
    <span style="width:7px;height:7px;border-radius:50%;background:#f59e0b;flex-shrink:0;"></span>
    Pending
    
    <span style="color:#d1d5db;">•</span>
    <span tabindex="0" class="bal-pending-coins__summary" data-pos="bottom" data-tip-pre
      data-tip="PRL  0.00000549 BTC
USDT  0.00000002 BTC
XMR  0.00000003 BTC">
      By coin
      <svg width="10" height="10" viewBox="0 0 24 24" fill="none"><path d="M12 22"/></svg>
    </span>
    
  </div>
  <div class="bal-breakdown__value" style="font-size:12px;">0.00000570 BTC</div>
</div>
<div class="bal-feed__body">
  <div class="bal-feed__name">Exchange</div>
  <div class="bal-feed__meta">Sept. 10, 2026, 10:45 p.m.</div>
</div>
<div class="bal-feed__amount">
  <span class="bal-feed__value bal-feed__value--positive">+0.00000138</span>
  <span class="bal-feed__unit">BTC</span>
</div>
<div class="bal-feed__body">
  <div class="bal-feed__name">Exchange</div>
  <div class="bal-feed__meta">Sept. 10, 2026, 8:45 p.m.</div>
</div>
<div class="bal-feed__amount">
  <span class="bal-feed__value bal-feed__value--positive">+0.00000183</span>
  <span class="bal-feed__unit">BTC</span>
</div>
<div class="bal-feed__body">
  <div class="bal-feed__name">Payout</div>
  <div class="bal-feed__meta">Sept. 9, 2026, 6:45 p.m.</div>
</div>
<div class="bal-feed__amount">
  <span class="bal-feed__value ">0.01000000</span>
  <span class="bal-feed__unit">BTC</span>
</div>
`

func TestParseBalancePage(t *testing.T) {
	now := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	b, err := ParseBalancePage(sampleHTML, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !nearly(b.Total, 0.00003880) {
		t.Errorf("Total = %v, want 0.00003880", b.Total)
	}
	if !nearly(b.Available, 0.00003305) {
		t.Errorf("Available = %v, want 0.00003305", b.Available)
	}
	if !nearly(b.Unpaid, 0.00000570) {
		t.Errorf("Unpaid = %v, want 0.00000570", b.Unpaid)
	}
	if len(b.Entries) != 3 {
		t.Fatalf("entries = %d, want 3 (got %+v)", len(b.Entries), b.Entries)
	}
	e := b.Entries[0]
	if !e.Positive || !nearly(e.Amount, 0.00000138) {
		t.Errorf("first entry = %+v, want positive 0.00000138", e)
	}
	wantTime := time.Date(2026, 9, 10, 22, 45, 0, 0, time.UTC)
	if e.Time.UTC() != wantTime {
		t.Errorf("entry time = %v, want %v (UTC 22:45 = p.m. 10:45)", e.Time.UTC(), wantTime)
	}
	// 第三条无 positive 类名 → 非正向（如 payout 扣减）
	if b.Entries[2].Positive {
		t.Errorf("payout entry should not be positive, got %+v", b.Entries[2])
	}
}

func TestParseBalancePageLoginRedirect(t *testing.T) {
	// 未登录时 SSR 渲染的是登录页：无 KPI，应报错而不是返回零值。
	if _, err := ParseBalancePage("<html><body>Log In</body></html>", time.Now()); err == nil {
		t.Fatal("expected error for page without balance KPI")
	}
}

func TestDayEarnedOnlyCountsTodayPositive(t *testing.T) {
	now := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	b, err := ParseBalancePage(sampleHTML, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 当日 = 0.00000138 + 0.00000183（9月9日的 payout 与当日无关）
	if got := b.dayEarned(now); !nearly(got, 0.00000321) {
		t.Errorf("dayEarned = %v, want 0.00000321", got)
	}
}

func nearly(a, b float64) bool {
	return math.Abs(a-b) < 1e-12
}
