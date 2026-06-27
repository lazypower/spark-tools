package seam

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch"
)

// Seam: the hfetch CLI download adapter <-> the hfetch library download adapter.
//
// CONTRACT: there is ONE answer to "what size/hash does this file have" — the
// repo tree listing. Both the CLI (`hfetch pull`) and the library
// (hfetch.Client.Pull, used by llm-run / llm-bench / llm-tidy) must resolve it
// the same way. They were two separate apiFileSource copies; the non-LFS fix
// landed in the CLI one only, so the library still asks HEAD — which reports
// size 0 for non-LFS git files and yields a silent 0-byte download.
//
// STATUS: RED until the adapter is collapsed to a single authority. The mock's
// tree listing reports the true size (684) while HEAD is unreliable (0); a
// correct library Pull must produce 684 bytes.
func TestSeam_LibraryPull_NonLFS_UsesTreeListingNotHead(t *testing.T) {
	const body = "{\"architectures\":[\"X\"]}" // a non-LFS config file
	size := len(body)
	gitOID := "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3" // 40-hex git blob SHA1

	tree := `[{"type":"file","path":"config.json","size":` + itoa(size) + `,"oid":"` + gitOID + `"}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/tree/main"):
			io.WriteString(w, tree) // tree listing — the authority, true size
		case strings.HasSuffix(r.URL.Path, "/resolve/main/config.json") && r.Method == http.MethodHead:
			w.Header().Set("Content-Length", "0") // HEAD is unreliable for non-LFS
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/resolve/main/config.json"):
			io.WriteString(w, body) // GET serves the real bytes
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HFETCH_HOME", home)
	t.Setenv("HFETCH_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("HFETCH_CONFIG_DIR", filepath.Join(home, "config"))
	t.Setenv("HFETCH_CACHE_DIR", filepath.Join(home, "cache"))

	client, err := hfetch.NewClient(hfetch.WithBaseURL(srv.URL), hfetch.WithToken("t"))
	if err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	lf, err := client.Pull(context.Background(), "org/model", "config.json", hfetch.PullOptions{OutputDir: out})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	st, err := os.Stat(filepath.Join(out, "config.json"))
	if err != nil {
		t.Fatalf("downloaded file: %v", err)
	}
	if st.Size() != int64(size) {
		t.Errorf("SEAM CONTRACT BROKEN (CLI adapter vs library adapter): library Pull produced %d bytes; non-LFS size must come from the tree listing (%d), not HEAD", st.Size(), size)
	}
	if lf.Size != int64(size) {
		t.Errorf("registered size %d should be the tree-listing size %d, not HEAD's", lf.Size, size)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
