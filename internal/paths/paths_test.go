package paths

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestXDG_EnvWins(t *testing.T) {
	cases := []struct {
		name   string
		env    string
		fn     func(string) string
		envVal string
	}{
		{"config", "XDG_CONFIG_HOME", XDGConfig, "/x/cfg"},
		{"data", "XDG_DATA_HOME", XDGData, "/x/data"},
		{"cache", "XDG_CACHE_HOME", XDGCache, "/x/cache"},
		{"state", "XDG_STATE_HOME", XDGState, "/x/state"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(c.env, c.envVal)
			if got := c.fn("myapp"); got != filepath.Join(c.envVal, "myapp") {
				t.Errorf("%s with env set = %q, want %q", c.name, got, filepath.Join(c.envVal, "myapp"))
			}
		})
	}
}

func TestXDG_HomeFallbackSuffix(t *testing.T) {
	// With the XDG var unset, each resolver falls back under the home dir with the
	// canonical relative segment.
	cases := []struct {
		env    string
		fn     func(string) string
		suffix string
	}{
		{"XDG_CONFIG_HOME", XDGConfig, filepath.Join(".config", "myapp")},
		{"XDG_DATA_HOME", XDGData, filepath.Join(".local", "share", "myapp")},
		{"XDG_CACHE_HOME", XDGCache, filepath.Join(".cache", "myapp")},
		{"XDG_STATE_HOME", XDGState, filepath.Join(".local", "state", "myapp")},
	}
	for _, c := range cases {
		t.Setenv(c.env, "")
		if got := c.fn("myapp"); !strings.HasSuffix(got, c.suffix) {
			t.Errorf("fallback %q must end with %q", got, c.suffix)
		}
	}
}

func TestHome_BestEffort(t *testing.T) {
	// Home never panics; on a normal system it is non-empty. (Best-effort: a ""
	// return is a tolerated degraded mode, not an error — see the package doc.)
	_ = Home()
}
