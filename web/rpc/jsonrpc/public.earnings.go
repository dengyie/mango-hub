package jsonrpc

import (
	"context"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/rpc"
)

// public.earnings.go — 账户级挖矿收益（Kryptex 等）的公开只读端点。
//
// 数据来源是 hub 后台轮询写入的 MiningEarningsSnapshot 表；
// 本端点只返回数值，不返回任何凭据（如拉取用的 session cookie）。

// publicGetMiningEarnings 返回最近的余额快照与近 N 小时的每小时间隔收益。
// 响应形状：
//
//	{
//	  "enabled": true,
//	  "latest": {"total_btc":0.0000388,"available_btc":...,"unpaid_btc":...,"day_earned_btc":...,"fetched_at":"..."},
//	  "hourly":  [ {"hour_utc":"...","earned_btc":0.00000142}, ... ]   // 按 hour 升序
//	}
//
// earned_btc 为相邻小时快照 total 的差值（正值），用于前端画增速曲线；
// 首个样本因没有前值返回 0。功能未启用（无任何快照）时 enabled=false。
// 只读单一 source：多数据源时各自序列不应混在一条差值曲线里。
func publicGetMiningEarnings(_ context.Context, _ *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	const (
		window = 7 * 24 * time.Hour
		source = "kryptex"
	)

	db := dbcore.GetDBInstance()
	if db == nil {
		return nil, rpc.MakeError(rpc.InternalError, "database not initialized", nil)
	}
	var rows []models.MiningEarningsSnapshot
	if err := db.
		Model(&models.MiningEarningsSnapshot{}).
		Where("source = ? AND hour_utc >= ?", source, time.Now().UTC().Add(-window).Truncate(time.Hour)).
		Order("hour_utc ASC").
		Find(&rows).Error; err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "failed to load earnings: "+err.Error(), nil)
	}
	if len(rows) == 0 {
		return map[string]any{"enabled": false}, nil
	}

	type hourlyItem struct {
		HourUTC   string  `json:"hour_utc"`
		EarnedBTC float64 `json:"earned_btc"`
		TotalBTC  float64 `json:"total_btc"`
	}
	hourly := make([]hourlyItem, 0, len(rows))
	prev := 0.0
	for i, r := range rows {
		earned := 0.0
		if i > 0 {
			earned = r.TotalBTC - prev
			if earned < 0 {
				earned = 0
			}
		}
		hourly = append(hourly, hourlyItem{
			HourUTC:   r.HourUTC.Format(time.RFC3339),
			EarnedBTC: round8(earned),
			TotalBTC:  r.TotalBTC,
		})
		prev = r.TotalBTC
	}

	latest := rows[len(rows)-1]
	return map[string]any{
		"enabled": true,
		"latest": map[string]any{
			"total_btc":      latest.TotalBTC,
			"available_btc":  latest.AvailableBTC,
			"unpaid_btc":     latest.UnpaidBTC,
			"day_earned_btc": latest.DayEarnedBTC,
			"fetched_at":     latest.FetchedAt.Format(time.RFC3339),
		},
		"hourly": hourly,
	}, nil
}

// round8 收敛浮点减法噪声（BTC 余额本身是 8 位小数）。
func round8(v float64) float64 {
	if v < 0 {
		v = 0
	}
	return float64(int64(v*1e8+0.5)) / 1e8
}

func init() {
	regPublic("getMiningEarnings", publicGetMiningEarnings, "Get account-level mining earnings (balance snapshots)")
}
