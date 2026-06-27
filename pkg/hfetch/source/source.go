// Package source provides the single download.FileSource adapter over the HF
// API client — the one authority for how a file's bytes and metadata are
// fetched. Both the CLI (cmd/hfetch) and the library (pkg/hfetch) use it, so a
// fix to fetch behavior can't land in one place and miss the other (the
// divergence that shipped a 0-byte non-LFS download). Enforced by pkg/seam.
package source

import (
	"context"
	"io"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/download"
)

// File is a download.FileSource for one repo file. size and sha256 come from
// the repo tree listing (the authority): size is always correct (HEAD reports 0
// for non-LFS git files); sha256 is the LFS content hash, empty for non-LFS
// files (whose content is verified by git-blob SHA1 in the completeness gate).
type File struct {
	client  *api.Client
	modelID string
	file    string
	size    int64
	sha256  string
}

// New builds a file source from tree-listing metadata.
func New(client *api.Client, modelID, file string, size int64, sha256 string) *File {
	return &File{client: client, modelID: modelID, file: file, size: size, sha256: sha256}
}

// Head returns the authoritative size and content hash without a network call.
func (s *File) Head(context.Context) (int64, string, error) {
	return s.size, s.sha256, nil
}

// Download opens a byte stream at offset, translating the API's range-not-
// supported signal into the download package's sentinel so the manager can
// fall back to a single stream (e.g. HuggingFace Xet storage).
func (s *File) Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error) {
	rc, size, err := s.client.DownloadFile(ctx, s.modelID, s.file, offset)
	if err != nil {
		if api.IsRangeNotSupported(err) {
			return nil, 0, download.ErrRangeNotSupported
		}
		return nil, 0, err
	}
	return rc, size, nil
}
