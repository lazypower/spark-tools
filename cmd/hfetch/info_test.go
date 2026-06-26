package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// quantServer mocks both the tree listing and resolve-file endpoints. fileBodies
// maps base filename -> content; the tree is derived from its keys.
func quantServer(t *testing.T, fileBodies map[string]string) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/tree/main"):
			var b strings.Builder
			b.WriteString("[")
			first := true
			for name := range fileBodies {
				if !first {
					b.WriteString(",")
				}
				first = false
				fmt.Fprintf(&b, `{"type":"file","path":%q,"size":10}`, name)
			}
			b.WriteString("]")
			io.WriteString(w, b.String())
		case strings.Contains(r.URL.Path, "/resolve/main/"):
			body, ok := fileBodies[path.Base(r.URL.Path)]
			if !ok {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return api.NewClient(api.WithBaseURL(srv.URL), api.WithToken("t"))
}

func TestFetchQuantInfo_NVFP4Model(t *testing.T) {
	client := quantServer(t, map[string]string{
		"config.json":          `{"architectures":["X"]}`,
		"hf_quant_config.json": `{"quantization":{"quant_algo":"NVFP4","kv_cache_quant_algo":"FP8"}}`,
	})
	got := fetchQuantInfo(context.Background(), client, "org/model")
	if got == nil || got.Algo != "NVFP4" || !got.KVCacheFP8 {
		t.Fatalf("expected NVFP4 + KV FP8, got %+v", got)
	}
}

func TestFetchQuantInfo_UnquantizedReturnsNil(t *testing.T) {
	client := quantServer(t, map[string]string{
		"config.json": `{"architectures":["LlamaForCausalLM"]}`,
	})
	if got := fetchQuantInfo(context.Background(), client, "org/model"); got != nil {
		t.Fatalf("unquantized model should report nil, got %+v", got)
	}
}

func TestFetchQuantInfo_NoConfigReturnsNil(t *testing.T) {
	client := quantServer(t, map[string]string{
		"model.safetensors": "weights",
	})
	if got := fetchQuantInfo(context.Background(), client, "org/model"); got != nil {
		t.Fatalf("model without config.json should report nil, got %+v", got)
	}
}
