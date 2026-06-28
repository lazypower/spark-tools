package reconcile

import (
	"encoding/json"
	"io"
)

// jsonDecode is a tiny helper to share the same parsing path across tests.
func jsonDecode(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
