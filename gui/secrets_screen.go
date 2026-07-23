package gui

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"codeforge/internal/secrets"
)

// buildSecretsScreen shows the vault credential dashboard allowing sets, copies, and deletes.
func (a *CodeForgeApp) buildSecretsScreen() fyne.CanvasObject {
	title := widget.NewLabel("Secrets Vault")
	title.TextStyle = fyne.TextStyle{Bold: true}

	addBtn := widget.NewButton("Add Secret", func() {
		a.showAddSecretModal()
	})

	header := container.NewBorder(nil, nil, nil, addBtn, title)

	listContainer := container.NewVBox()
	a.populateSecretsList(listContainer)

	scroll := container.NewVScroll(listContainer)
	scroll.SetMinSize(fyne.NewSize(0, 450))

	return container.NewBorder(header, nil, nil, nil, scroll)
}

func (a *CodeForgeApp) populateSecretsList(list *fyne.Container) {
	list.Objects = nil

	home, _ := os.UserHomeDir()
	storePath := filepath.Join(home, ".codeforge", "secrets.enc")

	store, err := secrets.LoadStore(storePath)
	if err != nil {
		list.Add(widget.NewLabel("Failed to load secrets vault: " + err.Error()))
		return
	}

	keys := store.List()
	if len(keys) == 0 {
		list.Add(widget.NewLabel("No secrets stored in vault. Click 'Add Secret' to register one."))
		return
	}

	for _, k := range keys {
		keyName := k // bind local copy

		keyLbl := widget.NewLabel(keyName)
		keyLbl.TextStyle = fyne.TextStyle{Bold: true}

		maskLbl := widget.NewLabel("••••••••••••")

		copyBtn := widget.NewButton("Copy", func() {
			val, err := store.Get(keyName)
			if err == nil {
				a.MainWindow.Clipboard().SetContent(val)
				// Silent copy - no dialog, maybe status update or log statement
				a.Logger.Log("secrets", "INFO", "Secret key %q copied to clipboard.", keyName)
			}
		})

		deleteBtn := widget.NewButton("Delete", func() {
			d := dialog.NewConfirm("Delete Secret", fmt.Sprintf("Are you sure you want to delete secret %q?", keyName), func(confirm bool) {
				if confirm {
					_ = store.Delete(keyName)
					a.populateSecretsList(list)
				}
			}, a.MainWindow)
			d.Show()
		})
		deleteBtn.Importance = widget.DangerImportance

		row := container.NewHBox(
			keyLbl,
			maskLbl,
			layout.NewSpacer(),
			copyBtn,
			deleteBtn,
		)
		list.Add(row)
	}

	list.Refresh()
}

func (a *CodeForgeApp) showAddSecretModal() {
	keyEntry := widget.NewEntry()
	keyEntry.SetPlaceHolder("AWS_ACCESS_KEY_ID")

	valEntry := widget.NewPasswordEntry()
	valEntry.SetPlaceHolder("Enter secret value")

	form := widget.NewForm(
		widget.NewFormItem("Key", keyEntry),
		widget.NewFormItem("Value", valEntry),
	)

	title := widget.NewLabel("🔒 Add New Secret")
	title.TextStyle = fyne.TextStyle{Bold: true}

	var modal *widget.PopUp

	saveBtn := widget.NewButton("Save Secret", func() {
		key := keyEntry.Text
		val := valEntry.Text
		if key == "" || val == "" {
			dialog.ShowError(fmt.Errorf("both Key and Value are required"), a.MainWindow)
			return
		}

		home, _ := os.UserHomeDir()
		storePath := filepath.Join(home, ".codeforge", "secrets.enc")

		store, err := secrets.LoadStore(storePath)
		if err == nil {
			err = store.Set(key, val)
			if err == nil {
				modal.Hide()
				a.NavigateTo("secrets") // refresh screen
				return
			}
		}
		dialog.ShowError(err, a.MainWindow)
	})
	saveBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("Cancel", func() {
		modal.Hide()
	})

	buttons := container.NewHBox(
		layout.NewSpacer(),
		cancelBtn,
		saveBtn,
	)

	box := container.NewVBox(
		title,
		widget.NewSeparator(),
		container.NewPadded(form),
		widget.NewSeparator(),
		buttons,
	)

	paddedContent := container.NewPadded(box)

	// Wrap in a fixed min-size container so inputs and buttons have generous space
	minSizeContainer := container.NewStack(paddedContent)
	minSizeContainer.Resize(fyne.NewSize(480, 240))

	// Ensure layout honors min size
	constrained := container.NewGridWrap(fyne.NewSize(480, 240), minSizeContainer)

	modal = widget.NewModalPopUp(constrained, a.MainWindow.Canvas())
	modal.Show()
}
