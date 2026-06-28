// Package source is a compatibility wrapper over internal/hubsource. The single
// download.FileSource adapter over the HF Hub client (the one authority for how a
// file's bytes + tree-listing metadata are fetched) moved to internal/hubsource
// during the /internal extraction; this thin alias keeps existing importers
// (cmd/hfetch, pkg/hfetch) compiling unchanged until they migrate. The File type
// alias carries its Head/Download methods over; New delegates to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/hubsource.
package source

import (
	"github.com/lazypower/spark-tools/internal/hub"
	hs "github.com/lazypower/spark-tools/internal/hubsource"
)

// File is a download.FileSource for one repo file (size/sha256 from the tree
// listing authority).
type File = hs.File

// New builds a file source from tree-listing metadata.
func New(client *hub.Client, modelID, file string, size int64, sha256 string) *File {
	return hs.New(client, modelID, file, size, sha256)
}
