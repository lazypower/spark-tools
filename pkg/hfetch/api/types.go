package api

import "time"

// Model represents a HuggingFace model repository.
type Model struct {
	ID          string      `json:"id"`
	Author      string      `json:"author"`
	Tags        []string    `json:"tags"`
	Downloads   int         `json:"downloads"`
	Likes       int         `json:"likes"`
	LastUpdated time.Time   `json:"lastModified"`
	Private     bool        `json:"private"`
	Gated       interface{} `json:"gated"` // bool or string depending on HF API version
	Siblings    []Sibling   `json:"siblings"`
}

// Sibling represents a file entry in the HF API /siblings response.
type Sibling struct {
	Filename string `json:"rfilename"`
}

// ModelFile represents a single file in a model repo with full metadata.
type ModelFile struct {
	Filename string `json:"path"`
	Size     int64  `json:"size"`
	BlobID   string `json:"oid"` // Git LFS OID (SHA256)
	LFS      *LFS   `json:"lfs"`
}

// LFS contains Git LFS metadata for a file.
type LFS struct {
	OID  string `json:"oid"`
	Size int64  `json:"size"`
}

// SearchOptions configures a model search.
type SearchOptions struct {
	Filter string // e.g. "gguf" to filter by tag
	Sort   string // "downloads", "lastModified", "trending"
	Limit  int
}
