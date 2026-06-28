package interlock

import (
	"slices"
	"testing"
)

func TestSplitNonEmpty_PreservesPathSpaces(t *testing.T) {
	// codex P1: a path with leading/trailing spaces must round-trip exactly, or
	// the echo-match desyncs and a protected path looks evictable. Only empty
	// lines are dropped; spaces in a path are preserved.
	got := splitNonEmpty("/models/foo \n  /models/bar\n\n/models/baz\n")
	want := []string{"/models/foo ", "  /models/bar", "/models/baz"}
	if !slices.Equal(got, want) {
		t.Errorf("splitNonEmpty must preserve path spaces, got %q want %q", got, want)
	}
}

func TestSplitNonEmpty_StripsCarriageReturn(t *testing.T) {
	got := splitNonEmpty("/models/foo\r\n")
	if len(got) != 1 || got[0] != "/models/foo" {
		t.Errorf("a trailing CR should be stripped, got %q", got)
	}
}
