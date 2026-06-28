package fileset

import (
	"slices"
	"sort"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

func file(name string) api.ModelFile { return api.ModelFile{Type: "file", Filename: name} }

func TestSelectVLLM_IncludesServeReadySet(t *testing.T) {
	files := []api.ModelFile{
		file("model-00001-of-00002.safetensors"),
		file("model-00002-of-00002.safetensors"),
		file("model.safetensors.index.json"),
		file("mtp.safetensors"), // extra weight not in index — must be kept
		file("config.json"),
		file("generation_config.json"),
		file("hf_quant_config.json"),
		file("tokenizer.json"),
		file("tokenizer_config.json"),
		file("tekken.json"),
		file("merges.txt"),
		file("tokenizer.model"),
		file("chat_template.jinja"),
		file("modeling_nemotron_h.py"),
		file("super_v3_reasoning_parser.py"), // parser plugin, not in auto_map
		file("preprocessor_config.json"),
		file("CHAT_SYSTEM_PROMPT.txt"),
	}
	got := names(SelectVLLM(files))
	for _, want := range []string{
		"model-00001-of-00002.safetensors", "mtp.safetensors",
		"model.safetensors.index.json", "config.json", "hf_quant_config.json",
		"tokenizer.json", "tekken.json", "merges.txt", "tokenizer.model",
		"chat_template.jinja", "modeling_nemotron_h.py",
		"super_v3_reasoning_parser.py", "preprocessor_config.json",
		"CHAT_SYSTEM_PROMPT.txt",
	} {
		if !contains(got, want) {
			t.Errorf("expected %q to be selected; got %v", want, got)
		}
	}
}

func TestSelectVLLM_ExcludesJunk(t *testing.T) {
	files := []api.ModelFile{
		file("model.safetensors"),
		file("config.json"),
		file("README.md"),
		file("bias.md"),
		file(".gitattributes"),
		file("recipe.yaml"),
		file("accuracy_chart.png"),
		file("evaluation.json"),
		file("preds.json"),
		file(".quant_summary.txt"),
	}
	got := names(SelectVLLM(files))
	for _, junk := range []string{
		"README.md", "bias.md", ".gitattributes", "recipe.yaml",
		"accuracy_chart.png", "evaluation.json", "preds.json", ".quant_summary.txt",
	} {
		if contains(got, junk) {
			t.Errorf("junk file %q should have been excluded; got %v", junk, got)
		}
	}
	if !contains(got, "model.safetensors") || !contains(got, "config.json") {
		t.Errorf("real files dropped: %v", got)
	}
}

func names(files []api.ModelFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Filename
	}
	sort.Strings(out)
	return out
}

func contains(s []string, v string) bool { return slices.Contains(s, v) }
