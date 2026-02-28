package profiles

import (
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
)

// BuiltinProfiles returns the set of built-in profiles that ship
// with llm-run. Users can override these by saving a profile with
// the same name.
func BuiltinProfiles() []Profile {
	return []Profile{
		{
			Name:        "default",
			Description: "General-purpose conversation",
			Config: engine.RunConfig{
				Temperature: 0.7,
				ContextSize: 0, // auto
			},
		},
		{
			Name:        "coding",
			Description: "Code generation",
			Config: engine.RunConfig{
				Temperature: 0.3,
				TopP:        0.9,
			},
		},
		{
			Name:        "creative",
			Description: "Creative writing",
			Config: engine.RunConfig{
				Temperature: 0.9,
				TopP:        0.95,
			},
		},
		{
			Name:        "precise",
			Description: "Factual/analytical",
			Config: engine.RunConfig{
				Temperature: 0.1,
				TopP:        0.8,
			},
		},
	}
}
