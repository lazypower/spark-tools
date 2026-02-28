// Package suite orchestrates benchmark runs: parsing configs,
// expanding job matrices, and sequencing execution.
package suite

import (
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// BenchmarkSuite is the top-level config defining a complete benchmark run.
type BenchmarkSuite struct {
	Name        string        `yaml:"name"        json:"name"`
	Description string        `yaml:"description" json:"description"`
	Defaults    JobDefaults   `yaml:"defaults"    json:"defaults"`
	Models      []ModelSpec   `yaml:"models"      json:"models"`
	Scenarios   []Scenario    `yaml:"scenarios"   json:"scenarios"`
	Settings    SuiteSettings `yaml:"settings"    json:"settings"`
}

// JobDefaults provides default values inherited by all jobs.
type JobDefaults struct {
	WarmupPrompts   int      `yaml:"warmup_prompts"   json:"warmup_prompts"`
	MeasurePrompts  int      `yaml:"measure_prompts"  json:"measure_prompts"`
	MaxTokens       int      `yaml:"max_tokens"       json:"max_tokens"`
	Temperature     float64  `yaml:"temperature"      json:"temperature"`
	CooldownSeconds int      `yaml:"cooldown_seconds" json:"cooldown_seconds"`
	Timeout         Duration `yaml:"timeout"          json:"timeout"`
}

// ModelSpec defines a model to benchmark.
type ModelSpec struct {
	Name   string   `yaml:"name"   json:"name"`
	Ref    string   `yaml:"ref"    json:"ref"`
	Quants []string `yaml:"quants" json:"quants"`
	Alias  string   `yaml:"alias"  json:"alias"`
}

// Scenario defines a benchmark scenario (set of conditions to test).
type Scenario struct {
	Name          string    `yaml:"name"           json:"name"`
	Description   string    `yaml:"description"    json:"description"`
	ContextSizes  []int     `yaml:"context_sizes"  json:"context_sizes"`
	BatchSizes    []int     `yaml:"batch_sizes"    json:"batch_sizes"`
	ParallelSlots []int     `yaml:"parallel_slots" json:"parallel_slots"`
	Prompts       PromptSet `yaml:"prompts"        json:"prompts"`
	MaxTokens     int       `yaml:"max_tokens"     json:"max_tokens"`
	Repeat        int       `yaml:"repeat"         json:"repeat"`
}

// PromptSet defines which prompts to use.
type PromptSet struct {
	Builtin string   `yaml:"builtin" json:"builtin,omitempty"`
	File    string   `yaml:"file"    json:"file,omitempty"`
	Inline  []string `yaml:"inline"  json:"inline,omitempty"`
}

// JobSpec defines the parameters for a single benchmark job.
type JobSpec struct {
	JobID          string    `json:"job_id"`
	ModelSpec      ModelSpec `json:"model_spec"`
	Quant          string    `json:"quant"`
	Scenario       Scenario  `json:"scenario"`
	RunIndex       int       `json:"run_index"`
	ScenarioID     string    `json:"scenario_id"`
	ContextSize    int       `json:"context_size"`
	BatchSize      int       `json:"batch_size"`
	ParallelSlots  int       `json:"parallel_slots"`
	MaxTokens      int       `json:"max_tokens"`
	Temperature    float64   `json:"temperature"`
	WarmupPrompts  int       `json:"warmup_prompts"`
	MeasurePrompts int       `json:"measure_prompts"`
	CooldownSecs   int       `json:"cooldown_secs"`
	Timeout        Duration  `json:"timeout"`
}

// SuiteSettings controls overall benchmark behavior.
type SuiteSettings struct {
	OutputDir            string   `yaml:"output_dir"              json:"output_dir"`
	OutputFormats        []string `yaml:"output_formats"          json:"output_formats"`
	AbortOnError         bool     `yaml:"abort_on_error"          json:"abort_on_error"`
	SystemCheck          bool     `yaml:"system_check"            json:"system_check"`
	DirtyMode            string   `yaml:"dirty_mode"              json:"dirty_mode"`
	CooldownBetween      int      `yaml:"cooldown_between"        json:"cooldown_between"`
	ServerStartupTimeout Duration `yaml:"server_startup_timeout"  json:"server_startup_timeout"`
	MetricsSampleMs      int      `yaml:"metrics_sample_ms"       json:"metrics_sample_ms"`
}

// Duration wraps time.Duration with YAML/JSON marshaling support.
// It accepts Go duration strings like "5m", "30s", "2m30s".
type Duration struct {
	time.Duration
}

// MarshalYAML encodes a Duration as a Go duration string.
func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// UnmarshalYAML decodes a Duration from a Go duration string.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string (e.g. \"5m\", \"30s\"): %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

// MarshalJSON encodes a Duration as a Go duration string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// UnmarshalJSON decodes a Duration from a Go duration string.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string (e.g. \"5m\", \"30s\"): %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}
