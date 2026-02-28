package gguf

// GGUF magic number and supported versions.
const (
	Magic   = 0x46475547 // "GGUF" in little-endian
	Version2 = 2
	Version3 = 3
)

// Metadata value types in the GGUF format.
const (
	TypeUint8   uint32 = 0
	TypeInt8    uint32 = 1
	TypeUint16  uint32 = 2
	TypeInt16   uint32 = 3
	TypeUint32  uint32 = 4
	TypeInt32   uint32 = 5
	TypeFloat32 uint32 = 6
	TypeBool    uint32 = 7
	TypeString  uint32 = 8
	TypeArray   uint32 = 9
	TypeUint64  uint32 = 10
	TypeInt64   uint32 = 11
	TypeFloat64 uint32 = 12
)

// GGUFMetadata contains parsed GGUF header information.
type GGUFMetadata struct {
	Architecture   string         `json:"architecture"`
	ParameterCount int64          `json:"parameter_count"`
	ContextLength  int            `json:"context_length"`
	QuantType      string         `json:"quant_type"`
	FileType       int            `json:"file_type"`
	HeadCount      int            `json:"head_count"`
	LayerCount     int            `json:"layer_count"`
	EmbeddingSize  int            `json:"embedding_size"`
	VocabSize      int            `json:"vocab_size"`
	Version        uint32         `json:"version"`
	TensorCount    uint64         `json:"tensor_count"`
	CustomMetadata map[string]any `json:"custom_metadata,omitempty"`
}

// FileTypeNames maps GGUF file_type integers to human-readable quantization names.
var FileTypeNames = map[int]string{
	0:  "F32",
	1:  "F16",
	2:  "Q4_0",
	3:  "Q4_1",
	6:  "Q5_0",
	7:  "Q5_1",
	8:  "Q8_0",
	9:  "Q8_1",
	10: "Q2_K",
	11: "Q3_K_S",
	12: "Q3_K_M",
	13: "Q3_K_L",
	14: "Q4_K_S",
	15: "Q4_K_M",
	16: "Q5_K_S",
	17: "Q5_K_M",
	18: "Q6_K",
	19: "IQ2_XXS",
	20: "IQ2_XS",
	21: "IQ3_XXS",
	22: "IQ1_S",
	23: "IQ4_NL",
	24: "IQ3_S",
	25: "IQ2_S",
	26: "IQ4_XS",
	27: "IQ1_M",
	28: "BF16",
	29: "Q4_0_4_4",
	30: "Q4_0_4_8",
	31: "Q4_0_8_8",
}

// QuantQualityLabel returns a short human-readable quality annotation
// for a given quantization type. Used in the interactive picker.
func QuantQualityLabel(quant string) string {
	switch quant {
	case "Q4_K_M":
		return "Best balance of quality/size"
	case "Q5_K_M":
		return "Higher quality"
	case "Q5_K_S":
		return "Higher quality, smaller"
	case "Q6_K":
		return "Near-lossless"
	case "Q8_0":
		return "Highest quality"
	case "Q4_K_S":
		return "Good quality, compact"
	case "Q3_K_L":
		return "Lower quality, small"
	case "Q3_K_M", "Q3_K_S":
		return "Low quality, very small"
	case "Q2_K":
		return "Lowest quality"
	case "IQ4_XS", "IQ4_NL":
		return "Smallest, lower quality"
	case "IQ3_XXS", "IQ3_S":
		return "Very small, low quality"
	case "IQ2_XXS", "IQ2_XS", "IQ2_S":
		return "Extremely small"
	case "IQ1_S", "IQ1_M":
		return "Minimum viable quality"
	case "F16", "BF16":
		return "Full precision (large)"
	case "F32":
		return "Full precision (very large)"
	default:
		return ""
	}
}

// QuantBitsPerWeight provides approximate bits-per-weight for common quantizations.
var QuantBitsPerWeight = map[string]float64{
	"F32":     32.0,
	"F16":     16.0,
	"BF16":    16.0,
	"Q8_0":    8.5,
	"Q8_1":    9.0,
	"Q6_K":    6.57,
	"Q5_K_M":  5.69,
	"Q5_K_S":  5.54,
	"Q5_0":    5.54,
	"Q5_1":    6.0,
	"Q4_K_M":  4.85,
	"Q4_K_S":  4.58,
	"Q4_0":    4.5,
	"Q4_1":    5.0,
	"Q3_K_L":  3.91,
	"Q3_K_M":  3.44,
	"Q3_K_S":  3.44,
	"Q2_K":    3.35,
	"IQ4_XS":  4.25,
	"IQ4_NL":  4.5,
	"IQ3_XXS": 3.06,
	"IQ3_S":   3.44,
	"IQ2_XXS": 2.06,
	"IQ2_XS":  2.31,
	"IQ2_S":   2.5,
	"IQ1_S":   1.56,
	"IQ1_M":   1.75,
}
