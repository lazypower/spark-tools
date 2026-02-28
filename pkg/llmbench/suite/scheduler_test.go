package suite

import (
	"testing"
)

func testSuite() *BenchmarkSuite {
	s, _ := ParseSuite([]byte(`
name: "test"
models:
  - name: "ModelA"
    ref: "owner/model-a"
    quants: ["Q4_K_M", "Q8_0"]
    alias: "model-a"
  - name: "ModelB"
    ref: "owner/model-b"
    quants: ["Q4_K_M"]
    alias: "model-b"
scenarios:
  - name: "throughput"
    context_sizes: [4096]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "short"
    repeat: 2
  - name: "scaling"
    context_sizes: [4096, 8192]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "medium"
    repeat: 1
`))
	return s
}

func TestExpandJobs_Count(t *testing.T) {
	s := testSuite()
	jobs := ExpandJobs(s)

	// model-a: 2 quants × (throughput:1×1×1×2 + scaling:2×1×1×1) = 2 × (2+2) = 8
	// model-b: 1 quant  × (throughput:1×1×1×2 + scaling:2×1×1×1) = 1 × (2+2) = 4
	// total = 12
	expected := 12
	if len(jobs) != expected {
		t.Errorf("job count: got %d, want %d", len(jobs), expected)
		for _, j := range jobs {
			t.Logf("  %s", j.JobID)
		}
	}
}

func TestExpandJobs_Ordering(t *testing.T) {
	s := testSuite()
	jobs := ExpandJobs(s)

	// First jobs should all be model-a, then model-b
	modelAEnd := -1
	modelBStart := -1
	for i, j := range jobs {
		if j.ModelSpec.Alias == "model-a" {
			modelAEnd = i
		}
		if j.ModelSpec.Alias == "model-b" && modelBStart == -1 {
			modelBStart = i
		}
	}
	if modelAEnd >= modelBStart {
		t.Errorf("model ordering: model-a jobs should come before model-b")
	}

	// Within model-a, Q4_K_M should come before Q8_0
	q4End := -1
	q8Start := -1
	for i, j := range jobs {
		if j.ModelSpec.Alias != "model-a" {
			continue
		}
		if j.Quant == "Q4_K_M" {
			q4End = i
		}
		if j.Quant == "Q8_0" && q8Start == -1 {
			q8Start = i
		}
	}
	if q4End >= q8Start {
		t.Errorf("quant ordering: Q4_K_M should come before Q8_0 within model-a")
	}
}

func TestExpandJobs_JobIDFormat(t *testing.T) {
	s := testSuite()
	jobs := ExpandJobs(s)

	// Simple scenario (single combo) should have short ID
	found := false
	for _, j := range jobs {
		if j.Scenario.Name == "throughput" && j.ModelSpec.Alias == "model-a" && j.Quant == "Q4_K_M" && j.RunIndex == 1 {
			if j.JobID != "model-a-Q4_K_M-throughput-1" {
				t.Errorf("simple job ID: got %q, want %q", j.JobID, "model-a-Q4_K_M-throughput-1")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find model-a Q4_K_M throughput run 1")
	}

	// Multi-combo scenario should have extended ID
	for _, j := range jobs {
		if j.Scenario.Name == "scaling" && j.ModelSpec.Alias == "model-a" && j.Quant == "Q4_K_M" {
			expected := "model-a-Q4_K_M-scaling-ctx"
			if len(j.JobID) < len(expected) || j.JobID[:len(expected)] != expected {
				t.Errorf("extended job ID should start with %q, got %q", expected, j.JobID)
			}
			break
		}
	}
}

func TestScenarioID_Stability(t *testing.T) {
	scenario := Scenario{
		Name:          "throughput",
		ContextSizes:  []int{4096},
		BatchSizes:    []int{512},
		ParallelSlots: []int{1},
		MaxTokens:     512,
		Repeat:        3,
		Prompts:       PromptSet{Builtin: "short"},
	}

	id1 := ScenarioID(scenario)
	id2 := ScenarioID(scenario)

	if id1 != id2 {
		t.Errorf("scenario ID not stable: %q != %q", id1, id2)
	}
	if len(id1) != 12 {
		t.Errorf("scenario ID length: got %d, want 12", len(id1))
	}
}

func TestScenarioID_DifferentInputs(t *testing.T) {
	s1 := Scenario{
		Name:          "throughput",
		ContextSizes:  []int{4096},
		BatchSizes:    []int{512},
		ParallelSlots: []int{1},
		MaxTokens:     512,
		Repeat:        3,
		Prompts:       PromptSet{Builtin: "short"},
	}
	s2 := s1
	s2.MaxTokens = 256

	id1 := ScenarioID(s1)
	id2 := ScenarioID(s2)

	if id1 == id2 {
		t.Error("different scenarios should have different IDs")
	}
}

func TestFilterJobs(t *testing.T) {
	s := testSuite()
	jobs := ExpandJobs(s)

	// Filter by model
	filtered := FilterJobs(jobs, []string{"model-a"})
	for _, j := range filtered {
		if j.ModelSpec.Alias != "model-a" {
			t.Errorf("filter by model: got %q", j.ModelSpec.Alias)
		}
	}
	if len(filtered) == 0 {
		t.Error("filter should return results")
	}

	// Filter by quant
	filtered = FilterJobs(jobs, []string{"Q8_0"})
	for _, j := range filtered {
		if j.Quant != "Q8_0" {
			t.Errorf("filter by quant: got %q", j.Quant)
		}
	}

	// No filter returns all
	filtered = FilterJobs(jobs, nil)
	if len(filtered) != len(jobs) {
		t.Errorf("no filter: got %d, want %d", len(filtered), len(jobs))
	}
}
