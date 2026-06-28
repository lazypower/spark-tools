// Package artifact is a compatibility wrapper over internal/serveartifact. The
// serving-fact detector (reads arch/tokenizer/quant/vision/remote-code off a
// verified model directory, and the Verify bridge to hfetch's completeness gate)
// moved to internal/serveartifact during the /internal extraction; this thin alias
// keeps existing importers (pkg/llmserve, cmd/llm-serve, pkg/seam) compiling
// unchanged until they migrate. Verify/DetectFacts delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/serveartifact.
package artifact

import (
	"github.com/lazypower/spark-tools/internal/hub"
	sa "github.com/lazypower/spark-tools/internal/serveartifact"
	"github.com/lazypower/spark-tools/internal/serving"
)

// Verify runs hfetch's completeness gate over the repo file tree and, on success,
// detects the serving-relevant facts from the local model directory.
func Verify(repoFiles []hub.ModelFile, dir string) (serving.ArtifactFacts, error) {
	return sa.Verify(repoFiles, dir)
}

// DetectFacts reads the serving-relevant facts off a verified model directory.
func DetectFacts(dir string) (serving.ArtifactFacts, error) {
	return sa.DetectFacts(dir)
}
