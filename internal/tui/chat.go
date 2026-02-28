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
	prog   *tea.Program // set after NewProgram, used to send streaming tokens

	// Conversation
	messages []api.Message
	input    string
	history  []string // command history

	// State
	streaming    bool
	streamBuf    strings.Builder
	err          error
	quitting     bool

	// Stats — updated from server-reported Usage after each response.
	promptTokens int // cumulative prompt tokens (latest server report)
	genTokens    int // cumulative completion tokens (latest server report)
	lastGenStart time.Time
	lastGenDur   time.Duration

	// Terminal
	width  int
	height int
}

// tokenMsg delivers a streamed token to the TUI.
type tokenMsg struct {
	content string
}

// streamDoneMsg signals the stream completed with usage and finish reason.
type streamDoneMsg struct {
	usage        api.Usage
	finishReason string
}

// streamErrMsg delivers a streaming error.
type streamErrMsg struct{ err error }

// RunChat launches the interactive chat TUI.
func RunChat(cfg ChatConfig) error {
	m := &chatModel{
		cfg:    cfg,
		client: cfg.Client,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	m.prog = p
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
		m.streamBuf.WriteString(msg.content)
		return m, nil

	case streamDoneMsg:
		m.streaming = false
		m.lastGenDur = time.Since(m.lastGenStart)

		content := m.streamBuf.String()
		if content != "" {
			m.messages = append(m.messages, api.Message{
				Role:    "assistant",
				Content: content,
			})
		}
		m.streamBuf.Reset()

		// Update token counts from server-reported usage.
		if msg.usage.TotalTokens > 0 {
			m.promptTokens = msg.usage.PromptTokens
			m.genTokens = msg.usage.CompletionTokens
		}

		// Warn if generation was truncated by token limit.
		if msg.finishReason == "length" {
			m.err = fmt.Errorf("response truncated (hit token limit)")
		}
		return m, nil

	case streamErrMsg:
		m.streaming = false
		m.err = msg.err
		m.streamBuf.Reset()
		return m, nil

	case saveResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.messages = append(m.messages, api.Message{
				Role:    "system",
				Content: fmt.Sprintf("Conversation saved to %s", msg.path),
			})
		}
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
	m.lastGenStart = time.Now()

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
		speed := 0.0
		if m.lastGenDur > 0 && m.genTokens > 0 {
			speed = float64(m.genTokens) / m.lastGenDur.Seconds()
		}
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

		var finishReason string
		usage, err := m.client.ChatCompletionStream(context.Background(), req, func(delta api.StreamDelta) {
			if len(delta.Choices) > 0 {
				choice := delta.Choices[0]
				if choice.Delta != nil && choice.Delta.Content != "" {
					m.prog.Send(tokenMsg{content: choice.Delta.Content})
				}
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}
			}
		})

		if err != nil {
			return streamErrMsg{err: err}
		}

		done := streamDoneMsg{finishReason: finishReason}
		if usage != nil {
			done.usage = *usage
		}
		return done
	}
}

// saveResultMsg delivers the result of a save operation.
type saveResultMsg struct {
	path string
	err  error
}

func (m *chatModel) saveConversation(filename string) tea.Cmd {
	return func() tea.Msg {
		var b strings.Builder
		for _, msg := range m.messages {
			b.WriteString(fmt.Sprintf("[%s]\n%s\n\n", msg.Role, msg.Content))
		}
		if err := os.WriteFile(filename, []byte(b.String()), 0644); err != nil {
			return saveResultMsg{err: fmt.Errorf("saving conversation: %w", err)}
		}
		return saveResultMsg{path: filename}
	}
}
