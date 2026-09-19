package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/rpc"
	v2 "github.com/komari-monitor/komari/protocol/v2"
	agent_runtime "github.com/komari-monitor/komari/web/agent"
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
