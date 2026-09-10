// Package earnings 轮询账户级挖矿收益（目前仅支持 Kryptex）。
//
// 设计约束：
//   - 轻量：一个 HTTP GET + 正则解析，不依赖浏览器/无头引擎。
//   - 凭据：管理员把已登录浏览器的 sessionid 等 Cookie 复制到站点设置
//     kryptex_session_cookie。Cookie 只存服务端配置表，本包绝不把它
//     写进日志或任何 API 响应。
//   - 失败容忍：网络失败或页面改版解析失败时记日志并跳过本轮，
//     保留历史快照；不 panic、不重试风暴（调度间隔即为重试间隔）。
package earnings

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/internal/config"
	"github.com/komari-monitor/komari/internal/scheduler"
	logger "github.com/komari-monitor/komari/utils/log"
	"gorm.io/gorm"
)

const (
	balanceURL     = "https://www.kryptex.com/site/balance"
	fetchUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
	fetchTimeout   = 20 * time.Second
	// 页面上未登录会 302 到 /login；按固定 UA 取页面，避免被反爬拦截。
	maxFeedEntries = 48 // 流水最多保留一天半的量，超出丢弃（仅用于当日求和）
)

// BalancePage 是从 Kryptex 余额页 SSR HTML 解析出的数值（单位 BTC）。
type BalancePage struct {
	Total     float64
	Available float64
	Unpaid    float64
	Entries   []BalanceEntry
}

// BalanceEntry 是收益流水中的一笔（Exchange 结算等）。
type BalanceEntry struct {
	Time     time.Time
	Amount   float64
	Positive bool
}

var (
	// KPI 主卡（Total balance）：标题在 bal-kpi-title 里，数值在其后
	// 300 字符内的 class="h2" 容器里（中间隔着闭合标签/空白/内联样式）。
	kpiRe = regexp.MustCompile(
		`bal-kpi-title"[^>]*>\s*([^<]+?)\s*<[\s\S]{0,300}?class="h2[^"]*"[^>]*>\s*([0-9.]+)\s*BTC`)
	// 分项卡（Available / Pending）：label 与 value 是相邻 div。
	breakdownRe = regexp.MustCompile(
		`bal-breakdown__label"[^>]*>\s*(?:<[^>]+>\s*)*([^<]+?)\s*</div>\s*<div class="bal-breakdown__value[^"]*"[^>]*>\s*([0-9.]+)\s*BTC`)
	// 流水条目：meta 里是时间文本，随后 400 字符内的 bal-feed__value
	// span 是金额；正向条目带 bal-feed__value--positive 类。
	entryRe = regexp.MustCompile(
		`bal-feed__meta">\s*([^<]+?)\s*</div>[\s\S]{0,400}?class="bal-feed__value([^"]*)">\s*\+?([0-9.]+)\s*</span>`)
	entryTimeLayout = "Jan. 2, 2006, 3:04 PM"
)

// parseFeedTime 解析页面流水时间。站点输出 "Sept. 10, 2026, 10:45 p.m."：
// September 用非标准四字母缩写（Go 只认 Sep），上午/下午是小写带点
// 的 "p.m."（Go 只认 PM）。归一后再解析。
func parseFeedTime(s string) (time.Time, error) {
	s = strings.Replace(s, "Sept.", "Sep.", 1)
	s = strings.ReplaceAll(s, "p.m.", "PM")
	s = strings.ReplaceAll(s, "a.m.", "AM")
	return time.Parse(entryTimeLayout, s)
}

// ParseBalancePage 从 /site/balance 的 HTML 提取余额与流水。
// 独立成函数便于单测：给定 HTML 片段即可验证，无需网络。
func ParseBalancePage(html string, now time.Time) (*BalancePage, error) {
	out := &BalancePage{}
	seen := map[string]bool{}
	for _, m := range kpiRe.FindAllStringSubmatch(html, -1) {
		title := strings.TrimSpace(m[1])
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(strings.ToLower(title), "total"):
			out.Total = v
			seen["total"] = true
		case strings.Contains(strings.ToLower(title), "rate"), strings.Contains(strings.ToLower(title), "汇率"):
			// Bitcoin Rate 等汇率卡片：复用同一标题结构，跳过
		}
	}
	for _, m := range breakdownRe.FindAllStringSubmatch(html, -1) {
		title := strings.TrimSpace(m[1])
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(strings.ToLower(title), "available"), strings.Contains(title, "可用"):
			out.Available = v
			seen["available"] = true
		case strings.Contains(strings.ToLower(title), "pending"), strings.Contains(title, "待处理"):
			out.Unpaid = v
			seen["unpaid"] = true
		}
	}
	if !seen["total"] {
		return nil, fmt.Errorf("balance KPI not found; page may require login or layout changed")
	}
	for i, m := range entryRe.FindAllStringSubmatch(html, -1) {
		if i >= maxFeedEntries {
			break
		}
		t, err := parseFeedTime(strings.TrimSpace(m[1]))
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			continue
		}
		out.Entries = append(out.Entries, BalanceEntry{Time: t, Amount: v, Positive: strings.Contains(m[2], "--positive")})
	}
	return out, nil
}

// dayEarned 汇总「今天（UTC）」的正向流水金额。
func (b *BalancePage) dayEarned(now time.Time) float64 {
	sum := 0.0
	today := now.UTC().Format("2006-01-02")
	for _, e := range b.Entries {
		if e.Positive && e.Time.UTC().Format("2006-01-02") == today {
			sum += e.Amount
		}
	}
	return sum
}

// fetchKryptexBalance 用站点设置中的 Cookie 拉取并解析余额页。
// Cookie 未配置时返回 (nil, nil)，调用方据此跳过本轮。
func fetchKryptexBalance(ctx context.Context) (*BalancePage, error) {
	cookie, err := config.GetAs[string](config.KryptexSessionCookieKey)
	if err != nil || strings.TrimSpace(cookie) == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, balanceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Cookie", cookie)

	client := &http.Client{
		Timeout: fetchTimeout,
		// 未登录时站点 302 到 /login；跟随与否都能从结果判断，但
		// 不跟随更省一次请求且语义清晰。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		return nil, fmt.Errorf("session cookie expired (redirected to login)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	return ParseBalancePage(string(body), time.Now())
}

// SaveSnapshot 以 UTC 小时为粒度写入一行快照（同小时先删后插，幂等）。
func SaveSnapshot(source string, b *BalancePage, now time.Time) error {
	db := dbcore.GetDBInstance()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	hour := now.UTC().Truncate(time.Hour)
	row := models.MiningEarningsSnapshot{
		Source:       source,
		HourUTC:      hour,
		TotalBTC:     b.Total,
		AvailableBTC: b.Available,
		UnpaidBTC:    b.Unpaid,
		DayEarnedBTC: b.dayEarned(now),
		FetchedAt:    now.UTC(),
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source = ? AND hour_utc = ?", source, hour).
			Delete(&models.MiningEarningsSnapshot{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
}

// PollOnce 抓取一次并落库；返回是否实际写入了快照。
func PollOnce(ctx context.Context) (bool, error) {
	b, err := fetchKryptexBalance(ctx)
	if err != nil {
		return false, err
	}
	if b == nil { // cookie 未配置，功能未启用
		return false, nil
	}
	if err := SaveSnapshot("kryptex", b, time.Now()); err != nil {
		return false, err
	}
	return true, nil
}

// StartPoller 注册到全局调度器。由 internal/server 的 registerScheduledWork 调用。
func StartPoller() {
	if err := scheduler.AddContextFunc("earnings:kryptex", "@every 10m", true, pollLogged); err != nil {
		logger.ErrorArgs("server", "Failed to add kryptex earnings task:", err)
	}
}

func pollLogged(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout+5*time.Second)
	defer cancel()
	wrote, err := PollOnce(ctx)
	if err != nil {
		logger.Warn("earnings", "Kryptex balance fetch failed", "error", err.Error())
		return
	}
	if wrote {
		logger.Info("earnings", "Kryptex balance snapshot saved")
	}
}
