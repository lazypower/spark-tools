package suite

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// ExpandJobs expands a BenchmarkSuite into an ordered list of JobSpecs.
// Jobs are ordered: model -> quant -> scenario -> config combos -> repeats.
func ExpandJobs(s *BenchmarkSuite) []JobSpec {
	var jobs []JobSpec

	for _, model := range s.Models {
		for _, quant := range model.Quants {
			for _, scenario := range s.Scenarios {
				scenarioID := ScenarioID(scenario)
				multiCombo := len(scenario.ContextSizes) > 1 ||
					len(scenario.BatchSizes) > 1 ||
					len(scenario.ParallelSlots) > 1
				for _, ctx := range scenario.ContextSizes {
					for _, batch := range scenario.BatchSizes {
						for _, parallel := range scenario.ParallelSlots {
							for run := 1; run <= scenario.Repeat; run++ {
								jobID := fmt.Sprintf("%s-%s-%s-%d",
									model.Alias, quant, scenario.Name, run)
								if multiCombo {
									jobID = fmt.Sprintf("%s-%s-%s-ctx%d-b%d-p%d-%d",
										model.Alias, quant, scenario.Name,
										ctx, batch, parallel, run)
								}
								js := JobSpec{
									JobID:          jobID,
									ModelSpec:      model,
									Quant:          quant,
									Scenario:       scenario,
									RunIndex:       run,
									ScenarioID:     scenarioID,
									ContextSize:    ctx,
									BatchSize:      batch,
									ParallelSlots:  parallel,
									MaxTokens:      scenario.MaxTokens,
									Temperature:    s.Defaults.Temperature,
									WarmupPrompts:  s.Defaults.WarmupPrompts,
									MeasurePrompts: s.Defaults.MeasurePrompts,
									CooldownSecs:   s.Defaults.CooldownSeconds,
									Timeout:        s.Defaults.Timeout,
								}
								jobs = append(jobs, js)
							}
						}
					}
				}
			}
		}
	}
	return jobs
}

// ScenarioID computes a stable content hash for a scenario.
// The hash is SHA-256 truncated to 12 hex characters, derived from
// the scenario's config fields and prompt set content.
func ScenarioID(s Scenario) string {
	content := struct {
		Name          string    `json:"name"`
		ContextSizes  []int     `json:"context_sizes"`
		BatchSizes    []int     `json:"batch_sizes"`
		ParallelSlots []int     `json:"parallel_slots"`
		MaxTokens     int       `json:"max_tokens"`
		Repeat        int       `json:"repeat"`
		Prompts       PromptSet `json:"prompts"`
	}{
		Name:          s.Name,
		ContextSizes:  s.ContextSizes,
		BatchSizes:    s.BatchSizes,
		ParallelSlots: s.ParallelSlots,
		MaxTokens:     s.MaxTokens,
		Repeat:        s.Repeat,
		Prompts:       s.Prompts,
	}
	data, _ := json.Marshal(content)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:6])
}

// FilterJobs returns only jobs whose ID matches any of the given patterns.
// A pattern matches if it appears as a substring of the job ID.
func FilterJobs(jobs []JobSpec, patterns []string) []JobSpec {
	if len(patterns) == 0 {
		return jobs
	}
	var filtered []JobSpec
	for _, j := range jobs {
		for _, p := range patterns {
			if strings.Contains(j.JobID, p) {
				filtered = append(filtered, j)
				break
			}
		}
	}
	return filtered
}
