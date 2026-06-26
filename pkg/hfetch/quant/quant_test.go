package quant

import "testing"

func TestParse_ModelOptNVFP4_WithKVCacheFP8(t *testing.T) {
	hq := []byte(`{"producer":{"name":"modelopt"},"quantization":{"quant_algo":"NVFP4","kv_cache_quant_algo":"FP8"}}`)
	got := Parse([]byte(`{}`), hq, nil)
	if got == nil || got.Method != "modelopt" || got.Algo != "NVFP4" || !got.KVCacheFP8 {
		t.Fatalf("expected modelopt NVFP4 + KV FP8, got %+v", got)
	}
	if got.String() != "modelopt NVFP4, KV-cache FP8" {
		t.Errorf("summary: %q", got.String())
	}
}

func TestParse_ModelOptFP8_NoKVCache(t *testing.T) {
	hq := []byte(`{"quantization":{"quant_algo":"FP8"}}`)
	got := Parse(nil, hq, nil)
	if got == nil || got.Algo != "FP8" || got.KVCacheFP8 {
		t.Fatalf("expected FP8 without KV-cache FP8, got %+v", got)
	}
}

func TestParse_GPTQ_FromSidecar(t *testing.T) {
	gc := []byte(`{"bits":4,"group_size":128}`)
	got := Parse([]byte(`{}`), nil, gc)
	if got == nil || got.Method != "gptq" || got.Algo != "GPTQ-Int4" {
		t.Fatalf("expected gptq GPTQ-Int4, got %+v", got)
	}
}

func TestParse_CompressedTensors_FromConfig(t *testing.T) {
	cfg := []byte(`{"architectures":["X"],"quantization_config":{"quant_method":"compressed-tensors","config_groups":{"group_0":{"weights":{"num_bits":4,"type":"int"}}}}}`)
	got := Parse(cfg, nil, nil)
	if got == nil || got.Method != "compressed-tensors" || got.Algo != "W4A16" {
		t.Fatalf("expected compressed-tensors W4A16, got %+v", got)
	}
}

func TestParse_CompressedTensorsFP8_KVCache(t *testing.T) {
	cfg := []byte(`{"quantization_config":{"quant_method":"compressed-tensors","config_groups":{"group_0":{"weights":{"num_bits":8,"type":"float"}}},"kv_cache_scheme":{"num_bits":8,"type":"float"}}}`)
	got := Parse(cfg, nil, nil)
	if got == nil || got.Algo != "FP8" || !got.KVCacheFP8 {
		t.Fatalf("expected FP8 weights + KV-cache FP8, got %+v", got)
	}
}

func TestParse_InPlaceGPTQ_FromConfig(t *testing.T) {
	cfg := []byte(`{"quantization_config":{"quant_method":"gptq","bits":4}}`)
	got := Parse(cfg, nil, nil)
	if got == nil || got.Method != "gptq" || got.Algo != "W4A16" {
		t.Fatalf("expected gptq W4A16, got %+v", got)
	}
}

func TestParse_Unquantized_ReturnsNil(t *testing.T) {
	if got := Parse([]byte(`{"architectures":["LlamaForCausalLM"]}`), nil, nil); got != nil {
		t.Fatalf("expected nil for unquantized model, got %+v", got)
	}
	if got := Parse(nil, nil, nil); got != nil {
		t.Fatalf("expected nil for no input, got %+v", got)
	}
}

func TestInfo_String_NilAndEmpty(t *testing.T) {
	var nilInfo *Info
	if nilInfo.String() != "none" {
		t.Errorf("nil Info should be 'none', got %q", nilInfo.String())
	}
}
