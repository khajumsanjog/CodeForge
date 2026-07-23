package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type WizardState struct {
	Step int // 1 to 5

	// Metadata
	Name        string
	Version     string
	Description string

	// Trigger
	TriggerType string
	GitRepo     string
	GitBranch   string
	FolderPath  string
	CronExpr    string

	// Deploy Target
	TargetType  string
	TargetName  string
	PathOption  string
	KeyOption   string
	RestartOpt  string
	RegionOpt   string
	RuntimeOpt  string
	MemoryOpt   string
	TimeoutOpt  string
	UserOption  string
	ExclOption  string
	ImageOption string
	ServerOption string
	PortOption  string
	PassOption  string

	// Tests
	RunTests    bool
	TestCmd     string

	// Alerts
	SlackOn     bool
	SlackHook   string
	EmailOn     bool
	EmailAddr   string
}

// buildNewPipelineScreen creates the step-by-step pipeline creation form wizard.
func (a *CodeForgeApp) buildNewPipelineScreen() fyne.CanvasObject {
	state := &WizardState{
		Step:        1,
		Version:     "1.0",
		TriggerType: "GitHub",
		TargetType:  "Local",
		MemoryOpt:   "512",
		TimeoutOpt:  "30",
		TestCmd:     "npm test",
	}

	stepArea := container.NewStack()
	progressLabel := widget.NewLabel("Step 1 of 5: Project details")

	var updateStepView func()

	nextBtn := widget.NewButton("Next", nil)
	backBtn := widget.NewButton("Back", nil)

	updateStepView = func() {
		progressLabel.SetText(fmt.Sprintf("Step %d of 5: %s", state.Step, getStepTitle(state.Step)))

		var stepContent fyne.CanvasObject
		switch state.Step {
		case 1:
			stepContent = a.buildWizardStep1(state)
			backBtn.Disable()
			nextBtn.SetText("Next")
		case 2:
			stepContent = a.buildWizardStep2(state)
			backBtn.Enable()
			nextBtn.SetText("Next")
		case 3:
			stepContent = a.buildWizardStep3(state)
			backBtn.Enable()
			nextBtn.SetText("Next")
		case 4:
			stepContent = a.buildWizardStep4(state)
			backBtn.Enable()
			nextBtn.SetText("Next")
		case 5:
			stepContent = a.buildWizardStep5(state)
			backBtn.Enable()
			nextBtn.SetText("Generate Pipeline")
		}

		stepArea.Objects = []fyne.CanvasObject{stepContent}
		stepArea.Refresh()
	}

	nextBtn.OnTapped = func() {
		if state.Step == 5 {
			// Generate
			a.generateKzmConfig(state)
			return
		}
		state.Step++
		updateStepView()
	}

	backBtn.OnTapped = func() {
		if state.Step > 1 {
			state.Step--
			updateStepView()
		}
	}

	updateStepView()

	footer := container.NewBorder(nil, nil, backBtn, nextBtn)
	wizardHeader := container.NewVBox(
		widget.NewLabel("New Pipeline Wizard"),
		progressLabel,
		widget.NewSeparator(),
	)

	return container.NewBorder(wizardHeader, footer, nil, nil, stepArea)
}

func getStepTitle(step int) string {
	switch step {
	case 1:
		return "Project Details"
	case 2:
		return "Trigger Configuration"
	case 3:
		return "Deploy Target Setup"
	case 4:
		return "Tests & Verification"
	case 5:
		return "Notifications & Preview"
	}
	return ""
}

func (a *CodeForgeApp) buildWizardStep1(state *WizardState) fyne.CanvasObject {
	nameEntry := widget.NewEntry()
	nameEntry.SetText(state.Name)
	nameEntry.OnChanged = func(s string) { state.Name = s }

	verEntry := widget.NewEntry()
	verEntry.SetText(state.Version)
	verEntry.OnChanged = func(s string) { state.Version = s }

	descEntry := widget.NewEntry()
	descEntry.SetText(state.Description)
	descEntry.OnChanged = func(s string) { state.Description = s }

	form := widget.NewForm(
		widget.NewFormItem("Project Name", nameEntry),
		widget.NewFormItem("Version", verEntry),
		widget.NewFormItem("Description", descEntry),
	)

	return container.NewScroll(form)
}

func (a *CodeForgeApp) buildWizardStep2(state *WizardState) fyne.CanvasObject {
	box := container.NewVBox()

	triggerTypes := []string{"GitHub", "GitLab", "Local Folder", "Schedule (Cron)", "Manual"}
	var subForm *fyne.Container
	subForm = container.NewVBox()

	var redrawSubForm func()
	redrawSubForm = func() {
		subForm.Objects = nil
		switch state.TriggerType {
		case "GitHub", "GitLab":
			repoEntry := widget.NewEntry()
			repoEntry.SetPlaceHolder("user/repository")
			repoEntry.SetText(state.GitRepo)
			repoEntry.OnChanged = func(s string) { state.GitRepo = s }

			branchEntry := widget.NewEntry()
			branchEntry.SetText("main")
			branchEntry.SetText(state.GitBranch)
			branchEntry.OnChanged = func(s string) { state.GitBranch = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("Repository", repoEntry),
				widget.NewFormItem("Branch", branchEntry),
			))
		case "Local Folder":
			folderEntry := widget.NewEntry()
			folderEntry.SetPlaceHolder("/absolute/path/to/folder")
			folderEntry.SetText(state.FolderPath)
			folderEntry.OnChanged = func(s string) { state.FolderPath = s }

			pickerBtn := widget.NewButton("Browse...", func() {
				dialog.ShowFolderOpen(func(lu fyne.ListableURI, err error) {
					if err == nil && lu != nil {
						folderEntry.SetText(lu.Path())
					}
				}, a.MainWindow)
			})

			subForm.Add(container.NewBorder(nil, nil, nil, pickerBtn, folderEntry))
		case "Schedule (Cron)":
			cronEntry := widget.NewEntry()
			cronEntry.SetPlaceHolder("5m / hourly / daily / cron expression")
			cronEntry.SetText(state.CronExpr)
			cronEntry.OnChanged = func(s string) { state.CronExpr = s }
			subForm.Add(widget.NewForm(widget.NewFormItem("Cron expression", cronEntry)))
		}
		subForm.Refresh()
	}

	drop := widget.NewSelect(triggerTypes, func(s string) {
		state.TriggerType = s
		redrawSubForm()
	})
	drop.SetSelected(state.TriggerType)

	box.Add(widget.NewForm(widget.NewFormItem("Trigger Type", drop)))
	box.Add(subForm)

	redrawSubForm()

	return container.NewScroll(box)
}

func (a *CodeForgeApp) buildWizardStep3(state *WizardState) fyne.CanvasObject {
	box := container.NewVBox()

	targetTypes := []string{"Local", "SSH", "Lambda", "cPanel", "S3", "Docker", "VPS"}
	subForm := container.NewVBox()

	redrawSubForm := func() {
		subForm.Objects = nil
		nameEntry := widget.NewEntry()
		nameEntry.SetText(state.TargetName)
		nameEntry.OnChanged = func(s string) { state.TargetName = s }

		switch state.TargetType {
		case "Local":
			nameEntry.SetText("local-deployment")
			pathEntry := widget.NewEntry()
			pathEntry.SetText(state.PathOption)
			pathEntry.OnChanged = func(s string) { state.PathOption = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("Target Identifier", nameEntry),
				widget.NewFormItem("Destination Path", pathEntry),
			))
		case "SSH":
			nameEntry.SetPlaceHolder("ubuntu@192.168.1.100")
			pathEntry := widget.NewEntry()
			pathEntry.SetText(state.PathOption)
			pathEntry.OnChanged = func(s string) { state.PathOption = s }

			keyEntry := widget.NewEntry()
			keyEntry.SetText(state.KeyOption)
			keyEntry.OnChanged = func(s string) { state.KeyOption = s }

			passEntry := widget.NewPasswordEntry()
			passEntry.SetText(state.PassOption)
			passEntry.OnChanged = func(s string) { state.PassOption = s }

			restartEntry := widget.NewEntry()
			restartEntry.SetText(state.RestartOpt)
			restartEntry.OnChanged = func(s string) { state.RestartOpt = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("SSH Host Address", nameEntry),
				widget.NewFormItem("Remote Path", pathEntry),
				widget.NewFormItem("Key Path", keyEntry),
				widget.NewFormItem("SSH Password", passEntry),
				widget.NewFormItem("Restart Command", restartEntry),
			))
		case "Lambda":
			nameEntry.SetPlaceHolder("my-api-prod")
			regionEntry := widget.NewEntry()
			regionEntry.SetText(state.RegionOpt)
			regionEntry.OnChanged = func(s string) { state.RegionOpt = s }

			runtimeEntry := widget.NewEntry()
			runtimeEntry.SetText(state.RuntimeOpt)
			runtimeEntry.OnChanged = func(s string) { state.RuntimeOpt = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("Lambda Function Name", nameEntry),
				widget.NewFormItem("AWS Region", regionEntry),
				widget.NewFormItem("Runtime", runtimeEntry),
			))
		case "cPanel":
			nameEntry.SetPlaceHolder("ftp.myhost.com")
			userEntry := widget.NewEntry()
			userEntry.SetText(state.UserOption)
			userEntry.OnChanged = func(s string) { state.UserOption = s }

			pathEntry := widget.NewEntry()
			pathEntry.SetText(state.PathOption)
			pathEntry.OnChanged = func(s string) { state.PathOption = s }

			keyEntry := widget.NewEntry()
			keyEntry.SetText(state.KeyOption)
			keyEntry.OnChanged = func(s string) { state.KeyOption = s }

			passEntry := widget.NewPasswordEntry()
			passEntry.SetText(state.PassOption)
			passEntry.OnChanged = func(s string) { state.PassOption = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("cPanel Host domain", nameEntry),
				widget.NewFormItem("Username", userEntry),
				widget.NewFormItem("Remote Path", pathEntry),
				widget.NewFormItem("Key Path", keyEntry),
				widget.NewFormItem("Password", passEntry),
			))
		case "S3":
			nameEntry.SetPlaceHolder("my-bucket-name")
			folderEntry := widget.NewEntry()
			folderEntry.SetText(state.PathOption)
			folderEntry.OnChanged = func(s string) { state.PathOption = s }

			regionEntry := widget.NewEntry()
			regionEntry.SetText(state.RegionOpt)
			regionEntry.OnChanged = func(s string) { state.RegionOpt = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("S3 Bucket Name", nameEntry),
				widget.NewFormItem("Source Folder Path", folderEntry),
				widget.NewFormItem("AWS Region", regionEntry),
			))
		case "Docker":
			nameEntry.SetText("docker-app")
			imageEntry := widget.NewEntry()
			imageEntry.SetText(state.ImageOption)
			imageEntry.OnChanged = func(s string) { state.ImageOption = s }

			serverEntry := widget.NewEntry()
			serverEntry.SetText(state.ServerOption)
			serverEntry.OnChanged = func(s string) { state.ServerOption = s }

			keyEntry := widget.NewEntry()
			keyEntry.SetText(state.KeyOption)
			keyEntry.OnChanged = func(s string) { state.KeyOption = s }

			passEntry := widget.NewPasswordEntry()
			passEntry.SetText(state.PassOption)
			passEntry.OnChanged = func(s string) { state.PassOption = s }

			portEntry := widget.NewEntry()
			portEntry.SetText(state.PortOption)
			portEntry.OnChanged = func(s string) { state.PortOption = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("Container Name", nameEntry),
				widget.NewFormItem("Image Tag", imageEntry),
				widget.NewFormItem("SSH Host Server", serverEntry),
				widget.NewFormItem("SSH Key Path", keyEntry),
				widget.NewFormItem("SSH Password", passEntry),
				widget.NewFormItem("Mapped Port", portEntry),
			))
		case "VPS":
			nameEntry.SetPlaceHolder("root@myserver.com")
			pathEntry := widget.NewEntry()
			pathEntry.SetText(state.PathOption)
			pathEntry.OnChanged = func(s string) { state.PathOption = s }

			keyEntry := widget.NewEntry()
			keyEntry.SetText(state.KeyOption)
			keyEntry.OnChanged = func(s string) { state.KeyOption = s }

			passEntry := widget.NewPasswordEntry()
			passEntry.SetText(state.PassOption)
			passEntry.OnChanged = func(s string) { state.PassOption = s }

			restartEntry := widget.NewEntry()
			restartEntry.SetText(state.RestartOpt)
			restartEntry.OnChanged = func(s string) { state.RestartOpt = s }

			subForm.Add(widget.NewForm(
				widget.NewFormItem("VPS Host Server", nameEntry),
				widget.NewFormItem("Target Path", pathEntry),
				widget.NewFormItem("SSH Key Path", keyEntry),
				widget.NewFormItem("SSH Password", passEntry),
				widget.NewFormItem("Restart Command", restartEntry),
			))
		}
		subForm.Refresh()
	}

	drop := widget.NewSelect(targetTypes, func(s string) {
		state.TargetType = s
		redrawSubForm()
	})
	drop.SetSelected(state.TargetType)

	box.Add(widget.NewForm(widget.NewFormItem("Target Type", drop)))
	box.Add(subForm)

	redrawSubForm()

	return container.NewScroll(box)
}

func (a *CodeForgeApp) buildWizardStep4(state *WizardState) fyne.CanvasObject {
	cmdEntry := widget.NewEntry()
	cmdEntry.SetText(state.TestCmd)
	cmdEntry.OnChanged = func(s string) { state.TestCmd = s }

	check := widget.NewCheck("Run tests before deploy phase", func(checked bool) {
		state.RunTests = checked
		if checked {
			cmdEntry.Enable()
		} else {
			cmdEntry.Disable()
		}
	})
	check.SetChecked(state.RunTests)

	if !state.RunTests {
		cmdEntry.Disable()
	}

	form := widget.NewForm(
		widget.NewFormItem("", check),
		widget.NewFormItem("Test Script Command", cmdEntry),
	)

	return container.NewScroll(form)
}

func (a *CodeForgeApp) buildWizardStep5(state *WizardState) fyne.CanvasObject {
	previewArea := widget.NewMultiLineEntry()
	previewArea.TextStyle = fyne.TextStyle{Monospace: true}

	slackHookEntry := widget.NewEntry()
	slackHookEntry.SetPlaceHolder("https://hooks.slack.com/...")
	slackHookEntry.SetText(state.SlackHook)
	slackHookEntry.OnChanged = func(s string) {
		state.SlackHook = s
		previewArea.SetText(a.generateKzmPreviewText(state))
	}

	slackCheck := widget.NewCheck("Slack notification", func(checked bool) {
		state.SlackOn = checked
		if checked {
			slackHookEntry.Enable()
		} else {
			slackHookEntry.Disable()
		}
		previewArea.SetText(a.generateKzmPreviewText(state))
	})
	slackCheck.SetChecked(state.SlackOn)

	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("ops@company.com")
	emailEntry.SetText(state.EmailAddr)
	emailEntry.OnChanged = func(s string) {
		state.EmailAddr = s
		previewArea.SetText(a.generateKzmPreviewText(state))
	}

	emailCheck := widget.NewCheck("Email notification", func(checked bool) {
		state.EmailOn = checked
		if checked {
			emailEntry.Enable()
		} else {
			emailEntry.Disable()
		}
		previewArea.SetText(a.generateKzmPreviewText(state))
	})
	emailCheck.SetChecked(state.EmailOn)

	if !state.SlackOn {
		slackHookEntry.Disable()
	}
	if !state.EmailOn {
		emailEntry.Disable()
	}

	form := widget.NewForm(
		widget.NewFormItem("", slackCheck),
		widget.NewFormItem("Slack Webhook URL", slackHookEntry),
		widget.NewFormItem("", emailCheck),
		widget.NewFormItem("Recipient Email", emailEntry),
	)

	previewArea.SetText(a.generateKzmPreviewText(state))

	layout := container.NewHSplit(form, previewArea)
	layout.SetOffset(0.4)

	return layout
}

func (a *CodeForgeApp) generateKzmPreviewText(state *WizardState) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("project %q\n", state.Name))
	sb.WriteString(fmt.Sprintf("version %q\n", state.Version))
	if state.Description != "" {
		sb.WriteString(fmt.Sprintf("description %q\n", state.Description))
	}
	sb.WriteString("\n")

	// Trigger
	switch state.TriggerType {
	case "GitHub":
		sb.WriteString(fmt.Sprintf("watch github %q on branch %q\n", state.GitRepo, state.GitBranch))
	case "GitLab":
		sb.WriteString(fmt.Sprintf("watch gitlab %q on branch %q\n", state.GitRepo, state.GitBranch))
	case "Local Folder":
		sb.WriteString(fmt.Sprintf("watch folder %q\n", state.FolderPath))
	case "Schedule (Cron)":
		sb.WriteString(fmt.Sprintf("every %q\n", state.CronExpr))
	default:
		sb.WriteString("on trigger \"deploy\"\n")
	}
	sb.WriteString("\n")

	// Secrets fallback
	sb.WriteString("use secrets from \"~/.codeforge/secrets.enc\"\n\n")

	// Before Steps
	if state.RunTests && state.TestCmd != "" {
		sb.WriteString("before deploy:\n")
		sb.WriteString(fmt.Sprintf("  run %q must pass or rollback\n\n", state.TestCmd))
	}

	// Deploy Target
	switch state.TargetType {
	case "Local":
		sb.WriteString(fmt.Sprintf("deploy to local %q:\n", state.TargetName))
		sb.WriteString(fmt.Sprintf("  path %q\n", state.PathOption))
	case "SSH":
		sb.WriteString(fmt.Sprintf("deploy to ssh %q at %q:\n", state.TargetName, state.PathOption))
		if state.KeyOption != "" {
			sb.WriteString(fmt.Sprintf("  key %q\n", state.KeyOption))
		}
		if state.PassOption != "" {
			sb.WriteString(fmt.Sprintf("  password %q\n", state.PassOption))
		}
		sb.WriteString(fmt.Sprintf("  restart %q\n", state.RestartOpt))
	case "Lambda":
		sb.WriteString(fmt.Sprintf("deploy to lambda %q:\n", state.TargetName))
		sb.WriteString(fmt.Sprintf("  region %q\n", state.RegionOpt))
		sb.WriteString(fmt.Sprintf("  runtime %q\n", state.RuntimeOpt))
		sb.WriteString("  memory 512\n")
		sb.WriteString("  timeout 30\n")
	case "cPanel":
		sb.WriteString(fmt.Sprintf("deploy to cpanel %q at %q:\n", state.TargetName, state.PathOption))
		sb.WriteString(fmt.Sprintf("  user %q\n", state.UserOption))
		if state.KeyOption != "" {
			sb.WriteString(fmt.Sprintf("  key %q\n", state.KeyOption))
		}
		if state.PassOption != "" {
			sb.WriteString(fmt.Sprintf("  password %q\n", state.PassOption))
		}
		sb.WriteString("  exclude \".env,vendor,.git\"\n")
	case "S3":
		sb.WriteString(fmt.Sprintf("deploy to s3 %q:\n", state.TargetName))
		sb.WriteString(fmt.Sprintf("  folder %q\n", state.PathOption))
		sb.WriteString(fmt.Sprintf("  region %q\n", state.RegionOpt))
		sb.WriteString("  public yes\n")
		sb.WriteString("  invalidate cloudfront\n")
	case "Docker":
		sb.WriteString(fmt.Sprintf("deploy to docker %q:\n", state.TargetName))
		sb.WriteString(fmt.Sprintf("  image %q\n", state.ImageOption))
		sb.WriteString(fmt.Sprintf("  server %q\n", state.ServerOption))
		if state.KeyOption != "" {
			sb.WriteString(fmt.Sprintf("  key %q\n", state.KeyOption))
		}
		if state.PassOption != "" {
			sb.WriteString(fmt.Sprintf("  password %q\n", state.PassOption))
		}
		sb.WriteString(fmt.Sprintf("  port %s\n", state.PortOption))
		sb.WriteString("  restart always\n")
	case "VPS":
		sb.WriteString(fmt.Sprintf("deploy to vps %q:\n", state.TargetName))
		sb.WriteString(fmt.Sprintf("  path %q\n", state.PathOption))
		if state.KeyOption != "" {
			sb.WriteString(fmt.Sprintf("  key %q\n", state.KeyOption))
		}
		if state.PassOption != "" {
			sb.WriteString(fmt.Sprintf("  password %q\n", state.PassOption))
		}
		sb.WriteString(fmt.Sprintf("  restart %q\n", state.RestartOpt))
		sb.WriteString("  git yes\n")
	}
	sb.WriteString("\n")

	if state.SlackOn && state.SlackHook != "" {
		sb.WriteString(fmt.Sprintf("notify slack %q\n", state.SlackHook))
	}
	if state.EmailOn && state.EmailAddr != "" {
		sb.WriteString(fmt.Sprintf("notify email %q\n", state.EmailAddr))
	}

	return sb.String()
}

func (a *CodeForgeApp) generateKzmConfig(state *WizardState) {
	if state.Name == "" {
		dialog.ShowError(fmt.Errorf("project Name is required"), a.MainWindow)
		return
	}

	kzmContent := a.generateKzmPreviewText(state)
	fileName := sanitizeFilename(state.Name) + ".kzm"

	home, _ := os.UserHomeDir()
	destPath := filepath.Join(home, ".codeforge", "pipelines", fileName)

	err := os.WriteFile(destPath, []byte(kzmContent), 0644)
	if err != nil {
		dialog.ShowError(err, a.MainWindow)
		return
	}

	a.Daemon.ReloadPipeline(destPath)
	dialog.ShowInformation("Pipeline Generated", fmt.Sprintf("Configuration generated successfully at: %s", destPath), a.MainWindow)
	a.NavigateTo("pipelines")
}

func sanitizeFilename(name string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	return strings.ToLower(r.Replace(name))
}
