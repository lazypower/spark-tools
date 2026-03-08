package gguf

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ParseShard reads a complete GGUF header from rs, including metadata KVs
// and tensor info entries. Unlike Parse(), it preserves typed KVs for
// round-trip writing and parses tensor info entries needed for merge.
func ParseShard(rs io.ReadSeeker) (*ShardHeader, error) {
	var magic uint32
	if err := binary.Read(rs, binary.LittleEndian, &magic); err != nil {
		return nil, fmt.Errorf("reading magic: %w", err)
	}
	if magic != Magic {
		return nil, fmt.Errorf("not a GGUF file (magic: 0x%08X, expected 0x%08X)", magic, Magic)
	}

	var version uint32
	if err := binary.Read(rs, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("reading version: %w", err)
	}
	if version != Version2 && version != Version3 {
		return nil, fmt.Errorf("unsupported GGUF version: %d (supported: 2, 3)", version)
	}

	var tensorCount, kvCount uint64
	if err := binary.Read(rs, binary.LittleEndian, &tensorCount); err != nil {
		return nil, fmt.Errorf("reading tensor count: %w", err)
	}
	if err := binary.Read(rs, binary.LittleEndian, &kvCount); err != nil {
		return nil, fmt.Errorf("reading kv count: %w", err)
	}

	hdr := &ShardHeader{
		Version:     version,
		TensorCount: tensorCount,
		Alignment:   32, // GGUF default
	}

	// Read metadata KV pairs.
	for i := uint64(0); i < kvCount; i++ {
		kv, err := readKV(rs)
		if err != nil {
			return nil, fmt.Errorf("reading kv %d: %w", i, err)
		}
		hdr.KVs = append(hdr.KVs, kv)

		if kv.Key == "general.alignment" {
			if a := asUint64(kv.Value); a > 0 {
				hdr.Alignment = a
			}
		}
	}

	// Read tensor info entries.
	for i := uint64(0); i < tensorCount; i++ {
		ti, err := readTensorInfo(rs)
		if err != nil {
			return nil, fmt.Errorf("reading tensor info %d: %w", i, err)
		}
		hdr.Tensors = append(hdr.Tensors, ti)
	}

	// Data section starts at the current position, padded to alignment.
	pos, err := rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("getting position: %w", err)
	}
	hdr.DataOffset = alignUp(uint64(pos), hdr.Alignment)

	return hdr, nil
}

// readKV reads a single GGUF key-value pair, preserving type information.
func readKV(r io.Reader) (KV, error) {
	key, err := readString(r)
	if err != nil {
		return KV{}, fmt.Errorf("reading key: %w", err)
	}

	var valueType uint32
	if err := binary.Read(r, binary.LittleEndian, &valueType); err != nil {
		return KV{}, fmt.Errorf("reading value type for %q: %w", key, err)
	}

	value, err := readValueForKV(r, valueType)
	if err != nil {
		return KV{}, fmt.Errorf("reading value for %q: %w", key, err)
	}

	return KV{Key: key, ValueType: valueType, Value: value}, nil
}

// readValueForKV reads a value, returning TypedArray for arrays to preserve
// element type information for round-trip serialization.
func readValueForKV(r io.Reader, vtype uint32) (any, error) {
	if vtype == TypeArray {
		return readTypedArray(r)
	}
	return readValue(r, vtype)
}

// readTypedArray reads a GGUF array, preserving the element type.
func readTypedArray(r io.Reader) (TypedArray, error) {
	var elemType uint32
	if err := binary.Read(r, binary.LittleEndian, &elemType); err != nil {
		return TypedArray{}, err
	}
	var length uint64
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return TypedArray{}, err
	}
	if length > 1<<24 {
		return TypedArray{}, fmt.Errorf("array too long: %d elements", length)
	}

	values := make([]any, length)
	for i := range length {
		v, err := readValue(r, elemType)
		if err != nil {
			return TypedArray{}, fmt.Errorf("reading array element %d: %w", i, err)
		}
		values[i] = v
	}
	return TypedArray{ElemType: elemType, Values: values}, nil
}

// readTensorInfo reads a single tensor info entry from a GGUF header.
func readTensorInfo(r io.Reader) (TensorInfo, error) {
	name, err := readString(r)
	if err != nil {
		return TensorInfo{}, fmt.Errorf("reading tensor name: %w", err)
	}

	var ndims uint32
	if err := binary.Read(r, binary.LittleEndian, &ndims); err != nil {
		return TensorInfo{}, fmt.Errorf("reading ndims: %w", err)
	}
	if ndims > 8 {
		return TensorInfo{}, fmt.Errorf("tensor %q has %d dimensions (max 8)", name, ndims)
	}

	dims := make([]uint64, ndims)
	for i := range ndims {
		if err := binary.Read(r, binary.LittleEndian, &dims[i]); err != nil {
			return TensorInfo{}, fmt.Errorf("reading dimension %d: %w", i, err)
		}
	}

	var ttype uint32
	if err := binary.Read(r, binary.LittleEndian, &ttype); err != nil {
		return TensorInfo{}, fmt.Errorf("reading tensor type: %w", err)
	}

	var offset uint64
	if err := binary.Read(r, binary.LittleEndian, &offset); err != nil {
		return TensorInfo{}, fmt.Errorf("reading tensor offset: %w", err)
	}

	return TensorInfo{
		Name:       name,
		NDims:      ndims,
		Dimensions: dims,
		Type:       ttype,
		Offset:     offset,
	}, nil
}

// alignUp rounds offset up to the next multiple of alignment.
func alignUp(offset, alignment uint64) uint64 {
	if alignment == 0 {
		return offset
	}
	return (offset + alignment - 1) &^ (alignment - 1)
}

func asUint64(v any) uint64 {
	switch n := v.(type) {
	case uint64:
		return n
	case uint32:
		return uint64(n)
	case int64:
		return uint64(n)
	case int32:
		return uint64(n)
	default:
		return 0
	}
}
