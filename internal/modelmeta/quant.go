// Package modelmeta derives serving-relevant model metadata from HuggingFace
// config files — quantization today — so callers can report it without
// downloading the weights. It interprets metadata only; it never performs
// quantization math (see specs/01-hfetch.md §3, §14.7). Extracted from
// pkg/hfetch/quant during the /internal extraction; that path remains a wrapper.
package modelmeta

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Info is the detected quantization of a model. A nil *QuantInfo means no
// quantization metadata was found (treat as full-precision / unknown).
type QuantInfo struct {
	Method     string // "modelopt" | "compressed-tensors" | "gptq" | "fp8" | ""
	Algo       string // "NVFP4" | "FP8" | "GPTQ-Int4" | "W4A16" | "" (best effort)
	KVCacheFP8 bool   // KV-cache served in FP8
}

// String renders a compact one-line summary, e.g. "modelopt NVFP4, KV-cache FP8".
func (i *QuantInfo) String() string {
	if i == nil || i.Method == "" {
		return "none"
	}
	s := i.Method
	if i.Algo != "" {
		s += " " + i.Algo
	}
	if i.KVCacheFP8 {
		s += ", KV-cache FP8"
	}
	return s
}

// Parse derives quant info from config.json plus optional sidecar configs
// (hf_quant_config.json for ModelOpt, quantize_config.json for GPTQ). Any
// argument may be nil/empty when that file is absent. Returns nil when no
// quantization is detected.
func ParseQuant(configJSON, hfQuantJSON, gptqJSON []byte) *QuantInfo {
	// ModelOpt (NVFP4/FP8) — the DGX Spark case. Its sidecar is authoritative.
	if len(hfQuantJSON) > 0 {
		var hq struct {
			Quantization struct {
				QuantAlgo        string `json:"quant_algo"`
				KVCacheQuantAlgo string `json:"kv_cache_quant_algo"`
			} `json:"quantization"`
		}
		if json.Unmarshal(hfQuantJSON, &hq) == nil && hq.Quantization.QuantAlgo != "" {
			return &QuantInfo{
				Method:     "modelopt",
				Algo:       hq.Quantization.QuantAlgo,
				KVCacheFP8: strings.EqualFold(hq.Quantization.KVCacheQuantAlgo, "FP8"),
			}
		}
	}

	// GPTQ — sidecar quantize_config.json.
	if len(gptqJSON) > 0 {
		var gc struct {
			Bits int `json:"bits"`
		}
		if json.Unmarshal(gptqJSON, &gc) == nil && gc.Bits > 0 {
			return &QuantInfo{Method: "gptq", Algo: fmt.Sprintf("GPTQ-Int%d", gc.Bits)}
		}
	}

	// Otherwise the quant config is embedded in config.json (compressed-tensors,
	// in-place gptq, fp8).
	if len(configJSON) > 0 {
		var cfg struct {
			QuantizationConfig *struct {
				QuantMethod  string `json:"quant_method"`
				Bits         int    `json:"bits"`
				Format       string `json:"format"`
				ConfigGroups map[string]struct {
					Weights struct {
						NumBits int    `json:"num_bits"`
						Type    string `json:"type"`
					} `json:"weights"`
				} `json:"config_groups"`
				KVCacheScheme *struct {
					NumBits int    `json:"num_bits"`
					Type    string `json:"type"`
				} `json:"kv_cache_scheme"`
			} `json:"quantization_config"`
		}
		if json.Unmarshal(configJSON, &cfg) == nil && cfg.QuantizationConfig != nil {
			qc := cfg.QuantizationConfig
			info := &QuantInfo{Method: qc.QuantMethod}
			switch {
			case qc.Bits > 0:
				info.Algo = fmt.Sprintf("W%dA16", qc.Bits)
			case len(qc.ConfigGroups) > 0:
				for _, g := range qc.ConfigGroups {
					if g.Weights.NumBits > 0 {
						if strings.Contains(strings.ToLower(g.Weights.Type), "float") {
							info.Algo = fmt.Sprintf("FP%d", g.Weights.NumBits)
						} else {
							info.Algo = fmt.Sprintf("W%dA16", g.Weights.NumBits)
						}
						break
					}
				}
			}
			if qc.KVCacheScheme != nil && qc.KVCacheScheme.NumBits == 8 &&
				strings.Contains(strings.ToLower(qc.KVCacheScheme.Type), "float") {
				info.KVCacheFP8 = true
			}
			if info.Method == "" && info.Algo == "" {
				return nil
			}
			return info
		}
	}

	return nil
}
