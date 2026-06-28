package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lazypower/spark-tools/internal/hub"
)

// The behavior suite (search, metadata, pagination, HEAD, range, download,
// retries, cache) lives in internal/hub; this locks the compat surface (alias
// identity, method ride-along, delegated constructor/options/predicate).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ *hub.Client = (*Client)(nil)
	var _ hub.Model = Model{}
	var _ hub.ModelFile = ModelFile{}
	var _ hub.LFS = LFS{}
	var _ hub.SearchOptions = SearchOptions{}
	if CacheTTL != hub.CacheTTL {
		t.Error("CacheTTL must re-export the authority value")
	}
}

func TestWrapper_ConstructorAndListFilesDelegate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"type":"file","path":"config.json"}]`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithToken("t"))
	files, err := c.ListFiles(context.Background(), "org/model")
	if err != nil {
		t.Fatalf("ListFiles via wrapper: %v", err)
	}
	if len(files) != 1 || files[0].Filename != "config.json" {
		t.Errorf("expected one file config.json, got %+v", files)
	}
}

func TestWrapper_IsRangeNotSupportedDelegates(t *testing.T) {
	if IsRangeNotSupported(nil) {
		t.Error("nil error is not a range-not-supported signal")
	}
}
