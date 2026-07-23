package gui

import (
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"codeforge/internal/daemon"
	"codeforge/internal/env"
	"codeforge/internal/logger"
)

// CodeForgeApp aggregates the GUI state, logging, and daemon instances.
type CodeForgeApp struct {
	FyneApp     fyne.App
	MainWindow  fyne.Window
	Daemon      *daemon.Daemon
	Logger      *logger.Logger
	Current     string // Name of current active screen
	ContentArea *fyne.Container
	SidebarArea *fyne.Container
	StatusLabel *widget.Label
	StatusDot   *MinSizeCircle
	Version     string
}

// NewApp instantiates the Fyne application and custom branding theme.
func NewApp(version string) *CodeForgeApp {
	a := app.NewWithID("com.khajumsanjog.codeforge")
	a.SetIcon(LoadLogo())
	a.Settings().SetTheme(&CodeForgeTheme{})

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".codeforge", "logs")
	l := logger.NewLogger(logDir)

	// Init daemon (API port from env or default 7080, 3 workers)
	d := daemon.NewDaemon(filepath.Join(home, ".codeforge"), env.GetPort(7080), 3, l)

	cfApp := &CodeForgeApp{
		FyneApp: a,
		Daemon:  d,
		Logger:  l,
		Current: "dashboard",
		Version: version,
	}

	return cfApp
}

// Run displays the splash screen, starts background operations, and kicks off the main loop.
func (a *CodeForgeApp) Run() {
	a.showSplash()
	a.FyneApp.Run()
}

// showSplash creates a borderless window displaying project details for 2 seconds.
func (a *CodeForgeApp) showSplash() {
	splash := a.FyneApp.NewWindow("CodeForge Loading")
	splash.Resize(fyne.NewSize(560, 360))
	splash.CenterOnScreen()

	logo := LoadLogo()
	img := canvas.NewImageFromResource(logo)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(180, 180))

	title := canvas.NewText("CodeForge", a.FyneApp.Settings().Theme().Color("primary", 0))
	title.TextSize = 28
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := widget.NewLabel("CI/CD - v" + a.Version)
	subtitle.Alignment = fyne.TextAlignCenter

	prog := widget.NewProgressBar()
	credit := canvas.NewText("KhajumSanjog", a.FyneApp.Settings().Theme().Color("foreground", 0))
	credit.TextSize = 11
	credit.TextStyle = fyne.TextStyle{Italic: true}

	// Layout elements
	centerBox := container.NewVBox(
		img,
		title,
		subtitle,
		container.NewPadded(prog),
	)

	bottomBox := container.NewBorder(nil, nil, nil, credit)
	splashContent := container.NewBorder(nil, bottomBox, nil, nil, centerBox)
	splash.SetContent(splashContent)
	splash.Show()

	// Simulate loading animation
	go func() {
		for i := 0.0; i <= 1.0; i += 0.05 {
			time.Sleep(100 * time.Millisecond)
			// SetValue is safe to call from goroutines in Fyne
			prog.SetValue(i)
		}

		// Initialize Daemon background loop (not a UI op — safe in goroutine)
		err := a.Daemon.Start()
		if err != nil {
			a.Logger.Log("daemon", "ERROR", "Failed to start daemon: %v", err)
		}

		// All UI operations MUST run on the main thread via fyne.Do()
		fyne.Do(func() {
			a.buildMainWindow()
			splash.Close()
			a.MainWindow.Show()
			a.setupSystemTray()
		})
	}()
}

// navigateTo handles screen swapping in the center pane.
func (a *CodeForgeApp) NavigateTo(screen string) {
	a.Current = screen
	var content fyne.CanvasObject

	switch screen {
	case "dashboard":
		content = a.buildDashboardScreen()
	case "pipelines":
		content = a.buildPipelineListScreen()
	case "new_pipeline":
		content = a.buildNewPipelineScreen()
	case "secrets":
		content = a.buildSecretsScreen()
	case "logs":
		content = a.buildLogViewerScreen()
	case "settings":
		content = a.buildSettingsScreen()
	case "about":
		content = a.buildAboutScreen()
	default:
		content = widget.NewLabel("Screen not found: " + screen)
	}

	a.ContentArea.Objects = []fyne.CanvasObject{content}
	a.ContentArea.Refresh()

	// Refresh sidebar links to show highlighted state
	a.refreshSidebar()
}
