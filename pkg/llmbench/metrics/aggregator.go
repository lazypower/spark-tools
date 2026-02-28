package metrics

import (
	"math"
	"sort"
)

// Aggregate computes ThroughputStats from a slice of float64 values.
func Aggregate(values []float64) ThroughputStats {
	n := len(values)
	if n == 0 {
		return ThroughputStats{}
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(n)

	var variance float64
	for _, v := range sorted {
		d := v - mean
		variance += d * d
	}
	if n > 1 {
		variance /= float64(n - 1)
	}

	return ThroughputStats{
		Mean:    mean,
		Median:  percentile(sorted, 0.50),
		P5:      percentile(sorted, 0.05),
		P95:     percentile(sorted, 0.95),
		StdDev:  math.Sqrt(variance),
		Min:     sorted[0],
		Max:     sorted[n-1],
		Samples: n,
	}
}

// percentile computes the p-th percentile using linear interpolation.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}

	// Use linear interpolation between the two nearest ranks
	rank := p * float64(n-1)
	lower := int(rank)
	upper := lower + 1
	if upper >= n {
		return sorted[n-1]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
