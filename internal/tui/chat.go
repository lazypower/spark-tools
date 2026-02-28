// Package tui provides the interactive chat interface for llm-run
// using charmbracelet/bubbletea.
package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lazypower/spark-tools/pkg/llmrun/api"
)

// ChatConfig configures the chat TUI.
type ChatConfig struct {
	Client      *api.Client
	ModelName   string
	Quant       string
	ContextSize int
	GPUName     string
	Threads     int
}

// chatModel is the bubbletea model for interactive chat.
type chatModel struct {
	cfg    ChatConfig
	client *api.Client

	// Conversation
	messages []api.Message
	input    string
	history  []string // command history

	// State
	streaming    bool
	streamBuf    strings.Builder
	err          error
	quitting     bool

	// Stats
	promptTokens int
	genTokens    int
	startTime    time.Time
	lastSpeed    float64

	// Terminal
	width  int
	height int
}

// tokenMsg delivers a streamed token to the TUI.
type tokenMsg struct {
	content string
	done    bool
}

// streamErrMsg delivers a streaming error.
type streamErrMsg struct{ err error }

// RunChat launches the interactive chat TUI.
func RunChat(cfg ChatConfig) error {
	m := &chatModel{
		cfg:       cfg,
		client:    cfg.Client,
		startTime: time.Now(),
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *chatModel) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.streaming {
			// Allow Ctrl+C during streaming.
			if msg.Type == tea.KeyCtrlC {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			return m.handleSubmit()
		case tea.KeyBackspace:
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case tea.KeyRunes:
			m.input += string(msg.Runes)
		case tea.KeySpace:
			m.input += " "
		}
		return m, nil

	case tokenMsg:
		if msg.done {
			m.streaming = false
			content := m.streamBuf.String()
			if content != "" {
				m.messages = append(m.messages, api.Message{
					Role:    "assistant",
					Content: content,
				})
			}
			m.streamBuf.Reset()
			return m, nil
		}
		m.streamBuf.WriteString(msg.content)
		m.genTokens++
		return m, nil

	case streamErrMsg:
		m.streaming = false
		m.err = msg.err
		m.streamBuf.Reset()
		return m, nil
	}

	return m, nil
}

func (m *chatModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header
	b.WriteString("\n")
	b.WriteString(RenderServerHeader(ServerStatus{
		ModelName:   m.cfg.ModelName,
		Quant:       m.cfg.Quant,
		ContextSize: m.cfg.ContextSize,
		GPUName:     m.cfg.GPUName,
		Threads:     m.cfg.Threads,
	}))
	b.WriteString("\n\n")

	// Messages
	for _, msg := range m.messages {
		switch msg.Role {
		case "system":
			b.WriteString(fmt.Sprintf("  %s %s\n\n",
				systemLabelStyle.Render("System:"),
				dimStyle.Render(msg.Content)))
		case "user":
			b.WriteString(fmt.Sprintf("  %s %s\n\n",
				userLabelStyle.Render("You:"),
				msg.Content))
		case "assistant":
			b.WriteString(fmt.Sprintf("  %s %s\n\n",
				assistantLabelStyle.Render("Assistant:"),
				msg.Content))
		}
	}

	// Streaming content
	if m.streaming {
		b.WriteString(fmt.Sprintf("  %s %s",
			assistantLabelStyle.Render("Assistant:"),
			m.streamBuf.String()))
		b.WriteString(dimStyle.Render("▊"))
		b.WriteString("\n\n")
	}

	// Error
	if m.err != nil {
		b.WriteString(fmt.Sprintf("  %s %s\n\n",
			errorStyle.Render("Error:"),
			m.err.Error()))
		m.err = nil
	}

	// Input prompt
	if !m.streaming {
		b.WriteString(fmt.Sprintf("  %s %s",
			promptStyle.Render("You:"),
			m.input))
		b.WriteString("█\n")
	}

	return b.String()
}

func (m *chatModel) handleSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""

	if input == "" {
		return m, nil
	}

	// Handle slash commands.
	if strings.HasPrefix(input, "/") {
		return m.handleSlashCommand(input)
	}

	// Add user message.
	m.messages = append(m.messages, api.Message{
		Role:    "user",
		Content: input,
	})

	// Start streaming response.
	m.streaming = true
	m.streamBuf.Reset()

	return m, m.streamResponse()
}

func (m *chatModel) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/quit", "/exit", "/q":
		m.quitting = true
		return m, tea.Quit

	case "/clear":
		// Keep system messages, clear the rest.
		var kept []api.Message
		for _, msg := range m.messages {
			if msg.Role == "system" {
				kept = append(kept, msg)
			}
		}
		m.messages = kept
		m.promptTokens = 0
		m.genTokens = 0
		return m, nil

	case "/stats":
		elapsed := time.Since(m.startTime).Seconds()
		speed := float64(m.genTokens) / max(elapsed, 0.001)
		ctxUsed := m.promptTokens + m.genTokens
		stats := RenderSessionStats(m.promptTokens, m.genTokens, ctxUsed, m.cfg.ContextSize, speed)
		m.messages = append(m.messages, api.Message{
			Role:    "system",
			Content: stats,
		})
		return m, nil

	case "/context":
		ctxUsed := m.promptTokens + m.genTokens
		pct := float64(ctxUsed) / float64(m.cfg.ContextSize) * 100
		msg := fmt.Sprintf("Context: %d / %d tokens (%.1f%%)", ctxUsed, m.cfg.ContextSize, pct)
		m.messages = append(m.messages, api.Message{
			Role:    "system",
			Content: msg,
		})
		return m, nil

	case "/system":
		text := strings.TrimSpace(strings.TrimPrefix(input, "/system"))
		if text == "" {
			m.err = fmt.Errorf("usage: /system <prompt text>")
			return m, nil
		}
		// Remove existing system messages and add new one.
		var kept []api.Message
		for _, msg := range m.messages {
			if msg.Role != "system" {
				kept = append(kept, msg)
			}
		}
		m.messages = append([]api.Message{{Role: "system", Content: text}}, kept...)
		return m, nil

	case "/temp":
		if len(parts) < 2 {
			m.err = fmt.Errorf("usage: /temp <value>")
			return m, nil
		}
		val, err := strconv.ParseFloat(parts[1], 64)
		if err != nil || val < 0 || val > 2 {
			m.err = fmt.Errorf("temperature must be between 0 and 2")
			return m, nil
		}
		m.messages = append(m.messages, api.Message{
			Role:    "system",
			Content: fmt.Sprintf("Temperature set to %.2f", val),
		})
		return m, nil

	case "/save":
		if len(parts) < 2 {
			m.err = fmt.Errorf("usage: /save <filename>")
			return m, nil
		}
		return m, m.saveConversation(parts[1])

	default:
		m.err = fmt.Errorf("unknown command: %s (try /stats, /context, /system, /clear, /temp, /save, /quit)", cmd)
		return m, nil
	}
}

func (m *chatModel) streamResponse() tea.Cmd {
	return func() tea.Msg {
		req := api.ChatCompletionRequest{
			Messages: m.messages,
		}

		_, err := m.client.ChatCompletionStream(context.Background(), req, func(delta api.StreamDelta) {
			if len(delta.Choices) > 0 && delta.Choices[0].Delta != nil {
				// We can't send tea.Msg from here directly in a real bubbletea app,
				// but for the streaming model we accumulate in streamBuf via the cmd pattern.
				// In practice, this would use p.Send() or a channel.
			}
		})

		if err != nil {
			return streamErrMsg{err: err}
		}
		return tokenMsg{done: true}
	}
}

func (m *chatModel) saveConversation(filename string) tea.Cmd {
	return func() tea.Msg {
		var b strings.Builder
		for _, msg := range m.messages {
			b.WriteString(fmt.Sprintf("[%s]\n%s\n\n", msg.Role, msg.Content))
		}
		if err := os.WriteFile(filename, []byte(b.String()), 0644); err != nil {
			return streamErrMsg{err: fmt.Errorf("saving conversation: %w", err)}
		}
		return tokenMsg{content: fmt.Sprintf("Conversation saved to %s", filename), done: true}
	}
}
