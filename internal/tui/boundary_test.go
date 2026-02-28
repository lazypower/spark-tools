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

func TestCheckBoundary_UserMarkerMidStream(t *testing.T) {
	input := "Sure, here is your answer.\n<|im_start|>user\nWhat about this?"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "Sure, here is your answer.\n" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

func TestCheckBoundary_SystemMarkerMidStream(t *testing.T) {
	input := "Response text<|im_start|>system\nYou are a helpful"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "Response text" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

func TestCheckBoundary_AssistantMarkerMidStream(t *testing.T) {
	input := "First answer\n<|im_start|>assistant\nSecond answer"
	clean, hit := checkBoundary(input)
	if !hit {
		t.Fatal("expected boundary hit")
	}
	if clean != "First answer\n" {
		t.Errorf("expected content before marker, got %q", clean)
	}
}

func TestStripChatTokens(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello world", "Hello world"},
		{"<|im_start|>assistant\nHello", "assistant\nHello"},
		{"Hello<|im_end|>", "Hello"},
		{"<|im_start|>Hello<|im_end|>", "Hello"},
	}
	for _, tt := range tests {
		got := stripChatTokens(tt.input)
		if got != tt.want {
			t.Errorf("stripChatTokens(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
