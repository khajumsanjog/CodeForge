package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"codeforge/internal/logger"
)

// buildLogViewerScreen creates a scrollable panel displaying color-coded monospace logs.
func (a *CodeForgeApp) buildLogViewerScreen() fyne.CanvasObject {
	// Query list of pipelines for dropdown selector
	home, _ := os.UserHomeDir()
	pipelinesDir := filepath.Join(home, ".codeforge", "pipelines")
	files, err := filepath.Glob(filepath.Join(pipelinesDir, "*.kzm"))
	
	projects := []string{"daemon"} // fallback
	// Add loaded pipelines from daemon
	for pName := range a.Daemon.GetPipelines() {
		if pName != "" {
			found := false
			for _, existing := range projects {
				if existing == pName {
					found = true
					break
				}
			}
			if !found {
				projects = append(projects, pName)
			}
		}
	}
	if err == nil {
		for _, f := range files {
			pName := strings.TrimSuffix(filepath.Base(f), ".kzm")
			found := false
			for _, existing := range projects {
				if existing == pName {
					found = true
					break
				}
			}
			if !found {
				projects = append(projects, pName)
			}
		}
	}

	selectedProject := projects[0]
	if a.SelectedLogProject != "" {
		for _, pName := range projects {
			if strings.EqualFold(pName, a.SelectedLogProject) {
				selectedProject = pName
				break
			}
		}
	}

	logLinesContainer := container.NewVBox()
	scroll := container.NewVScroll(logLinesContainer)
	scroll.SetMinSize(fyne.NewSize(0, 450))

	// Live tail flags
	liveTail := true
	autoScroll := true

	// Helper to load logs into pane
	reloadLogs := func() {
		logDir := filepath.Join(home, ".codeforge", "logs")
		l := logger.NewLogger(logDir)

		lines, err := l.TailLines(selectedProject, 200)

		fyne.Do(func() {
			logLinesContainer.Objects = nil
			if err != nil {
				logLinesContainer.Add(widget.NewLabel("Failed to load logs: " + err.Error()))
				logLinesContainer.Refresh()
				return
			}

			for _, line := range lines {
				logLinesContainer.Add(createColorLogLine(line, a))
			}

			logLinesContainer.Refresh()

			if autoScroll {
				scroll.ScrollToBottom()
			}
		})
	}

	reloadLogs()

	// Selector dropdown
	projSelect := widget.NewSelect(projects, func(s string) {
		selectedProject = s
		reloadLogs()
	})
	projSelect.SetSelected(selectedProject)

	// Live tail polling thread
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if a.Current != "logs" {
					return
				}
				if liveTail {
					reloadLogs()
				}
			}
		}
	}()

	// UI Controls
	tailCheck := widget.NewCheck("Live Tail", func(checked bool) {
		liveTail = checked
	})
	tailCheck.SetChecked(liveTail)

	scrollCheck := widget.NewCheck("Auto Scroll", func(checked bool) {
		autoScroll = checked
	})
	scrollCheck.SetChecked(autoScroll)

	exportBtn := widget.NewButton("Export Logs", func() {
		d := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil || writer == nil {
				return
			}
			defer writer.Close()

			// Write active logs to selected file
			logDir := filepath.Join(home, ".codeforge", "logs")
			l := logger.NewLogger(logDir)
			lines, _ := l.TailLines(selectedProject, 2000)

			var sb strings.Builder
			for _, line := range lines {
				var entry logger.LogEntry
				if err := json.Unmarshal([]byte(line), &entry); err == nil {
					sb.WriteString(fmt.Sprintf("[%s] [%s] %s\n", entry.Timestamp, entry.Level, entry.Message))
				} else {
					sb.WriteString(line + "\n")
				}
			}

			_, _ = writer.Write([]byte(sb.String()))
		}, a.MainWindow)
		d.Show()
	})

	controls := container.NewHBox(
		widget.NewLabel("Project:"),
		projSelect,
		tailCheck,
		scrollCheck,
		layout.NewSpacer(),
		exportBtn,
	)

	return container.NewBorder(controls, nil, nil, nil, scroll)
}

func createColorLogLine(line string, a *CodeForgeApp) fyne.CanvasObject {
	var entry logger.LogEntry
	text := line
	level := "INFO"

	if err := json.Unmarshal([]byte(line), &entry); err == nil {
		text = fmt.Sprintf("[%s] [%s] %s", entry.Timestamp, entry.Level, entry.Message)
		level = entry.Level
	}

	lbl := canvas.NewText(text, a.FyneApp.Settings().Theme().Color("foreground", 0))
	lbl.TextSize = 11
	lbl.TextStyle = fyne.TextStyle{Monospace: true}

	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "success") || strings.Contains(lowerText, "✓") || level == "SUCCESS" {
		lbl.Color = a.FyneApp.Settings().Theme().Color("success", 0)
	} else if strings.Contains(lowerText, "failed") || strings.Contains(lowerText, "error") || strings.Contains(lowerText, "✗") || level == "ERROR" || level == "FAILED" {
		lbl.Color = a.FyneApp.Settings().Theme().Color("error", 0)
	} else if strings.Contains(lowerText, "warning") || strings.Contains(lowerText, "⚠") || level == "WARNING" {
		lbl.Color = a.FyneApp.Settings().Theme().Color("warning", 0)
	}

	return lbl
}
