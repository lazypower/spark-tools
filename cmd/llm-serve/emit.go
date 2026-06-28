package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/llmserve"
	"github.com/lazypower/spark-tools/pkg/llmserve/artifact"
	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
	"github.com/lazypower/spark-tools/pkg/llmserve/emit"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

func emitCmd() *cobra.Command {
	var (
		modelDir    string
		name        string
		caps        []string
		ctx         int
		dtype       string
		image       string
		accelerator string
		target      string
		port        int
		mounts      []string
		repoTree    string
	)

	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Resolve a serve request and emit a validated vLLM launch spec",
		Long: "Resolve {model dir + capabilities + hardware} into a validated vLLM launch spec.\n\n" +
			"The model directory must be an hfetch-verified artifact. Pass --repo-tree (a saved\n" +
			"hfetch tree listing) to re-run the completeness gate before emitting; otherwise the\n" +
			"artifact is trusted to have been gated at pull time.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			capList, err := parseCaps(caps)
			if err != nil {
				return err
			}
			mountList, err := parseMounts(mounts)
			if err != nil {
				return err
			}
			tgt, err := parseTarget(target)
			if err != nil {
				return err
			}

			facts, err := resolveFacts(modelDir, repoTree, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			req := contract.Request{
				ServedName:   name,
				Capabilities: capList,
				ContextLen:   ctx,
				Dtype:        dtype,
				Target:       llmserve.Fingerprint{Engine: image, Accelerator: accelerator},
			}
			host := emit.Host{Image: imageRef(image), Port: port, Volumes: mountList}

			res, err := llmserve.Emit(req, facts, tgt, host)
			if err != nil {
				return err
			}

			// Staleness warnings go to stderr (loud), the spec to stdout (pipeable).
			for _, w := range res.Resolved.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			fmt.Fprint(cmd.OutOrStdout(), res.Spec)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&modelDir, "model-dir", "", "path to the hfetch-verified model directory (required)")
	f.StringVar(&name, "name", "", "served model name / alias (required)")
	f.StringSliceVar(&caps, "cap", nil, "requested capability (repeatable): guided-decoding, thinking, tool-calling, vision")
	f.IntVar(&ctx, "ctx", 0, "max model length (tokens); 0 leaves it to the host default")
	f.StringVar(&dtype, "dtype", "", "vLLM --dtype (default auto)")
	f.StringVar(&image, "image", "", "engine image digest/tag, e.g. vllm/vllm-openai@v0.23.0 (required) — also the fingerprint engine")
	f.StringVar(&accelerator, "accelerator", "nvidia:gb10:sm121", "target accelerator fingerprint (vendor:arch)")
	f.StringVar(&target, "target", "compose", "render target: compose, docker-run, quadlet")
	f.IntVar(&port, "port", 8000, "host port to map to container :8000")
	f.StringArrayVar(&mounts, "mount", nil, "read-only model mount host:container (repeatable)")
	f.StringVar(&repoTree, "repo-tree", "", "path to a saved hfetch tree listing (JSON); enables the completeness gate before emit")
	_ = cmd.MarkFlagRequired("model-dir")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

// resolveFacts produces verified artifact facts. With a repo tree it runs the
// hfetch completeness gate (artifact.Verify); without one it detects facts and
// warns that the artifact was not re-verified.
func resolveFacts(modelDir, repoTree string, stderr interface{ Write([]byte) (int, error) }) (serving.ArtifactFacts, error) {
	if repoTree != "" {
		files, err := loadRepoTree(repoTree)
		if err != nil {
			return serving.ArtifactFacts{}, err
		}
		return artifact.Verify(files, modelDir)
	}
	fmt.Fprintf(stderr, "warning: emitting without --repo-tree; trusting that %q was completeness-gated at hfetch pull time\n", modelDir)
	return artifact.DetectFacts(modelDir)
}

func loadRepoTree(path string) ([]api.ModelFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading repo tree: %w", err)
	}
	var files []api.ModelFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("parsing repo tree JSON: %w", err)
	}
	return files, nil
}

func parseCaps(caps []string) ([]serving.Capability, error) {
	valid := map[string]serving.Capability{
		string(serving.GuidedDecoding): serving.GuidedDecoding,
		string(serving.Thinking):       serving.Thinking,
		string(serving.ToolCalling):    serving.ToolCalling,
		string(serving.Vision):         serving.Vision,
	}
	var out []serving.Capability
	for _, c := range caps {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		capability, ok := valid[c]
		if !ok {
			return nil, fmt.Errorf("unknown capability %q (have: guided-decoding, thinking, tool-calling, vision)", c)
		}
		out = append(out, capability)
	}
	return out, nil
}

func parseMounts(mounts []string) ([]emit.Mount, error) {
	var out []emit.Mount
	for _, m := range mounts {
		host, container, ok := strings.Cut(m, ":")
		if !ok || host == "" || container == "" {
			return nil, fmt.Errorf("invalid --mount %q, want host:container", m)
		}
		out = append(out, emit.Mount{Host: host, Container: container})
	}
	return out, nil
}

func parseTarget(target string) (emit.Target, error) {
	t := emit.Target(target)
	if slices.Contains(emit.Targets(), t) {
		return t, nil
	}
	return "", fmt.Errorf("unknown target %q (have: compose, docker-run, quadlet)", target)
}

// imageRef converts a fingerprint-style engine ref (image@tag) into a runnable
// image reference for the container runtime. A real content digest (image@sha256:…)
// is already a valid ref and is left as-is; a tag suffix (image@v0.23.0) becomes
// image:v0.23.0. Plain refs are untouched.
func imageRef(image string) string {
	i := strings.LastIndex(image, "@")
	if i < 0 {
		return image
	}
	suffix := image[i+1:]
	if strings.Contains(suffix, ":") { // a real digest like sha256:… — valid as @digest
		return image
	}
	return image[:i] + ":" + suffix
}
