package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// buildSidebar populates the sidebar panel with navigation entries and the daemon power control.
func (a *CodeForgeApp) buildSidebar() {
	// 1. Sidebar Header (Logo 100x100 + Text)
	logo := LoadLogo()
	img := canvas.NewImageFromResource(logo)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(100, 100))

	headerText := canvas.NewText("CodeForge", a.FyneApp.Settings().Theme().Color("primary", 0))
	headerText.TextSize = 22
	headerText.Alignment = fyne.TextAlignCenter
	headerText.TextStyle = fyne.TextStyle{Bold: true}

	header := container.NewVBox(
		container.NewCenter(img),
		headerText,
		widget.NewSeparator(),
	)

	a.SidebarArea.Objects = []fyne.CanvasObject{header}

	// 2. Navigation Items
	navItems := []struct {
		Label  string
		Screen string
	}{
		{"🏠 Dashboard", "dashboard"},
		{"⚡ Pipelines", "pipelines"},
		{"🔒 Secrets", "secrets"},
		{"📋 Logs", "logs"},
		{"⚙️ Settings", "settings"},
		{"ℹ️ About", "about"},
	}

	for _, item := range navItems {
		screenName := item.Screen
		btn := widget.NewButton(item.Label, func() {
			a.NavigateTo(screenName)
		})

		// Highlight active navigation tab
		if a.Current == screenName {
			btn.Importance = widget.HighImportance
		} else {
			btn.Importance = widget.LowImportance
		}

		a.SidebarArea.Add(btn)
	}

	a.SidebarArea.Add(widget.NewSeparator())

	// 3. Daemon Toggle Control at Bottom
	_, running := a.getDaemonPid()
	var toggleBtn *widget.Button
	if running {
		toggleBtn = widget.NewButton("● Running", func() {
			a.Daemon.Stop()
			a.refreshSidebar()
		})
		toggleBtn.Importance = widget.WarningImportance // Green is default success, warning is amber/orange, we can style it
	} else {
		toggleBtn = widget.NewButton("○ Stopped", func() {
			_ = a.Daemon.Start()
			a.refreshSidebar()
		})
		toggleBtn.Importance = widget.DangerImportance
	}

	a.SidebarArea.Add(container.NewMax(toggleBtn))
}

func (a *CodeForgeApp) refreshSidebar() {
	// Re-construct the sidebar elements
	a.SidebarArea.Objects = nil
	a.buildSidebar()
	a.SidebarArea.Refresh()
}
