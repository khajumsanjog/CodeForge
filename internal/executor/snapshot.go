package executor

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileMeta holds metadata and payload for differential comparison.
type FileMeta struct {
	ContentB64 string `json:"content"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Mode       uint32 `json:"mode"`
}

// Snapshot represents a versioned workspace snapshot.
type Snapshot struct {
	Project   string              `json:"project"`
	Timestamp string              `json:"timestamp"`
	Files     map[string]FileMeta `json:"files"` // relPath -> FileMeta
}

// DiffResult describes differential changes between current directory state and snapshot.
type DiffResult struct {
	Modified   []string
	Missing    []string
	Extraneous []string
	Unchanged  []string
}

func (d DiffResult) Summary() string {
	return fmt.Sprintf("Diff Summary: %d modified, %d missing, %d extraneous (pruned), %d unchanged",
		len(d.Modified), len(d.Missing), len(d.Extraneous), len(d.Unchanged))
}

// SaveSnapshot walks sourceDir, computes SHA256 hashes & encodes files into a versioned JSON snapshot.
func SaveSnapshot(project, sourceDir, snapshotDir string) (string, error) {
	resolvedDir := resolvePath(snapshotDir)
	projectDir := filepath.Join(resolvedDir, sanitizeFilename(project))
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return "", err
	}

	snap := Snapshot{
		Project:   project,
		Timestamp: time.Now().Format(time.RFC3339),
		Files:     make(map[string]FileMeta),
	}

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		if shouldSkip(rel) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		hash := sha256.Sum256(data)
		hashStr := hex.EncodeToString(hash[:])

		snap.Files[rel] = FileMeta{
			ContentB64: base64.StdEncoding.EncodeToString(data),
			SHA256:     hashStr,
			Size:       info.Size(),
			Mode:       uint32(info.Mode()),
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	snapData, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}

	filename := fmt.Sprintf("%d.json", time.Now().UnixNano())
	snapPath := filepath.Join(projectDir, filename)
	if err := os.WriteFile(snapPath, snapData, 0644); err != nil {
		return "", err
	}

	return snapPath, nil
}

// LoadSnapshot parses a JSON snapshot with backward compatibility for legacy snapshots.
func LoadSnapshot(snapshotPath string) (*Snapshot, error) {
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Project   string                     `json:"project"`
		Timestamp string                     `json:"timestamp"`
		Files     map[string]json.RawMessage `json:"files"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	snap := &Snapshot{
		Project:   raw.Project,
		Timestamp: raw.Timestamp,
		Files:     make(map[string]FileMeta),
	}

	for rel, rawMsg := range raw.Files {
		var strVal string
		if err := json.Unmarshal(rawMsg, &strVal); err == nil {
			// Legacy format: string content base64
			fileData, _ := base64.StdEncoding.DecodeString(strVal)
			hash := sha256.Sum256(fileData)
			snap.Files[rel] = FileMeta{
				ContentB64: strVal,
				SHA256:     hex.EncodeToString(hash[:]),
				Size:       int64(len(fileData)),
				Mode:       0644,
			}
		} else {
			var meta FileMeta
			if err := json.Unmarshal(rawMsg, &meta); err == nil {
				snap.Files[rel] = meta
			}
		}
	}

	return snap, nil
}

// ComputeDiff calculates differential changes between outputDir and snapshot.
func ComputeDiff(snapshot *Snapshot, outputDir string) (DiffResult, error) {
	diff := DiffResult{}
	targetFiles := make(map[string]string) // relPath -> SHA256

	if _, err := os.Stat(outputDir); err == nil {
		_ = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(outputDir, path)
			if err != nil || shouldSkip(rel) {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			hash := sha256.Sum256(data)
			targetFiles[rel] = hex.EncodeToString(hash[:])
			return nil
		})
	}

	// Compare snapshot files against target files
	for rel, meta := range snapshot.Files {
		targetHash, exists := targetFiles[rel]
		if !exists {
			diff.Missing = append(diff.Missing, rel)
		} else if targetHash != meta.SHA256 {
			diff.Modified = append(diff.Modified, rel)
		} else {
			diff.Unchanged = append(diff.Unchanged, rel)
		}
	}

	// Check for extraneous files created during failed deployments
	for rel := range targetFiles {
		if _, inSnap := snapshot.Files[rel]; !inSnap {
			diff.Extraneous = append(diff.Extraneous, rel)
		}
	}

	return diff, nil
}

// RestoreSnapshot performs Git-like version control differential rollback.
// Only modifies files that changed, restores missing files, and prunes extraneous files created after snapshot.
func RestoreSnapshot(snapshotPath, outputDir string) error {
	snap, err := LoadSnapshot(snapshotPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	diff, err := ComputeDiff(snap, outputDir)
	if err != nil {
		return err
	}

	// 1. Delete extraneous files created during failed deployment
	for _, rel := range diff.Extraneous {
		targetPath := filepath.Join(outputDir, rel)
		_ = os.Remove(targetPath)
	}

	// 2. Restore modified & missing files
	toRestore := append(diff.Modified, diff.Missing...)
	for _, rel := range toRestore {
		meta, ok := snap.Files[rel]
		if !ok {
			continue
		}

		fileData, err := base64.StdEncoding.DecodeString(meta.ContentB64)
		if err != nil {
			return err
		}

		filePath := filepath.Join(outputDir, rel)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}

		mode := os.FileMode(meta.Mode)
		if mode == 0 {
			mode = 0644
		}

		if err := os.WriteFile(filePath, fileData, mode); err != nil {
			return err
		}
	}

	return nil
}

// GetLatestSnapshot finds and returns the path to the newest snapshot for the project.
func GetLatestSnapshot(project, snapshotDir string) (string, error) {
	resolvedDir := filepath.Join(resolvePath(snapshotDir), sanitizeFilename(project))
	matches, err := filepath.Glob(filepath.Join(resolvedDir, "*.json"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no snapshots found for project %q", project)
	}

	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// PruneSnapshots keeps only the last N snapshot files, deleting any older versions.
func PruneSnapshots(project, snapshotDir string, keepLast int) {
	if keepLast <= 0 {
		return
	}
	resolvedDir := filepath.Join(resolvePath(snapshotDir), sanitizeFilename(project))
	matches, err := filepath.Glob(filepath.Join(resolvedDir, "*.json"))
	if err != nil || len(matches) <= keepLast {
		return
	}

	sort.Strings(matches)
	toDelete := len(matches) - keepLast
	for i := 0; i < toDelete; i++ {
		_ = os.Remove(matches[i])
	}
}

func resolvePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func sanitizeFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}

func shouldSkip(relPath string) bool {
	parts := strings.Split(relPath, string(filepath.Separator))
	for _, p := range parts {
		if p == ".git" || p == "node_modules" || p == "vendor" || p == ".kzm" || p == ".codeforge" {
			return true
		}
	}
	return false
}
