package clients

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

func reloadMinerClient(t *testing.T, db *gorm.DB, uuid string) models.Client {
	t.Helper()
	var got models.Client
	if err := db.First(&got, "uuid = ?", uuid).Error; err != nil {
		t.Fatalf("reload client: %v", err)
	}
	return got
}
