package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/rpc"
	v1 "github.com/komari-monitor/komari/protocol/v1"
	agent_runtime "github.com/komari-monitor/komari/web/agent"
)

func isolateLatestReports(t *testing.T) {
	t.Helper()
	agent_runtime.DeleteLatestReport("node-mining-live")
	t.Cleanup(func() {
		agent_runtime.DeleteLatestReport("node-mining-live")
	})
}

func TestGetNodesLatestStatusCopiesAndMasksMining(t *testing.T) {
	isolateLatestReports(t)

	uuid := "node-mining-live"
	db := dbcore.GetDBInstance()
	db.Where("uuid = ?", uuid).Delete(&models.Client{})
	if err := db.Create(&models.Client{UUID: uuid, Name: "dgn-miner", Token: "mining-live-token"}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	const fullWallet = "krxXGNKMD4/vps01"
	agent_runtime.RecordReport(v1.Report{
		UUID:      uuid,
		UpdatedAt: time.Now().UTC(),
		Mining: &v1.MiningReport{
			Algorithm:    "xelishashv3",
			Pool:         "xel-hk.kryptex.network:7019",
			Wallet:       fullWallet,
			Hashrate1Min: 1094.16,
		},
	})

	decodeMining := func(t *testing.T, res any) *v1.MiningReport {
		t.Helper()
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got struct {
			Mining *v1.MiningReport `json:"mining"`
		}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Mining == nil {
			t.Fatalf("mining missing in latest status: %s", b)
		}
		return got.Mining
	}

	guestCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{})
	guestRes, jerr := getNodesLatestStatus(guestCtx, rpc.NewRequest(1, "common:getNodesLatestStatus", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("guest latest status: %+v", jerr)
	}
	guestMining := decodeMining(t, guestRes)
	if guestMining.Wallet != "krxX***D4/vps01" {
		t.Fatalf("guest wallet = %q, want masked", guestMining.Wallet)
	}
	if guestMining.Algorithm != "xelishashv3" || guestMining.Hashrate1Min != 1094.16 {
		t.Fatalf("guest mining payload = %#v", guestMining)
	}

	stored := agent_runtime.GetLatestReport()[uuid]
	if stored == nil || stored.Mining == nil || stored.Mining.Wallet != fullWallet {
		t.Fatalf("guest mask mutated runtime report: %#v", stored)
	}

	adminCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Principal: rpc.NewUserPrincipal("admin-test"),
	})
	adminRes, jerr := getNodesLatestStatus(adminCtx, rpc.NewRequest(1, "common:getNodesLatestStatus", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("admin latest status: %+v", jerr)
	}
	adminMining := decodeMining(t, adminRes)
	if adminMining.Wallet != fullWallet {
		t.Fatalf("admin wallet = %q, want full", adminMining.Wallet)
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
	agent_runtime.RecordReport(v1.Report{UUID: uuid, UpdatedAt: time.Now().UTC()})

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
