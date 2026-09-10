package earnings

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
)

// initTestDB 把 SQLite 指到临时目录并完成初始化 + AutoMigrate。
// dbcore.Initialize 用 once.Do，包内测试只能初始化一次。
func initTestDB(t *testing.T) {
	t.Helper()
	flags.DatabaseFile = filepath.Join(t.TempDir(), "komari.db")
	if err := dbcore.Initialize(); err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
}

func TestSaveSnapshotIdempotentPerHour(t *testing.T) {
	initTestDB(t)
	now := time.Date(2026, 9, 11, 10, 5, 0, 0, time.UTC)
	b := &BalancePage{Total: 0.00003880, Available: 0.00003305, Unpaid: 0.00000570}

	if err := SaveSnapshot("kryptex", b, now); err != nil {
		t.Fatalf("save: %v", err)
	}
	// 同一小时再存（数值更新）→ 覆盖而不是追加
	b2 := &BalancePage{Total: 0.00003900, Available: 0.00003320, Unpaid: 0.00000570}
	if err := SaveSnapshot("kryptex", b2, now.Add(20*time.Minute)); err != nil {
		t.Fatalf("save again: %v", err)
	}

	var rows []models.MiningEarningsSnapshot
	db := dbcore.GetDBInstance()
	if err := db.Order("hour_utc").Find(&rows).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (same UTC hour must overwrite)", len(rows))
	}
	if rows[0].TotalBTC != b2.Total {
		t.Errorf("TotalBTC = %v, want latest %v", rows[0].TotalBTC, b2.Total)
	}
	if rows[0].FetchedAt != now.Add(20*time.Minute).UTC() {
		t.Errorf("FetchedAt = %v, want %v", rows[0].FetchedAt, now.Add(20*time.Minute).UTC())
	}

	// 下一小时 → 新行
	if err := SaveSnapshot("kryptex", b2, now.Add(70*time.Minute)); err != nil {
		t.Fatalf("save next hour: %v", err)
	}
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("query2: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows after next-hour save = %d, want 2", len(rows))
	}
}
