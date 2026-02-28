package syscheck

import (
	"testing"
)

func TestParseDirtyMode(t *testing.T) {
	tests := []struct {
		input string
		want  DirtyMode
		err   bool
	}{
		{"", DirtyModeAbort, false},
		{"abort", DirtyModeAbort, false},
		{"warn", DirtyModeWarn, false},
		{"force", DirtyModeForce, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseDirtyMode(tt.input)
			if tt.err {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckResult(t *testing.T) {
	r := CheckResult{
		Name:    "test",
		Failed:  false,
		Message: "all good",
	}
	if r.Failed {
		t.Error("should not be failed")
	}
	if r.Name != "test" {
		t.Errorf("name: got %q", r.Name)
	}
}
