package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmrun/api"
)

// handleSlashCommand mutates the model and returns a tea.Cmd; none of these
// branches touch the terminal or network, so they lock real user-facing chat
// behavior hermetically. (The bubbletea Run loop and streaming are deferred —
// see docs/internal-seam-test-audit.md.)

func newModel(msgs ...api.Message) *chatModel {
	return &chatModel{cfg: ChatConfig{ContextSize: 4096}, messages: msgs}
}

func TestSlash_ClearKeepsSystem(t *testing.T) {
	m := newModel(
		api.Message{Role: "system", Content: "you are helpful"},
		api.Message{Role: "user", Content: "hi"},
		api.Message{Role: "assistant", Content: "hello"},
	)
	m.promptTokens, m.genTokens = 10, 20
	m.handleSlashCommand("/clear")
	if len(m.messages) != 1 || m.messages[0].Role != "system" {
		t.Fatalf("/clear must keep only system messages, got %+v", m.messages)
	}
	if m.promptTokens != 0 || m.genTokens != 0 {
		t.Errorf("/clear must reset token counters, got %d/%d", m.promptTokens, m.genTokens)
	}
}

func TestSlash_SystemReplacesAndPrepends(t *testing.T) {
	m := newModel(
		api.Message{Role: "system", Content: "old"},
		api.Message{Role: "user", Content: "hi"},
	)
	m.handleSlashCommand("/system you are a pirate")
	if m.messages[0].Role != "system" || m.messages[0].Content != "you are a pirate" {
		t.Fatalf("/system must replace+prepend the system prompt, got %+v", m.messages)
	}
	// Exactly one system message remains.
	n := 0
	for _, msg := range m.messages {
		if msg.Role == "system" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly one system message, got %d", n)
	}
}

func TestSlash_SystemEmptyIsUsageError(t *testing.T) {
	m := newModel()
	m.handleSlashCommand("/system   ")
	if m.err == nil || !strings.Contains(m.err.Error(), "usage") {
		t.Errorf("empty /system must set a usage error, got %v", m.err)
	}
}

func TestSlash_TempValidation(t *testing.T) {
	for _, bad := range []string{"/temp", "/temp abc", "/temp -1", "/temp 2.5"} {
		m := newModel()
		m.handleSlashCommand(bad)
		if m.err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	m := newModel()
	m.handleSlashCommand("/temp 0.7")
	if m.err != nil {
		t.Errorf("/temp 0.7 must be accepted, got %v", m.err)
	}
}

func TestSlash_UnknownCommand(t *testing.T) {
	m := newModel()
	m.handleSlashCommand("/bogus")
	if m.err == nil || !strings.Contains(m.err.Error(), "unknown command") {
		t.Errorf("unknown command must set an error, got %v", m.err)
	}
}

func TestSlash_QuitSetsQuitting(t *testing.T) {
	for _, q := range []string{"/quit", "/exit", "/q"} {
		m := newModel()
		m.handleSlashCommand(q)
		if !m.quitting {
			t.Errorf("%q must set quitting", q)
		}
	}
}

func TestSlash_ContextReportsUsage(t *testing.T) {
	m := newModel()
	m.promptTokens, m.genTokens = 1024, 1024 // 2048 / 4096 = 50%
	m.handleSlashCommand("/context")
	last := m.messages[len(m.messages)-1]
	if !strings.Contains(last.Content, "2048 / 4096") || !strings.Contains(last.Content, "50.0%") {
		t.Errorf("/context must report usage math, got %q", last.Content)
	}
}

func TestSlash_SaveUsageErrorAndWrite(t *testing.T) {
	// Missing filename → usage error, no cmd.
	m := newModel()
	if _, cmd := m.handleSlashCommand("/save"); cmd != nil {
		t.Error("/save without filename must not return a write cmd")
	}
	if m.err == nil {
		t.Error("/save without filename must set a usage error")
	}

	// With a path → the returned cmd writes role-tagged content.
	path := filepath.Join(t.TempDir(), "convo.txt")
	m2 := newModel(
		api.Message{Role: "user", Content: "ping"},
		api.Message{Role: "assistant", Content: "pong"},
	)
	_, cmd := m2.handleSlashCommand("/save " + path)
	if cmd == nil {
		t.Fatal("/save <file> must return a write cmd")
	}
	msg := cmd()
	res, ok := msg.(saveResultMsg)
	if !ok || res.err != nil {
		t.Fatalf("save cmd must succeed, got %+v", msg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("conversation file not written: %v", err)
	}
	for _, want := range []string{"[user]", "ping", "[assistant]", "pong"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("saved conversation missing %q, got:\n%s", want, data)
		}
	}
}
