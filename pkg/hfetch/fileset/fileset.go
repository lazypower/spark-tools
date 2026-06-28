// Package fileset is a compatibility wrapper over internal/fileset. The
// serve-ready completeness gate and vLLM/GGUF selection authority moved to
// internal/fileset during the /internal extraction; this thin alias keeps
// existing importers (cmd/hfetch, pkg/llmserve/artifact, pkg/seam) compiling
// unchanged until they migrate.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/fileset.
package fileset

import (
	ifs "github.com/lazypower/spark-tools/internal/fileset"
	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// Selection profile (alias) and its values.
type Profile = ifs.Profile

const (
	ProfileGGUF = ifs.ProfileGGUF
	ProfileVLLM = ifs.ProfileVLLM
)

// Completeness types (aliases).
type (
	Issue  = ifs.Issue
	Report = ifs.Report
)

// Verify runs the serve-ready completeness gate. Delegates to internal/fileset.
func Verify(repoFiles []api.ModelFile, localDir string) (*Report, error) {
	return ifs.Verify(repoFiles, localDir)
}

// SelectVLLM returns the complete serve-ready safetensors fileset.
func SelectVLLM(files []api.ModelFile) []api.ModelFile { return ifs.SelectVLLM(files) }
