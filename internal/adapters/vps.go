package adapters

import (
	"context"
	"fmt"
	"strings"

	"codeforge/internal/kzm"

	"github.com/pkg/sftp"
)

// VPSAdapter handles standard VPS deployment cycles using Git pulls, Rsync, or shell restarts.
type VPSAdapter struct{}

// NewVPSAdapter returns a new VPSAdapter instance.
func NewVPSAdapter() *VPSAdapter {
	return &VPSAdapter{}
}

// Deploy connects to the VPS remote server, runs pull commands, and triggers systemctl/pm2 restarts.
func (a *VPSAdapter) Deploy(ctx context.Context, target *kzm.DeployTarget, sourceDir string) error {
	user, host, err := parseSSHHost(target.Name)
	if err != nil {
		return err
	}

	keyPath := target.Options["key"]
	path := target.Options["path"]
	restartCmd := target.Options["restart"]
	gitPull := target.Options["git"]

	password := target.Options["password"]

	client, err := dialSSH(user, host, keyPath, password)
	if err != nil {
		return fmt.Errorf("failed to dial VPS host: %w", err)
	}
	defer client.Close()

	// 1. Run update logic (e.g. git pull in the path, or upload)
	if gitPull == "yes" || gitPull == "true" {
		if path == "" {
			return fmt.Errorf("VPS git pull deploy requires path option")
		}
		gitCmd := fmt.Sprintf("cd %s && git pull", path)
		err = runRemoteCmd(client, gitCmd)
		if err != nil {
			return fmt.Errorf("git pull failed on remote VPS: %w", err)
		}
	} else {
		// Fallback: SFTP upload if path is specified and not git-triggered
		if path != "" {
			sftpClient, err := sftp.NewClient(client)
			if err == nil {
				defer sftpClient.Close()
				err = uploadDirSFTP(ctx, sftpClient, sourceDir, path)
				if err != nil {
					return fmt.Errorf("SFTP upload failed on remote VPS: %w", err)
				}
			} else {
				return fmt.Errorf("failed to open SFTP session: %w", err)
			}
		}
	}

	// 2. Restart PM2 or Systemctl service
	if restartCmd != "" {
		err = runRemoteCmd(client, restartCmd)
		if err != nil {
			return fmt.Errorf("restart action failed on VPS: %w", err)
		}
	}

	return nil
}

// Rollback is standard (trigger deploy with snapshot path).
func (a *VPSAdapter) Rollback(ctx context.Context, target *kzm.DeployTarget, sourceDir string, snapshotPath string) error {
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

// Status checks SSH connectivity.
func (a *VPSAdapter) Status(ctx context.Context, target *kzm.DeployTarget) (string, error) {
	user, host, err := parseSSHHost(target.Name)
	if err != nil {
		return "ERROR", err
	}

	keyPath := target.Options["key"]
	password := target.Options["password"]
	client, err := dialSSH(user, host, keyPath, password)
	if err != nil {
		return "DISCONNECTED", nil
	}
	defer client.Close()

	// Check system load/uptime as status indicator
	session, err := client.NewSession()
	if err != nil {
		return "CONNECTED", nil
	}
	defer session.Close()

	out, err := session.CombinedOutput("uptime")
	if err != nil {
		return "CONNECTED", nil
	}

	return "ACTIVE - " + strings.TrimSpace(string(out)), nil
}
