package suite

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDuration_YAMLRoundTrip(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"30s", 30 * time.Second},
		{"2m30s", 2*time.Minute + 30*time.Second},
		{"1h", time.Hour},
		{"500ms", 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Unmarshal
			var d Duration
			yamlData := []byte(tt.input)
			if err := yaml.Unmarshal(yamlData, &d); err != nil {
				t.Fatalf("unmarshal %q: %v", tt.input, err)
			}
			if d.Duration != tt.expected {
				t.Errorf("unmarshal: got %v, want %v", d.Duration, tt.expected)
			}

			// Marshal round-trip
			out, err := yaml.Marshal(d)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var d2 Duration
			if err := yaml.Unmarshal(out, &d2); err != nil {
				t.Fatalf("unmarshal round-trip: %v", err)
			}
			if d2.Duration != tt.expected {
				t.Errorf("round-trip: got %v, want %v", d2.Duration, tt.expected)
			}
		})
	}
}

func TestDuration_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		duration time.Duration
		jsonStr  string
	}{
		{5 * time.Minute, `"5m0s"`},
		{30 * time.Second, `"30s"`},
		{2*time.Minute + 30*time.Second, `"2m30s"`},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			d := Duration{tt.duration}

			data, err := json.Marshal(d)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tt.jsonStr {
				t.Errorf("marshal: got %s, want %s", data, tt.jsonStr)
			}

			var d2 Duration
			if err := json.Unmarshal(data, &d2); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if d2.Duration != tt.duration {
				t.Errorf("round-trip: got %v, want %v", d2.Duration, tt.duration)
			}
		})
	}
}

func TestDuration_YAMLInvalidInput(t *testing.T) {
	var d Duration
	err := yaml.Unmarshal([]byte("not-a-duration"), &d)
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}
