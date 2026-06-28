package ui

import (
	"strings"
	"testing"
)

// PickGGUFFile and Confirm drive charmbracelet/huh forms via form.Run(), which
// requires an interactive TTY and cannot run hermetically (see the deferral note
// in docs/internal-seam-test-audit.md). The one branch that executes BEFORE the
// form — the empty-items guard — is the hermetic seam we can lock here.

func TestPickGGUFFile_EmptyItemsErrors(t *testing.T) {
	got, err := PickGGUFFile("pick one", nil)
	if err == nil {
		t.Fatal("PickGGUFFile with no items must return an error, not block on a form")
	}
	if got != "" {
		t.Errorf("expected empty selection on error, got %q", got)
	}
	if !strings.Contains(err.Error(), "no files") {
		t.Errorf("error should explain there are no files, got %q", err.Error())
	}
}
