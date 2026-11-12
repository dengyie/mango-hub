package jsonrpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dengyie/mangohub/hub/pkg/rpc"
	"github.com/dengyie/mangohub/hub/protocol/v2"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
)

// 游客可见性回归（P1）：public:getClientRecentRecords 直通最近状态缓存，
// 缓存内是原始 v2.Report（含 mining 钱包/矿池、gpu、disk_io）。游客必须拿不到
// 这些敏感字段；登录管理员保持全量。形状（嵌套 cpu/ram/load/network）不变，
// 前端 liveDataToRecords 依赖该形状。
func TestGuestRecentRecordsStripSensitiveFields(t *testing.T) {
	uuid := "recent-guest-strip-node"
	now := time.Now().UTC()
	agent_runtime.RecordReport(v2.Report{
		UUID: uuid,
		CPU:  v2.CPUReport{Usage: 12.5},
		Ram:  v2.RamReport{Used: 1024},
		Load: v2.LoadReport{Load1: 0.5},
		Network: v2.NetworkReport{
			Up: 100, Down: 200, TotalUp: 300, TotalDown: 400,
		},
		GPU: &v2.GPUDetailReport{AverageUsage: 55},
		Mining: &v2.MiningReport{
			Algorithm:    "xelishashv3",
			Hashrate1Min: 1318,
			Wallet:       "WALLETADDR123456",
			Pool:         "stratum+tcp://10.144.144.2:7019",
		},
		DiskIO:    []v2.DiskIOReport{{Device: "sda", ReadBytes: 1, WriteBytes: 2}},
		Uptime:    3600,
		UpdatedAt: now,
	})
	t.Cleanup(func() {
		agent_runtime.DeleteLatestReport(uuid)
	})

	guestCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Principal: rpc.NewAnonymousPrincipal(),
	})
	guestRes, jerr := publicGetClientRecentRecords(guestCtx, rpc.NewRequest(1, "public:getClientRecentRecords", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("guest recent records: %+v", jerr)
	}
	guestJSON, err := json.Marshal(guestRes)
	if err != nil {
		t.Fatalf("marshal guest: %v", err)
	}
	guestStr := string(guestJSON)
	for _, leak := range []string{"mining", "wallet", "pool", "worker", "disk_io", "gpu", "WALLETADDR123456"} {
		if strings.Contains(guestStr, leak) {
			t.Fatalf("guest recent records leaked %q: %s", leak, guestStr)
		}
	}
	// 嵌套形状保持：cpu/ram/load/network 顶层字段仍存在（liveDataToRecords 依赖）。
	var guestReports []map[string]any
	if err := json.Unmarshal(guestJSON, &guestReports); err != nil {
		t.Fatalf("guest shape not []v2.Report: %v", err)
	}
	if len(guestReports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(guestReports))
	}
	if _, ok := guestReports[0]["cpu"]; !ok {
		t.Fatalf("guest report missing cpu field (shape break): %s", guestStr)
	}

	adminCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Principal: rpc.NewUserPrincipal("admin-uuid"),
	})
	adminRes, jerr := publicGetClientRecentRecords(adminCtx, rpc.NewRequest(2, "public:getClientRecentRecords", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("admin recent records: %+v", jerr)
	}
	adminJSON, err := json.Marshal(adminRes)
	if err != nil {
		t.Fatalf("marshal admin: %v", err)
	}
	adminStr := string(adminJSON)
	for _, keep := range []string{"mining", "wallet", "disk_io", "gpu"} {
		if !strings.Contains(adminStr, keep) {
			t.Fatalf("admin recent records lost %q: %s", keep, adminStr)
		}
	}
}
