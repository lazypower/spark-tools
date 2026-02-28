package engine

import (
	"fmt"
	"strconv"
)

// BuildCommand translates a RunConfig into a llama.cpp command line,
// gated by the detected Capabilities.
//
// Returns the command (binary path + flags), a list of warnings for
// degraded features, and an error for hard failures.
//
// Capability gating per spec section 5.3:
//   - Graceful degradation (warn + omit): FlashAttention, NUMA, MLock
//   - Hard error: GPULayers -1 on CPU-only build, ServerMode without llama-server
//   - ExtraArgs are always appended verbatim
func BuildCommand(cfg RunConfig, caps Capabilities) (cmd []string, warnings []string, err error) {
	// --- Hard errors ---

	// Server mode requires llama-server binary.
	if cfg.ServerMode && !caps.ServerMode {
		return nil, nil, fmt.Errorf("llama-server not found. Ensure llama.cpp was built with server support")
	}

	// GPU offload on CPU-only build.
	if cfg.GPULayers == -1 && caps.Backend == "cpu" {
		return nil, nil, fmt.Errorf("llama.cpp was built without GPU support. Rebuild with CUDA or set gpu-layers to 0")
	}

	// --- Select binary ---
	var binary string
	if cfg.ServerMode {
		binary = lookupBinary(caps.BinaryDir, "llama-server")
		if binary == "" {
			binary = caps.BinaryPath
		}
	} else {
		binary = lookupBinary(caps.BinaryDir, "llama-cli")
		if binary == "" {
			// Fall back to whatever we have.
			binary = caps.BinaryPath
		}
	}
	cmd = []string{binary}

	// --- Model ---
	if cfg.ModelPath != "" {
		cmd = append(cmd, "--model", cfg.ModelPath)
	}

	// --- Hardware allocation ---
	if cfg.GPULayers != 0 {
		cmd = append(cmd, "--n-gpu-layers", strconv.Itoa(cfg.GPULayers))
	}
	if cfg.Threads > 0 {
		cmd = append(cmd, "--threads", strconv.Itoa(cfg.Threads))
	}
	if cfg.MainGPU > 0 {
		cmd = append(cmd, "--main-gpu", strconv.Itoa(cfg.MainGPU))
	}

	// --- Memory and context ---
	if cfg.ContextSize > 0 {
		cmd = append(cmd, "--ctx-size", strconv.Itoa(cfg.ContextSize))
	}
	if cfg.BatchSize > 0 {
		cmd = append(cmd, "--batch-size", strconv.Itoa(cfg.BatchSize))
	}
	if cfg.UBatchSize > 0 {
		cmd = append(cmd, "--ubatch-size", strconv.Itoa(cfg.UBatchSize))
	}

	// --- Generation parameters ---
	if cfg.Temperature > 0 {
		cmd = append(cmd, "--temp", strconv.FormatFloat(cfg.Temperature, 'f', -1, 64))
	}
	if cfg.TopP > 0 {
		cmd = append(cmd, "--top-p", strconv.FormatFloat(cfg.TopP, 'f', -1, 64))
	}
	if cfg.TopK > 0 {
		cmd = append(cmd, "--top-k", strconv.Itoa(cfg.TopK))
	}
	if cfg.RepeatPenalty > 0 {
		cmd = append(cmd, "--repeat-penalty", strconv.FormatFloat(cfg.RepeatPenalty, 'f', -1, 64))
	}
	if cfg.Seed != 0 {
		cmd = append(cmd, "--seed", strconv.Itoa(cfg.Seed))
	}

	// --- Server mode flags ---
	if cfg.ServerMode {
		if cfg.Host != "" {
			cmd = append(cmd, "--host", cfg.Host)
		}
		if cfg.Port > 0 {
			cmd = append(cmd, "--port", strconv.Itoa(cfg.Port))
		}
		if cfg.Parallel > 0 {
			cmd = append(cmd, "--parallel", strconv.Itoa(cfg.Parallel))
		}
		if cfg.APIKey != "" {
			cmd = append(cmd, "--api-key", cfg.APIKey)
		}
	}

	// --- Chat template override ---
	if cfg.ChatTemplate != "" {
		cmd = append(cmd, "--chat-template", cfg.ChatTemplate)
	}

	// --- System prompt ---
	if cfg.SystemPrompt != "" {
		cmd = append(cmd, "--system-prompt", cfg.SystemPrompt)
	}

	// --- Advanced flags with capability gating ---

	// FlashAttention: graceful degradation.
	if cfg.FlashAttention {
		if caps.FlashAttention {
			cmd = append(cmd, "--flash-attn", "on")
		} else {
			warnings = append(warnings, "flash attention requested but not supported by this llama.cpp build; omitting --flash-attn")
		}
	}

	// MMap.
	if cfg.MMap {
		if caps.MMap {
			cmd = append(cmd, "--mmap")
		} else {
			warnings = append(warnings, "mmap requested but not supported by this llama.cpp build; omitting --mmap")
		}
	}

	// MLock: graceful degradation.
	if cfg.MLock {
		if caps.MLock {
			cmd = append(cmd, "--mlock")
		} else {
			warnings = append(warnings, "mlock requested but not supported by this llama.cpp build; omitting --mlock")
		}
	}

	// NUMA: graceful degradation.
	if cfg.NumaStrategy != NumaDisabled {
		if caps.NUMA {
			cmd = append(cmd, "--numa", cfg.NumaStrategy.String())
		} else {
			warnings = append(warnings, fmt.Sprintf("NUMA strategy %q requested but not supported by this llama.cpp build; omitting --numa", cfg.NumaStrategy.String()))
		}
	}

	// --- Extra args (always pass through verbatim) ---
	cmd = append(cmd, cfg.ExtraArgs...)

	return cmd, warnings, nil
}
