package jsonrpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
)

// initEarningsTestDB 在独立临时库上初始化 dbcore（jsonrpc 包测试首个触库者）。
func initEarningsTestDB(t *testing.T) {
	t.Helper()
	flags.DatabaseFile = filepath.Join(t.TempDir(), "komari.db")
	if err := dbcore.Initialize(); err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
}

func TestPublicGetMiningEarningsDisabled(t *testing.T) {
	initEarningsTestDB(t)
	res, rpcErr := publicGetMiningEarnings(context.Background(), nil)
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if v, _ := m["enabled"].(bool); v {
		t.Fatalf("enabled = true with empty table, want false")
	}
}

func TestPublicGetMiningEarningsHourlyAndLatest(t *testing.T) {
	initEarningsTestDB(t)
	db := dbcore.GetDBInstance()
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	// 混入另一个 source 的行：hourly 差值序列必须只基于 kryptex，
	// 否则两种来源交错会把对方当"前一小时"算出错误的 earned_btc。
	other := models.MiningEarningsSnapshot{
		Source: "otherpool", HourUTC: base.Add(30 * time.Minute), TotalBTC: 99,
		FetchedAt: base.Add(30 * time.Minute),
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("seed other source: %v", err)
	}
	// 三小时快照：total 递增（模拟挖矿收益增长）
	totals := []float64{0.00001000, 0.00001238, 0.00001506}
	for i, total := range totals {
		row := models.MiningEarningsSnapshot{
			Source: "kryptex", HourUTC: base.Add(time.Duration(i) * time.Hour),
			TotalBTC: total, AvailableBTC: total, FetchedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	res, rpcErr := publicGetMiningEarnings(context.Background(), nil)
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	// JSON 序列化往返一次，让断言面向线上真实返回形状（map/slice），而不是内部类型。
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, _ := m["enabled"].(bool); !v {
		t.Fatal("enabled = false, want true")
	}
	hourly := m["hourly"].([]any)
	if len(hourly) != 3 {
		t.Fatalf("hourly len = %d, want 3", len(hourly))
	}
	// 首样本无前值 → 0；后续为相邻差值
	first := hourly[0].(map[string]any)
	if got := first["earned_btc"].(float64); got != 0 {
		t.Errorf("first earned = %v, want 0", got)
	}
	second := hourly[1].(map[string]any)
	if got := second["earned_btc"].(float64); got != 0.00000238 {
		t.Errorf("second earned = %v, want 0.00000238", got)
	}
	third := hourly[2].(map[string]any)
	if got := third["earned_btc"].(float64); got != 0.00000268 {
		t.Errorf("third earned = %v, want 0.00000268", got)
	}

	latest := m["latest"].(map[string]any)
	if got := latest["total_btc"].(float64); got != 0.00001506 {
		t.Errorf("latest total = %v, want 0.00001506", got)
	}
	if latest["fetched_at"].(string) == "" {
		t.Error("latest.fetched_at empty")
	}
	// 凭据永不出现在响应里
	for key := range m {
		if key == "cookie" || key == "session" {
			t.Errorf("response leaked credential-bearing key %q", key)
		}
	}
}
