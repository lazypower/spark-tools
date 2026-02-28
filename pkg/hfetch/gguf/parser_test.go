package gguf

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildTestGGUF creates a minimal valid GGUF v3 binary header for testing.
func buildTestGGUF(kvPairs map[string]any) []byte {
	var buf bytes.Buffer

	// Magic + version + tensor count + KV count
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(Version3))
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensor count
	binary.Write(&buf, binary.LittleEndian, uint64(len(kvPairs)))

	for key, value := range kvPairs {
		writeString(&buf, key)
		switch v := value.(type) {
		case string:
			binary.Write(&buf, binary.LittleEndian, TypeString)
			writeString(&buf, v)
		case uint32:
			binary.Write(&buf, binary.LittleEndian, TypeUint32)
			binary.Write(&buf, binary.LittleEndian, v)
		case int32:
			binary.Write(&buf, binary.LittleEndian, TypeInt32)
			binary.Write(&buf, binary.LittleEndian, v)
		case uint64:
			binary.Write(&buf, binary.LittleEndian, TypeUint64)
			binary.Write(&buf, binary.LittleEndian, v)
		case bool:
			binary.Write(&buf, binary.LittleEndian, TypeBool)
			if v {
				binary.Write(&buf, binary.LittleEndian, uint8(1))
			} else {
				binary.Write(&buf, binary.LittleEndian, uint8(0))
			}
		}
	}

	return buf.Bytes()
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
