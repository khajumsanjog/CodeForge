package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"codeforge/internal/adapters"
	"codeforge/internal/kzm"
	"codeforge/internal/logger"
	"codeforge/internal/progress"
	"codeforge/internal/secrets"
)

// StepResult stores execution statistics and stdout/stderr output of an individual step.
type StepResult struct {
	Command  string        `json:"command"`
	Success  bool          `json:"success"`
	Output   string        `json:"output"`
	Duration time.Duration `json:"duration"`
}

// ExecutionResult aggregates the outcomes of all pipeline stages.
type ExecutionResult struct {
	Success    bool          `json:"success"`
	Steps      []StepResult  `json:"steps"`
	Duration   time.Duration `json:"duration"`
	Error      error         `json:"error"`
	RolledBack bool          `json:"rolled_back"`
}

// Executor orchestrates execution of KZM files.
type Executor struct {
	logger *logger.Logger
	env    string // active run environment override (e.g. "production", "staging")
}

// NewExecutor creates a new Executor instance.
func NewExecutor(l *logger.Logger, env string) *Executor {
	return &Executor{
		logger: l,
		env:    env,
	}
}

// Execute runs the full pipeline sequence described in the program AST.
func (e *Executor) Execute(ctx context.Context, prog *kzm.Program, sourceDir string) ExecutionResult {
	start := time.Now()
	res := ExecutionResult{
		Success: true,
		Steps:   []StepResult{},
	}

	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			e.logger.LogException(prog.Meta.Name, r, stack)
			res.Success = false
			res.Error = fmt.Errorf("panic exception: %v", r)
		}
	}()

	e.logger.Log(prog.Meta.Name, "INFO", "Starting deployment pipeline for %q (v%s)...", prog.Meta.Name, prog.Meta.Version)

	// Step 1: Setup - Load Secrets and Variables
	secMap := make(map[string]string)
	if prog.Secrets != nil {
		secPath := secrets.ResolvePath(prog.Secrets.Path)
		secStore, err := secrets.LoadStore(secPath)
		if err != nil {
			res.Success = false
			res.Error = fmt.Errorf("failed to load secret store: %w", err)
			return res
		}
		for _, key := range secStore.List() {
			val, _ := secStore.Get(key)
			secMap[key] = val
		}
	}

	varMap := make(map[string]string)
	for _, v := range prog.Variables {
		varMap[v.Key] = v.Value
	}

	// Active Env Target selection
	activeTarget := prog.Deploy
	if e.env != "" {
		for _, envObj := range prog.Environments {
			if strings.EqualFold(envObj.Name, e.env) {
				activeTarget = envObj.Target
				e.logger.Log(prog.Meta.Name, "INFO", "Using environment override target: %q", e.env)
				break
			}
		}
	}

	// 2. Run Before Deploy Phase
	if prog.Before != nil {
		e.logger.Log(prog.Meta.Name, "INFO", "Running before-deploy phase...")
		err := e.runPhase(ctx, prog.Before, sourceDir, varMap, secMap, &res)
		if err != nil {
			e.logger.Log(prog.Meta.Name, "ERROR", "Before-deploy phase failed: %v", err)
			res.Success = false
			res.Error = err
			if shouldRollback(prog.Before) {
				e.triggerRollback(ctx, prog, activeTarget, sourceDir, &res)
			}
			res.Duration = time.Since(start)
			return res
		}
	}

	// 3. Take Snapshot
	snapshotDir := "~/.codeforge/snapshots"
	if activeTarget != nil {
		e.logger.Log(prog.Meta.Name, "INFO", "Creating deployment snapshot...")
		snapshotPath, err := SaveSnapshot(prog.Meta.Name, sourceDir, snapshotDir)
		if err != nil {
			e.logger.Log(prog.Meta.Name, "WARNING", "Failed to create snapshot: %v", err)
		} else {
			e.logger.Log(prog.Meta.Name, "INFO", "Deployment snapshot created: %s", snapshotPath)
			// Prune old snapshots
			PruneSnapshots(prog.Meta.Name, snapshotDir, prog.KeepLast)
		}
	}

	// 4. Deploy Target execution
	if activeTarget != nil {
		e.logger.Log(prog.Meta.Name, "INFO", "Deploying to %s target %q...", activeTarget.Type, activeTarget.Name)

		// Resolve options / envs
		resolvedTarget := resolveTargetMetadata(activeTarget, varMap, secMap)

		// Instanciate target adapter
		adapter, err := adapters.GetAdapter(resolvedTarget.Type)
		if err != nil {
			res.Success = false
			res.Error = fmt.Errorf("failed to get target adapter: %w", err)
			e.logger.Log(prog.Meta.Name, "ERROR", "Deployment target adapter error: %v", res.Error)
			e.triggerRollback(ctx, prog, activeTarget, sourceDir, &res)
			res.Duration = time.Since(start)
			return res
		}

		// Set target env variables in the process env context
		for k, v := range resolvedTarget.EnvVars {
			os.Setenv(k, v)
			defer os.Unsetenv(k)
		}

		// Calculate files & size for progress bar
		var totalFiles, totalBytes int64
		_ = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && !shouldSkip(path) {
				totalFiles++
				totalBytes += info.Size()
			}
			return nil
		})

		tracker := progress.NewTracker(totalFiles, totalBytes)
		var lastProgressLog time.Time
		tracker.RegisterCallback(func(snap progress.Snapshot) {
			if time.Since(lastProgressLog) > 400*time.Millisecond || snap.Percentage >= 100 {
				lastProgressLog = time.Now()
				bar := progress.RenderCLIProgressBar(snap, 24)
				e.logger.Log(prog.Meta.Name, "INFO", "Transfer Progress: %s", bar)
			}
		})
		deployCtx := progress.WithTracker(ctx, tracker)

		// Perform Deploy
		deployStart := time.Now()
		err = adapter.Deploy(deployCtx, resolvedTarget, sourceDir)
		deployDur := time.Since(deployStart)

		stepRes := StepResult{
			Command:  fmt.Sprintf("deploy to %s %q", resolvedTarget.Type, resolvedTarget.Name),
			Duration: deployDur,
		}

		if err != nil {
			stepRes.Success = false
			stepRes.Output = err.Error()
			res.Steps = append(res.Steps, stepRes)
			res.Success = false
			res.Error = fmt.Errorf("deploy failed: %w", err)

			e.logger.Log(prog.Meta.Name, "ERROR", "Deployment failed: %v. Initiating rollback...", err)
			e.triggerRollback(ctx, prog, activeTarget, sourceDir, &res)
			res.Duration = time.Since(start)
			return res
		}

		stepRes.Success = true
		stepRes.Output = "Deployment successful"
		res.Steps = append(res.Steps, stepRes)
		e.logger.Log(prog.Meta.Name, "SUCCESS", "Deployment successfully sent to %s target %q.", resolvedTarget.Type, resolvedTarget.Name)
	}

	// 5. Run After Deploy Phase
	if prog.After != nil {
		e.logger.Log(prog.Meta.Name, "INFO", "Running after-deploy phase...")
		err := e.runPhase(ctx, prog.After, sourceDir, varMap, secMap, &res)
		if err != nil {
			e.logger.Log(prog.Meta.Name, "ERROR", "After-deploy phase failed: %v", err)
			res.Success = false
			res.Error = err
			if shouldRollback(prog.After) {
				e.triggerRollback(ctx, prog, activeTarget, sourceDir, &res)
			}
			res.Duration = time.Since(start)
			return res
		}
	}

	e.logger.Log(prog.Meta.Name, "SUCCESS", "Deployment pipeline finished successfully in %s.", time.Since(start).Round(time.Millisecond))
	res.Duration = time.Since(start)
	return res
}

func (e *Executor) runPhase(ctx context.Context, phase *kzm.Phase, sourceDir string, vars, secs map[string]string, res *ExecutionResult) error {
	for _, step := range phase.Steps {
		// Environment condition check
		if step.Condition != nil {
			activeEnvName := e.env
			if activeEnvName == "" {
				activeEnvName = "production" // default env context
			}
			if !strings.EqualFold(activeEnvName, step.Condition.EnvValue) {
				e.logger.Log("executor", "INFO", "Skipping step (env condition does not match %q, active: %q)", step.Condition.EnvValue, activeEnvName)
				continue
			}
		}

		stepStart := time.Now()
		var stepErr error
		var stepOut string

		if strings.HasPrefix(step.Command, "run: ") {
			rawCmd := strings.TrimPrefix(step.Command, "run: ")
			stepOut, stepErr = e.runShellCommand(ctx, rawCmd, sourceDir, vars, secs)
		} else if strings.HasPrefix(step.Command, "copy: ") {
			rawCmd := strings.TrimPrefix(step.Command, "copy: ")
			stepErr = e.runCopyCommand(rawCmd, vars, secs)
			if stepErr == nil {
				stepOut = "Files copied successfully"
			}
		} else if strings.HasPrefix(step.Command, "set: ") {
			rawCmd := strings.TrimPrefix(step.Command, "set: ")
			stepErr = e.runSetCommand(rawCmd, vars, secs)
			if stepErr == nil {
				stepOut = "Environment variables configured"
			}
		} else if strings.HasPrefix(step.Command, "plugin: ") {
			rawCmd := strings.TrimPrefix(step.Command, "plugin: ")
			stepOut, stepErr = e.runPluginCommand(ctx, rawCmd)
		} else {
			stepErr = fmt.Errorf("unknown command format: %s", step.Command)
		}

		stepDur := time.Since(stepStart)
		displayCmd := redactSecrets(step.Command, secs)

		stepRes := StepResult{
			Command:  displayCmd,
			Success:  stepErr == nil,
			Output:   stepOut,
			Duration: stepDur,
		}
		res.Steps = append(res.Steps, stepRes)

		if stepErr != nil {
			if stepOut != "" {
				e.logger.Log("executor", "ERROR", "Step %q output:\n%s", displayCmd, stepOut)
			}
			if step.MustPass {
				return fmt.Errorf("critical step %q failed: %w", displayCmd, stepErr)
			}
			e.logger.Log("executor", "WARNING", "Non-critical step %q failed (continuing): %v", displayCmd, stepErr)
		} else {
			e.logger.Log("executor", "INFO", "Step %q completed successfully.", displayCmd)
		}
	}
	return nil
}

func (e *Executor) runShellCommand(ctx context.Context, cmdStr string, sourceDir string, vars, secs map[string]string) (string, error) {
	substituted := substituteVars(cmdStr, vars, secs)

	// Create command execution context with a 5-minute timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var shell, flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/C"
	} else {
		shell = "sh"
		flag = "-c"
	}

	cmd := exec.CommandContext(timeoutCtx, shell, flag, substituted)
	cmd.Dir = sourceDir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	output := outBuf.String() + errBuf.String()
	if err != nil {
		return output, fmt.Errorf("command execution failed: %v", err)
	}

	return output, nil
}

func (e *Executor) runCopyCommand(cmdStr string, vars, secs map[string]string) error {
	// Format: "src" to "dest"
	parts := strings.Split(cmdStr, " to ")
	if len(parts) != 2 {
		return fmt.Errorf("invalid copy syntax (expected 'copy \"src\" to \"dest\"')")
	}

	src := strings.Trim(parts[0], "\" ")
	dst := strings.Trim(parts[1], "\" ")

	src = substituteVars(src, vars, secs)
	dst = substituteVars(dst, vars, secs)

	src = resolvePath(src)
	dst = resolvePath(dst)

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return filepath.Walk(src, func(path string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			targetPath := filepath.Join(dst, rel)
			if fileInfo.IsDir() {
				return os.MkdirAll(targetPath, fileInfo.Mode())
			}
			return copyFile(path, targetPath)
		})
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return copyFile(src, dst)
}

func (e *Executor) runSetCommand(cmdStr string, vars, secs map[string]string) error {
	// Format: KEY = VALUE
	parts := strings.Split(cmdStr, "=")
	if len(parts) != 2 {
		return fmt.Errorf("invalid set syntax (expected 'set KEY = \"VALUE\"')")
	}

	key := strings.TrimSpace(parts[0])
	val := strings.Trim(strings.TrimSpace(parts[1]), "\"")

	val = substituteVars(val, vars, secs)
	return os.Setenv(key, val)
}

func (e *Executor) runPluginCommand(ctx context.Context, pluginName string) (string, error) {
	// Standard plugins stub
	return fmt.Sprintf("Plugin %s run complete (stub)", pluginName), nil
}

func (e *Executor) triggerRollback(ctx context.Context, prog *kzm.Program, target *kzm.DeployTarget, sourceDir string, res *ExecutionResult) {
	if target == nil {
		return
	}
	e.logger.Log(prog.Meta.Name, "WARNING", "Initiating rollback procedure for %q...", prog.Meta.Name)

	snapshotDir := "~/.codeforge/snapshots"
	latestSnap, err := GetLatestSnapshot(prog.Meta.Name, snapshotDir)
	if err != nil {
		e.logger.Log(prog.Meta.Name, "ERROR", "Rollback failed: no snapshot available: %v", err)
		return
	}

	e.logger.Log(prog.Meta.Name, "INFO", "Restoring files from snapshot %s...", filepath.Base(latestSnap))
	if snapObj, snapErr := LoadSnapshot(latestSnap); snapErr == nil {
		if diff, diffErr := ComputeDiff(snapObj, sourceDir); diffErr == nil {
			e.logger.Log(prog.Meta.Name, "INFO", "Differential Rollback Analysis: %s", diff.Summary())
		}
	}
	tmpRestore, err := os.MkdirTemp("", "codeforge-restore-*")
	if err != nil {
		e.logger.Log(prog.Meta.Name, "ERROR", "Failed to create temp restore directory: %v", err)
		return
	}
	defer os.RemoveAll(tmpRestore)

	err = RestoreSnapshot(latestSnap, tmpRestore)
	if err != nil {
		e.logger.Log(prog.Meta.Name, "ERROR", "Failed to restore snapshot: %v", err)
		return
	}

	adapter, err := adapters.GetAdapter(target.Type)
	if err != nil {
		e.logger.Log(prog.Meta.Name, "ERROR", "Rollback failed to load target adapter: %v", err)
		return
	}

	err = adapter.Rollback(ctx, target, sourceDir, tmpRestore)
	if err != nil {
		e.logger.Log(prog.Meta.Name, "ERROR", "Rollback deploy action failed: %v", err)
		return
	}

	res.RolledBack = true
	e.logger.Log(prog.Meta.Name, "SUCCESS", "Rollback successfully completed.")
}

func substituteVars(input string, vars, secs map[string]string) string {
	// Substitute secrets first: $secret.KEY
	for k, v := range secs {
		input = strings.ReplaceAll(input, "$secret."+k, v)
	}
	// Substitute variables: $VAR
	for k, v := range vars {
		input = strings.ReplaceAll(input, "$"+k, v)
	}
	return input
}

func redactSecrets(input string, secs map[string]string) string {
	// Replaces decrypted secret values with [SECRET] for displays
	// Or we can just keep $secret.KEY untouched. Since substituteVars replaces it,
	// keeping it as $secret.KEY in displays is exactly what we want!
	// We only replace if raw values of secrets are found in the string.
	for k, v := range secs {
		if v != "" {
			input = strings.ReplaceAll(input, v, fmt.Sprintf("[SECRET.%s]", k))
		}
	}
	return input
}

func shouldRollback(phase *kzm.Phase) bool {
	for _, step := range phase.Steps {
		if step.OrRollback {
			return true
		}
	}
	return false
}

func resolveTargetMetadata(target *kzm.DeployTarget, vars, secs map[string]string) *kzm.DeployTarget {
	resTarget := &kzm.DeployTarget{
		Type:    target.Type,
		Name:    substituteVars(target.Name, vars, secs),
		Options: make(map[string]string),
		EnvVars: make(map[string]string),
		Line:    target.Line,
	}

	for k, v := range target.Options {
		resTarget.Options[k] = substituteVars(v, vars, secs)
	}
	for k, v := range target.EnvVars {
		resTarget.EnvVars[k] = substituteVars(v, vars, secs)
	}

	return resTarget
}

func copyFile(src, dst string) error {
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

	_, err = io.Copy(out, in)
	return err
}
