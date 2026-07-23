package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"codeforge/internal/daemon"
	"codeforge/internal/logger"
	"codeforge/internal/progress"
)

// showPipelineDetailScreen swaps the main view with a detailed tabbed window for the selected pipeline.
func (a *CodeForgeApp) showPipelineDetailScreen(project string) {
	a.FyneApp.Settings().Theme() // avoid warning
	p, ok := a.Daemon.GetPipelines()[project]

	if !ok {
		dialog.ShowError(fmt.Errorf("project %q not found", project), a.MainWindow)
		return
	}

	// Header Info
	title := widget.NewLabel(project)
	title.TextStyle = fyne.TextStyle{Bold: true}

	statusBadge := widget.NewLabel("Status: " + p.LastStatus)

	runNowBtn := widget.NewButton("Run Now", func() {
		_ = a.Daemon.Trigger(project, "manual trigger from detail screen")
		a.showPipelineDetailScreen(project) // refresh
	})
	rollbackBtn := widget.NewButton("Rollback", func() {
		_ = a.Daemon.TriggerRollback(project)
		a.showPipelineDetailScreen(project) // refresh
	})
	deleteBtn := widget.NewButton("Delete", func() {
		a.confirmDeletePipeline(project, p.Path)
	})
	deleteBtn.Importance = widget.DangerImportance

	buttons := container.NewHBox(runNowBtn, rollbackBtn, deleteBtn)
	header := container.NewBorder(nil, nil, statusBadge, buttons, title)

	// Tab 1: Overview
	overviewTab := a.buildOverviewTab(p)

	// Tab 2: Run History
	historyTab := a.buildHistoryTab(project)

	// Tab 3: Config Editor
	configTab := a.buildConfigTab(p)

	tabs := container.NewAppTabs(
		container.NewTabItem("Overview", overviewTab),
		container.NewTabItem("Run History", historyTab),
		container.NewTabItem("Config (.kzm)", configTab),
	)

	detailLayout := container.NewBorder(header, nil, nil, nil, tabs)
	a.ContentArea.Objects = []fyne.CanvasObject{detailLayout}
	a.ContentArea.Refresh()
}

func (a *CodeForgeApp) buildOverviewTab(p *daemon.Pipeline) fyne.CanvasObject {
	grid := container.NewVBox()

	// Live transfer progress bar if running
	if tracker := progress.GetGlobalTracker(p.Program.Meta.Name); tracker != nil {
		snap := tracker.GetSnapshot()
		projTitle := widget.NewLabel("⚡ Live Execution Progress")
		projTitle.TextStyle = fyne.TextStyle{Bold: true}

		bar := widget.NewProgressBar()
		bar.SetValue(snap.Percentage / 100.0)

		speedStr := progress.FormatBytes(int64(snap.SpeedBytesPerSec)) + "/s"
		detailsStr := fmt.Sprintf("%d/%d files  •  %s / %s  •  @ %s",
			snap.TransferredFiles, snap.TotalFiles,
			progress.FormatBytes(snap.TransferredBytes), progress.FormatBytes(snap.TotalBytes),
			speedStr)

		detailsLbl := widget.NewLabel(detailsStr)
		fileLbl := widget.NewLabel(fmt.Sprintf("File: %s", snap.CurrentFile))
		fileLbl.TextStyle = fyne.TextStyle{Italic: true}

		progressCard := widget.NewCard("Live Transfer Progress", "", container.NewVBox(
			projTitle,
			bar,
			detailsLbl,
			fileLbl,
		))
		grid.Add(progressCard)
		grid.Add(widget.NewSeparator())
	}

	// Triggers
	grid.Add(widget.NewLabel("Triggers:"))
	for _, trig := range p.Program.Triggers {
		var desc string
		if trig.Source == "github" || trig.Source == "gitlab" {
			desc = fmt.Sprintf("  ● %s push: %s (branch %s)", trig.Source, trig.Repo, trig.Branch)
		} else if trig.Source == "folder" {
			desc = fmt.Sprintf("  ● folder watch: %s", trig.Path)
		} else if trig.Source == "cron" {
			desc = fmt.Sprintf("  ● cron scheduled: %s", trig.Cron)
		} else {
			desc = "  ● manual trigger"
		}
		grid.Add(widget.NewLabel(desc))
	}

	// Deploy Target
	if p.Program.Deploy != nil {
		grid.Add(widget.NewLabel("\nDeploy Target:"))
		grid.Add(widget.NewLabel(fmt.Sprintf("  ● Destination: %s (%q)", p.Program.Deploy.Type, p.Program.Deploy.Name)))
		for k, v := range p.Program.Deploy.Options {
			grid.Add(widget.NewLabel(fmt.Sprintf("    - %s: %s", k, v)))
		}
	}

	// Steps
	if p.Program.Before != nil {
		grid.Add(widget.NewLabel("\nBefore Deploy Steps:"))
		for _, step := range p.Program.Before.Steps {
			grid.Add(widget.NewLabel("  - " + step.Command))
		}
	}

	if p.Program.After != nil {
		grid.Add(widget.NewLabel("\nAfter Deploy Steps:"))
		for _, step := range p.Program.After.Steps {
			grid.Add(widget.NewLabel("  - " + step.Command))
		}
	}

	return container.NewScroll(grid)
}

type ParsedRun struct {
	Index     int
	Status    string
	Time      time.Time
	Duration  string
	TriggerBy string
	Logs      []string
}

func (a *CodeForgeApp) buildHistoryTab(project string) fyne.CanvasObject {
	listContainer := container.NewVBox()

	// Query project logs
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".codeforge", "logs")
	l := logger.NewLogger(logDir)

	files, err := l.GetLogFilesForProject(project)
	if err != nil || len(files) == 0 {
		return widget.NewLabel("No execution history available.")
	}

	runs := []ParsedRun{}
	runIndex := 1

	// Parse log files chronologically to count runs
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		var currentRun *ParsedRun

		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}

			var entry logger.LogEntry
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				if strings.Contains(entry.Message, "Starting deployment pipeline") {
					if currentRun != nil {
						runs = append(runs, *currentRun)
					}
					t, _ := time.Parse(time.RFC3339, entry.Timestamp)
					currentRun = &ParsedRun{
						Index:     runIndex,
						Time:      t,
						Status:    "running",
						TriggerBy: "Manual / webhook",
						Logs:      []string{line},
					}
					runIndex++
				} else if currentRun != nil {
					currentRun.Logs = append(currentRun.Logs, line)
					if strings.Contains(entry.Message, "finished successfully") {
						currentRun.Status = "success"
						currentRun.Duration = extractDurationFromLog(entry.Message)
					} else if strings.Contains(entry.Message, "Pipeline execution failed") {
						currentRun.Status = "failed"
						currentRun.Duration = extractDurationFromLog(entry.Message)
					} else if strings.Contains(entry.Message, "Rollback successfully completed") {
						currentRun.Status = "rollback"
					}
				}
			}
		}

		if currentRun != nil {
			runs = append(runs, *currentRun)
		}
	}

	// Sort runs in reverse order (newest first)
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}

	for _, r := range runs {
		run := r // copy local
		dotColor := a.FyneApp.Settings().Theme().Color("success", 0)
		if run.Status == "failed" {
			dotColor = a.FyneApp.Settings().Theme().Color("error", 0)
		} else if run.Status == "rollback" {
			dotColor = a.FyneApp.Settings().Theme().Color("warning", 0)
		}

		statusDot := NewMinSizeCircle(dotColor)
		statusDot.SetMinSize(fyne.NewSize(8, 8))

		rowTitle := widget.NewLabel(fmt.Sprintf("#%d   %s", run.Index, run.Time.Format("Jan 02 15:04")))
		rowTitle.TextStyle = fyne.TextStyle{Bold: true}

		rowTrigger := widget.NewLabel("Duration: " + run.Duration)
		rowBtn := widget.NewButton("View Run Logs", func() {
			a.showRunLogsModal(run)
		})

		row := container.NewHBox(
			container.NewGridWrap(fyne.NewSize(12, 12), container.NewCenter(statusDot)),
			rowTitle,
			rowTrigger,
			layout.NewSpacer(),
			rowBtn,
		)
		listContainer.Add(row)
	}

	if len(runs) == 0 {
		return widget.NewLabel("No execution runs recorded yet.")
	}

	return container.NewScroll(listContainer)
}

func (a *CodeForgeApp) buildConfigTab(p *daemon.Pipeline) fyne.CanvasObject {
	data, err := os.ReadFile(p.Path)
	configStr := string(data)
	if err != nil {
		configStr = "# Error reading file content"
	}

	editor := widget.NewMultiLineEntry()
	editor.SetText(configStr)
	editor.TextStyle = fyne.TextStyle{Monospace: true}

	saveBtn := widget.NewButton("Save Changes", func() {
		err := os.WriteFile(p.Path, []byte(editor.Text), 0644)
		if err != nil {
			dialog.ShowError(err, a.MainWindow)
		} else {
			a.Daemon.ReloadPipeline(p.Path)
			dialog.ShowInformation("Saved", "Configuration file saved successfully.", a.MainWindow)
			a.showPipelineDetailScreen(p.Program.Meta.Name)
		}
	})

	return container.NewBorder(nil, saveBtn, nil, nil, editor)
}

func (a *CodeForgeApp) showRunLogsModal(run ParsedRun) {
	logContent := container.NewVBox()

	for _, line := range run.Logs {
		var entry logger.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			lbl := widget.NewLabel(fmt.Sprintf("[%s] %s", entry.Level, entry.Message))
			lbl.TextStyle = fyne.TextStyle{Monospace: true}
			logContent.Add(lbl)
		} else {
			lbl := widget.NewLabel(line)
			lbl.TextStyle = fyne.TextStyle{Monospace: true}
			logContent.Add(lbl)
		}
	}

	scroll := container.NewVScroll(logContent)
	scroll.SetMinSize(fyne.NewSize(600, 400))

	closeBtn := widget.NewButton("Close", func() {
		a.MainWindow.Canvas().Overlays().Top().Hide()
	})

	modal := widget.NewModalPopUp(
		container.NewBorder(nil, closeBtn, nil, nil, scroll),
		a.MainWindow.Canvas(),
	)
	modal.Show()
}

func extractDurationFromLog(msg string) string {
	if strings.Contains(msg, "in ") {
		parts := strings.Split(msg, "in ")
		if len(parts) == 2 {
			return strings.TrimSuffix(parts[1], ".")
		}
	}
	return "unknown"
}

// showEditPipelineScreen navigates to the pipeline detail screen (which contains the config editor).
func (a *CodeForgeApp) showEditPipelineScreen(project, path string) {
	a.showPipelineDetailScreen(project)
}
