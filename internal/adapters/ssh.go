package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeforge/internal/kzm"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHAdapter manages secure SSH/SFTP connections and deployments to remote servers.
type SSHAdapter struct{}

// NewSSHAdapter returns a new SSHAdapter instance.
func NewSSHAdapter() *SSHAdapter {
	return &SSHAdapter{}
}

// Deploy establishes an SFTP session and uploads workspace files to the remote server path, then triggers a restart command.
func (a *SSHAdapter) Deploy(ctx context.Context, target *kzm.DeployTarget, sourceDir string) error {
	user, host, err := parseSSHHost(target.Name)
	if err != nil {
		return err
	}

	keyPath := target.Options["key"]
	destDir := target.Options["path"]
	if destDir == "" {
		return fmt.Errorf("SSH deploy target requires a destination path (use 'at \"/path\"')")
	}

	password := target.Options["password"]

	client, err := dialSSH(user, host, keyPath, password)
	if err != nil {
		return err
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to open SFTP session: %w", err)
	}
	defer sftpClient.Close()

	// Upload directory
	err = uploadDirSFTP(ctx, sftpClient, sourceDir, destDir)
	if err != nil {
		return fmt.Errorf("SFTP upload failed: %w", err)
	}

	// Run optional restart command
	if restartCmd, ok := target.Options["restart"]; ok && restartCmd != "" {
		session, err := client.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create SSH session for restart: %w", err)
		}
		defer session.Close()

		output, err := session.CombinedOutput(restartCmd)
		if err != nil {
			return fmt.Errorf("restart command failed: %v, output: %s", err, string(output))
		}
	}

	return nil
}

// Rollback uploads from a snapshot path.
func (a *SSHAdapter) Rollback(ctx context.Context, target *kzm.DeployTarget, sourceDir string, snapshotPath string) error {
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

// Status checks if the SSH port is open and can be connected.
func (a *SSHAdapter) Status(ctx context.Context, target *kzm.DeployTarget) (string, error) {
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
	client.Close()
	return "CONNECTED", nil
}

func parseSSHHost(name string) (string, string, error) {
	parts := strings.Split(name, "@")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid SSH target address %q (expected format user@host)", name)
	}
	return parts[0], parts[1], nil
}

func dialSSH(user, host, keyPath, password string) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	// Read provided key path or look at standard defaults
	keyPaths := []string{}
	if keyPath != "" {
		keyPaths = append(keyPaths, resolvePath(keyPath))
	} else if password == "" {
		// Default directories (only if password is not provided)
		home, err := os.UserHomeDir()
		if err == nil {
			keyPaths = append(keyPaths, filepath.Join(home, ".ssh", "id_rsa"))
			keyPaths = append(keyPaths, filepath.Join(home, ".ssh", "id_ed25519"))
		}
	}

	var signers []ssh.Signer
	for _, kp := range keyPaths {
		data, err := os.ReadFile(kp)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err == nil {
			signers = append(signers, signer)
		}
	}

	if len(signers) > 0 {
		authMethods = append(authMethods, ssh.PublicKeys(signers...))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no valid SSH private keys or password provided")
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if !strings.Contains(host, ":") {
		host = host + ":22"
	}

	return ssh.Dial("tcp", host, config)
}

func uploadDirSFTP(ctx context.Context, client *sftp.Client, sourceDir, destDir string) error {
	// Ensure destination directory exists remote
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

		if shouldSkip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		remotePath := filepath.ToSlash(filepath.Join(destDir, rel))

		if info.IsDir() {
			return client.MkdirAll(remotePath)
		}

		// Upload file
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
