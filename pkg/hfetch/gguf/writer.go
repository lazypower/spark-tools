package gguf

import (
	"encoding/binary"
	"io"
)

// writeGGUFHeader writes the GGUF file header (magic, version, counts).
func writeGGUFHeader(w io.Writer, version uint32, tensorCount, kvCount uint64) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(Magic)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, version); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, tensorCount); err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, kvCount)
}

// writeKVBin writes a single GGUF key-value pair.
func writeKVBin(w io.Writer, kv KV) error {
	if err := writeStringBin(w, kv.Key); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, kv.ValueType); err != nil {
		return err
	}
	return writeValueBin(w, kv.ValueType, kv.Value)
}

// writeTensorInfoBin writes a single tensor info entry.
func writeTensorInfoBin(w io.Writer, ti TensorInfo) error {
	if err := writeStringBin(w, ti.Name); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, ti.NDims); err != nil {
		return err
	}
	for _, d := range ti.Dimensions {
		if err := binary.Write(w, binary.LittleEndian, d); err != nil {
			return err
		}
	}
	if err := binary.Write(w, binary.LittleEndian, ti.Type); err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, ti.Offset)
}

// writeStringBin writes a GGUF string (uint64 length prefix + bytes).
func writeStringBin(w io.Writer, s string) error {
	if err := binary.Write(w, binary.LittleEndian, uint64(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

// writeValueBin writes a GGUF value of the given type.
func writeValueBin(w io.Writer, vtype uint32, value any) error {
	switch vtype {
	case TypeBool:
		var b uint8
		if value.(bool) {
			b = 1
		}
		return binary.Write(w, binary.LittleEndian, b)
	case TypeString:
		return writeStringBin(w, value.(string))
	case TypeArray:
		ta := value.(TypedArray)
		if err := binary.Write(w, binary.LittleEndian, ta.ElemType); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint64(len(ta.Values))); err != nil {
			return err
		}
		for _, v := range ta.Values {
			if err := writeValueBin(w, ta.ElemType, v); err != nil {
				return err
			}
		}
		return nil
	default:
		// Scalar numeric types: binary.Write handles uint8, int8, uint16, etc.
		return binary.Write(w, binary.LittleEndian, value)
	}
}
