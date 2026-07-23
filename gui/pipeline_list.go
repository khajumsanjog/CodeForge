package gui

import (
	"fmt"
	"io"
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

	"codeforge/internal/kzm"
	"codeforge/internal/progress"
)

// buildPipelineListScreen creates the main listing window for all registered .kzm configurations.
func (a *CodeForgeApp) buildPipelineListScreen() fyne.CanvasObject {
	// Header
	title := widget.NewLabel("Pipelines")
	title.TextStyle = fyne.TextStyle{Bold: true}

	addBtn := widget.NewButton("+ Add Pipeline", func() {
		a.showAddPipelineOptions()
	})

	header := container.NewBorder(nil, nil, nil, addBtn, title)

	// List content
	listContainer := container.NewVBox()
	a.populatePipelineCards(listContainer)

	scroll := container.NewVScroll(listContainer)
	scroll.SetMinSize(fyne.NewSize(0, 500))

	return container.NewBorder(header, nil, nil, nil, scroll)
}

func (a *CodeForgeApp) populatePipelineCards(list *fyne.Container) {
	home, _ := os.UserHomeDir()
	pipelinesDir := filepath.Join(home, ".codeforge", "pipelines")
	files, err := filepath.Glob(filepath.Join(pipelinesDir, "*.kzm"))
	if err != nil || len(files) == 0 {
		list.Add(widget.NewLabel("No pipelines registered. Click '+ Add Pipeline' to create one."))
		return
	}

	for _, file := range files {
		filePath := file // bind local copy

		// Load/parse file to display details
		projectName := strings.TrimSuffix(filepath.Base(filePath), ".kzm")
		triggerInfo := "Trigger: manual"
		status := "IDLE"
		duration := ""
		lastRunTime := "never"

		data, err := os.ReadFile(filePath)
		if err == nil {
			lexer := kzm.NewLexer(string(data))
			if tokens, err := lexer.Tokenize(); err == nil {
				parser := kzm.NewParser(tokens)
				if prog, err := parser.Parse(); err == nil {
					projectName = prog.Meta.Name

					// Get trigger strings
					if len(prog.Triggers) > 0 {
						trig := prog.Triggers[0]
						if trig.Source == "github" || trig.Source == "gitlab" {
							triggerInfo = fmt.Sprintf("%s: %s → %s", trig.Source, trig.Repo, trig.Branch)
						} else if trig.Source == "folder" {
							triggerInfo = fmt.Sprintf("folder: %s", trig.Path)
						} else if trig.Source == "cron" {
							triggerInfo = fmt.Sprintf("cron: %s", trig.Cron)
						}
					}
				}
			}
		}

		// Read pipeline live status if daemon tracks it
		if p, ok := a.Daemon.GetPipelines()[projectName]; ok {
			status = p.LastStatus
			duration = p.LastDuration.Round(time.Millisecond).String()
			if !p.LastRun.IsZero() {
				duration = fmt.Sprintf("Duration: %s", p.LastDuration.Round(time.Millisecond).String())
				lastRunTime = formatRelativeTime(p.LastRun)
			}
		}

		// 1. Status dot
		dotColor := a.FyneApp.Settings().Theme().Color("shadow", 0)
		switch strings.ToUpper(status) {
		case "SUCCESS":
			dotColor = a.FyneApp.Settings().Theme().Color("success", 0)
		case "FAILED":
			dotColor = a.FyneApp.Settings().Theme().Color("error", 0)
		case "ROLLBACK":
			dotColor = a.FyneApp.Settings().Theme().Color("warning", 0)
		case "RUNNING":
			dotColor = a.FyneApp.Settings().Theme().Color("primary", 0)
		}
		statusDot := NewMinSizeCircle(dotColor)
		statusDot.SetMinSize(fyne.NewSize(12, 12))

		// Labels
		nameLbl := widget.NewLabel(projectName)
		nameLbl.TextStyle = fyne.TextStyle{Bold: true}

		trigLbl := widget.NewLabel(triggerInfo)
		trigLbl.TextStyle = fyne.TextStyle{Italic: true}

		var runTimeText string
		if duration != "" {
			runTimeText = fmt.Sprintf("Last run: %s · %s", lastRunTime, duration)
		} else {
			runTimeText = fmt.Sprintf("Last run: %s", lastRunTime)
		}
		runTimeLbl := widget.NewLabel(runTimeText)

		// Check if live data transfer progress is running
		var progressBox fyne.CanvasObject
		if tracker := progress.GetGlobalTracker(projectName); tracker != nil {
			snap := tracker.GetSnapshot()
			bar := widget.NewProgressBar()
			bar.SetValue(snap.Percentage / 100.0)

			speedStr := progress.FormatBytes(int64(snap.SpeedBytesPerSec)) + "/s"
			detailsStr := fmt.Sprintf("Transferring: %d/%d files (%s / %s @ %s)",
				snap.TransferredFiles, snap.TotalFiles,
				progress.FormatBytes(snap.TransferredBytes), progress.FormatBytes(snap.TotalBytes),
				speedStr)
			progressBox = container.NewVBox(bar, widget.NewLabel(detailsStr))
		}

		// Clickable body container redirecting to details
		infoVBox := container.NewVBox(
			container.NewHBox(nameLbl, layout.NewSpacer(), widget.NewLabel(status)),
			trigLbl,
			runTimeLbl,
		)
		if progressBox != nil {
			infoVBox.Add(progressBox)
		}

		// Action buttons
		runBtn := widget.NewButton("Run", func() {
			_ = a.Daemon.Trigger(projectName, "manual trigger from list")
			a.NavigateTo("pipelines") // refresh
		})
		logsBtn := widget.NewButton("Logs", func() {
			a.SelectedLogProject = projectName
			a.NavigateTo("logs")
		})
		editBtn := widget.NewButton("Edit", func() {
			a.showEditPipelineScreen(projectName, filePath)
		})
		deleteBtn := widget.NewButton("✕", func() {
			a.confirmDeletePipeline(projectName, filePath)
		})
		deleteBtn.Importance = widget.DangerImportance

		buttonsBox := container.NewHBox(runBtn, logsBtn, editBtn, deleteBtn)

		cardBorder := container.NewBorder(
			nil,
			nil,
			container.NewGridWrap(fyne.NewSize(16, 16), container.NewCenter(statusDot)),
			buttonsBox,
			infoVBox,
		)

		// Create a button wrapper around the details box so the whole card body is clickable
		bodyBtn := widget.NewButton("", func() {
			a.showPipelineDetailScreen(projectName)
		})
		// Theme-adaptive card background styling
		bg := canvas.NewRectangle(a.FyneApp.Settings().Theme().Color("inputBackground", 0))
		bg.CornerRadius = 8

		cardStack := container.NewStack(bg, bodyBtn, container.NewPadded(cardBorder))
		list.Add(cardStack)
	}
}

func (a *CodeForgeApp) showAddPipelineOptions() {
	dialog := widget.NewModalPopUp(
		container.NewVBox(
			widget.NewLabel("Add Pipeline:"),
			widget.NewButton("Create New Pipeline Wizard", func() {
				a.MainWindow.Canvas().Overlays().Top().Hide()
				a.NavigateTo("new_pipeline")
			}),
			widget.NewButton("Open Existing .kzm File", func() {
				a.MainWindow.Canvas().Overlays().Top().Hide()
				a.showKzmFilePicker()
			}),
			widget.NewButton("Cancel", func() {
				a.MainWindow.Canvas().Overlays().Top().Hide()
			}),
		),
		a.MainWindow.Canvas(),
	)
	dialog.Show()
}

func (a *CodeForgeApp) showKzmFilePicker() {
	d := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		defer reader.Close()

		// Copy file to ~/.codeforge/pipelines/
		home, _ := os.UserHomeDir()
		destPath := filepath.Join(home, ".codeforge", "pipelines", reader.URI().Name())
		
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err == nil {
			defer out.Close()
			_, _ = io.Copy(out, reader)
			a.Daemon.ReloadPipeline(destPath)
			a.NavigateTo("pipelines")
		}
	}, a.MainWindow)
	d.Show()
}

func (a *CodeForgeApp) confirmDeletePipeline(project, path string) {
	d := dialog.NewConfirm("Delete Pipeline", fmt.Sprintf("Are you sure you want to delete pipeline %q?", project), func(confirm bool) {
		if confirm {
			_ = os.Remove(path)
			// Remove from daemon mapping
			a.Daemon.RemovePipeline(project)
			a.NavigateTo("pipelines")
		}
	}, a.MainWindow)
	d.Show()
}
