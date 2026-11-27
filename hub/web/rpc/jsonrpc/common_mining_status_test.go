package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/pkg/rpc"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
	v2 "github.com/dengyie/mangohub/protocol/v2"
)

func isolateLatestReports(t *testing.T) {
	t.Helper()
	agent_runtime.DeleteLatestReport("node-mining-live")
	t.Cleanup(func() {
		agent_runtime.DeleteLatestReport("node-mining-live")
	})
}

func TestGetNodesLatestStatusCopiesSlimMining(t *testing.T) {
	isolateLatestReports(t)

	uuid := "node-mining-live"
	db := dbcore.GetDBInstance()
	db.Where("uuid = ?", uuid).Delete(&models.Client{})
	if err := db.Create(&models.Client{UUID: uuid, Name: "dgn-miner", Token: "mining-live-token"}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	const fullWallet = "krxXGNKMD4/vps01"
	agent_runtime.RecordReport(v2.Report{
		UUID:      uuid,
		UpdatedAt: time.Now().UTC(),
		Mining: &v2.MiningReport{
			Algorithm:    "xelishashv3",
			Pool:         "xel-hk.kryptex.network:7019",
			Wallet:       fullWallet,
			Hashrate1Min: 1094.16,
			SharesValid:  12,
		},
		// 新 agent 形态:档位在报告顶层,不在 mining 里。
		Governor: &v2.GovernorReport{
			Tier:        "eco",
			IdleSec:     7,
			GpuPct:      5.25,
			PausedByGov: true,
		},
	})

	guestCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{})
	guestRes, jerr := getNodesLatestStatus(guestCtx, rpc.NewRequest(1, "common:getNodesLatestStatus", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("guest latest status: %+v", jerr)
	}
	b, err := json.Marshal(guestRes)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mining, ok := got["mining"].(map[string]any)
	if !ok {
		t.Fatalf("mining missing in latest status: %s", b)
	}
	if mining["algorithm"] != "xelishashv3" {
		t.Fatalf("algorithm = %#v", mining["algorithm"])
	}
	if mining["hashrate_1min"] != 1094.16 {
		t.Fatalf("hashrate_1min = %#v", mining["hashrate_1min"])
	}
	gov, ok := mining["governor"].(map[string]any)
	if !ok {
		t.Fatalf("governor missing in latest status: %s", b)
	}
	if gov["tier"] != "eco" {
		t.Fatalf("governor tier = %#v", gov["tier"])
	}
	// 游客只拿档位:idle_sec 等于「主人多久没碰键盘」,gpu_pct 能反推负载,属行为隐私。
	for _, priv := range []string{"idle_sec", "gpu_pct"} {
		if _, present := gov[priv]; present {
			t.Fatalf("guest hot-path leaked %q: %s", priv, b)
		}
	}
	if _, present := gov["paused_by_gov"]; present {
		t.Fatalf("hot-path leaked paused_by_gov: %s", b)
	}
	for _, leaked := range []string{"wallet", "pool", "shares_valid", "shares_total", "power_w"} {
		if _, present := mining[leaked]; present {
			t.Fatalf("hot-path mining leaked %q: %s", leaked, b)
		}
	}
	if _, present := got["wallet"]; present {
		t.Fatalf("wallet leaked at record root: %s", b)
	}

	stored := agent_runtime.GetLatestReport()[uuid]
	if stored == nil || stored.Mining == nil || stored.Mining.Wallet != fullWallet {
		t.Fatalf("slim DTO mutated runtime report: %#v", stored)
	}

	adminCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Principal: rpc.NewUserPrincipal("admin-test"),
	})
	adminRes, jerr := getNodesLatestStatus(adminCtx, rpc.NewRequest(1, "common:getNodesLatestStatus", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("admin latest status: %+v", jerr)
	}
	ab, err := json.Marshal(adminRes)
	if err != nil {
		t.Fatalf("marshal admin: %v", err)
	}
	var adminGot map[string]any
	if err := json.Unmarshal(ab, &adminGot); err != nil {
		t.Fatalf("unmarshal admin: %v", err)
	}
	adminMining, ok := adminGot["mining"].(map[string]any)
	if !ok {
		t.Fatalf("admin mining missing: %s", ab)
	}
	if _, present := adminMining["wallet"]; present {
		t.Fatalf("admin latest-status still carries wallet: %s", ab)
	}
	// 管理员拿得到隐私字段,用于排查「为什么降档了」。
	adminGov, ok := adminMining["governor"].(map[string]any)
	if !ok {
		t.Fatalf("admin governor missing: %s", ab)
	}
	if adminGov["idle_sec"] != float64(7) || adminGov["gpu_pct"] != 5.25 {
		t.Fatalf("admin governor projection = %#v", adminGov)
	}
	if _, present := adminGov["paused_by_gov"]; present {
		t.Fatalf("admin hot-path leaked paused_by_gov: %s", ab)
	}
}

// 回归:调节器进 pause 档会停掉 srbminer 服务,矿工 API 随之消失,报告里 mining 整体缺失。
// 此时面板仍必须看到「暂停(让卡)」——所以 hub 要为只有档位的报告造出 mining 块。
func TestGetNodesLatestStatusGovernorWithoutMining(t *testing.T) {
	isolateLatestReports(t)
	uuid := "node-mining-live"
	db := dbcore.GetDBInstance()
	db.Where("uuid = ?", uuid).Delete(&models.Client{})
	if err := db.Create(&models.Client{UUID: uuid, Name: "paused", Token: "paused-token"}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	agent_runtime.RecordReport(v2.Report{
		UUID:      uuid,
		UpdatedAt: time.Now().UTC(),
		Governor:  &v2.GovernorReport{Tier: "pause", GpuPct: 44.2, PausedByGov: true},
	})

	guestCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{})
	res, jerr := getNodesLatestStatus(guestCtx, rpc.NewRequest(1, "common:getNodesLatestStatus", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("latest status: %+v", jerr)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mining, ok := got["mining"].(map[string]any)
	if !ok {
		t.Fatalf("mining block missing for governor-only report (pause tier would vanish): %s", b)
	}
	gov, ok := mining["governor"].(map[string]any)
	if !ok {
		t.Fatalf("governor missing: %s", b)
	}
	if gov["tier"] != "pause" {
		t.Fatalf("governor tier = %#v, want pause", gov["tier"])
	}
	// 不能为了塞档位而伪造矿工数据(否则前端会渲染出假的「Mining · 0 H/s」)。
	for _, fake := range []string{"algorithm", "hashrate_1min"} {
		if _, present := mining[fake]; present {
			t.Fatalf("governor-only report fabricated %q: %s", fake, b)
		}
	}
}

// 滚动升级期间旧 agent 仍把档位放在 mining.governor 里,必须继续被识别。
func TestGetNodesLatestStatusLegacyNestedGovernor(t *testing.T) {
	isolateLatestReports(t)
	uuid := "node-mining-live"
	db := dbcore.GetDBInstance()
	db.Where("uuid = ?", uuid).Delete(&models.Client{})
	if err := db.Create(&models.Client{UUID: uuid, Name: "legacy", Token: "legacy-token"}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	agent_runtime.RecordReport(v2.Report{
		UUID:      uuid,
		UpdatedAt: time.Now().UTC(),
		Mining: &v2.MiningReport{
			Algorithm: "pearlhash",
			Governor:  &v2.GovernorReport{Tier: "full", IdleSec: 300, GpuPct: 0.4},
		},
	})

	guestCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{})
	res, jerr := getNodesLatestStatus(guestCtx, rpc.NewRequest(1, "common:getNodesLatestStatus", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("latest status: %+v", jerr)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mining := got["mining"].(map[string]any)
	if mining["algorithm"] != "pearlhash" {
		t.Fatalf("legacy mining algorithm lost: %s", b)
	}
	gov, ok := mining["governor"].(map[string]any)
	if !ok || gov["tier"] != "full" {
		t.Fatalf("legacy nested governor not projected: %s", b)
	}
}

func TestGetNodesLatestStatusOmitsMiningWhenAbsent(t *testing.T) {
	isolateLatestReports(t)
	uuid := "node-mining-live"
	db := dbcore.GetDBInstance()
	db.Where("uuid = ?", uuid).Delete(&models.Client{})
	if err := db.Create(&models.Client{UUID: uuid, Name: "plain", Token: "plain-token"}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	agent_runtime.RecordReport(v2.Report{UUID: uuid, UpdatedAt: time.Now().UTC()})

	guestCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{})
	res, jerr := getNodesLatestStatus(guestCtx, rpc.NewRequest(1, "common:getNodesLatestStatus", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("latest status: %+v", jerr)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["mining"]; ok {
		t.Fatalf("mining present without report: %s", b)
	}
}
