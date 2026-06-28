package ollama

import "time"

// Model is one entry returned by GET /api/tags.
type Model struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest,omitempty"`
}

// TagsResponse is the body returned by GET /api/tags.
type TagsResponse struct {
	Models []Model `json:"models"`
}

// deleteRequest is the body sent to DELETE /api/delete.
type deleteRequest struct {
	Name string `json:"name"`
}

// pullRequest is the body sent to POST /api/pull.
type pullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

// pullStatus is one event in the streaming response from POST /api/pull.
type pullStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
