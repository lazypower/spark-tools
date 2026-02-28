package suite

import (
	"testing"
	"time"
)

const specExampleYAML = `
name: "DGX Spark Model Comparison"
description: "Compare 32B and 70B models across quantizations on DGX Spark GB10"

defaults:
  warmup_prompts: 3
  measure_prompts: 10
  max_tokens: 512
  temperature: 0.0
  cooldown_seconds: 10
  timeout: 5m

models:
  - name: "Qwen2.5-Coder-32B"
    ref: "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF"
    quants: ["Q4_K_M", "Q5_K_M", "Q6_K", "Q8_0"]
    alias: "qwen-32b"
  - name: "Qwen2.5-72B"
    ref: "bartowski/Qwen2.5-72B-Instruct-GGUF"
    quants: ["Q4_K_M", "Q5_K_M"]
    alias: "qwen-72b"

scenarios:
  - name: "throughput"
    description: "Raw generation throughput"
    context_sizes: [4096]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "short"
    max_tokens: 512
    repeat: 3
  - name: "context-scaling"
    description: "Performance across context sizes"
    context_sizes: [4096, 8192, 16384, 32768]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "medium"
    max_tokens: 256
    repeat: 2

settings:
  output_dir: "./results"
  output_formats: ["json", "terminal"]
  system_check: true
  cooldown_between: 15
  server_startup_timeout: 2m
`

func TestParseSuite_SpecExample(t *testing.T) {
	s, err := ParseSuite([]byte(specExampleYAML))
	if err != nil {
		t.Fatalf("ParseSuite: %v", err)
	}

	if s.Name != "DGX Spark Model Comparison" {
		t.Errorf("name: got %q", s.Name)
	}
	if len(s.Models) != 2 {
		t.Fatalf("models: got %d, want 2", len(s.Models))
	}
	if s.Models[0].Alias != "qwen-32b" {
		t.Errorf("model[0] alias: got %q", s.Models[0].Alias)
	}
	if len(s.Models[0].Quants) != 4 {
		t.Errorf("model[0] quants: got %d, want 4", len(s.Models[0].Quants))
	}
	if len(s.Scenarios) != 2 {
		t.Fatalf("scenarios: got %d, want 2", len(s.Scenarios))
	}
	if s.Scenarios[0].Repeat != 3 {
		t.Errorf("scenario[0] repeat: got %d, want 3", s.Scenarios[0].Repeat)
	}
	if s.Defaults.Timeout.Duration != 5*time.Minute {
		t.Errorf("defaults.timeout: got %v, want 5m", s.Defaults.Timeout.Duration)
	}
	if s.Settings.ServerStartupTimeout.Duration != 2*time.Minute {
		t.Errorf("settings.server_startup_timeout: got %v, want 2m", s.Settings.ServerStartupTimeout.Duration)
	}
	if s.Settings.CooldownBetween != 15 {
		t.Errorf("settings.cooldown_between: got %d, want 15", s.Settings.CooldownBetween)
	}
}

func TestParseSuite_DefaultsApplied(t *testing.T) {
	yaml := `
name: "minimal"
models:
  - name: "test"
    ref: "owner/repo"
    quants: ["Q4_K_M"]
scenarios:
  - name: "basic"
    prompts:
      builtin: "short"
`
	s, err := ParseSuite([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseSuite: %v", err)
	}

	// Check defaults were applied
	if s.Defaults.WarmupPrompts != 3 {
		t.Errorf("warmup_prompts: got %d, want 3", s.Defaults.WarmupPrompts)
	}
	if s.Defaults.MeasurePrompts != 10 {
		t.Errorf("measure_prompts: got %d, want 10", s.Defaults.MeasurePrompts)
	}
	if s.Defaults.MaxTokens != 512 {
		t.Errorf("max_tokens: got %d, want 512", s.Defaults.MaxTokens)
	}
	if s.Settings.DirtyMode != "abort" {
		t.Errorf("dirty_mode: got %q, want abort", s.Settings.DirtyMode)
	}
	if s.Settings.MetricsSampleMs != 500 {
		t.Errorf("metrics_sample_ms: got %d, want 500", s.Settings.MetricsSampleMs)
	}
	if s.Models[0].Alias != "test" {
		t.Errorf("model alias default: got %q, want %q", s.Models[0].Alias, "test")
	}
	if s.Scenarios[0].ContextSizes[0] != 4096 {
		t.Errorf("scenario context_sizes default: got %v", s.Scenarios[0].ContextSizes)
	}
	if s.Scenarios[0].Repeat != 1 {
		t.Errorf("scenario repeat default: got %d, want 1", s.Scenarios[0].Repeat)
	}
	if s.Scenarios[0].MaxTokens != 512 {
		t.Errorf("scenario max_tokens default: got %d, want 512", s.Scenarios[0].MaxTokens)
	}
}

func TestParseSuite_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing name",
			yaml: `
models:
  - ref: "owner/repo"
    quants: ["Q4_K_M"]
scenarios:
  - name: "basic"
    prompts:
      builtin: "short"
`,
			want: "suite name is required",
		},
		{
			name: "no models",
			yaml: `
name: "test"
scenarios:
  - name: "basic"
    prompts:
      builtin: "short"
`,
			want: "at least one model",
		},
		{
			name: "model without ref",
			yaml: `
name: "test"
models:
  - name: "bad"
    quants: ["Q4_K_M"]
scenarios:
  - name: "basic"
    prompts:
      builtin: "short"
`,
			want: "requires a ref",
		},
		{
			name: "model without quants",
			yaml: `
name: "test"
models:
  - name: "bad"
    ref: "owner/repo"
scenarios:
  - name: "basic"
    prompts:
      builtin: "short"
`,
			want: "at least one quant",
		},
		{
			name: "no scenarios",
			yaml: `
name: "test"
models:
  - ref: "owner/repo"
    quants: ["Q4_K_M"]
`,
			want: "at least one scenario",
		},
		{
			name: "scenario without prompts",
			yaml: `
name: "test"
models:
  - ref: "owner/repo"
    quants: ["Q4_K_M"]
scenarios:
  - name: "basic"
`,
			want: "requires a prompt source",
		},
		{
			name: "scenario with multiple prompt sources",
			yaml: `
name: "test"
models:
  - ref: "owner/repo"
    quants: ["Q4_K_M"]
scenarios:
  - name: "basic"
    prompts:
      builtin: "short"
      file: "custom.txt"
`,
			want: "exactly one prompt source",
		},
		{
			name: "invalid dirty_mode",
			yaml: `
name: "test"
models:
  - ref: "owner/repo"
    quants: ["Q4_K_M"]
scenarios:
  - name: "basic"
    prompts:
      builtin: "short"
settings:
  dirty_mode: "invalid"
`,
			want: "invalid dirty_mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSuite([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error")
			}
			if !contains(err.Error(), tt.want) {
				t.Errorf("error %q should contain %q", err.Error(), tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
