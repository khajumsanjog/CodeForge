package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDifferentialSnapshotAndRollback(t *testing.T) {
	// Create source directory
	srcDir, err := os.MkdirTemp("", "codeforge-test-src-*")
	if err != nil {
		t.Fatalf("failed to create src temp dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	// Create test files
	file1 := filepath.Join(srcDir, "app.txt")
	file2 := filepath.Join(srcDir, "config.json")
	_ = os.WriteFile(file1, []byte("version 1.0"), 0644)
	_ = os.WriteFile(file2, []byte(`{"env": "prod"}`), 0644)

	// Save snapshot
	snapDir, err := os.MkdirTemp("", "codeforge-test-snaps-*")
	if err != nil {
		t.Fatalf("failed to create snap temp dir: %v", err)
	}
	defer os.RemoveAll(snapDir)

	snapPath, err := SaveSnapshot("testproj", srcDir, snapDir)
	if err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	// Now simulate a deployment target where app.txt is modified and extra file is added
	targetDir, err := os.MkdirTemp("", "codeforge-test-target-*")
	if err != nil {
		t.Fatalf("failed to create target temp dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	_ = os.WriteFile(filepath.Join(targetDir, "app.txt"), []byte("CORRUPTED BUILD"), 0644)
	_ = os.WriteFile(filepath.Join(targetDir, "config.json"), []byte(`{"env": "prod"}`), 0644)
	_ = os.WriteFile(filepath.Join(targetDir, "extra.tmp"), []byte("junk data"), 0644)

	// Verify diff calculation
	snap, err := LoadSnapshot(snapPath)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	diff, err := ComputeDiff(snap, targetDir)
	if err != nil {
		t.Fatalf("ComputeDiff failed: %v", err)
	}

	if len(diff.Modified) != 1 || diff.Modified[0] != "app.txt" {
		t.Errorf("expected 1 modified file (app.txt), got: %v", diff.Modified)
	}
	if len(diff.Extraneous) != 1 || diff.Extraneous[0] != "extra.tmp" {
		t.Errorf("expected 1 extraneous file (extra.tmp), got: %v", diff.Extraneous)
	}
	if len(diff.Unchanged) != 1 || diff.Unchanged[0] != "config.json" {
		t.Errorf("expected 1 unchanged file (config.json), got: %v", diff.Unchanged)
	}

	// Perform Differential Rollback
	err = RestoreSnapshot(snapPath, targetDir)
	if err != nil {
		t.Fatalf("RestoreSnapshot failed: %v", err)
	}

	// Verify app.txt was reverted
	appContent, _ := os.ReadFile(filepath.Join(targetDir, "app.txt"))
	if string(appContent) != "version 1.0" {
		t.Errorf("expected reverted content 'version 1.0', got '%s'", string(appContent))
	}

	// Verify extra.tmp was deleted
	if _, err := os.Stat(filepath.Join(targetDir, "extra.tmp")); !os.IsNotExist(err) {
		t.Errorf("expected extra.tmp to be pruned, but file still exists")
	}

	// Verify config.json is still intact
	cfgContent, _ := os.ReadFile(filepath.Join(targetDir, "config.json"))
	if string(cfgContent) != `{"env": "prod"}` {
		t.Errorf("expected unchanged config content, got '%s'", string(cfgContent))
	}
}
