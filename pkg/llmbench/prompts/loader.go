package prompts

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAMLPromptFile represents a YAML prompt file format.
type YAMLPromptFile struct {
	Prompts []YAMLPrompt `yaml:"prompts"`
}

// YAMLPrompt represents a single prompt in YAML format.
type YAMLPrompt struct {
	Text           string `yaml:"text"`
	ExpectedTokens int    `yaml:"expected_tokens"`
}

// LoadFile loads prompts from a file. It supports two formats:
//   - Text format: prompts separated by "---" on its own line
//   - YAML format: structured prompts with optional expected_tokens
func LoadFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading prompt file: %w", err)
	}
	content := string(data)

	// Try YAML format first if it looks like YAML
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		return parseYAMLPrompts(data)
	}

	// Text format: split by ---
	prompts := splitPrompts(content)
	if len(prompts) == 0 {
		return nil, fmt.Errorf("prompt file %q contains no prompts", path)
	}
	return prompts, nil
}

func parseYAMLPrompts(data []byte) ([]string, error) {
	var f YAMLPromptFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing YAML prompts: %w", err)
	}
	if len(f.Prompts) == 0 {
		return nil, fmt.Errorf("YAML prompt file contains no prompts")
	}
	prompts := make([]string, len(f.Prompts))
	for i, p := range f.Prompts {
		prompts[i] = strings.TrimSpace(p.Text)
	}
	return prompts, nil
}
