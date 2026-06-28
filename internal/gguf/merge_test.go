package gguf

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// testTensor describes a tensor for building test GGUF files.
type testTensor struct {
	Name string
	Dims []uint64
	Type uint32
	Data []byte
}

// buildTestGGUFFile creates a complete valid GGUF file with KVs, tensor infos,
// and tensor data. Returns the raw bytes.
func buildTestGGUFFile(kvPairs map[string]any, tensors []testTensor) []byte {
	var buf bytes.Buffer

	// Header.
	binary.Write(&buf, binary.LittleEndian, uint32(Magic))
	binary.Write(&buf, binary.LittleEndian, uint32(Version3))
	binary.Write(&buf, binary.LittleEndian, uint64(len(tensors)))
	binary.Write(&buf, binary.LittleEndian, uint64(len(kvPairs)))

	// KV pairs.
	for key, value := range kvPairs {
		writeString(&buf, key)
		writeTypedValue(&buf, value)
	}

	// Tensor info entries.
	// First, calculate where data section starts so we can set offsets.
	// We need to know the size of tensor infos to calculate data offset.
	var infoBuf bytes.Buffer
	alignment := uint64(32)
	dataOffset := uint64(0)
	for _, t := range tensors {
		// name string
		binary.Write(&infoBuf, binary.LittleEndian, uint64(len(t.Name)))
		infoBuf.WriteString(t.Name)
		// ndims
		binary.Write(&infoBuf, binary.LittleEndian, uint32(len(t.Dims)))
		// dims
		for _, d := range t.Dims {
			binary.Write(&infoBuf, binary.LittleEndian, d)
		}
		// type
		binary.Write(&infoBuf, binary.LittleEndian, t.Type)
		// offset placeholder (will be filled later)
		binary.Write(&infoBuf, binary.LittleEndian, uint64(0))
	}

	// Calculate data section start.
	headerSize := uint64(buf.Len()) + uint64(infoBuf.Len())
	dataStart := align(headerSize, alignment)

	// Now rewrite tensor infos with correct offsets.
	var infoBuf2 bytes.Buffer
	runningOffset := uint64(0)
	for _, t := range tensors {
		binary.Write(&infoBuf2, binary.LittleEndian, uint64(len(t.Name)))
		infoBuf2.WriteString(t.Name)
		binary.Write(&infoBuf2, binary.LittleEndian, uint32(len(t.Dims)))
		for _, d := range t.Dims {
			binary.Write(&infoBuf2, binary.LittleEndian, d)
		}
		binary.Write(&infoBuf2, binary.LittleEndian, t.Type)
		binary.Write(&infoBuf2, binary.LittleEndian, runningOffset)
		runningOffset = align(runningOffset+uint64(len(t.Data)), alignment)
	}

	buf.Write(infoBuf2.Bytes())

	// Pad to alignment.
	dataOffset = align(uint64(buf.Len()), alignment)
	if dataOffset > uint64(buf.Len()) {
		buf.Write(make([]byte, dataOffset-uint64(buf.Len())))
	}

	// Write tensor data.
	for i, t := range tensors {
		buf.Write(t.Data)
		// Pad to alignment (skip for last tensor).
		if i < len(tensors)-1 {
			padded := align(uint64(len(t.Data)), alignment)
			if padded > uint64(len(t.Data)) {
				buf.Write(make([]byte, padded-uint64(len(t.Data))))
			}
		}
	}

	_ = dataStart
	return buf.Bytes()
}

func align(offset, alignment uint64) uint64 {
	return (offset + alignment - 1) &^ (alignment - 1)
}

func TestParseShard_BasicHeader(t *testing.T) {
	tensorData := make([]byte, 64) // 16 float32s
	for i := range 16 {
		binary.LittleEndian.PutUint32(tensorData[i*4:], uint32(i+1))
	}

	data := buildTestGGUFFile(
		map[string]any{
			"general.architecture": "llama",
			"general.file_type":    uint32(15),
		},
		[]testTensor{
			{Name: "weight.0", Dims: []uint64{4, 4}, Type: 0, Data: tensorData},
		},
	)

	hdr, err := ParseShard(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ParseShard: %v", err)
	}

	if hdr.TensorCount != 1 {
		t.Errorf("tensor count: expected 1, got %d", hdr.TensorCount)
	}
	if len(hdr.KVs) != 2 {
		t.Errorf("kv count: expected 2, got %d", len(hdr.KVs))
	}
	if len(hdr.Tensors) != 1 {
		t.Errorf("tensor info count: expected 1, got %d", len(hdr.Tensors))
	}
	if hdr.Tensors[0].Name != "weight.0" {
		t.Errorf("tensor name: expected weight.0, got %q", hdr.Tensors[0].Name)
	}
	if hdr.Tensors[0].NDims != 2 {
		t.Errorf("tensor ndims: expected 2, got %d", hdr.Tensors[0].NDims)
	}
	if hdr.Alignment != 32 {
		t.Errorf("alignment: expected 32, got %d", hdr.Alignment)
	}
	if hdr.DataOffset == 0 {
		t.Error("data offset should be non-zero")
	}
}

func TestMergeShards_TwoShards(t *testing.T) {
	dir := t.TempDir()

	// Build two shard files with split metadata.
	tensor1Data := bytes.Repeat([]byte{0xAA}, 64)
	tensor2Data := bytes.Repeat([]byte{0xBB}, 128)

	shard0 := buildTestGGUFFile(
		map[string]any{
			"general.architecture": "llama",
			"general.file_type":    uint32(15),
			"split.no":             uint16(0),
			"split.count":          uint16(2),
			"split.tensors.count":  uint32(2),
		},
		[]testTensor{
			{Name: "layer.0.weight", Dims: []uint64{16, 4}, Type: 0, Data: tensor1Data},
		},
	)

	shard1 := buildTestGGUFFile(
		map[string]any{
			"split.no":            uint16(1),
			"split.count":        uint16(2),
			"split.tensors.count": uint32(2),
		},
		[]testTensor{
			{Name: "layer.1.weight", Dims: []uint64{32, 4}, Type: 0, Data: tensor2Data},
		},
	)

	shard0Path := filepath.Join(dir, "model-Q4_K_M-00001-of-00002.gguf")
	shard1Path := filepath.Join(dir, "model-Q4_K_M-00002-of-00002.gguf")
	mergedPath := filepath.Join(dir, "merged.gguf")

	if err := os.WriteFile(shard0Path, shard0, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shard1Path, shard1, 0644); err != nil {
		t.Fatal(err)
	}

	// Merge.
	if err := MergeShards([]string{shard0Path, shard1Path}, mergedPath); err != nil {
		t.Fatalf("MergeShards: %v", err)
	}

	// Verify merged file is valid GGUF.
	f, err := os.Open(mergedPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	hdr, err := ParseShard(f)
	if err != nil {
		t.Fatalf("ParseShard on merged: %v", err)
	}

	// Should have 2 tensors total.
	if hdr.TensorCount != 2 {
		t.Errorf("merged tensor count: expected 2, got %d", hdr.TensorCount)
	}
	if len(hdr.Tensors) != 2 {
		t.Errorf("merged tensor infos: expected 2, got %d", len(hdr.Tensors))
	}

	// Should have KVs from shard 0 minus split.* keys.
	for _, kv := range hdr.KVs {
		if kv.Key == "split.no" || kv.Key == "split.count" || kv.Key == "split.tensors.count" {
			t.Errorf("split key %q should have been stripped", kv.Key)
		}
	}

	// Verify architecture KV survived.
	foundArch := false
	for _, kv := range hdr.KVs {
		if kv.Key == "general.architecture" && kv.Value == "llama" {
			foundArch = true
		}
	}
	if !foundArch {
		t.Error("general.architecture KV not found in merged file")
	}

	// Verify tensor names.
	names := make(map[string]bool)
	for _, ti := range hdr.Tensors {
		names[ti.Name] = true
	}
	if !names["layer.0.weight"] || !names["layer.1.weight"] {
		t.Errorf("expected both tensor names, got: %v", names)
	}

	// Verify tensor data is readable at the right offsets.
	for _, ti := range hdr.Tensors {
		buf := make([]byte, 4) // read first 4 bytes
		if _, err := f.ReadAt(buf, int64(hdr.DataOffset+ti.Offset)); err != nil {
			t.Errorf("reading tensor %q data: %v", ti.Name, err)
			continue
		}
		switch ti.Name {
		case "layer.0.weight":
			if buf[0] != 0xAA {
				t.Errorf("tensor layer.0.weight: expected 0xAA, got 0x%02X", buf[0])
			}
		case "layer.1.weight":
			if buf[0] != 0xBB {
				t.Errorf("tensor layer.1.weight: expected 0xBB, got 0x%02X", buf[0])
			}
		}
	}
}

func TestMergeShards_NeedsTwoShards(t *testing.T) {
	err := MergeShards([]string{"/tmp/single.gguf"}, "/tmp/out.gguf")
	if err == nil {
		t.Fatal("expected error for single shard")
	}
}

func TestWriteAndParseRoundTrip(t *testing.T) {
	// Build a GGUF, parse it with ParseShard, write it back, parse again.
	tensorData := bytes.Repeat([]byte{0xCC}, 32)
	original := buildTestGGUFFile(
		map[string]any{
			"general.architecture": "qwen2",
			"test.uint32_val":      uint32(42),
			"test.string_val":      "hello",
		},
		[]testTensor{
			{Name: "embed", Dims: []uint64{8, 4}, Type: 0, Data: tensorData},
		},
	)

	hdr, err := ParseShard(bytes.NewReader(original))
	if err != nil {
		t.Fatalf("ParseShard: %v", err)
	}

	// Write it back using our writer.
	var buf bytes.Buffer
	if err := writeGGUFHeader(&buf, hdr.Version, hdr.TensorCount, uint64(len(hdr.KVs))); err != nil {
		t.Fatal(err)
	}
	for _, kv := range hdr.KVs {
		if err := writeKVBin(&buf, kv); err != nil {
			t.Fatalf("writeKVBin %q: %v", kv.Key, err)
		}
	}
	for _, ti := range hdr.Tensors {
		if err := writeTensorInfoBin(&buf, ti); err != nil {
			t.Fatal(err)
		}
	}

	// Pad to alignment.
	padded := align(uint64(buf.Len()), hdr.Alignment)
	if padded > uint64(buf.Len()) {
		buf.Write(make([]byte, padded-uint64(buf.Len())))
	}

	// Copy tensor data from original.
	buf.Write(original[hdr.DataOffset:])

	// Parse the round-tripped version.
	hdr2, err := ParseShard(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ParseShard round-trip: %v", err)
	}

	if hdr2.TensorCount != hdr.TensorCount {
		t.Errorf("tensor count: %d != %d", hdr2.TensorCount, hdr.TensorCount)
	}
	if len(hdr2.KVs) != len(hdr.KVs) {
		t.Errorf("kv count: %d != %d", len(hdr2.KVs), len(hdr.KVs))
	}
	if hdr2.Tensors[0].Name != "embed" {
		t.Errorf("tensor name: expected embed, got %q", hdr2.Tensors[0].Name)
	}

	// Verify KV values round-tripped.
	kvMap := make(map[string]KV)
	for _, kv := range hdr2.KVs {
		kvMap[kv.Key] = kv
	}
	if kv, ok := kvMap["general.architecture"]; !ok || kv.Value != "qwen2" {
		t.Errorf("architecture KV: expected qwen2, got %v", kvMap["general.architecture"])
	}
	if kv, ok := kvMap["test.uint32_val"]; !ok || kv.Value != uint32(42) {
		t.Errorf("uint32 KV: expected 42, got %v", kvMap["test.uint32_val"])
	}
	if kv, ok := kvMap["test.string_val"]; !ok || kv.Value != "hello" {
		t.Errorf("string KV: expected hello, got %v", kvMap["test.string_val"])
	}
}
