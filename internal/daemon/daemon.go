package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"codeforge/internal/adapters"
	"codeforge/internal/executor"
	"codeforge/internal/kzm"
	"codeforge/internal/logger"
	"codeforge/internal/notifier"
	"codeforge/internal/secrets"
)

// Pipeline represents a managed deployment script instance.
type Pipeline struct {
	Path         string
	Program      *kzm.Program
	LastStatus   string // IDLE, RUNNING, SUCCESS, FAILED, ROLLBACK
	LastRun      time.Time
	LastDuration time.Duration
}

// Daemon coordinates workers, watch events, schedulers, and REST operations.
type Daemon struct {
	mu         sync.RWMutex
	pipelines  map[string]*Pipeline
	logger     *logger.Logger
	executor   *executor.Executor
	watcher    *Watcher
	scheduler  *Scheduler
	apiServer  *APIServer
	httpServer *http.Server

	// Config params
	apiPort int
	workers int
	cfgDir  string

	// Worker fields
	jobChan   chan string
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	activeRun map[string]bool // project -> isRunning
}

// NewDaemon creates a Daemon instance.
func NewDaemon(cfgDir string, apiPort int, workerCount int, l *logger.Logger) *Daemon {
	resolvedDir := secrets.ResolvePath(cfgDir)
	_ = os.MkdirAll(filepath.Join(resolvedDir, "pipelines"), 0755)

	d := &Daemon{
		pipelines: make(map[string]*Pipeline),
		logger:    l,
		executor:  executor.NewExecutor(l, ""),
		apiPort:   apiPort,
		workers:   workerCount,
		cfgDir:    resolvedDir,
		activeRun: make(map[string]bool),
	}

	return d
}

// Start loads pipeline configs, boots daemon workers, schedules, and starts the API port.
func (d *Daemon) Start() error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return nil
	}
	d.running = true
	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.jobChan = make(chan string, 100)
	d.mu.Unlock()

	d.logger.Log("daemon", "INFO", "Starting CodeForge Daemon...")

	// 1. Initialize Watcher and Scheduler
	var err error
	d.watcher, err = NewWatcher(d, d.logger)
	if err != nil {
		d.Stop()
		return fmt.Errorf("failed to init watcher: %w", err)
	}

	d.scheduler = NewScheduler(d, d.logger)

	// 2. Start Workers
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.workerLoop(i + 1)
	}

	// 3. Load pipelines config files
	pipelinesDir := filepath.Join(d.cfgDir, "pipelines")
	files, err := filepath.Glob(filepath.Join(pipelinesDir, "*.kzm"))
	if err == nil {
		for _, file := range files {
			d.ReloadPipeline(file)
		}
	}

	// 4. Start File Watcher on config directory
	err = d.watcher.WatchFolder(pipelinesDir)
	if err != nil {
		d.logger.Log("daemon", "WARNING", "Could not watch pipelines directory: %v", err)
	}
	d.watcher.Start(d.ctx)

	// 5. Start Scheduler
	d.scheduler.Start(d.ctx)

	// 6. Start API server
	d.apiServer = NewAPIServer(d)
	d.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", d.apiPort),
		Handler: d.apiServer,
	}

	// Write PID file
	pidPath := filepath.Join(d.cfgDir, "daemon.pid")
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)

	// Bind API port
	listener, err := net.Listen("tcp", d.httpServer.Addr)
	if err != nil {
		d.Stop()
		return fmt.Errorf("API listener failed: %w", err)
	}

	go func() {
		d.logger.Log("daemon", "INFO", "REST API listening on port %d", d.apiPort)
		if err := d.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			d.logger.Log("daemon", "ERROR", "API server failed: %v", err)
		}
	}()

	return nil
}

// Stop shuts down the API listener, watchers, timers, and waits for running worker threads (up to 30s).
func (d *Daemon) Stop() {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	d.mu.Unlock()

	d.logger.Log("daemon", "INFO", "Stopping CodeForge Daemon...")

	// Cancel context to stop scheduler and watcher routines
	if d.cancel != nil {
		d.cancel()
	}

	if d.watcher != nil {
		d.watcher.Stop()
	}

	// Shutdown REST API
	if d.httpServer != nil {
		shutdownCtx, apiCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer apiCancel()
		_ = d.httpServer.Shutdown(shutdownCtx)
	}

	// Close job channel to drain workers
	if d.jobChan != nil {
		close(d.jobChan)
	}

	// Wait with timeout (up to 30s)
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		d.logger.Log("daemon", "SUCCESS", "Daemon stopped successfully.")
	case <-time.After(30 * time.Second):
		d.logger.Log("daemon", "WARNING", "Graceful stop timed out. Forcing exit.")
	}

	// Remove PID file
	_ = os.Remove(filepath.Join(d.cfgDir, "daemon.pid"))
}

// Trigger puts a project name in the job channel to queue deployment execution.
func (d *Daemon) Trigger(project, reason string) error {
	d.mu.RLock()
	p, ok := d.pipelines[project]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("project %q not found", project)
	}

	d.logger.Log(project, "INFO", "Pipeline triggered: %s", reason)
	d.jobChan <- p.Program.Meta.Name
	return nil
}

// TriggerWithSHA triggers deployment with an associated git commit SHA.
func (d *Daemon) TriggerWithSHA(project, reason, sha string) {
	d.mu.Lock()
	p, ok := d.pipelines[project]
	if ok && p.Program != nil {
		// Embed commit SHA dynamically into variables or targets if needed
		// For now we set it as environment variable dynamically
		os.Setenv("CODEFORGE_COMMIT_SHA", sha)
	}
	d.mu.Unlock()

	_ = d.Trigger(project, reason+" [Commit: "+formatSHA(sha)+"]")
}

// TriggerRollback triggers direct rollback to the latest saved snapshot.
func (d *Daemon) TriggerRollback(project string) error {
	d.mu.RLock()
	p, ok := d.pipelines[project]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("project %q not found", project)
	}

	d.logger.Log(project, "WARNING", "Triggering manual rollback...")

	// We queue the rollback using executor in a separate background routine to avoid blocking
	go func() {
		defer func() {
			if r := recover(); r != nil {
				d.logger.Log(project, "ERROR", "Panic recovered in rollback: %v", r)
			}
		}()

		// Locate target
		target := p.Program.Deploy
		if target == nil {
			d.logger.Log(project, "ERROR", "Rollback failed: deploy target is not defined")
			return
		}

		res := executor.ExecutionResult{}
		
		// Find source workspace folder (parent of .kzm file location or config)
		sourceDir := filepath.Dir(p.Path)

		d.logger.Log(project, "INFO", "Rolling back to latest snapshot...")
		// Use local workspace dir or snapshot path
		// We call triggerRollback helper directly
		latestSnap, err := executor.GetLatestSnapshot(project, "~/.codeforge/snapshots")
		if err != nil {
			d.logger.Log(project, "ERROR", "Rollback failed: %v", err)
			return
		}

		tmpRestore, err := os.MkdirTemp("", "codeforge-restore-*")
		if err != nil {
			d.logger.Log(project, "ERROR", "Rollback failed: %v", err)
			return
		}
		defer os.RemoveAll(tmpRestore)

		err = executor.RestoreSnapshot(latestSnap, tmpRestore)
		if err != nil {
			d.logger.Log(project, "ERROR", "Rollback restore failed: %v", err)
			return
		}

		adapter, err := adapters.GetAdapter(target.Type)
		if err != nil {
			d.logger.Log(project, "ERROR", "Rollback load adapter failed: %v", err)
			return
		}

		d.mu.Lock()
		p.LastStatus = "RUNNING"
		d.mu.Unlock()

		err = adapter.Rollback(context.Background(), target, sourceDir, tmpRestore)
		
		d.mu.Lock()
		if err != nil {
			p.LastStatus = "FAILED"
			d.logger.Log(project, "ERROR", "Rollback execution failed: %v", err)
		} else {
			p.LastStatus = "ROLLBACK"
			d.logger.Log(project, "SUCCESS", "Rollback execution completed successfully.")
			res.RolledBack = true
		}
		p.LastRun = time.Now()
		d.mu.Unlock()

		// Send Slack/Email notifier
		d.dispatchNotifications(p.Program, "ROLLBACK", 0, "", res)
	}()

	return nil
}

// ReloadPipeline parses and updates the pipeline definition, registering schedule/folder triggers.
func (d *Daemon) ReloadPipeline(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		d.logger.Log("daemon", "ERROR", "Failed to read pipeline config %s: %v", path, err)
		return
	}

	lexer := kzm.NewLexer(string(data))
	tokens, err := lexer.Tokenize()
	if err != nil {
		d.logger.Log("daemon", "ERROR", "Lexical errors in config %s: %v", path, err)
		return
	}

	parser := kzm.NewParser(tokens)
	prog, err := parser.Parse()
	if err != nil {
		d.logger.Log("daemon", "ERROR", "Parse error in config %s: %v", path, err)
		return
	}

	valRes := kzm.Validate(prog)
	if len(valRes.Errors) > 0 {
		for _, e := range valRes.Errors {
			d.logger.Log("daemon", "ERROR", "Validation error in %s: %s (line %d)", path, e.Message, e.Line)
		}
		return
	}

	projectName := strings.TrimSpace(prog.Meta.Name)
	if projectName == "" {
		// Fallback to filename
		projectName = strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), ".kzm"))
	}
	// Always keep prog.Meta.Name clean
	prog.Meta.Name = projectName

	// Unregister old schedules first
	if d.scheduler != nil {
		d.scheduler.UnregisterPipelineSchedules(projectName)
	}

	// Register schedules
	cronExprs := []string{}
	for _, trig := range prog.Triggers {
		if trig.Source == "cron" {
			cronExprs = append(cronExprs, trig.Cron)
		}
	}
	if d.scheduler != nil && len(cronExprs) > 0 {
		d.scheduler.RegisterPipelineSchedules(projectName, cronExprs)
	}

	// Setup folder triggers
	for _, trig := range prog.Triggers {
		if trig.Source == "folder" && d.watcher != nil {
			_ = d.watcher.WatchFolder(trig.Path)
		}
	}

	// Save or update pipeline map
	d.pipelines[projectName] = &Pipeline{
		Path:       path,
		Program:    prog,
		LastStatus: "IDLE",
	}

	d.logger.Log("daemon", "SUCCESS", "Loaded pipeline %q successfully.", projectName)
}

func (d *Daemon) workerLoop(workerID int) {
	defer d.wg.Done()
	d.logger.Log("daemon", "INFO", "Worker #%d started.", workerID)

	for project := range d.jobChan {
		d.mu.Lock()
		p, ok := d.pipelines[project]
		d.mu.Unlock()

		if !ok {
			continue
		}

		// Avoid running same pipeline concurrently
		d.mu.Lock()
		if d.activeRun[project] {
			d.mu.Unlock()
			d.logger.Log(project, "WARNING", "Pipeline is already running. Skipping trigger.")
			continue
		}
		d.activeRun[project] = true
		p.LastStatus = "RUNNING"
		d.mu.Unlock()

		// Run pipeline
		d.runPipeline(p)

		d.mu.Lock()
		d.activeRun[project] = false
		d.mu.Unlock()
	}
	d.logger.Log("daemon", "INFO", "Worker #%d stopped.", workerID)
}

func (d *Daemon) runPipeline(p *Pipeline) {
	// Re-load pipeline config dynamically to ensure latest changes are applied
	data, err := os.ReadFile(p.Path)
	var prog *kzm.Program
	if err == nil {
		lexer := kzm.NewLexer(string(data))
		if tokens, err := lexer.Tokenize(); err == nil {
			parser := kzm.NewParser(tokens)
			if parsedProg, err := parser.Parse(); err == nil {
				prog = parsedProg
			}
		}
	}

	if prog == nil {
		prog = p.Program
	}

	// Resolve source workspace (clones GitHub repo if trigger github is set)
	sourceDir := d.resolveSourceWorkspace(p, prog)

	// Execute
	exec := executor.NewExecutor(d.logger, "")
	res := exec.Execute(d.ctx, prog, sourceDir)

	d.mu.Lock()
	p.LastRun = time.Now()
	p.LastDuration = res.Duration
	if res.Success {
		p.LastStatus = "SUCCESS"
	} else {
		if res.RolledBack {
			p.LastStatus = "ROLLBACK"
		} else {
			p.LastStatus = "FAILED"
		}
	}
	d.mu.Unlock()

	// 1. Dispatch notifications
	status := "SUCCESS"
	var errMsg string
	if !res.Success {
		if res.RolledBack {
			status = "ROLLBACK"
		} else {
			status = "FAILED"
		}
		if res.Error != nil {
			errMsg = res.Error.Error()
		}
	}

	d.dispatchNotifications(prog, status, res.Duration, errMsg, res)
}

func (d *Daemon) dispatchNotifications(prog *kzm.Program, status string, duration time.Duration, errMsg string, res executor.ExecutionResult) {
	// Prepare Notification payload
	commitSHA := os.Getenv("CODEFORGE_COMMIT_SHA")
	payload := notifier.Payload{
		Project:   prog.Meta.Name,
		Status:    status,
		Duration:  duration,
		Trigger:   "Daemon trigger",
		Timestamp: time.Now(),
		CommitSHA: commitSHA,
		ErrorMsg:  errMsg,
	}

	for _, n := range prog.Notifiers {
		switch strings.ToLower(n.Channel) {
		case "slack":
			var bearerToken, signingKey string
			if cfgData, err := os.ReadFile(filepath.Join(d.cfgDir, "settings.json")); err == nil {
				var savedCfg struct {
					SlackBearerToken string `json:"slack_bearer_token"`
					SlackSigningKey  string `json:"slack_signing_key"`
				}
				if json.Unmarshal(cfgData, &savedCfg) == nil {
					bearerToken = savedCfg.SlackBearerToken
					signingKey = savedCfg.SlackSigningKey
				}
			}
			sl := notifier.NewSlackNotifierWithAuth(n.Target, bearerToken, signingKey)
			err := sl.Send(payload)
			if err != nil {
				d.logger.Log(prog.Meta.Name, "WARNING", "Slack notification failed: %v", err)
			}
		case "email":
			// Load SMTP configs: check MAIL_FROM_ADDRESS / SMTP_FROM first, then settings.json override
			host := os.Getenv("SMTP_HOST")
			portStr := os.Getenv("SMTP_PORT")
			user := os.Getenv("SMTP_USER")
			pass := os.Getenv("SMTP_PASS")
			from := os.Getenv("MAIL_FROM_ADDRESS")
			if from == "" {
				from = os.Getenv("SMTP_FROM")
			}

			// Override with user's saved settings.json values (higher priority)
			if cfgData, err := os.ReadFile(filepath.Join(d.cfgDir, "settings.json")); err == nil {
				var savedCfg struct {
					SMTPHost       string `json:"smtp_host"`
					SMTPPort       int    `json:"smtp_port"`
					SMTPUser       string `json:"smtp_user"`
					SMTPPass       string `json:"smtp_pass"`
					EmailAddresses string `json:"email_addresses"`
					MailFrom       string `json:"mail_from"`
				}
				if json.Unmarshal(cfgData, &savedCfg) == nil {
					if savedCfg.SMTPHost != "" {
						host = savedCfg.SMTPHost
					}
					if savedCfg.SMTPPort > 0 {
						portStr = strconv.Itoa(savedCfg.SMTPPort)
					}
					if savedCfg.SMTPUser != "" {
						user = savedCfg.SMTPUser
					}
					if savedCfg.SMTPPass != "" {
						pass = savedCfg.SMTPPass
					}
					if savedCfg.MailFrom != "" {
						from = savedCfg.MailFrom
					}
				}
			}

			port, _ := strconv.Atoi(portStr)
			if port == 0 {
				port = 587 // standard default
			}

			if host != "" && user != "" {
				cfg := notifier.EmailConfig{
					Host:     host,
					Port:     port,
					User:     user,
					Password: pass,
					From:     from,
				}
				em := notifier.NewEmailNotifier(cfg, n.Target)
				err := em.Send(payload)
				if err != nil {
					d.logger.Log(prog.Meta.Name, "WARNING", "Email notification failed: %v", err)
				}
			} else {
				d.logger.Log(prog.Meta.Name, "WARNING", "SMTP settings not configured. Skipping email alert.")
			}
		}
	}
}

func formatSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func sanitizeFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}

func (d *Daemon) resolveSourceWorkspace(p *Pipeline, prog *kzm.Program) string {
	home, _ := os.UserHomeDir()
	projectName := strings.TrimSpace(prog.Meta.Name)
	if projectName == "" {
		projectName = strings.TrimSpace(strings.TrimSuffix(filepath.Base(p.Path), ".kzm"))
	}
	workspaceDir := filepath.Join(home, ".codeforge", "workspaces", sanitizeFilename(projectName))

	// 1. Check if a folder trigger specifies a custom local path
	for _, trig := range prog.Triggers {
		if trig.Source == "folder" && trig.Path != "" {
			return secrets.ResolvePath(trig.Path)
		}
	}

	// 2. Check if a GitHub or GitLab trigger is configured with a repository
	for _, trig := range prog.Triggers {
		if (trig.Source == "github" || trig.Source == "gitlab" || trig.Repo != "") && trig.Repo != "" {
			repoURL := trig.Repo
			if !strings.HasPrefix(repoURL, "http://") && !strings.HasPrefix(repoURL, "https://") && !strings.HasPrefix(repoURL, "git@") {
				repoURL = fmt.Sprintf("https://github.com/%s.git", strings.TrimPrefix(repoURL, "github.com/"))
			}

			_ = os.MkdirAll(filepath.Dir(workspaceDir), 0755)

			if _, err := os.Stat(filepath.Join(workspaceDir, ".git")); err == nil {
				d.logger.Log(projectName, "INFO", "Pulling latest git changes from %s...", repoURL)
				cmd := exec.Command("git", "-C", workspaceDir, "pull")
				_ = cmd.Run()
			} else {
				d.logger.Log(projectName, "INFO", "Cloning git repository %s into workspace...", repoURL)
				_ = os.RemoveAll(workspaceDir)
				var cmd *exec.Cmd
				if trig.Branch != "" {
					cmd = exec.Command("git", "clone", "-b", trig.Branch, repoURL, workspaceDir)
				} else {
					cmd = exec.Command("git", "clone", repoURL, workspaceDir)
				}
				if err := cmd.Run(); err != nil {
					d.logger.Log(projectName, "WARNING", "Git clone failed: %v", err)
					return workspaceDir
				}
			}

			// Read the HEAD commit SHA and expose it for notifications
			if shaOut, err := exec.Command("git", "-C", workspaceDir, "rev-parse", "HEAD").Output(); err == nil {
				sha := strings.TrimSpace(string(shaOut))
				os.Setenv("CODEFORGE_COMMIT_SHA", sha)
				d.logger.Log(projectName, "INFO", "Commit: %s", formatSHA(sha))
			}

			return workspaceDir
		}
	}

	// 3. Fallback: if p.Path is in ~/.codeforge/pipelines, isolate workspace to prevent deploying .kzm files
	cfgDir := filepath.Dir(p.Path)
	if strings.Contains(cfgDir, ".codeforge") && strings.HasSuffix(cfgDir, "pipelines") {
		_ = os.MkdirAll(workspaceDir, 0755)
		return workspaceDir
	}

	return cfgDir
}

// GetPipelines returns a copy of the active pipelines map.
func (d *Daemon) GetPipelines() map[string]*Pipeline {
	d.mu.RLock()
	defer d.mu.RUnlock()

	res := make(map[string]*Pipeline)
	for k, v := range d.pipelines {
		res[k] = v
	}
	return res
}

// RemovePipeline unregisters a pipeline.
func (d *Daemon) RemovePipeline(project string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.pipelines, project)
}
