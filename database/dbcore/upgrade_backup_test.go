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

// TestPrunePreRestoreBackupsKeepsNewest 验证 pre-restore 备份同样只保留最近 keep 份。
func TestPrunePreRestoreBackupsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"pre-restore-20260813-164156.zip",
		"pre-restore-20260814-095824.zip",
		"pre-restore-20260814-104705.zip",
		"pre-restore-20260814-111658.zip",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}

	if err := prunePreRestoreBackups(dir, 2); err != nil {
		t.Fatalf("prunePreRestoreBackups: %v", err)
	}

	got := listZipsByPrefix(t, dir, "pre-restore-*.zip")
	want := []string{
		"pre-restore-20260814-104705.zip",
		"pre-restore-20260814-111658.zip",
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

// TestPruneBackupZipsMillisecondTimestamp 验证毫秒级时间戳文件名（同秒多次备份）
// 字典序仍然正确：同秒内 .000 < .500 < .999，且不与其他前缀文件混淆。
func TestPruneBackupZipsMillisecondTimestamp(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"upgrade-20260814-104705.000.zip",
		"upgrade-20260814-104705.500.zip",
		"upgrade-20260814-104705.999.zip",
		"upgrade-20260814-104706.000.zip",
		"pre-restore-20260814-104705.500.zip", // 不同前缀，不应被 upgrade prune 影响
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}

	if err := pruneUpgradeBackups(dir, 3); err != nil {
		t.Fatalf("pruneUpgradeBackups: %v", err)
	}

	got := listZipsByPrefix(t, dir, "upgrade-*.zip")
	want := []string{
		"upgrade-20260814-104705.500.zip",
		"upgrade-20260814-104705.999.zip",
		"upgrade-20260814-104706.000.zip",
	}
	if len(got) != len(want) {
		t.Fatalf("remaining = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("remaining[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	// pre-restore 前缀不应被 upgrade prune 误删
	if _, err := os.Stat(filepath.Join(dir, "pre-restore-20260814-104705.500.zip")); err != nil {
		t.Fatalf("pre-restore zip should be untouched: %v", err)
	}
}

// listUpgradeZips 列出 dir 下 upgrade-*.zip 文件名（已排序）。
func listUpgradeZips(t *testing.T, dir string) []string {
	t.Helper()
	return listZipsByPrefix(t, dir, "upgrade-*.zip")
}

// listZipsByPrefix 列出 dir 下匹配 glob pattern 的 zip 文件名（已排序）。
func listZipsByPrefix(t *testing.T, dir, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
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
