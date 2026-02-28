// Package ui provides interactive terminal components using
// charmbracelet/huh for selection pickers and confirmations.
package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/lazypower/spark-tools/internal/progress"
	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
)

// PickerItem represents a selectable file in the GGUF picker.
type PickerItem struct {
	Filename      string
	Quantization  string
	Size          int64
	BitsPerWeight float64
	Fit           gguf.FitResult
}

// PickGGUFFile presents an interactive picker for GGUF files and returns
// the selected filename. Returns empty string if the user cancels.
func PickGGUFFile(title string, items []PickerItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no files to pick from")
	}

	var options []huh.Option[string]
	for _, item := range items {
		fitLabel := ""
		if item.Fit.Status != gguf.FitUnknown {
			fitLabel = "  " + item.Fit.Label
		}

		label := item.Quantization
		if label == "" {
			label = item.Filename
		}
		label = fmt.Sprintf("%-10s %s%s", label, progress.FormatSize(item.Size), fitLabel)
		options = append(options, huh.NewOption(label, item.Filename))
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Description("Select a file to download").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}

// Confirm shows a yes/no confirmation prompt.
func Confirm(title string) (bool, error) {
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Value(&confirmed),
		),
	)
	if err := form.Run(); err != nil {
		return false, err
	}
	return confirmed, nil
}
