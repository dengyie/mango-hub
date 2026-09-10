package models

import "time"

// MiningEarningsSnapshot 是账户级挖矿收益快照（如 Kryptex 交易所余额页），
// 与节点无关，由 hub 后台轮询外部站点写入。
//
// 同一 UTC 小时只保留一行：轮询器先删后插，幂等且避免表无限增长。
// 解析失败时不写行，保留上一小时的快照供前端展示。
type MiningEarningsSnapshot struct {
	ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Source       string    `json:"source" gorm:"type:varchar(32);uniqueIndex:uidx_earnings_hour,priority:1"` // 例如 "kryptex"
	HourUTC      time.Time `json:"hour_utc" gorm:"uniqueIndex:uidx_earnings_hour,priority:2;type:timestamp"`
	TotalBTC     float64   `json:"total_btc" gorm:"type:double"`
	AvailableBTC float64   `json:"available_btc" gorm:"type:double"`
	UnpaidBTC    float64   `json:"unpaid_btc" gorm:"type:double"`
	DayEarnedBTC float64   `json:"day_earned_btc" gorm:"type:double"` // 当日流水求和
	FetchedAt    time.Time `json:"fetched_at" gorm:"type:timestamp"`
}
