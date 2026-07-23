package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type AppSettings struct {
	ConfigDir    string `json:"config_dir"`
	APIPort      int    `json:"api_port"`
	Workers      int    `json:"workers"`
	PollInterval int    `json:"poll_interval"`
	ThemeMode    string `json:"theme_mode"` // Dark, Light
	SlackWebhook string `json:"slack_webhook"`
	EmailAddress string `json:"email_address"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUser     string `json:"smtp_user"`
	SMTPPass     string `json:"smtp_pass"`
}

func loadSettings() *AppSettings {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codeforge", "settings.json")

	cfg := &AppSettings{
		ConfigDir:    filepath.Join(home, ".codeforge"),
		APIPort:      7080,
		Workers:      3,
		PollInterval: 30,
		ThemeMode:    "Dark",
	}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	return cfg
}

func (s *AppSettings) save() error {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codeforge", "settings.json")

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// buildSettingsScreen constructs the global configuration dashboard.
func (a *CodeForgeApp) buildSettingsScreen() fyne.CanvasObject {
	cfg := loadSettings()

	// 1. Daemon Settings
	daemonTitle := widget.NewLabel("Daemon Settings")
	daemonTitle.TextStyle = fyne.TextStyle{Bold: true}

	cfgDirEntry := widget.NewEntry()
	cfgDirEntry.SetText(cfg.ConfigDir)

	pickerBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFolderOpen(func(lu fyne.ListableURI, err error) {
			if err == nil && lu != nil {
				cfgDirEntry.SetText(lu.Path())
			}
		}, a.MainWindow)
	})

	dirContainer := container.NewBorder(nil, nil, nil, pickerBtn, cfgDirEntry)

	portEntry := widget.NewEntry()
	portEntry.SetText(fmt.Sprintf("%d", cfg.APIPort))

	workersSlider := widget.NewSlider(1, 10)
	workersSlider.SetValue(float64(cfg.Workers))
	workersLabel := widget.NewLabel(fmt.Sprintf("%d workers", cfg.Workers))
	workersSlider.OnChanged = func(v float64) {
		workersLabel.SetText(fmt.Sprintf("%d workers", int(v)))
	}

	pollSlider := widget.NewSlider(10, 300)
	pollSlider.SetValue(float64(cfg.PollInterval))
	pollLabel := widget.NewLabel(fmt.Sprintf("%ds interval", cfg.PollInterval))
	pollSlider.OnChanged = func(v float64) {
		pollLabel.SetText(fmt.Sprintf("%ds interval", int(v)))
	}

	daemonForm := widget.NewForm(
		widget.NewFormItem("Config Directory", dirContainer),
		widget.NewFormItem("API Port", portEntry),
		widget.NewFormItem("Worker Count", container.NewBorder(nil, nil, nil, workersLabel, workersSlider)),
		widget.NewFormItem("Poll Interval", container.NewBorder(nil, nil, nil, pollLabel, pollSlider)),
	)

	// 2. Appearance Settings
	appTitle := widget.NewLabel("Appearance")
	appTitle.TextStyle = fyne.TextStyle{Bold: true}

	themeSelect := widget.NewSelect([]string{"Dark", "Light"}, func(s string) {})
	themeSelect.SetSelected(cfg.ThemeMode)

	appForm := widget.NewForm(
		widget.NewFormItem("Theme Mode", themeSelect),
	)

	// 3. Notifications Settings
	notifTitle := widget.NewLabel("Default Notifications")
	notifTitle.TextStyle = fyne.TextStyle{Bold: true}

	slackEntry := widget.NewEntry()
	slackEntry.SetText(cfg.SlackWebhook)

	emailEntry := widget.NewEntry()
	emailEntry.SetText(cfg.EmailAddress)

	smtpHost := widget.NewEntry()
	smtpHost.SetText(cfg.SMTPHost)

	smtpPort := widget.NewEntry()
	smtpPort.SetText(fmt.Sprintf("%d", cfg.SMTPPort))

	smtpUser := widget.NewEntry()
	smtpUser.SetText(cfg.SMTPUser)

	smtpPass := widget.NewPasswordEntry()
	smtpPass.SetText(cfg.SMTPPass)
	smtpPass.SetPlaceHolder("App password or SMTP password")

	notifForm := widget.NewForm(
		widget.NewFormItem("Slack Webhook URL", slackEntry),
		widget.NewFormItem("SMTP Host", smtpHost),
		widget.NewFormItem("SMTP Port", smtpPort),
		widget.NewFormItem("SMTP Username", smtpUser),
		widget.NewFormItem("SMTP Password", smtpPass),
		widget.NewFormItem("Default Recipient", emailEntry),
	)

	// 4. Danger Zone
	dangerTitle := widget.NewLabel("Danger Zone")
	dangerTitle.TextStyle = fyne.TextStyle{Bold: true}

	clearLogsBtn := widget.NewButton("Clear all logs", func() {
		d := dialog.NewConfirm("Clear Logs", "Are you sure you want to delete all daily logs? This action is irreversible.", func(confirm bool) {
			if confirm {
				home, _ := os.UserHomeDir()
				logDir := filepath.Join(home, ".codeforge", "logs")
				_ = os.RemoveAll(logDir)
				_ = os.MkdirAll(logDir, 0755)
			}
		}, a.MainWindow)
		d.Show()
	})
	clearLogsBtn.Importance = widget.DangerImportance

	resetSettingsBtn := widget.NewButton("Reset all settings", func() {
		d := dialog.NewConfirm("Reset Settings", "Reset all daemon parameters and themes to defaults?", func(confirm bool) {
			if confirm {
				home, _ := os.UserHomeDir()
				path := filepath.Join(home, ".codeforge", "settings.json")
				_ = os.Remove(path)
				a.NavigateTo("settings") // refresh
			}
		}, a.MainWindow)
		d.Show()
	})
	resetSettingsBtn.Importance = widget.DangerImportance

	dangerRow := container.NewHBox(clearLogsBtn, resetSettingsBtn)

	// Save Button at bottom
	saveBtn := widget.NewButton("Save Config Changes", func() {
		cfg.ConfigDir = cfgDirEntry.Text
		_, _ = fmt.Sscanf(portEntry.Text, "%d", &cfg.APIPort)
		cfg.Workers = int(workersSlider.Value)
		cfg.PollInterval = int(pollSlider.Value)
		cfg.ThemeMode = themeSelect.Selected
		cfg.SlackWebhook = slackEntry.Text
		cfg.EmailAddress = emailEntry.Text
		cfg.SMTPHost = smtpHost.Text
		_, _ = fmt.Sscanf(smtpPort.Text, "%d", &cfg.SMTPPort)
		cfg.SMTPUser = smtpUser.Text
		cfg.SMTPPass = smtpPass.Text

		err := cfg.save()
		if err != nil {
			dialog.ShowError(err, a.MainWindow)
		} else {
			// Apply theme immediately
			a.applyTheme(cfg.ThemeMode)
			dialog.ShowInformation("Saved", "Settings saved and theme applied.", a.MainWindow)
		}
	})
	saveBtn.Importance = widget.HighImportance

	settingsLayout := container.NewVBox(
		daemonTitle,
		daemonForm,
		widget.NewSeparator(),
		appTitle,
		appForm,
		widget.NewSeparator(),
		notifTitle,
		notifForm,
		widget.NewSeparator(),
		dangerTitle,
		dangerRow,
		widget.NewSeparator(),
		saveBtn,
	)

	return container.NewScroll(settingsLayout)
}
