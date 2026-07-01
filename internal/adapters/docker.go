package adapters

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"codeforge/internal/kzm"

	"golang.org/x/crypto/ssh"
)

// DockerAdapter manages building, pushing, and deploying Docker images on remote hosts.
type DockerAdapter struct{}

// NewDockerAdapter returns a new DockerAdapter instance.
func NewDockerAdapter() *DockerAdapter {
	return &DockerAdapter{}
}

// Deploy builds the Docker image locally, pushes it, pulls it remote, and restarts the container.
func (a *DockerAdapter) Deploy(ctx context.Context, target *kzm.DeployTarget, sourceDir string) error {
	image := target.Options["image"]
	if image == "" {
		return fmt.Errorf("Docker deploy target requires option 'image'")
	}
	server := target.Options["server"]
	if server == "" {
		return fmt.Errorf("Docker deploy target requires option 'server' (user@host)")
	}

	port := target.Options["port"]
	restart := target.Options["restart"]
	if restart == "" {
		restart = "always"
	}

	// 1. Locally build image
	buildCmd := exec.CommandContext(ctx, "docker", "build", "-t", image, sourceDir)
	if output, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("local docker build failed: %v, output: %s", err, string(output))
	}

	// 2. Locally push image
	pushCmd := exec.CommandContext(ctx, "docker", "push", image)
	if output, err := pushCmd.CombinedOutput(); err != nil {
		// Log warning but continue in case they are deploying locally or push is not needed
		fmt.Printf("Warning: docker push failed (continuing in case local/cache is used): %v, output: %s\n", err, string(output))
	}

	// 3. Remote deploy via SSH
	user, host, err := parseSSHHost(server)
	if err != nil {
		return err
	}

	keyPath := target.Options["key"]
	password := target.Options["password"]
	client, err := dialSSH(user, host, keyPath, password)
	if err != nil {
		return fmt.Errorf("failed to dial remote server: %w", err)
	}
	defer client.Close()

	// Run pull command
	err = runRemoteCmd(client, fmt.Sprintf("docker pull %s", image))
	if err != nil {
		return fmt.Errorf("remote docker pull failed: %w", err)
	}

	// Stop & remove existing container
	containerName := target.Name
	_ = runRemoteCmd(client, fmt.Sprintf("docker stop %s", containerName))
	_ = runRemoteCmd(client, fmt.Sprintf("docker rm %s", containerName))

	// Start new container
	runCmd := fmt.Sprintf("docker run -d --name %s", containerName)
	if port != "" {
		runCmd += fmt.Sprintf(" -p %s:%s", port, port)
	}
	if restart != "" {
		runCmd += fmt.Sprintf(" --restart %s", restart)
	}
	// Append environment variables
	for k, v := range target.EnvVars {
		runCmd += fmt.Sprintf(" -e %s=%q", k, v)
	}
	runCmd += fmt.Sprintf(" %s", image)

	err = runRemoteCmd(client, runCmd)
	if err != nil {
		return fmt.Errorf("remote docker run failed: %w", err)
	}

	return nil
}

// Rollback is standard (for Docker we just redeploy the rollback version or image).
func (a *DockerAdapter) Rollback(ctx context.Context, target *kzm.DeployTarget, sourceDir string, snapshotPath string) error {
	// Redepoly from local snapshot
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

// Status checks remote connection and if the container is running.
func (a *DockerAdapter) Status(ctx context.Context, target *kzm.DeployTarget) (string, error) {
	server := target.Options["server"]
	if server == "" {
		return "ERROR", fmt.Errorf("missing server")
	}
	user, host, err := parseSSHHost(server)
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

	// Check if container is running
	session, err := client.NewSession()
	if err != nil {
		return "CONNECTED", nil
	}
	defer session.Close()

	cmd := fmt.Sprintf("docker inspect -f '{{.State.Status}}' %s", target.Name)
	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return "STOPPED", nil
	}

	status := strings.TrimSpace(string(output))
	return strings.ToUpper(status), nil
}

func runRemoteCmd(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(cmd)
}
