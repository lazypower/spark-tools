package progress

import "testing"

func TestFormatSize(t *testing.T) {
	const (
		kb = 1024
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	cases := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"just-below-kb", kb - 1, "1023 B"},
		{"exact-kb", kb, "1.0 KB"},
		{"mid-kb", kb + kb/2, "1.5 KB"},
		{"just-below-mb", mb - 1, "1024.0 KB"},
		{"exact-mb", mb, "1.0 MB"},
		{"mid-mb", mb + mb/2, "1.5 MB"},
		{"just-below-gb", gb - 1, "1024.0 MB"},
		{"exact-gb", gb, "1.0 GB"},
		{"multi-gb", 3*gb + gb/4, "3.2 GB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatSize(c.bytes); got != c.want {
				t.Errorf("FormatSize(%d) = %q, want %q", c.bytes, got, c.want)
			}
		})
	}
}

func TestFormatSpeed(t *testing.T) {
	cases := []struct {
		bps  float64
		want string
	}{
		{0, "0 B/s"},
		{512, "512 B/s"},
		{1024, "1.0 KB/s"},
		{1024 * 1024, "1.0 MB/s"},
	}
	for _, c := range cases {
		if got := FormatSpeed(c.bps); got != c.want {
			t.Errorf("FormatSpeed(%v) = %q, want %q", c.bps, got, c.want)
		}
	}
}

func TestBar(t *testing.T) {
	const (
		full  = '█'
		empty = '░'
	)
	count := func(s string, r rune) int {
		n := 0
		for _, c := range s {
			if c == r {
				n++
			}
		}
		return n
	}

	t.Run("clamps below zero to empty", func(t *testing.T) {
		got := Bar(-0.5, 10)
		if count(got, full) != 0 || count(got, empty) != 10 {
			t.Errorf("Bar(-0.5,10) = %q, want all empty", got)
		}
	})
	t.Run("clamps above one to full", func(t *testing.T) {
		got := Bar(1.5, 10)
		if count(got, full) != 10 || count(got, empty) != 0 {
			t.Errorf("Bar(1.5,10) = %q, want all full", got)
		}
	})
	t.Run("half fill", func(t *testing.T) {
		got := Bar(0.5, 10)
		if count(got, full) != 5 || count(got, empty) != 5 {
			t.Errorf("Bar(0.5,10) = %q, want 5 full 5 empty", got)
		}
	})
	t.Run("zero fraction is all empty", func(t *testing.T) {
		got := Bar(0, 4)
		if count(got, full) != 0 || count(got, empty) != 4 {
			t.Errorf("Bar(0,4) = %q, want all empty", got)
		}
	})
	t.Run("full fraction is all full", func(t *testing.T) {
		got := Bar(1, 4)
		if count(got, full) != 4 {
			t.Errorf("Bar(1,4) = %q, want all full", got)
		}
	})
	t.Run("width is honored in rune count", func(t *testing.T) {
		// Width counts runes, not bytes — the block glyphs are multi-byte.
		if n := len([]rune(Bar(0.3, 20))); n != 20 {
			t.Errorf("Bar(0.3,20) rune width = %d, want 20", n)
		}
	})
}
