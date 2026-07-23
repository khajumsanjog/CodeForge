package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// buildMainWindow designs the core layout shell containing a header, sidebar, status bar, and content pane.
func (a *CodeForgeApp) buildMainWindow() fyne.Window {
	a.MainWindow = a.FyneApp.NewWindow("CodeForge — CI/CD - v" + a.Version)
	a.MainWindow.SetIcon(LoadLogo())
	a.MainWindow.Resize(fyne.NewSize(1100, 700))
	a.MainWindow.SetOnClosed(func() {
		a.Daemon.Stop()
	})

	// 1. Build Title Bar Header
	header := a.buildHeader()

	// 2. Build Sidebar Navigation
	a.SidebarArea = container.NewVBox()
	a.buildSidebar()
	sidebarWrapper := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(140, 600), a.SidebarArea),
		widget.NewSeparator(),
	)

	// 3. Build Bottom Status Bar
	statusBar := a.buildStatusBar()

	// 4. Build Content Pane
	a.ContentArea = container.NewStack()

	// Assemble layout
	mainLayout := container.NewBorder(
		header,
		statusBar,
		sidebarWrapper,
		nil,
		a.ContentArea,
	)

	a.MainWindow.SetContent(mainLayout)

	// Route to default page
	a.NavigateTo("dashboard")

	// Start status bar update scheduler
	go a.statusBarUpdater()

	return a.MainWindow
}

func (a *CodeForgeApp) buildHeader() fyne.CanvasObject {
	logo := LoadLogo()
	img := canvas.NewImageFromResource(logo)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(48, 48))

	title := canvas.NewText("CodeForge", a.FyneApp.Settings().Theme().Color("primary", 0))
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}

	sep := widget.NewLabel("·")
	tagline := widget.NewLabel("CI/CD - v" + a.Version)

	credit := canvas.NewText("KhajumSanjog", a.FyneApp.Settings().Theme().Color("foreground", 0))
	credit.TextSize = 11
	credit.TextStyle = fyne.TextStyle{Italic: true}

	left := container.NewHBox(img, title, sep, tagline)
	right := container.NewBorder(nil, nil, nil, credit)

	header := container.NewBorder(nil, widget.NewSeparator(), nil, right, left)
	return header
}

func (a *CodeForgeApp) buildStatusBar() fyne.CanvasObject {
	a.StatusDot = NewMinSizeCircle(a.FyneApp.Settings().Theme().Color("error", 0))
	a.StatusDot.SetMinSize(fyne.NewSize(8, 8))

	a.StatusLabel = widget.NewLabel("Daemon: Stopped")
	pipelinesLabel := widget.NewLabel("Pipelines: 0")
	lastDeployLabel := widget.NewLabel("Last deploy: never")

	credit := widget.NewLabel("KhajumSanjog")

	left := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(12, 12), container.NewCenter(a.StatusDot)),
		a.StatusLabel,
		widget.NewLabel("·"),
		pipelinesLabel,
		widget.NewLabel("·"),
		lastDeployLabel,
	)

	right := container.NewBorder(nil, nil, nil, credit)

	// Goroutine updates status labels regularly
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.FyneApp.Settings().Theme() // avoid warning
			// Query stats
			_, running := a.getDaemonPid()
			count := a.getPipelineCount()

			fyne.Do(func() {
				pipelinesLabel.SetText(fmt.Sprintf("Pipelines: %d", count))
				if running {
					a.StatusDot.FillColor = a.FyneApp.Settings().Theme().Color("success", 0)
				} else {
					a.StatusDot.FillColor = a.FyneApp.Settings().Theme().Color("error", 0)
				}
				a.StatusDot.Refresh()
			})
		}
	}()

	statusBar := container.NewBorder(widget.NewSeparator(), nil, nil, right, left)
	return statusBar
}

func (a *CodeForgeApp) statusBarUpdater() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		pid, running := a.getDaemonPid()

		fyne.Do(func() {
			if running {
				a.StatusLabel.SetText(fmt.Sprintf("Daemon: Running (PID: %d)", pid))
			} else {
				a.StatusLabel.SetText("Daemon: Stopped")
			}
		})
	}
}

func (a *CodeForgeApp) getDaemonPid() (int, bool) {
	home, _ := os.UserHomeDir()
	pidPath := filepath.Join(home, ".codeforge", "daemon.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}



	// Try loading direct int
	var pidInt int
	_, err = fmt.Sscanf(string(data), "%d", &pidInt)
	if err != nil {
		return 0, false
	}

	return pidInt, true
}

func (a *CodeForgeApp) getPipelineCount() int {
	// Query local folders or API status
	home, _ := os.UserHomeDir()
	pipelinesDir := filepath.Join(home, ".codeforge", "pipelines")
	files, err := filepath.Glob(filepath.Join(pipelinesDir, "*.kzm"))
	if err != nil {
		return 0
	}
	return len(files)
}
