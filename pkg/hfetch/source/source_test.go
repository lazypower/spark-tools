package source

import (
	"context"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// Head must return the size/hash injected from the tree listing, never a
// network HEAD — otherwise non-LFS files get size 0 (a 0-byte download). The
// client base URL points nowhere; if Head dialed out, this would error.
func TestFile_HeadUsesInjectedMetadata(t *testing.T) {
	s := New(api.NewClient(api.WithBaseURL("http://127.0.0.1:0"), api.WithToken("t")),
		"org/model", "config.json", 1234, "")

	size, sha, err := s.Head(context.Background())
	if err != nil {
		t.Fatalf("Head should not error (no network): %v", err)
	}
	if size != 1234 || sha != "" {
		t.Errorf("Head should echo injected tree metadata, got size=%d sha=%q", size, sha)
	}
}
