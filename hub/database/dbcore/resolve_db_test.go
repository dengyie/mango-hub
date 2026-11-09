package dbcore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDatabaseFileWithPaths(t *testing.T) {
	tempDir := t.TempDir()
	mangoPath := filepath.Join(tempDir, "mangohub.db")
	komariPath := filepath.Join(tempDir, "komari.db")

	// Case 1: Neither exists -> default to mangoPath
	if got := resolveDatabaseFileWithPaths("", mangoPath, komariPath); got != mangoPath {
		t.Fatalf("neither exists: got %s, want %s", got, mangoPath)
	}

	// Case 2: Only komari.db exists (populated) -> select komari.db
	if err := os.WriteFile(komariPath, []byte("sqlite database content"), 0644); err != nil {
		t.Fatalf("failed to write komari.db: %v", err)
	}
	if got := resolveDatabaseFileWithPaths("", mangoPath, komariPath); got != komariPath {
		t.Fatalf("only komari.db exists: got %s, want %s", got, komariPath)
	}

	// Case 3: Empty (0-byte) mangohub.db exists alongside populated komari.db
	// MUST NOT shadow komari.db -> must select komari.db!
	if err := os.WriteFile(mangoPath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to write empty mangohub.db: %v", err)
	}
	if got := resolveDatabaseFileWithPaths("", mangoPath, komariPath); got != komariPath {
		t.Fatalf("empty mangohub.db shadowing komari.db: got %s, want %s", got, komariPath)
	}

	// Case 4: Populated mangohub.db exists alongside populated komari.db
	// (Post-migration) -> prefer mangohub.db
	if err := os.WriteFile(mangoPath, []byte("new mangohub database content"), 0644); err != nil {
		t.Fatalf("failed to write populated mangohub.db: %v", err)
	}
	if got := resolveDatabaseFileWithPaths("", mangoPath, komariPath); got != mangoPath {
		t.Fatalf("populated mangohub.db exists: got %s, want %s", got, mangoPath)
	}

	// Case 5: Custom database file passed explicitly -> return custom path directly
	customPath := filepath.Join(tempDir, "custom.db")
	if got := resolveDatabaseFileWithPaths(customPath, mangoPath, komariPath); got != customPath {
		t.Fatalf("custom db file specified: got %s, want %s", got, customPath)
	}
}
