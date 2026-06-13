package llmtidy

import (
	"encoding/json"
	"net/http"
)

func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}
