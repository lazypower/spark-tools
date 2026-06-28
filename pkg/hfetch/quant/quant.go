// Package quant is a compatibility wrapper over internal/modelmeta. The quant
// parsing authority moved to internal/modelmeta during the /internal extraction;
// this thin alias keeps existing importers (cmd/hfetch, pkg/llmserve/artifact)
// compiling unchanged until they migrate to modelmeta directly.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/modelmeta and use
// modelmeta.ParseQuant / modelmeta.QuantInfo.
package quant

import "github.com/lazypower/spark-tools/internal/modelmeta"

// Info is the detected quantization of a model. Alias of modelmeta.QuantInfo.
type Info = modelmeta.QuantInfo

// Parse derives quant Info from config.json plus optional sidecar configs.
// Delegates to modelmeta.ParseQuant — the single authority.
func Parse(configJSON, hfQuantJSON, gptqJSON []byte) *Info {
	return modelmeta.ParseQuant(configJSON, hfQuantJSON, gptqJSON)
}
