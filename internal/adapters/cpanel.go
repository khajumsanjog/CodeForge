package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codeforge/internal/kzm"

	"github.com/pkg/sftp"
)

// CPanelAdapter deploys site files directly onto cPanel hosting directories.
type CPanelAdapter struct{}

// NewCPanelAdapter returns a new CPanelAdapter instance.
func NewCPanelAdapter() *CPanelAdapter {
	return &CPanelAdapter{}
}

// Deploy uploads directory structure to cPanel remote directory using SFTP.
func (a *CPanelAdapter) Deploy(ctx context.Context, target *kzm.DeployTarget, sourceDir string) error {
	host := target.Name
	user := target.Options["user"]
	if user == "" {
		return fmt.Errorf("cPanel deploy requires option 'user'")
	}

	destDir := target.Options["path"]
	if destDir == "" {
		return fmt.Errorf("cPanel deploy requires destination path (use 'at \"/public_html\"')")
	}

	// Read options for key and password
	keyPath := target.Options["key"]
	password := target.Options["password"]

	client, err := dialSSH(user, host, keyPath, password)
	if err != nil {
		return fmt.Errorf("failed to connect to cPanel host %s: %w", host, err)
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to start cPanel SFTP session: %w", err)
	}
	defer sftpClient.Close()

	// Parse exclude lists
	excludes := []string{}
	if exOption, ok := target.Options["exclude"]; ok && exOption != "" {
		excludes = strings.Split(exOption, ",")
	}

	// Upload directory filtering exclusions
	return uploadCPanelDir(ctx, sftpClient, sourceDir, destDir, excludes)
}

// Rollback restores cPanel workspace from local snapshots.
func (a *CPanelAdapter) Rollback(ctx context.Context, target *kzm.DeployTarget, sourceDir string, snapshotPath string) error {
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

// Status tests cPanel host connection state.
func (a *CPanelAdapter) Status(ctx context.Context, target *kzm.DeployTarget) (string, error) {
	user := target.Options["user"]
	if user == "" {
		return "ERROR", fmt.Errorf("missing user")
	}
	keyPath := target.Options["key"]
	password := target.Options["password"]
	client, err := dialSSH(user, target.Name, keyPath, password)
	if err != nil {
		return "DISCONNECTED", nil
	}
	client.Close()
	return "CONNECTED", nil
}

func uploadCPanelDir(ctx context.Context, client *sftp.Client, sourceDir, destDir string, excludes []string) error {
	destDir = filepath.ToSlash(destDir)
	_ = client.MkdirAll(destDir)

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Check global and target-specific exclusions
		if shouldSkip(rel) || isExcluded(rel, excludes) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		remotePath := filepath.ToSlash(filepath.Join(destDir, rel))

		if info.IsDir() {
			return client.MkdirAll(remotePath)
		}

		// File copy
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := client.Create(remotePath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func isExcluded(rel string, excludes []string) bool {
	for _, ex := range excludes {
		trimmed := strings.TrimSpace(ex)
		if trimmed == "" {
			continue
		}
		if strings.Contains(rel, trimmed) {
			return true
		}
	}
	return false
}
