package suite

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadSuite reads and parses a benchmark suite from a YAML file.
func LoadSuite(path string) (*BenchmarkSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return ParseSuite(data)
}

// ParseSuite parses a benchmark suite from raw YAML bytes.
func ParseSuite(data []byte) (*BenchmarkSuite, error) {
	var s BenchmarkSuite
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	applyDefaults(&s)
	if err := Validate(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate checks the suite config for errors.
func Validate(s *BenchmarkSuite) error {
	if s.Name == "" {
		return fmt.Errorf("validation: suite name is required")
	}
	if len(s.Models) == 0 {
		return fmt.Errorf("validation: at least one model is required")
	}
	for i, m := range s.Models {
		if m.Ref == "" {
			return fmt.Errorf("validation: model[%d] (%q) requires a ref", i, m.Name)
		}
		if len(m.Quants) == 0 {
			return fmt.Errorf("validation: model[%d] (%q) requires at least one quant", i, m.Name)
		}
	}
	if len(s.Scenarios) == 0 {
		return fmt.Errorf("validation: at least one scenario is required")
	}
	for i, sc := range s.Scenarios {
		if sc.Name == "" {
			return fmt.Errorf("validation: scenario[%d] requires a name", i)
		}
		if err := validatePromptSet(sc.Prompts, i); err != nil {
			return err
		}
	}
	switch s.Settings.DirtyMode {
	case "", "abort", "warn", "force":
		// valid
	default:
		return fmt.Errorf("validation: invalid dirty_mode %q (valid: abort, warn, force)", s.Settings.DirtyMode)
	}
	return nil
}

func validatePromptSet(ps PromptSet, scenarioIdx int) error {
	sources := 0
	if ps.Builtin != "" {
		sources++
	}
	if ps.File != "" {
		sources++
	}
	if len(ps.Inline) > 0 {
		sources++
	}
	if sources == 0 {
		return fmt.Errorf("validation: scenario[%d] requires a prompt source (builtin, file, or inline)", scenarioIdx)
	}
	if sources > 1 {
		return fmt.Errorf("validation: scenario[%d] must specify exactly one prompt source", scenarioIdx)
	}
	return nil
}

// applyDefaults fills in zero-valued fields with sensible defaults.
func applyDefaults(s *BenchmarkSuite) {
	d := &s.Defaults
	if d.WarmupPrompts == 0 {
		d.WarmupPrompts = 3
	}
	if d.MeasurePrompts == 0 {
		d.MeasurePrompts = 10
	}
	if d.MaxTokens == 0 {
		d.MaxTokens = 512
	}
	if d.CooldownSeconds == 0 {
		d.CooldownSeconds = 10
	}
	if d.Timeout.Duration == 0 {
		d.Timeout.Duration = 5 * time.Minute
	}

	st := &s.Settings
	if st.DirtyMode == "" {
		st.DirtyMode = "abort"
	}
	if st.MetricsSampleMs == 0 {
		st.MetricsSampleMs = 500
	}
	if st.ServerStartupTimeout.Duration == 0 {
		st.ServerStartupTimeout.Duration = 2 * time.Minute
	}
	if len(st.OutputFormats) == 0 {
		st.OutputFormats = []string{"json", "terminal"}
	}
	if st.CooldownBetween == 0 {
		st.CooldownBetween = 10
	}

	for i := range s.Models {
		if s.Models[i].Alias == "" {
			s.Models[i].Alias = s.Models[i].Name
		}
	}

	for i := range s.Scenarios {
		sc := &s.Scenarios[i]
		if len(sc.ContextSizes) == 0 {
			sc.ContextSizes = []int{4096}
		}
		if len(sc.BatchSizes) == 0 {
			sc.BatchSizes = []int{512}
		}
		if len(sc.ParallelSlots) == 0 {
			sc.ParallelSlots = []int{1}
		}
		if sc.MaxTokens == 0 {
			sc.MaxTokens = d.MaxTokens
		}
		if sc.Repeat == 0 {
			sc.Repeat = 1
		}
	}
}
