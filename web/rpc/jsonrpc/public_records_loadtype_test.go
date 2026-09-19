package jsonrpc

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/internal/metricstore"
	"github.com/komari-monitor/komari/pkg/metric"
	"github.com/komari-monitor/komari/pkg/rpc"
	v2 "github.com/komari-monitor/komari/protocol/v2"
)

// useMetricsTestStore 挂载一块内存 metric store 供 API 层集成测试使用。
func useMetricsTestStore(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	policy := metric.RollupPolicy{
		RawRetention: metricstore.DefaultRollupRawRetention,
		Tiers: []metric.RollupTier{
			{Interval: time.Minute, Retention: 600 * time.Minute},
			{Interval: 5 * time.Minute, Retention: 3000 * time.Minute},
			{Interval: time.Hour, Retention: 600 * time.Hour},
		},
		Compression: 30,
	}
	dsn := fmt.Sprintf("file:jsonrpc-metrics-%d?mode=memory&cache=shared", time.Now().UnixNano())
	s, err := metric.Open(ctx, metric.SQLite(dsn, metric.WithMaxOpenConns(1), metric.WithRollupPolicy(policy)))
	if err != nil {
		t.Fatalf("open metric store: %v", err)
	}
	if err := metricstore.EnsureBuiltinMetricDefinitions(ctx, s); err != nil {
		t.Fatalf("create metric definitions: %v", err)
	}
	restore := metricstore.SwapStoreForTest(t, s)
	// SwapStoreForTest 在 restore 时关闭传入 store；这里兜底防止测试序异常
	t.Cleanup(restore)
}

func callGetRecordsByUUID(t *testing.T, uuid, loadType, hours string) (map[string]any, *rpc.JsonRpcError) {
	t.Helper()
	params := map[string]any{"uuid": uuid}
	if loadType != "" {
		params["load_type"] = loadType
	}
	if hours != "" {
		params["hours"] = hours
	}
	result, jerr := rpc.Invoke("public:getRecordsByUUID", params)
	if jerr != nil {
		return nil, jerr
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	return m, nil
}

// load_type=mining/gpu 曾在 validLoadTypes 白名单外被 Invalid params 拒绝，
// 而处理函数内存在对应分支（死分支）。该测试锁定白名单与分支的一致性。
func TestGetRecordsByUUIDAcceptsMiningAndGpuLoadTypes(t *testing.T) {
	useMetricsTestStore(t)
	ctx := context.Background()

	// 公开节点（非 hidden），游客可见
	db := dbcore.GetDBInstance()
	if err := db.Where("uuid = ?", "node-mining").Delete(&models.Client{}).Error; err != nil {
		t.Fatalf("clean client: %v", err)
	}
	if err := db.Create(&models.Client{UUID: "node-mining", Name: "miner"}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	// 样本时间戳必须严格落在过去（API 用 End=now 查询；已过去的分钟边界不会
	// 被 End 排除）。用当前分钟的上一分钟，且等待跨过该边界。
	base := time.Now().UTC().Truncate(time.Minute)
		report := v2.Report{
			UUID:      "node-mining",
			UpdatedAt: base.Add(-39 * time.Second),
			CPU:       v2.CPUReport{Usage: 10},
			Mining: &v2.MiningReport{
				Algorithm:    "pearlhash",
				Pool:         "prl-eu.kryptex.network:7048",
				Wallet:       "krxXGNKMD4/test",
				Hashrate1Min: 1e12,
			},
		}
	if _, err := metricstore.WriteReport(ctx, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if err := metricstore.FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// load_type=mining 必须返回 mining_records 而不是 Invalid params
	resp, jerr := callGetRecordsByUUID(t, "node-mining", "mining", "4")
	if jerr != nil {
		t.Fatalf("load_type=mining rejected: %+v", jerr)
	}
	if resp["has_mining_data"] != true {
		t.Fatalf("has_mining_data = %v, want true (resp=%v)", resp["has_mining_data"], resp)
	}
	records, ok := resp["mining_records"].([]metricstore.MiningRecord)
	if !ok || len(records) != 1 {
		t.Fatalf("mining_records = %#v, want 1 record", resp["mining_records"])
	}
	// 游客路径：rig 已掩码（完整地址断言由 TestGuestMiningRecordsMaskWallet /
	// TestMaskWalletAddr 承担；这里只需断言掩码值与算力透传）
	if records[0].Rig != "krxX***D4/test" || records[0].Hashrate != 1e12 {
		t.Fatalf("mining record = %#v", records[0])
	}

	// load_type=gpu 同理（无 GPU 数据时走 has_gpu_data=false 而非报错）
	resp, jerr = callGetRecordsByUUID(t, "node-mining", "gpu", "4")
	if jerr != nil {
		t.Fatalf("load_type=gpu rejected: %+v", jerr)
	}
	if resp["has_gpu_data"] != false {
		t.Fatalf("has_gpu_data = %v, want false", resp["has_gpu_data"])
	}

	// 未知 load_type 仍应拒绝
	if _, jerr = callGetRecordsByUUID(t, "node-mining", "bogus", "4"); jerr == nil {
		t.Fatal("load_type=bogus accepted, want Invalid params")
	}
}

// 游客访问 /api/records/load 时 mining_records.rig 必须是掩码钱包，
// 不能出现完整地址（Finding 4 收敛：地址是资金属性信息）。
func TestGuestMiningRecordsMaskWallet(t *testing.T) {
	useMetricsTestStore(t)
	ctx := context.Background()

	db := dbcore.GetDBInstance()
	db.Where("uuid = ?", "node-mask").Delete(&models.Client{})
	if err := db.Create(&models.Client{UUID: "node-mask", Name: "mask", Token: "mask-token"}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	// 样本锚在已结束的分钟且等 now 跨过该边界，避免 End=now 排除样本
	base := time.Now().UTC().Truncate(time.Minute).Add(-39 * time.Second)
	report := v2.Report{
		UUID:      "node-mask",
		UpdatedAt: base,
		Mining: &v2.MiningReport{
			Algorithm:    "pearlhash",
			Pool:         "prl-eu.kryptex.network:7048",
			Wallet:       "krxXGNKMD4/home-win",
			Hashrate1Min: 1e12,
		},
	}
	if _, err := metricstore.WriteReport(ctx, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if err := metricstore.FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// rpc.Invoke 无登录态上下文 → 游客路径
	resp, jerr := callGetRecordsByUUID(t, "node-mask", "mining", "4")
	if jerr != nil {
		t.Fatalf("load_type=mining rejected: %+v", jerr)
	}
	records, ok := resp["mining_records"].([]metricstore.MiningRecord)
	if !ok || len(records) != 1 {
		t.Fatalf("mining_records = %#v", resp["mining_records"])
	}
	rig := records[0].Rig
	if rig != "krxX***D4/home-win" {
		t.Fatalf("rig = %q, want krxX***D4/home-win", rig)
	}
	if strings.Contains(rig, "krxXGNKMD4") {
		t.Fatalf("full wallet leaked to guest: %q", rig)
	}
}

func TestMaskWalletAddr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"krxXGNKMD4/home-win", "krxX***D4/home-win"},
		{"0x1234567890abcdef", "0x12***ef"},
		{"short1", "short1"},     // <=6 原样
		{"", ""},                 // 空串
		{"ab/cd", "ab/cd"},       // 地址部分太短
		{"XEN9999999999", "XEN9***99"},
	}
	for _, c := range cases {
		if got := maskWalletAddr(c.in); got != c.want {
			t.Errorf("maskWalletAddr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
