package gguf

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildTestGGUF creates a minimal valid GGUF v3 binary header for testing.
// testArrayValue wraps an array element type and its values for buildTestGGUF.
type testArrayValue struct {
	ElemType uint32
	Values   []any
}

func buildTestGGUF(kvPairs map[string]any) []byte {
	var buf bytes.Buffer

	// Magic + version + tensor count + KV count
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(Version3))
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensor count
	binary.Write(&buf, binary.LittleEndian, uint64(len(kvPairs)))

	for key, value := range kvPairs {
		writeString(&buf, key)
		writeTypedValue(&buf, value)
	}

	return buf.Bytes()
}

func writeTypedValue(buf *bytes.Buffer, value any) {
	switch v := value.(type) {
	case string:
		binary.Write(buf, binary.LittleEndian, TypeString)
		writeString(buf, v)
	case uint8:
		binary.Write(buf, binary.LittleEndian, TypeUint8)
		binary.Write(buf, binary.LittleEndian, v)
	case int8:
		binary.Write(buf, binary.LittleEndian, TypeInt8)
		binary.Write(buf, binary.LittleEndian, v)
	case uint16:
		binary.Write(buf, binary.LittleEndian, TypeUint16)
		binary.Write(buf, binary.LittleEndian, v)
	case int16:
		binary.Write(buf, binary.LittleEndian, TypeInt16)
		binary.Write(buf, binary.LittleEndian, v)
	case uint32:
		binary.Write(buf, binary.LittleEndian, TypeUint32)
		binary.Write(buf, binary.LittleEndian, v)
	case int32:
		binary.Write(buf, binary.LittleEndian, TypeInt32)
		binary.Write(buf, binary.LittleEndian, v)
	case float32:
		binary.Write(buf, binary.LittleEndian, TypeFloat32)
		binary.Write(buf, binary.LittleEndian, v)
	case float64:
		binary.Write(buf, binary.LittleEndian, TypeFloat64)
		binary.Write(buf, binary.LittleEndian, v)
	case uint64:
		binary.Write(buf, binary.LittleEndian, TypeUint64)
		binary.Write(buf, binary.LittleEndian, v)
	case int64:
		binary.Write(buf, binary.LittleEndian, TypeInt64)
		binary.Write(buf, binary.LittleEndian, v)
	case bool:
		binary.Write(buf, binary.LittleEndian, TypeBool)
		if v {
			binary.Write(buf, binary.LittleEndian, uint8(1))
		} else {
			binary.Write(buf, binary.LittleEndian, uint8(0))
		}
	case testArrayValue:
		binary.Write(buf, binary.LittleEndian, TypeArray)
		binary.Write(buf, binary.LittleEndian, v.ElemType)
		binary.Write(buf, binary.LittleEndian, uint64(len(v.Values)))
		for _, elem := range v.Values {
			writeRawValue(buf, v.ElemType, elem)
		}
	}
}

// writeRawValue writes a value without the type prefix (used for array elements).
func writeRawValue(buf *bytes.Buffer, vtype uint32, value any) {
	switch vtype {
	case TypeUint8:
		binary.Write(buf, binary.LittleEndian, value.(uint8))
	case TypeInt8:
		binary.Write(buf, binary.LittleEndian, value.(int8))
	case TypeUint16:
		binary.Write(buf, binary.LittleEndian, value.(uint16))
	case TypeInt16:
		binary.Write(buf, binary.LittleEndian, value.(int16))
	case TypeUint32:
		binary.Write(buf, binary.LittleEndian, value.(uint32))
	case TypeInt32:
		binary.Write(buf, binary.LittleEndian, value.(int32))
	case TypeFloat32:
		binary.Write(buf, binary.LittleEndian, value.(float32))
	case TypeUint64:
		binary.Write(buf, binary.LittleEndian, value.(uint64))
	case TypeInt64:
		binary.Write(buf, binary.LittleEndian, value.(int64))
	case TypeFloat64:
		binary.Write(buf, binary.LittleEndian, value.(float64))
	case TypeBool:
		if value.(bool) {
			binary.Write(buf, binary.LittleEndian, uint8(1))
		} else {
			binary.Write(buf, binary.LittleEndian, uint8(0))
		}
	case TypeString:
		writeString(buf, value.(string))
	}
}

func writeString(buf *bytes.Buffer, s string) {
	binary.Write(buf, binary.LittleEndian, uint64(len(s)))
	buf.WriteString(s)
}

func TestParse_BasicMetadata(t *testing.T) {
	data := buildTestGGUF(map[string]any{
		"general.architecture": "llama",
		"general.file_type":    uint32(15), // Q4_K_M
		"llama.context_length": uint32(4096),
		"llama.block_count":    uint32(32),
		"llama.embedding_length": uint32(4096),
		"llama.attention.head_count": uint32(32),
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if meta.Architecture != "llama" {
		t.Errorf("architecture: expected llama, got %q", meta.Architecture)
	}
	if meta.QuantType != "Q4_K_M" {
		t.Errorf("quant: expected Q4_K_M, got %q", meta.QuantType)
	}
	if meta.ContextLength != 4096 {
		t.Errorf("context: expected 4096, got %d", meta.ContextLength)
	}
	if meta.LayerCount != 32 {
		t.Errorf("layers: expected 32, got %d", meta.LayerCount)
	}
	if meta.EmbeddingSize != 4096 {
		t.Errorf("embedding: expected 4096, got %d", meta.EmbeddingSize)
	}
	if meta.HeadCount != 32 {
		t.Errorf("heads: expected 32, got %d", meta.HeadCount)
	}
	if meta.Version != 3 {
		t.Errorf("version: expected 3, got %d", meta.Version)
	}
}

func TestParse_InvalidMagic(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(0xDEADBEEF))

	_, err := Parse(&buf)
	if err == nil {
		t.Fatal("expected error for invalid magic")
	}
}

func TestParse_UnsupportedVersion(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(99))

	_, err := Parse(&buf)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestParse_Version2(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(Version2))
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensors
	binary.Write(&buf, binary.LittleEndian, uint64(1)) // 1 KV pair

	writeString(&buf, "general.architecture")
	binary.Write(&buf, binary.LittleEndian, TypeString)
	writeString(&buf, "qwen2")

	meta, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse v2: %v", err)
	}
	if meta.Architecture != "qwen2" {
		t.Errorf("expected qwen2, got %q", meta.Architecture)
	}
	if meta.Version != 2 {
		t.Errorf("version: expected 2, got %d", meta.Version)
	}
}

func TestParse_AllValueTypes(t *testing.T) {
	data := buildTestGGUF(map[string]any{
		"custom.uint8_val":   uint8(42),
		"custom.int8_val":    int8(-7),
		"custom.uint16_val":  uint16(1024),
		"custom.int16_val":   int16(-256),
		"custom.uint32_val":  uint32(100000),
		"custom.int32_val":   int32(-50000),
		"custom.float32_val": float32(3.14),
		"custom.bool_true":   true,
		"custom.bool_false":  false,
		"custom.uint64_val":  uint64(9999999999),
		"custom.int64_val":   int64(-9999999999),
		"custom.float64_val": float64(2.71828),
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	tests := []struct {
		key  string
		want any
	}{
		{"custom.uint8_val", uint8(42)},
		{"custom.int8_val", int8(-7)},
		{"custom.uint16_val", uint16(1024)},
		{"custom.int16_val", int16(-256)},
		{"custom.uint32_val", uint32(100000)},
		{"custom.int32_val", int32(-50000)},
		{"custom.float32_val", float32(3.14)},
		{"custom.bool_true", true},
		{"custom.bool_false", false},
		{"custom.uint64_val", uint64(9999999999)},
		{"custom.int64_val", int64(-9999999999)},
		{"custom.float64_val", float64(2.71828)},
	}

	for _, tt := range tests {
		got, ok := meta.CustomMetadata[tt.key]
		if !ok {
			t.Errorf("key %q not found in custom metadata", tt.key)
			continue
		}
		if got != tt.want {
			t.Errorf("key %q: got %v (%T), want %v (%T)", tt.key, got, got, tt.want, tt.want)
		}
	}
}

func TestParse_ArrayValues(t *testing.T) {
	data := buildTestGGUF(map[string]any{
		"custom.int_array": testArrayValue{
			ElemType: TypeUint32,
			Values:   []any{uint32(10), uint32(20), uint32(30)},
		},
		"custom.str_array": testArrayValue{
			ElemType: TypeString,
			Values:   []any{"hello", "world"},
		},
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Check int array
	intArr, ok := meta.CustomMetadata["custom.int_array"]
	if !ok {
		t.Fatal("custom.int_array not found")
	}
	arr, ok := intArr.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", intArr)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	if arr[0] != uint32(10) || arr[1] != uint32(20) || arr[2] != uint32(30) {
		t.Errorf("unexpected int array values: %v", arr)
	}

	// Check string array
	strArr, ok := meta.CustomMetadata["custom.str_array"]
	if !ok {
		t.Fatal("custom.str_array not found")
	}
	sarr, ok := strArr.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", strArr)
	}
	if len(sarr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(sarr))
	}
	if sarr[0] != "hello" || sarr[1] != "world" {
		t.Errorf("unexpected string array values: %v", sarr)
	}
}

func TestParse_ParameterCount(t *testing.T) {
	data := buildTestGGUF(map[string]any{
		"general.parameter_count": uint64(7_000_000_000),
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if meta.ParameterCount != 7_000_000_000 {
		t.Errorf("expected parameter count 7000000000, got %d", meta.ParameterCount)
	}
}

func TestParse_VocabSize(t *testing.T) {
	data := buildTestGGUF(map[string]any{
		"llama.vocab_size": uint32(32000),
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if meta.VocabSize != 32000 {
		t.Errorf("expected vocab size 32000, got %d", meta.VocabSize)
	}
}

func TestParse_FileTypeToQuantType(t *testing.T) {
	// Test that file_type is mapped to QuantType even when set as the
	// only metadata (testing the fallback after the KV loop).
	data := buildTestGGUF(map[string]any{
		"general.file_type": uint32(8), // Q8_0
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if meta.QuantType != "Q8_0" {
		t.Errorf("expected Q8_0, got %q", meta.QuantType)
	}
}

func TestParse_UnknownValueType(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(Version3))
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensors
	binary.Write(&buf, binary.LittleEndian, uint64(1)) // 1 KV pair

	writeString(&buf, "bad_key")
	binary.Write(&buf, binary.LittleEndian, uint32(255)) // unknown type

	_, err := Parse(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error for unknown value type")
	}
}

func TestReadString_TooLong(t *testing.T) {
	var buf bytes.Buffer
	// Write a string length that exceeds the 1MB sanity limit
	binary.Write(&buf, binary.LittleEndian, uint64(1<<20+1))
	_, err := readString(&buf)
	if err == nil {
		t.Fatal("expected error for string exceeding 1MB")
	}
}

func TestReadArray_TooLong(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(TypeUint32)) // elem type
	binary.Write(&buf, binary.LittleEndian, uint64(1<<24+1))   // too many elements
	_, err := readArray(&buf)
	if err == nil {
		t.Fatal("expected error for array exceeding 16M elements")
	}
}

func TestAsInt_AllTypes(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int
	}{
		{"uint32", uint32(42), 42},
		{"int32", int32(-42), -42},
		{"uint64", uint64(100), 100},
		{"int64", int64(-100), -100},
		{"uint16", uint16(300), 300},
		{"int16", int16(-300), -300},
		{"uint8", uint8(255), 255},
		{"int8", int8(-127), -127},
		{"string_fallback", "not a number", 0},
		{"float_fallback", float64(3.14), 0},
		{"nil_fallback", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := asInt(tt.val)
			if got != tt.want {
				t.Errorf("asInt(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestAsInt64_AllTypes(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int64
	}{
		{"uint64", uint64(9999999999), 9999999999},
		{"int64", int64(-9999999999), -9999999999},
		{"uint32", uint32(42), 42},
		{"int32", int32(-42), -42},
		{"string_fallback", "not a number", 0},
		{"nil_fallback", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := asInt64(tt.val)
			if got != tt.want {
				t.Errorf("asInt64(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestAsString_NonString(t *testing.T) {
	if got := asString(42); got != "" {
		t.Errorf("asString(42) = %q, want empty", got)
	}
	if got := asString(nil); got != "" {
		t.Errorf("asString(nil) = %q, want empty", got)
	}
	if got := asString("hello"); got != "hello" {
		t.Errorf("asString(\"hello\") = %q, want \"hello\"", got)
	}
}

func TestParse_ArchSpecificKeysWithInt32(t *testing.T) {
	// Exercise applyMetadata with int32 values for architecture-specific
	// keys, testing asInt with int32 input.
	data := buildTestGGUF(map[string]any{
		"mamba.context_length":        int32(2048),
		"mamba.block_count":           int32(24),
		"mamba.embedding_length":      int32(2560),
		"mamba.attention.head_count":  int32(16),
		"mamba.vocab_size":            int32(50000),
		"general.architecture":        "mamba",
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if meta.Architecture != "mamba" {
		t.Errorf("architecture: expected mamba, got %q", meta.Architecture)
	}
	if meta.ContextLength != 2048 {
		t.Errorf("context_length: expected 2048, got %d", meta.ContextLength)
	}
	if meta.LayerCount != 24 {
		t.Errorf("block_count: expected 24, got %d", meta.LayerCount)
	}
	if meta.EmbeddingSize != 2560 {
		t.Errorf("embedding_length: expected 2560, got %d", meta.EmbeddingSize)
	}
	if meta.HeadCount != 16 {
		t.Errorf("head_count: expected 16, got %d", meta.HeadCount)
	}
	if meta.VocabSize != 50000 {
		t.Errorf("vocab_size: expected 50000, got %d", meta.VocabSize)
	}
}

func TestParse_EmptyKVPairs(t *testing.T) {
	data := buildTestGGUF(map[string]any{})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if meta.Architecture != "" {
		t.Errorf("expected empty architecture, got %q", meta.Architecture)
	}
	if meta.QuantType != "" {
		t.Errorf("expected empty quant type, got %q", meta.QuantType)
	}
}

func TestParse_BoolValues(t *testing.T) {
	data := buildTestGGUF(map[string]any{
		"custom.flag_true":  true,
		"custom.flag_false": false,
	})

	meta, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got, ok := meta.CustomMetadata["custom.flag_true"]; !ok || got != true {
		t.Errorf("expected true, got %v", got)
	}
	if got, ok := meta.CustomMetadata["custom.flag_false"]; !ok || got != false {
		t.Errorf("expected false, got %v", got)
	}
}

func TestParse_TruncatedHeader(t *testing.T) {
	// Only magic, no version — should fail reading version
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))

	_, err := Parse(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error for truncated header (missing version)")
	}
}

func TestParse_TruncatedTensorCount(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(Version3))
	// no tensor count or kv count

	_, err := Parse(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error for truncated header (missing tensor count)")
	}
}

func TestParse_TruncatedKVCount(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(Version3))
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensor count
	// missing kv count

	_, err := Parse(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected error for truncated header (missing kv count)")
	}
}
