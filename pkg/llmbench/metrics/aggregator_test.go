package metrics

import (
	"math"
	"testing"
)

func TestAggregate_KnownDataset(t *testing.T) {
	// 10 values: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	stats := Aggregate(values)

	assertClose(t, "mean", stats.Mean, 5.5)
	assertClose(t, "median", stats.Median, 5.5)
	assertClose(t, "min", stats.Min, 1.0)
	assertClose(t, "max", stats.Max, 10.0)
	if stats.Samples != 10 {
		t.Errorf("samples: got %d, want 10", stats.Samples)
	}
	// P5 should be near the lower end
	if stats.P5 < 1.0 || stats.P5 > 2.0 {
		t.Errorf("p5: got %f, expected between 1.0 and 2.0", stats.P5)
	}
	// P95 should be near the upper end
	if stats.P95 < 9.0 || stats.P95 > 10.0 {
		t.Errorf("p95: got %f, expected between 9.0 and 10.0", stats.P95)
	}
}

func TestAggregate_SingleValue(t *testing.T) {
	stats := Aggregate([]float64{42.0})
	assertClose(t, "mean", stats.Mean, 42.0)
	assertClose(t, "median", stats.Median, 42.0)
	assertClose(t, "min", stats.Min, 42.0)
	assertClose(t, "max", stats.Max, 42.0)
	assertClose(t, "stddev", stats.StdDev, 0.0)
	if stats.Samples != 1 {
		t.Errorf("samples: got %d, want 1", stats.Samples)
	}
}

func TestAggregate_Empty(t *testing.T) {
	stats := Aggregate(nil)
	if stats.Samples != 0 {
		t.Errorf("samples: got %d, want 0", stats.Samples)
	}
	if stats.Mean != 0 {
		t.Errorf("mean: got %f, want 0", stats.Mean)
	}
}

func TestAggregate_StdDev(t *testing.T) {
	// Known dataset: [2, 4, 4, 4, 5, 5, 7, 9]
	// Population mean = 5, sample stddev = sqrt(4.571...) ≈ 2.138
	values := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	stats := Aggregate(values)
	// Sample std dev of this dataset
	if stats.StdDev < 2.0 || stats.StdDev > 2.2 {
		t.Errorf("stddev: got %f, expected ~2.138", stats.StdDev)
	}
}

func TestAggregate_UnsortedInput(t *testing.T) {
	values := []float64{9, 1, 5, 3, 7}
	stats := Aggregate(values)
	assertClose(t, "min", stats.Min, 1.0)
	assertClose(t, "max", stats.Max, 9.0)
	assertClose(t, "mean", stats.Mean, 5.0)
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %f, want %f", name, got, want)
	}
}
