package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Enter must submit the message even in multiline mode — the previous binding
// (Enter=newline, Alt+Enter=submit) left no way to send on macOS terminals that
// don't map Alt to Meta.
func TestChat_EnterSubmits(t *testing.T) {
	m := &chatModel{cfg: ChatConfig{ContextSize: 4096, MultiLine: true}}
	m.input = "hello"
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.input != "" {
		t.Errorf("Enter must submit (clear the input), got %q", m.input)
	}
	if len(m.messages) != 1 || m.messages[0].Content != "hello" {
		t.Errorf("Enter must submit the typed message, got %+v", m.messages)
	}
}

// Alt+Enter is the multiline newline affordance and must NOT submit.
func TestChat_AltEnterInsertsNewline(t *testing.T) {
	m := &chatModel{cfg: ChatConfig{ContextSize: 4096, MultiLine: true}}
	m.input = "line1"
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if m.input != "line1\n" {
		t.Errorf("Alt+Enter must insert a newline, got %q", m.input)
	}
	if len(m.messages) != 0 {
		t.Error("Alt+Enter must not submit")
	}
}

// Backspace must delete a whole rune — byte-slicing corrupts multibyte chars.
func TestChat_BackspaceIsRuneAware(t *testing.T) {
	m := &chatModel{cfg: ChatConfig{ContextSize: 4096}}
	m.input = "café"
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.input != "caf" {
		t.Errorf("backspace must delete a whole rune (é), got %q", m.input)
	}
}

// Pasted content must have carriage returns stripped — bracketed paste can carry
// raw \r that would corrupt the line.
func TestChat_PasteStripsCarriageReturns(t *testing.T) {
	m := &chatModel{cfg: ChatConfig{ContextSize: 4096}}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\rb")})
	if m.input != "ab" {
		t.Errorf("paste must strip \\r, got %q", m.input)
	}
}
