// Package prompts manages benchmark prompt sets: built-in sets,
// custom prompt loading, and sizing via tokenization or byte-length.
package prompts

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed sets/*.txt
var builtinFS embed.FS

// BuiltinSetInfo describes a built-in prompt set.
type BuiltinSetInfo struct {
	Name        string
	Description string
	Count       int
}

// BuiltinSets returns information about all available built-in prompt sets.
func BuiltinSets() []BuiltinSetInfo {
	entries, _ := builtinFS.ReadDir("sets")
	var sets []BuiltinSetInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".txt")
		prompts, _ := LoadBuiltin(name)
		sets = append(sets, BuiltinSetInfo{
			Name:        name,
			Description: builtinDescription(name),
			Count:       len(prompts),
		})
	}
	sort.Slice(sets, func(i, j int) bool {
		return sets[i].Name < sets[j].Name
	})
	return sets
}

// LoadBuiltin loads a built-in prompt set by name.
func LoadBuiltin(name string) ([]string, error) {
	data, err := builtinFS.ReadFile(fmt.Sprintf("sets/%s.txt", name))
	if err != nil {
		return nil, fmt.Errorf("built-in prompt set %q not found", name)
	}
	return splitPrompts(string(data)), nil
}

// splitPrompts splits text by "---" separators, trimming whitespace.
func splitPrompts(text string) []string {
	parts := strings.Split(text, "\n---\n")
	var prompts []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			prompts = append(prompts, p)
		}
	}
	return prompts
}

func builtinDescription(name string) string {
	switch name {
	case "short":
		return "Short prompts (~30-80 tokens, 20 prompts)"
	case "medium":
		return "Medium prompts (~400-600 tokens, 15 prompts)"
	case "long":
		return "Long prompts (~1500-2500 tokens, 10 prompts)"
	case "code":
		return "Code-specific prompts (~200-800 tokens, 15 prompts)"
	case "reasoning":
		return "Multi-step reasoning prompts (~300-600 tokens, 10 prompts)"
	default:
		return ""
	}
}
