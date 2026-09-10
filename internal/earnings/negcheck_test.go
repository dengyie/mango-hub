package earnings

import (
	"testing"
	"time"
)

// 负向用例：未知标题的 breakdown 卡（"Total payout"）不得影响
// Available/Pending 的识别，也不能被误认为 Total。
func TestParseUnknownBreakdownCardIgnored(t *testing.T) {
	html := `
<div class="bal-kpi-title" style="x">Total balance</span>
<div class="h2">0.00003880 BTC</div>
<div class="bal-breakdown__label" style="x">Available</div>
<div class="bal-breakdown__value">0.00003305 BTC</div>
<div class="bal-breakdown__label" style="x">Pending</div>
<div class="bal-breakdown__value">0.00000570 BTC</div>
<div class="bal-breakdown__label" style="x">Total payout</div>
<div class="bal-breakdown__value">9.99999999 BTC</div>
`
	now := time.Now()
	b, err := ParseBalancePage(html, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !nearly(b.Total, 0.00003880) {
		t.Errorf("Total = %v, want 0.00003880 (unknown card must not override)", b.Total)
	}
	if !nearly(b.Available, 0.00003305) || !nearly(b.Unpaid, 0.00000570) {
		t.Errorf("Available/Unpaid = %v/%v, want 0.00003305/0.00000570", b.Available, b.Unpaid)
	}
}

// 负金额条目（减项）现在应被保留为非正向条目，而不是被丢弃。
func TestParseNegativeFeedEntry(t *testing.T) {
	html := `
<div class="bal-kpi-title" style="x">Total balance</span>
<div class="h2">0.00003880 BTC</div>
<div class="bal-breakdown__label" style="x">Available</div>
<div class="bal-breakdown__value">0.00003305 BTC</div>
<div class="bal-breakdown__label" style="x">Pending</div>
<div class="bal-breakdown__value">0.00000570 BTC</div>
<div class="bal-feed__meta">Sept. 10, 2026, 10:45 p.m.</div>
<div class="bal-feed__amount"><span class="bal-feed__value bal-feed__value--positive">+0.00000138</span></div>
<div class="bal-feed__meta">Sept. 9, 2026, 6:45 p.m.</div>
<div class="bal-feed__amount"><span class="bal-feed__value">-0.00050000</span></div>
`
	now := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	b, err := ParseBalancePage(html, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(b.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (negative entry must be kept)", len(b.Entries))
	}
	neg := b.Entries[1]
	if neg.Positive || !nearly(neg.Amount, -0.0005) {
		t.Errorf("negative entry = %+v, want positive=false amount=-0.0005", neg)
	}
	// 负向条目不计入当日收益
	if !nearly(b.dayEarned(now), 0.00000138) {
		t.Errorf("dayEarned = %v, want 0.00000138", b.dayEarned(now))
	}
}
