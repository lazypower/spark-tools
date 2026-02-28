package suite

import (
	"testing"
)

func TestDryRun(t *testing.T) {
	s := testSuite()
	runner := NewRunner()

	jobs := runner.DryRun(s)
	if len(jobs) == 0 {
		t.Fatal("DryRun should return jobs")
	}
	// Same count as ExpandJobs
	expected := ExpandJobs(s)
	if len(jobs) != len(expected) {
		t.Errorf("DryRun: got %d jobs, want %d", len(jobs), len(expected))
	}
}

func TestDryRun_WithFilter(t *testing.T) {
	s := testSuite()
	runner := NewRunner(WithJobFilter([]string{"model-a"}))

	jobs := runner.DryRun(s)
	for _, j := range jobs {
		if j.ModelSpec.Alias != "model-a" {
			t.Errorf("filter should only include model-a, got %q", j.ModelSpec.Alias)
		}
	}
}

func TestNewRunner_Defaults(t *testing.T) {
	runner := NewRunner()
	if runner == nil {
		t.Fatal("NewRunner returned nil")
	}
}

func TestRun_RequiresEngine(t *testing.T) {
	s := testSuite()
	runner := NewRunner()
	_, err := runner.Run(nil, s)
	if err == nil {
		t.Error("expected error without engine")
	}
}
