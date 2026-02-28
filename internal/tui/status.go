package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// ServerStatus holds display information for the server status bar.
type ServerStatus struct {
	ModelName   string
	Quant       string
	ContextSize int
	GPUName     string
	Threads     int
	Host        string
	Port        int
	Parallel    int
}

// RenderServerHeader renders the server info header box.
func RenderServerHeader(s ServerStatus) string {
	modelLabel := s.ModelName
	if s.Quant != "" {
		modelLabel += " (" + s.Quant + ")"
	}

	var details []string
	if s.ContextSize > 0 {
		details = append(details, fmt.Sprintf("Context: %d tokens", s.ContextSize))
	}
	if s.GPUName != "" {
		details = append(details, fmt.Sprintf("GPU: %s", s.GPUName))
	}
	if s.Threads > 0 {
		details = append(details, fmt.Sprintf("Threads: %d", s.Threads))
	}

	title := headerStyle.Render(modelLabel)
	info := dimStyle.Render(joinPipe(details))

	content := title + "\n" + info
	return headerBoxStyle.Render(content)
}

// RenderServerEndpoints renders the endpoint list for server mode.
func RenderServerEndpoints(host string, port int) string {
	base := fmt.Sprintf("http://%s:%d", host, port)
	lines := fmt.Sprintf("  Endpoints (served by llama-server):\n"+
		"    POST %s/v1/chat/completions    (OpenAI-compatible)\n"+
		"    POST %s/v1/completions         (OpenAI-compatible)\n"+
		"    GET  %s/v1/models              (OpenAI-compatible)\n"+
		"    GET  %s/health                 (llama-server health check)",
		base, base, base, base)
	return lines
}

// RenderSessionStats renders session statistics.
func RenderSessionStats(promptTokens, genTokens, ctxUsed, ctxTotal int, tokPerSec float64) string {
	ctxPct := float64(ctxUsed) / float64(ctxTotal) * 100

	content := fmt.Sprintf(
		"%s\n"+
			"  Prompt tokens:    %-8d  Speed:  %.1f tok/s\n"+
			"  Generated tokens: %-8d  Context used: %.1f%% (%d/%d)",
		statsLabelStyle.Render("Session Stats"),
		promptTokens, tokPerSec,
		genTokens, ctxPct, ctxUsed, ctxTotal,
	)
	return statsBoxStyle.Render(content)
}

func joinPipe(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += lipgloss.NewStyle().Faint(true).Render(" │ ")
		}
		result += p
	}
	return result
}
