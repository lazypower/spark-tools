// Package gguf provides GGUF file format awareness: header parsing,
// quantization type detection, and metadata extraction.
package gguf

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// Parse reads a GGUF header from r and returns the parsed metadata.
// Only the header is read — tensor data is not consumed.
// Supports GGUF v2 and v3.
func Parse(r io.Reader) (*GGUFMetadata, error) {
	// Read magic number.
	var magic uint32
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, fmt.Errorf("reading magic: %w", err)
	}
	if magic != Magic {
		return nil, fmt.Errorf("not a GGUF file (magic: 0x%08X, expected 0x%08X)", magic, Magic)
	}

	// Read version.
	var version uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("reading version: %w", err)
	}
	if version != Version2 && version != Version3 {
		return nil, fmt.Errorf("unsupported GGUF version: %d (supported: 2, 3)", version)
	}

	// Read tensor count and metadata KV count.
	var tensorCount, kvCount uint64
	if err := binary.Read(r, binary.LittleEndian, &tensorCount); err != nil {
		return nil, fmt.Errorf("reading tensor count: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &kvCount); err != nil {
		return nil, fmt.Errorf("reading kv count: %w", err)
	}

	meta := &GGUFMetadata{
		Version:        version,
		TensorCount:    tensorCount,
		CustomMetadata: make(map[string]any),
	}

	// Parse metadata key-value pairs.
	for range kvCount {
		key, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("reading key: %w", err)
		}

		var valueType uint32
		if err := binary.Read(r, binary.LittleEndian, &valueType); err != nil {
			return nil, fmt.Errorf("reading value type for %q: %w", key, err)
		}

		value, err := readValue(r, valueType)
		if err != nil {
			return nil, fmt.Errorf("reading value for %q: %w", key, err)
		}

		applyMetadata(meta, key, value)
	}

	// Derive quant type name from file_type if not already set.
	if meta.QuantType == "" && meta.FileType != 0 {
		if name, ok := FileTypeNames[meta.FileType]; ok {
			meta.QuantType = name
		}
	}

	return meta, nil
}

// applyMetadata maps well-known GGUF keys to structured fields.
func applyMetadata(meta *GGUFMetadata, key string, value any) {
	// Strip architecture prefix for common keys.
	// e.g., "llama.context_length" → check "context_length"
	suffix := key
	if i := strings.Index(key, "."); i >= 0 {
		suffix = key[i+1:]
	}

	switch key {
	case "general.architecture":
		meta.Architecture = asString(value)
	case "general.file_type":
		meta.FileType = asInt(value)
		if name, ok := FileTypeNames[meta.FileType]; ok {
			meta.QuantType = name
		}
	case "general.parameter_count":
		meta.ParameterCount = asInt64(value)
	}

	// Architecture-specific keys (e.g., "llama.context_length").
	switch suffix {
	case "context_length":
		meta.ContextLength = asInt(value)
	case "block_count":
		meta.LayerCount = asInt(value)
	case "attention.head_count":
		meta.HeadCount = asInt(value)
	case "embedding_length":
		meta.EmbeddingSize = asInt(value)
	case "vocab_size":
		meta.VocabSize = asInt(value)
	default:
		meta.CustomMetadata[key] = value
	}
}

func readString(r io.Reader) (string, error) {
	var length uint64
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	if length > 1<<20 { // Sanity check: 1MB max string
		return "", fmt.Errorf("string too long: %d bytes", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readValue(r io.Reader, vtype uint32) (any, error) {
	switch vtype {
	case TypeUint8:
		var v uint8
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeInt8:
		var v int8
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeUint16:
		var v uint16
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeInt16:
		var v int16
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeUint32:
		var v uint32
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeInt32:
		var v int32
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeFloat32:
		var v float32
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeBool:
		var v uint8
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return false, err
		}
		return v != 0, nil
	case TypeString:
		return readString(r)
	case TypeArray:
		return readArray(r)
	case TypeUint64:
		var v uint64
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeInt64:
		var v int64
		return v, binary.Read(r, binary.LittleEndian, &v)
	case TypeFloat64:
		var v float64
		return v, binary.Read(r, binary.LittleEndian, &v)
	default:
		return nil, fmt.Errorf("unknown value type: %d", vtype)
	}
}

func readArray(r io.Reader) (any, error) {
	var elemType uint32
	if err := binary.Read(r, binary.LittleEndian, &elemType); err != nil {
		return nil, err
	}
	var length uint64
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length > 1<<24 { // Sanity: 16M elements max
		return nil, fmt.Errorf("array too long: %d elements", length)
	}

	result := make([]any, length)
	for i := range length {
		v, err := readValue(r, elemType)
		if err != nil {
			return nil, fmt.Errorf("reading array element %d: %w", i, err)
		}
		result[i] = v
	}
	return result, nil
}

// Type conversion helpers.

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt(v any) int {
	switch n := v.(type) {
	case uint32:
		return int(n)
	case int32:
		return int(n)
	case uint64:
		return int(n)
	case int64:
		return int(n)
	case uint16:
		return int(n)
	case int16:
		return int(n)
	case uint8:
		return int(n)
	case int8:
		return int(n)
	default:
		return 0
	}
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case uint64:
		return int64(n)
	case int64:
		return n
	case uint32:
		return int64(n)
	case int32:
		return int64(n)
	default:
		return 0
	}
}
