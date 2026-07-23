package gui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"codeforge/internal/env"
	"codeforge/internal/logger"
	"codeforge/internal/progress"
)

// buildDashboardScreen creates the central dashboard canvas containing cards, recent activities, and shortcuts.
func (a *CodeForgeApp) buildDashboardScreen() fyne.CanvasObject {
	// Query stats
	regCount := a.getPipelineCount()
	successCount, failedCount := a.queryLast7DaysStats()
	runningCount := a.getRunningPipelinesCount()

	// Stat colors
	primaryColor := a.FyneApp.Settings().Theme().Color("primary", 0)
	successColor := a.FyneApp.Settings().Theme().Color("success", 0)
	errorColor := a.FyneApp.Settings().Theme().Color("error", 0)
	warningColor := a.FyneApp.Settings().Theme().Color("warning", 0)

	// Stat Grid Layout
	card1 := createStatCard("PIPELINES", fmt.Sprintf("%d", regCount), "registered", primaryColor, a)
	card2 := createStatCard("SUCCESS", fmt.Sprintf("%d", successCount), "last 7 days", successColor, a)
	card3 := createStatCard("FAILED", fmt.Sprintf("%d", failedCount), "last 7 days", errorColor, a)
	card4 := createStatCard("RUNNING", fmt.Sprintf("%d", runningCount), "right now", warningColor, a)

	statsGrid := container.NewGridWithColumns(4, card1, card2, card3, card4)

	// Middle Section: Recent Activity
	activityList := container.NewVBox()
	a.populateActivityList(activityList)

	activityScroll := container.NewVScroll(activityList)
	activityScroll.SetMinSize(fyne.NewSize(0, 300))

	activityCard := widget.NewCard("Recent Activity", "Last 10 pipeline execution runs", activityScroll)

	// Bottom Section: Quick Actions
	newBtn := widget.NewButton("+ New Pipeline", func() {
		a.NavigateTo("new_pipeline")
	})
	triggerBtn := widget.NewButton("▶ Trigger Deploy", func() {
		a.showTriggerDialog()
	})
	rollbackBtn := widget.NewButton("↺ Rollback", func() {
		a.showRollbackDialog()
	})
	restartBtn := widget.NewButton("🔄 Restart Daemon", func() {
		a.Daemon.Stop()
		_ = a.Daemon.Start()
		a.refreshSidebar()
		a.NavigateTo("dashboard")
	})

	actionsRow := container.NewGridWithColumns(4, newBtn, triggerBtn, rollbackBtn, restartBtn)
	actionsCard := widget.NewCard("Quick Actions", "", actionsRow)

	// Live Active Transfers Card
	activeTransfersCard := a.buildActiveTransfersCard()

	var dashboardLayout *fyne.Container
	if activeTransfersCard != nil {
		dashboardLayout = container.NewVBox(
			statsGrid,
			activeTransfersCard,
			activityCard,
			actionsCard,
		)
	} else {
		dashboardLayout = container.NewVBox(
			statsGrid,
			activityCard,
			actionsCard,
		)
	}

	// Auto-refresh loop
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if a.Current != "dashboard" {
					return
				}
				fyne.Do(func() {
					// Easier to navigate to dashboard to redraw
					a.NavigateTo("dashboard")
				})
			}
		}
	}()

	return container.NewScroll(dashboardLayout)
}

func createStatCard(title string, value string, subtext string, topBorderColor color.Color, a *CodeForgeApp) *fyne.Container {
	bg := canvas.NewRectangle(color.NRGBA{R: 0x24, G: 0x24, B: 0x3B, A: 0xFF}) // Sleek dark card container
	bg.CornerRadius = 8

	border := canvas.NewRectangle(topBorderColor)
	border.SetMinSize(fyne.NewSize(0, 4))

	lblText := canvas.NewText(title, a.FyneApp.Settings().Theme().Color("primary", 0))
	lblText.TextSize = 11
	lblText.TextStyle = fyne.TextStyle{Bold: true}
	lblText.Alignment = fyne.TextAlignCenter

	valText := canvas.NewText(value, a.FyneApp.Settings().Theme().Color("foreground", 0))
	valText.TextSize = 32
	valText.TextStyle = fyne.TextStyle{Bold: true}
	valText.Alignment = fyne.TextAlignCenter

	subText := canvas.NewText(subtext, a.FyneApp.Settings().Theme().Color("foreground", 0))
	subText.TextSize = 10
	subText.TextStyle = fyne.TextStyle{Italic: true}
	subText.Alignment = fyne.TextAlignCenter

	content := container.NewVBox(
		border,
		widget.NewLabel(""), // padding spacing
		lblText,
		valText,
		subText,
		widget.NewLabel(""),
	)

	return container.NewStack(bg, content)
}

func (a *CodeForgeApp) queryLast7DaysStats() (int, int) {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".codeforge", "logs")

	files, err := filepath.Glob(filepath.Join(logDir, "*.log"))
	if err != nil {
		return 0, 0
	}

	successCount := 0
	failedCount := 0
	cutoff := time.Now().AddDate(0, 0, -7)

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || info.ModTime().Before(cutoff) {
			continue
		}

		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var entry logger.LogEntry
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				if strings.Contains(entry.Message, "Pipeline executed successfully") || strings.Contains(entry.Message, "finished successfully") {
					successCount++
				} else if strings.Contains(entry.Message, "failed") || strings.Contains(entry.Message, "Error") {
					failedCount++
				}
			}
		}
	}

	return successCount, failedCount
}

func (a *CodeForgeApp) getRunningPipelinesCount() int {
	// Query status API
	url := fmt.Sprintf("%s/status", env.GetAPIURL(7080))
	resp, err := http.Get(url)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var list []struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(body, &list)

	running := 0
	for _, p := range list {
		if strings.ToUpper(p.Status) == "RUNNING" {
			running++
		}
	}
	return running
}

func (a *CodeForgeApp) populateActivityList(listContainer *fyne.Container) {
	// Look through recent logs across all files
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".codeforge", "logs")
	files, err := filepath.Glob(filepath.Join(logDir, "*.log"))
	if err != nil || len(files) == 0 {
		listContainer.Add(widget.NewLabel("No recent activity found."))
		return
	}

	// Sort files by date (newest first)
	for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
		files[i], files[j] = files[j], files[i]
	}

	type Activity struct {
		Project   string
		Status    string
		Time      string
		Duration  string
		Timestamp time.Time
	}

	activities := []Activity{}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		// Find project name from file basename (e.g. project-date.log)
		base := filepath.Base(file)
		parts := strings.Split(base, "-")
		if len(parts) < 2 {
			continue
		}
		projectName := strings.Join(parts[:len(parts)-4], "-") // handle dashes
		if projectName == "" {
			projectName = parts[0]
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}

			var entry logger.LogEntry
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				// Parse trigger or completion statements
				if strings.Contains(entry.Message, "finished successfully") || strings.Contains(entry.Message, "Pipeline execution failed") {
					status := "success"
					if strings.Contains(entry.Message, "failed") {
						status = "failed"
					}

					t, _ := time.Parse(time.RFC3339, entry.Timestamp)
					duration := "30s"
					if strings.Contains(entry.Message, "in ") {
						subParts := strings.Split(entry.Message, "in ")
						if len(subParts) == 2 {
							duration = strings.TrimSuffix(subParts[1], ".")
						}
					}

					activities = append(activities, Activity{
						Project:   projectName,
						Status:    status,
						Time:      formatRelativeTime(t),
						Duration:  duration,
						Timestamp: t,
					})
				}
			}
		}
	}

	// Sort activities by timestamp (newest first)
	for i := 0; i < len(activities); i++ {
		for j := i + 1; j < len(activities); j++ {
			if activities[j].Timestamp.After(activities[i].Timestamp) {
				activities[i], activities[j] = activities[j], activities[i]
			}
		}
	}

	// Display top 10
	limit := 10
	if len(activities) < limit {
		limit = len(activities)
	}

	for i := 0; i < limit; i++ {
		act := activities[i]
		statusDot := NewMinSizeCircle(a.FyneApp.Settings().Theme().Color("success", 0))
		if act.Status == "failed" {
			statusDot.FillColor = a.FyneApp.Settings().Theme().Color("error", 0)
		}
		statusDot.SetMinSize(fyne.NewSize(8, 8))

		projectNameLabel := widget.NewLabel(act.Project)
		projectNameLabel.TextStyle = fyne.TextStyle{Bold: true}

		statusLabel := widget.NewLabel(act.Status)
		timeLabel := widget.NewLabel(act.Time)
		durLabel := widget.NewLabel(act.Duration)

		logsBtn := widget.NewButton("View Logs", func() {
			a.NavigateTo("logs") // navigate to logs viewer
		})

		row := container.NewHBox(
			container.NewGridWrap(fyne.NewSize(12, 12), container.NewCenter(statusDot)),
			projectNameLabel,
			statusLabel,
			timeLabel,
			durLabel,
			layout.NewSpacer(),
			logsBtn,
		)

		listContainer.Add(row)
	}

	if len(activities) == 0 {
		listContainer.Add(widget.NewLabel("No recent activity found."))
	}
}

func formatRelativeTime(t time.Time) string {
	diff := time.Since(t)
	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
}

func (a *CodeForgeApp) showTriggerDialog() {
	// Show modal selector of pipelines
	home, _ := os.UserHomeDir()
	pipelinesDir := filepath.Join(home, ".codeforge", "pipelines")
	files, err := filepath.Glob(filepath.Join(pipelinesDir, "*.kzm"))
	if err != nil || len(files) == 0 {
		return
	}

	var options []string
	for _, file := range files {
		options = append(options, strings.TrimSuffix(filepath.Base(file), ".kzm"))
	}

	selected := options[0]
	drop := widget.NewSelect(options, func(s string) {
		selected = s
	})
	drop.SetSelected(selected)

	dialog := widget.NewModalPopUp(
		container.NewVBox(
			widget.NewLabel("Select pipeline to run:"),
			drop,
			container.NewHBox(
				widget.NewButton("Cancel", func() {
					a.MainWindow.Canvas().Overlays().Top().Hide()
				}),
				widget.NewButton("Trigger", func() {
					_ = a.Daemon.Trigger(selected, "manual GUI Trigger")
					a.MainWindow.Canvas().Overlays().Top().Hide()
				}),
			),
		),
		a.MainWindow.Canvas(),
	)
	dialog.Show()
}

func (a *CodeForgeApp) showRollbackDialog() {
	home, _ := os.UserHomeDir()
	pipelinesDir := filepath.Join(home, ".codeforge", "pipelines")
	files, err := filepath.Glob(filepath.Join(pipelinesDir, "*.kzm"))
	if err != nil || len(files) == 0 {
		return
	}

	var options []string
	for _, file := range files {
		options = append(options, strings.TrimSuffix(filepath.Base(file), ".kzm"))
	}

	selected := options[0]
	drop := widget.NewSelect(options, func(s string) {
		selected = s
	})
	drop.SetSelected(selected)

	dialog := widget.NewModalPopUp(
		container.NewVBox(
			widget.NewLabel("Select pipeline to rollback:"),
			drop,
			container.NewHBox(
				widget.NewButton("Cancel", func() {
					a.MainWindow.Canvas().Overlays().Top().Hide()
				}),
				widget.NewButton("Rollback", func() {
					_ = a.Daemon.TriggerRollback(selected)
					a.MainWindow.Canvas().Overlays().Top().Hide()
				}),
			),
		),
		a.MainWindow.Canvas(),
	)
	dialog.Show()
}

func (a *CodeForgeApp) buildActiveTransfersCard() fyne.CanvasObject {
	activeMap := progress.GetAllActiveProgress()
	if len(activeMap) == 0 {
		return nil
	}

	box := container.NewVBox()
	for project, snap := range activeMap {
		projTitle := widget.NewLabel(fmt.Sprintf("⚡ Transferring: %s", project))
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

		itemBox := container.NewVBox(
			projTitle,
			bar,
			detailsLbl,
			fileLbl,
		)
		box.Add(container.NewPadded(itemBox))
	}

	return widget.NewCard("Active Data Transfers", "Live pipeline execution progress", box)
}
