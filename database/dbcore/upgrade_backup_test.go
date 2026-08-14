package dbcore

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestPruneUpgradeBackupsKeepsNewest 验证 pruneUpgradeBackups 保留最近 keep 份、
// 删除更旧的 upgrade-*.zip（文件名 UTC 时间戳，字典序即时间序）。
func TestPruneUpgradeBackupsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"upgrade-20260813-164156.zip",
		"upgrade-20260813-202035.zip",
		"upgrade-20260814-065851.zip",
		"upgrade-20260814-095824.zip",
		"upgrade-20260814-104705.zip",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}

	if err := pruneUpgradeBackups(dir, 3); err != nil {
		t.Fatalf("pruneUpgradeBackups: %v", err)
	}

	got := listUpgradeZips(t, dir)
	want := []string{
		"upgrade-20260814-065851.zip",
		"upgrade-20260814-095824.zip",
		"upgrade-20260814-104705.zip",
	}
	if len(got) != len(want) {
		t.Fatalf("remaining = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("remaining[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestPruneUpgradeBackupsUnderLimit 验证数量不超过 keep 时不做删除。
func TestPruneUpgradeBackupsUnderLimit(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"upgrade-20260814-095824.zip",
		"upgrade-20260814-104705.zip",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}

	if err := pruneUpgradeBackups(dir, 3); err != nil {
		t.Fatalf("pruneUpgradeBackups: %v", err)
	}
	if got := listUpgradeZips(t, dir); len(got) != 2 {
		t.Fatalf("remaining = %v, want 2", got)
	}
}

// TestPruneUpgradeBackupsMixedFiles 验证只匹配 upgrade-*.zip，不动其他文件。
func TestPruneUpgradeBackupsMixedFiles(t *testing.T) {
	dir := t.TempDir()
	fixtures := []string{
		"upgrade-20260813-164156.zip",
		"upgrade-20260814-104705.zip",
		"pre-restore-20260814-100000.zip",
		"komari.db",
	}
	for _, n := range fixtures {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}

	if err := pruneUpgradeBackups(dir, 1); err != nil {
		t.Fatalf("pruneUpgradeBackups: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	for _, want := range []string{"upgrade-20260814-104705.zip", "pre-restore-20260814-100000.zip", "komari.db"} {
		if !got[want] {
			t.Fatalf("missing %s in %v", want, got)
		}
	}
	if got["upgrade-20260813-164156.zip"] {
		t.Fatalf("old upgrade zip should be pruned, got %v", got)
	}
}

// listUpgradeZips 列出 dir 下 upgrade-*.zip 文件名（已排序）。
func listUpgradeZips(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "upgrade-*.zip"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	sort.Strings(names)
	return names
}