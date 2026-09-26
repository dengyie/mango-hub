package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/rpc"
)

func TestGuestNodeListsHideMinerCapability(t *testing.T) {
	uuid := "node-miner-visibility"
	db := dbcore.GetDBInstance()
	db.Where("uuid = ?", uuid).Delete(&models.Client{})
	now := time.Now().UTC()
	if err := db.Create(&models.Client{
		UUID:              uuid,
		Token:             "miner-visibility-token",
		Name:              "visible-miner",
		MinerConfigured:   true,
		MinerControllable: true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	t.Cleanup(func() {
		db.Where("uuid = ?", uuid).Delete(&models.Client{})
	})

	guestCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Principal: rpc.NewAnonymousPrincipal(),
	})
	guestRes, jerr := getNodes(guestCtx, rpc.NewRequest(1, "common:getNodes", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("guest getNodes: %+v", jerr)
	}
	guestJSON, err := json.Marshal(guestRes)
	if err != nil {
		t.Fatalf("marshal guest: %v", err)
	}
	var guest map[string]any
	if err := json.Unmarshal(guestJSON, &guest); err != nil {
		t.Fatalf("unmarshal guest: %v", err)
	}
	if guest["miner_configured"] == true || guest["miner_controllable"] == true {
		t.Fatalf("guest getNodes leaked miner capability: %s", guestJSON)
	}

	publicRes, jerr := publicGetNodesInformation(guestCtx, rpc.NewRequest(1, "public:getNodesInformation", nil))
	if jerr != nil {
		t.Fatalf("public nodes: %+v", jerr)
	}
	publicJSON, err := json.Marshal(publicRes)
	if err != nil {
		t.Fatalf("marshal public: %v", err)
	}
	var public []map[string]any
	if err := json.Unmarshal(publicJSON, &public); err != nil {
		t.Fatalf("unmarshal public: %v", err)
	}
	for _, node := range public {
		if node["uuid"] == uuid && (node["miner_configured"] == true || node["miner_controllable"] == true) {
			t.Fatalf("public node list leaked miner capability: %s", publicJSON)
		}
	}

	adminCtx := rpc.NewContextWithMeta(context.Background(), &rpc.ContextMeta{
		Principal: rpc.NewUserPrincipal("admin-test"),
	})
	adminRes, jerr := getNodes(adminCtx, rpc.NewRequest(1, "common:getNodes", map[string]any{"uuid": uuid}))
	if jerr != nil {
		t.Fatalf("admin getNodes: %+v", jerr)
	}
	adminJSON, err := json.Marshal(adminRes)
	if err != nil {
		t.Fatalf("marshal admin: %v", err)
	}
	var admin map[string]any
	if err := json.Unmarshal(adminJSON, &admin); err != nil {
		t.Fatalf("unmarshal admin: %v", err)
	}
	if admin["miner_configured"] != true || admin["miner_controllable"] != true {
		t.Fatalf("admin lost miner capability: %s", adminJSON)
	}
}
