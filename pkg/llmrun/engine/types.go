package engine

import (
	"fmt"
	"strings"
	"time"
)

// RunConfig represents a complete inference configuration.
type RunConfig struct {
	// Model
	ModelRef  string `json:"modelRef,omitempty"`  // hfetch model reference or local path
	ModelPath string `json:"modelPath,omitempty"` // Resolved absolute path to .gguf file

	// Hardware allocation
	GPULayers int `json:"gpuLayers,omitempty"` // Number of layers to offload to GPU (-1 = all)
	Threads   int `json:"threads,omitempty"`   // CPU threads for inference
	MainGPU   int `json:"mainGPU,omitempty"`   // Primary GPU index

	// Memory and context
	ContextSize int `json:"contextSize,omitempty"` // Context window size in tokens
	BatchSize   int `json:"batchSize,omitempty"`   // Batch size for prompt processing
	UBatchSize  int `json:"uBatchSize,omitempty"`  // Micro-batch size

	// Generation
	Temperature   float64 `json:"temperature,omitempty"`
	TopP          float64 `json:"topP,omitempty"`
	TopK          int     `json:"topK,omitempty"`
	RepeatPenalty float64 `json:"repeatPenalty,omitempty"`
	Seed          int     `json:"seed,omitempty"` // -1 = random

	// Server mode
	ServerMode bool   `json:"serverMode,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Parallel   int    `json:"parallel,omitempty"` // Parallel request slots
	APIKey     string `json:"apiKey,omitempty"`   // Required API key for server

	// Prompt
	SystemPrompt string `json:"systemPrompt,omitempty"`

	// Chat template
	ChatTemplate string `json:"chatTemplate,omitempty"` // Override GGUF chat template (e.g. "chatml")

	// Reasoning
	ReasoningBudget int `json:"reasoningBudget,omitempty"` // --reasoning-budget; 0 disables thinking, -1 = default (omit)

	// Advanced
	FlashAttention bool         `json:"flashAttention,omitempty"`
	MMap           bool         `json:"mmap,omitempty"`
	MLock          bool         `json:"mlock,omitempty"`
	NumaStrategy   NumaStrategy `json:"numaStrategy,omitempty"`
	ExtraArgs      []string     `json:"extraArgs,omitempty"` // Pass-through args to llama.cpp
}

// NumaStrategy controls NUMA memory allocation policy.
type NumaStrategy int

const (
	NumaDisabled   NumaStrategy = iota // No NUMA awareness (default)
	NumaDistribute                     // Distribute across NUMA nodes
	NumaIsolate                        // Isolate to a single NUMA node
)

func (n NumaStrategy) String() string {
	switch n {
	case NumaDistribute:
		return "distribute"
	case NumaIsolate:
		return "isolate"
	default:
		return "disabled"
	}
}

// MarshalJSON encodes NumaStrategy as a string.
func (n NumaStrategy) MarshalJSON() ([]byte, error) {
	return []byte(`"` + n.String() + `"`), nil
}

// UnmarshalJSON decodes NumaStrategy from a string.
func (n *NumaStrategy) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	parsed, err := ParseNumaStrategy(s)
	if err != nil {
		return err
	}
	*n = parsed
	return nil
}

// ParseNumaStrategy parses a string into a NumaStrategy.
func ParseNumaStrategy(s string) (NumaStrategy, error) {
	switch strings.ToLower(s) {
	case "", "disabled":
		return NumaDisabled, nil
	case "distribute":
		return NumaDistribute, nil
	case "isolate":
		return NumaIsolate, nil
	default:
		return NumaDisabled, fmt.Errorf("unknown NUMA strategy: %q (valid: disabled, distribute, isolate)", s)
	}
}

// Capabilities represents what the detected llama.cpp build supports.
type Capabilities struct {
	Version        string `json:"version"`
	Backend        string `json:"backend"` // "cuda", "metal", "vulkan", "cpu"
	CUDACompute    string `json:"cudaCompute,omitempty"`
	FlashAttention bool   `json:"flashAttention"`
	NUMA           bool   `json:"numa"`
	MMap           bool   `json:"mmap"`
	MLock          bool   `json:"mlock"`
	ServerMode     bool   `json:"serverMode"`  // llama-server binary present
	BenchMode      bool   `json:"benchMode"`   // llama-bench binary present
	BinaryPath     string `json:"binaryPath"`  // Path to the llama-server or llama-cli binary
	BinaryDir      string `json:"binaryDir"`   // Directory containing binaries
}

// Process represents a running llama.cpp instance.
type Process struct {
	Cmd       *processHandle
	Config    RunConfig
	Caps      Capabilities
	Endpoint  string // API endpoint when in server mode
	PIDFile   string
	LogFile   string // Path to the stderr/stdout log file
	StartedAt time.Time
}

// processHandle wraps the os/exec Cmd for testability.
type processHandle struct {
	pid    int
	cmd    interface{ Wait() error }
	cancel func()

	// Background reaper: Launch spawns a goroutine that calls cmd.Wait()
	// immediately. The result lands in waitErr and done is closed, preventing
	// zombie processes regardless of what the caller does.
	done    chan struct{}
	waitErr error
}

// HealthStatus represents the health of a running server.
type HealthStatus struct {
	Status    string `json:"status"` // "ok", "loading", "error"
	SlotsIdle int    `json:"slots_idle,omitempty"`
	SlotsUsed int    `json:"slots_processing,omitempty"`
}

// ProcessStats holds runtime statistics.
type ProcessStats struct {
	Uptime          time.Duration
	RequestsServed  int64
	TokensGenerated int64
}

// Stats returns runtime statistics for the process.
func (p *Process) Stats() ProcessStats {
	if p == nil {
		return ProcessStats{}
	}
	return ProcessStats{
		Uptime: time.Since(p.StartedAt),
	}
}
