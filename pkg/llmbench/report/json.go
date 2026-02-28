package report

import (
	"encoding/json"

	"github.com/lazypower/spark-tools/pkg/llmbench/store"
)

// JSON returns the run result as compact JSON.
func JSON(result *store.RunResult) ([]byte, error) {
	return json.Marshal(result)
}

// JSONPretty returns the run result as pretty-printed JSON.
func JSONPretty(result *store.RunResult) ([]byte, error) {
	return json.MarshalIndent(result, "", "  ")
}
