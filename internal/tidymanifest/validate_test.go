package tidymanifest

import (
	"strings"
	"testing"
)

func TestValidateAcceptsGood(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Ollama:  []OllamaModelSpec{{Name: "qwen2.5-coder:32b"}, {Name: "llama3.3"}},
		GGUF: []GGUFModelSpec{
			{Repo: "unsloth/Qwen3.5-122B-A10B-GGUF", Quant: "Q4_K_M"},
			{Repo: "unsloth/Qwen3.5-122B-A10B-GGUF", Quant: "Q5_K_M"},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsNil(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("expected error for nil manifest")
	}
}

func TestValidateRejectsBadVersion(t *testing.T) {
	if err := Validate(&Manifest{Version: 0}); err == nil {
		t.Fatal("expected error for version 0")
	}
	if err := Validate(&Manifest{Version: 99}); err == nil {
		t.Fatal("expected error for version 99")
	}
}

func TestValidateRejectsEmptyOllamaName(t *testing.T) {
	m := &Manifest{Version: 1, Ollama: []OllamaModelSpec{{Name: "   "}}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "ollama[0]") {
		t.Fatalf("expected ollama[0] error, got %v", err)
	}
}

func TestValidateRejectsEmptyGGUFRepo(t *testing.T) {
	m := &Manifest{Version: 1, GGUF: []GGUFModelSpec{{Repo: ""}}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "gguf[0]") {
		t.Fatalf("expected gguf[0] error, got %v", err)
	}
}

func TestValidateRejectsDuplicateOllamaAfterNormalization(t *testing.T) {
	m := &Manifest{Version: 1, Ollama: []OllamaModelSpec{
		{Name: "llama3.3"},
		{Name: "llama3.3:latest"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestValidateRejectsDuplicateGGUF(t *testing.T) {
	m := &Manifest{Version: 1, GGUF: []GGUFModelSpec{
		{Repo: "Org/Repo", Quant: "Q4_K_M"},
		{Repo: "org/repo", Quant: "Q4_K_M"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}
