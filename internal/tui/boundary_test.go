package tui

import "testing"

func TestCheckBoundary_NoMarker(t *testing.T) {
	clean, hit := checkBoundary("Hello, how can I help you today?")
	if hit {
		t.Error("expected no boundary hit")
	}
	if clean != "Hello, how can I help you today?" {
		t.Errorf("expected unchanged content, got %q", clean)
	}
}

// --- ChatML family ---

func TestCheckBoundary_ChatML_User(t *testing.T) {
	input := "Sure, here is your answer.\n<|im_start|>user\nWhat about this?"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "Sure, here is your answer.\n" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

func TestCheckBoundary_ChatML_System(t *testing.T) {
	input := "Response text<|im_start|>system\nYou are a helpful"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "Response text" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

func TestCheckBoundary_ChatML_Assistant(t *testing.T) {
	input := "First answer\n<|im_start|>assistant\nSecond answer"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "First answer\n" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

// --- Llama 3 family ---

func TestCheckBoundary_Llama3_User(t *testing.T) {
	input := "Here you go.\n<|start_header_id|>user<|end_header_id|>\nThanks"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "Here you go.\n" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

func TestCheckBoundary_Llama3_Assistant(t *testing.T) {
	input := "Done.\n<|start_header_id|>assistant<|end_header_id|>\nSure"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "Done.\n" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

// --- Alpaca / text-style templates ---

func TestCheckBoundary_Alpaca_Instruction(t *testing.T) {
	input := "The answer is 42.\n### Instruction:\nWhat is the meaning?"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "The answer is 42." {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

func TestCheckBoundary_Alpaca_Human(t *testing.T) {
	input := "Sure thing.\n### Human:\nTell me more"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "Sure thing." {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

// --- Strip tokens ---

func TestStripChatTokens(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello world", "Hello world"},
		{"<|im_start|>assistant\nHello", "assistant\nHello"},
		{"Hello<|im_end|>", "Hello"},
		{"<|im_start|>Hello<|im_end|>", "Hello"},
		// Llama 3
		{"<|start_header_id|>assistant<|end_header_id|>\nHi", "assistant\nHi"},
		{"Done<|eot_id|>", "Done"},
	}
	for _, tt := range tests {
		got := stripChatTokens(tt.input)
		if got != tt.want {
			t.Errorf("stripChatTokens(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
