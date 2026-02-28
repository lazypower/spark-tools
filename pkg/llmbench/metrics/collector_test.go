package metrics

import (
	"testing"
)

func TestExtractTimings(t *testing.T) {
	raw := `{
		"timings": {
			"prompt_n": 42,
			"prompt_ms": 22.8,
			"predicted_n": 512,
			"predicted_ms": 10500.5
		}
	}`
	timings, err := ExtractTimings([]byte(raw))
	if err != nil {
		t.Fatalf("ExtractTimings: %v", err)
	}
	if timings.PromptN != 42 {
		t.Errorf("prompt_n: got %d, want 42", timings.PromptN)
	}
	if timings.PredictedN != 512 {
		t.Errorf("predicted_n: got %d, want 512", timings.PredictedN)
	}
	assertClose(t, "prompt_ms", timings.PromptMs, 22.8)
	assertClose(t, "predicted_ms", timings.PredictedMs, 10500.5)
}

func TestComputeRates(t *testing.T) {
	timings := &Timings{
		PromptN:     100,
		PromptMs:    50.0, // 50ms => 100 / 0.05 = 2000 tok/s
		PredictedN:  500,
		PredictedMs: 10000.0, // 10s => 500/10 = 50 tok/s
	}
	prompt, gen := ComputeRates(timings)
	assertClose(t, "prompt tok/s", prompt, 2000.0)
	assertClose(t, "gen tok/s", gen, 50.0)
}

func TestComputeRates_ZeroMs(t *testing.T) {
	timings := &Timings{
		PromptN:     100,
		PromptMs:    0,
		PredictedN:  500,
		PredictedMs: 0,
	}
	prompt, gen := ComputeRates(timings)
	assertClose(t, "prompt tok/s", prompt, 0.0)
	assertClose(t, "gen tok/s", gen, 0.0)
}

func TestCollector_FullPipeline(t *testing.T) {
	c := NewCollector()

	// Add 5 samples
	for i := 0; i < 5; i++ {
		c.Add(RawSample{
			PromptTokens:    42,
			PredictedTokens: 100,
			PromptMs:        20.0 + float64(i),
			PredictedMs:     2000.0 + float64(i*100),
			TTFTMs:          50.0 + float64(i*5),
			EndToEndMs:      2020.0 + float64(i*100),
			PromptBytes:     168,
		})
	}

	if c.Count() != 5 {
		t.Errorf("count: got %d, want 5", c.Count())
	}

	results := c.Collect()
	if results.PromptEval.Samples != 5 {
		t.Errorf("prompt eval samples: got %d, want 5", results.PromptEval.Samples)
	}
	if results.Generation.Samples != 5 {
		t.Errorf("generation samples: got %d, want 5", results.Generation.Samples)
	}
	if results.FirstTokenTime.Samples != 5 {
		t.Errorf("ttft samples: got %d, want 5", results.FirstTokenTime.Samples)
	}
	if results.EndToEnd.Samples != 5 {
		t.Errorf("e2e samples: got %d, want 5", results.EndToEnd.Samples)
	}
	if len(results.RawSamples) != 5 {
		t.Errorf("raw samples: got %d, want 5", len(results.RawSamples))
	}

	// Prompt eval rate should be positive
	if results.PromptEval.Mean <= 0 {
		t.Errorf("prompt eval mean should be > 0, got %f", results.PromptEval.Mean)
	}
	// Generation rate should be positive
	if results.Generation.Mean <= 0 {
		t.Errorf("generation mean should be > 0, got %f", results.Generation.Mean)
	}
}

func TestCollector_EmptyCollect(t *testing.T) {
	c := NewCollector()
	results := c.Collect()
	if results.PromptEval.Samples != 0 {
		t.Errorf("expected 0 samples for empty collector")
	}
}
