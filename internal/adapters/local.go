package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codeforge/internal/kzm"
	"codeforge/internal/progress"
)

// LocalAdapter manages deployments onto the local filesystem of the CI/CD server.
type LocalAdapter struct{}

// NewLocalAdapter returns a new LocalAdapter instance.
func NewLocalAdapter() *LocalAdapter {
	return &LocalAdapter{}
}

// Deploy copies files recursively from the source directory to the destination path specified in targets options.
func (a *LocalAdapter) Deploy(ctx context.Context, target *kzm.DeployTarget, sourceDir string) error {
	dest := strings.TrimSpace(target.Options["path"])
	if dest == "" {
		dest = strings.TrimSpace(target.Name)
	}
	if dest == "" {
		return fmt.Errorf("local deploy target requires a destination path")
	}

	// Log resolved source and destination clearly
	dest = resolvePath(dest)

	// Validate sourceDir actually has files to deploy
	if _, err := os.Stat(sourceDir); err != nil {
		return fmt.Errorf("source directory does not exist: %s", sourceDir)
	}

	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create destination dir: %w", err)
	}

	tracker := progress.GetTracker(ctx)

	copiedCount := 0
	// Copy files recursively from sourceDir to dest
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip VCS directories and dependencies
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		if tracker != nil {
			tracker.StartFile(rel)
		}

		err = copyFileWithTracker(path, targetPath, tracker)

		if tracker != nil {
			tracker.CompleteFile()
		}
		if err == nil {
			copiedCount++
		}
		return err
	})

	if err == nil && copiedCount == 0 {
		return fmt.Errorf("deployment completed but no files were copied from %s — check that the source directory contains files", sourceDir)
	}

	return err
}

// Rollback restores the files using a snapshot path.
func (a *LocalAdapter) Rollback(ctx context.Context, target *kzm.DeployTarget, sourceDir string, snapshotPath string) error {
	// For local rollback, we can simply Deploy from the snapshot directory
	if snapshotPath == "" {
		return fmt.Errorf("rollback requires a valid snapshot path")
	}
	mockTarget := &kzm.DeployTarget{
		Type:    target.Type,
		Name:    target.Name,
		Options: target.Options,
		Line:    target.Line,
	}
	return a.Deploy(ctx, mockTarget, snapshotPath)
}

// Status returns the status of the local destination folder.
func (a *LocalAdapter) Status(ctx context.Context, target *kzm.DeployTarget) (string, error) {
	dest := target.Options["path"]
	if dest == "" {
		dest = target.Name
	}
	dest = resolvePath(dest)
	if _, err := os.Stat(dest); err != nil {
		return "INACTIVE (Path not found)", nil
	}
	return "ACTIVE", nil
}

func copyFile(src, dst string) error {
	return copyFileWithTracker(src, dst, nil)
}

func copyFileWithTracker(src, dst string, tracker *progress.Tracker) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	var r io.Reader = in
	if tracker != nil {
		r = progress.NewProgressReader(in, tracker)
	}

	_, err = io.Copy(out, r)
	return err
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

func resolvePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
