package clients

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/logger"
)

func TestApplyClientInfoUpdateFlipsMinerFlagsOff(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&models.Client{}); err != nil {
		t.Fatalf("migrate client: %v", err)
	}

	now := time.Now().UTC()
	client := models.Client{
		UUID:              "miner-flag-client",
		Token:             "miner-flag-token",
		Name:              "miner-flag",
		MinerConfigured:   true,
		MinerControllable: true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	// An older agent omits both flags. Stored identity must stay put.
	if err := applyClientInfoUpdate(db, client.UUID, map[string]interface{}{
		"updated_at": now,
		"cpu_cores":  4,
	}); err != nil {
		t.Fatalf("omit flags: %v", err)
	}
	got := reloadMinerClient(t, db, client.UUID)
	if !got.MinerConfigured || !got.MinerControllable || got.CpuCores != 4 {
		t.Fatalf("omitted flags were cleared or cpu was not saved: %+v", got)
	}

	// A restarted agent without the miner flags must be able to store false.
	// Updates(map) would skip these zeros and leave the old true in place.
	if err := applyClientInfoUpdate(db, client.UUID, map[string]interface{}{
		"updated_at":         now,
		"miner_configured":   false,
		"miner_controllable": false,
	}); err != nil {
		t.Fatalf("clear flags: %v", err)
	}
	got = reloadMinerClient(t, db, client.UUID)
	if got.MinerConfigured || got.MinerControllable {
		t.Fatalf("false flags were skipped: configured=%v controllable=%v", got.MinerConfigured, got.MinerControllable)
	}

	if err := applyClientInfoUpdate(db, client.UUID, map[string]interface{}{
		"updated_at":         now,
		"miner_configured":   true,
		"miner_controllable": false,
	}); err != nil {
		t.Fatalf("set configured only: %v", err)
	}
	got = reloadMinerClient(t, db, client.UUID)
	if !got.MinerConfigured || got.MinerControllable {
		t.Fatalf("configured-only update failed: configured=%v controllable=%v", got.MinerConfigured, got.MinerControllable)
	}
}

func TestApplyClientInfoUpdateRejectsNonBoolMinerFlags(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&models.Client{}); err != nil {
		t.Fatalf("migrate client: %v", err)
	}

	now := time.Now().UTC()
	client := models.Client{
		UUID:      "miner-flag-types",
		Token:     "miner-flag-types-token",
		Name:      "miner-flag-types",
		CpuCores:  2,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	err = applyClientInfoUpdate(db, client.UUID, map[string]interface{}{
		"updated_at":         now,
		"cpu_cores":          8,
		"miner_configured":   "false",
		"miner_controllable": 0,
	})
	if err == nil {
		t.Fatal("non-bool miner flags were accepted")
	}
	got := reloadMinerClient(t, db, client.UUID)
	if got.CpuCores != 2 || got.MinerConfigured || got.MinerControllable {
		t.Fatalf("rejected payload still changed the row: %+v", got)
	}

	if err := applyClientInfoUpdate(db, client.UUID, map[string]interface{}{
		"updated_at":         now,
		"cpu_cores":          6,
		"miner_configured":   nil,
		"miner_controllable": nil,
	}); err != nil {
		t.Fatalf("null flags: %v", err)
	}
	got = reloadMinerClient(t, db, client.UUID)
	if got.CpuCores != 6 || got.MinerConfigured || got.MinerControllable {
		t.Fatalf("null flags changed stored identity or skipped cpu: %+v", got)
	}
}

func TestSaveClientCanTurnMinerFlagOff(t *testing.T) {
	db := openMinerDB(t)
	now := time.Now().UTC()
	client := models.Client{
		UUID:              "miner-flag-edit",
		Token:             "miner-flag-edit-token",
		Name:              "before",
		MinerConfigured:   true,
		MinerControllable: true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	if err := saveClient(db, client.UUID, map[string]interface{}{
		"uuid":               client.UUID,
		"name":               "after",
		"miner_configured":   false,
		"miner_controllable": true,
	}); err != nil {
		t.Fatalf("save client: %v", err)
	}
	got := reloadMinerClient(t, db, client.UUID)
	if got.Name != "after" || got.MinerConfigured || !got.MinerControllable {
		t.Fatalf("edit did not store false beside the other true flag: %+v", got)
	}
}

func TestSaveClientKeepsTheRowWhenUuidIsTheWhereKey(t *testing.T) {
	db := openMinerDB(t)
	now := time.Now().UTC()
	client := models.Client{
		UUID:      "miner-edit-uuid",
		Token:     "miner-edit-uuid-token",
		Name:      "before",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	if err := saveClient(db, client.UUID, map[string]interface{}{
		"uuid": client.UUID,
		"name": "after",
	}); err != nil {
		t.Fatalf("save client: %v", err)
	}
	got := reloadMinerClient(t, db, client.UUID)
	if got.Name != "after" || got.UUID != client.UUID {
		t.Fatalf("edit with its own uuid did not rename the row: %+v", got)
	}

	err := saveClient(db, client.UUID, map[string]interface{}{
		"uuid": "rewritten-uuid",
		"name": "stolen",
	})
	if err == nil {
		t.Fatal("changing uuid through an edit was accepted")
	}
	got = reloadMinerClient(t, db, client.UUID)
	if got.Name != "after" {
		t.Fatalf("rejected uuid rewrite still changed the row: %+v", got)
	}
	var rewritten int64
	if err := db.Model(&models.Client{}).Where("uuid = ?", "rewritten-uuid").Count(&rewritten).Error; err != nil {
		t.Fatalf("count rewritten uuid: %v", err)
	}
	if rewritten != 0 {
		t.Fatalf("rejected uuid rewrite created a row")
	}
}

func TestApplyClientInfoUpdateRollsBackWhenFlagWriteFails(t *testing.T) {
	db := openMinerDB(t)
	now := time.Now().UTC()
	client := models.Client{
		UUID:      "miner-flag-rollback",
		Token:     "miner-flag-rollback-token",
		Name:      "rollback",
		CpuCores:  2,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		if len(tx.Statement.Selects) > 0 {
			_ = tx.AddError(fmt.Errorf("injected flag write failure"))
			return
		}
		callbacks.Update(&callbacks.Config{})(tx)
	})

	err := applyClientInfoUpdate(db, client.UUID, map[string]interface{}{
		"updated_at":         now,
		"cpu_cores":          9,
		"miner_configured":   true,
		"miner_controllable": false,
	})
	if err == nil {
		t.Fatal("flag write failure was ignored")
	}
	got := reloadMinerClient(t, db, client.UUID)
	if got.CpuCores != 2 || got.MinerConfigured || got.MinerControllable {
		t.Fatalf("failed flag write left the basic-info update committed: %+v", got)
	}
}

func openMinerDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&models.Client{}); err != nil {
		t.Fatalf("migrate client: %v", err)
	}
	return db
}

func reloadMinerClient(t *testing.T, db *gorm.DB, uuid string) models.Client {
	t.Helper()
	var got models.Client
	if err := db.First(&got, "uuid = ?", uuid).Error; err != nil {
		t.Fatalf("reload client: %v", err)
	}
	return got
}
