package main

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"", 0, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"2h", 2 * time.Hour, false},
		{"junk", 0, true},
	}
	for _, tc := range cases {
		got, err := parseDuration(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("parseDuration(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDuration(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestHumanAge(t *testing.T) {
	now := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		offset time.Duration
		want   string
	}{
		{-3 * time.Hour, "today"},
		{-26 * time.Hour, "1 day ago"},
		{-10 * 24 * time.Hour, "10 days ago"},
	}
	for _, tc := range cases {
		got := humanAge(now.Add(tc.offset), now)
		if got != tc.want {
			t.Errorf("humanAge(%v) = %q, want %q", tc.offset, got, tc.want)
		}
	}
	if humanAge(time.Time{}, now) != "" {
		t.Error("zero time should format empty")
	}
}
