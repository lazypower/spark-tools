package main

import "testing"

func TestResolveStreams(t *testing.T) {
	// Explicit flag wins.
	if got := resolveStreams(8); got != 8 {
		t.Errorf("flag value must win, got %d", got)
	}
	// Env fallback when no flag.
	t.Setenv("HFETCH_STREAMS", "6")
	if got := resolveStreams(0); got != 6 {
		t.Errorf("HFETCH_STREAMS must apply, got %d", got)
	}
	// Bad env → default 4.
	t.Setenv("HFETCH_STREAMS", "garbage")
	if got := resolveStreams(0); got != 4 {
		t.Errorf("invalid env must fall back to 4, got %d", got)
	}
}

func TestRedactToken(t *testing.T) {
	// A long token shows only its first 8 chars — never the secret tail.
	got := redactToken("hf_abcdefghijklmnopqrstuvwxyz")
	if got != "hf_abcde..." {
		t.Errorf("redactToken leaked or mangled, got %q", got)
	}
	if len(got) > len("hf_abcde...") {
		t.Errorf("redaction must not expose more than 8 chars: %q", got)
	}
	// A short token is still elided.
	if redactToken("short") != "short..." {
		t.Errorf("short token redaction wrong: %q", redactToken("short"))
	}
}

func TestTokenSourceLabel(t *testing.T) {
	cases := map[string]string{
		"flag":    "--token flag",
		"env":     "HFETCH_TOKEN environment variable",
		"unknown": "none",
	}
	for src, want := range cases {
		if got := tokenSourceLabel(src); got != want {
			t.Errorf("tokenSourceLabel(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestParseBandwidth(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"100MB/s", 100 * 1024 * 1024, false},
		{"50mb", 50 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"512KB", 512 * 1024, false},
		{"2048B", 2048, false},
		{"  10 MB/s ", 10 * 1024 * 1024, false},
		{"0MB", 0, true},
		{"-5MB", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parseBandwidth(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseBandwidth(%q) should error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseBandwidth(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}
