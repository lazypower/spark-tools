package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/hub"
	"github.com/lazypower/spark-tools/internal/serveartifact"
	"github.com/lazypower/spark-tools/internal/servecontract"
	"github.com/lazypower/spark-tools/internal/servespec"
	"github.com/lazypower/spark-tools/internal/serving"
	"github.com/lazypower/spark-tools/pkg/llmserve"
)

func emitCmd() *cobra.Command {
	var (
		modelDir    string
		name        string
		caps        []string
		ctx         int
		gpuMemUtil  float64
		maxNumSeqs  int
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
			if err := validateBudgetFlags(cmd); err != nil {
				return err
			}
			accelerator = resolveAccelerator(accelerator, cmd.ErrOrStderr())
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

			req := servecontract.Request{
				ServedName:   name,
				Capabilities: capList,
				ContextLen:   ctx,
				GPUMemUtil:   gpuMemUtil,
				MaxNumSeqs:   maxNumSeqs,
				Dtype:        dtype,
				Target:       llmserve.Fingerprint{Engine: image, Accelerator: accelerator},
			}
			host := servespec.Host{Image: imageRef(image), Port: port, Volumes: mountList}

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
	f.Float64Var(&gpuMemUtil, "gpu-mem-util", 0, "vLLM --gpu-memory-utilization fraction (0,1]; 0/unset defers to the hardware default, then vLLM's own (0.9). Set a cap to co-reside instances on one box")
	f.IntVar(&maxNumSeqs, "max-num-seqs", 0, "vLLM --max-num-seqs (max concurrent sequences); 0/unset defers to the hardware default, then vLLM's own. Lower it to shrink a co-resident member's KV footprint")
	f.StringVar(&dtype, "dtype", "", "vLLM --dtype (default auto)")
	f.StringVar(&image, "image", "", "engine image digest/tag, e.g. vllm/vllm-openai@v0.23.0 (required) — also the fingerprint engine")
	f.StringVar(&accelerator, "accelerator", "", acceleratorFlagUsage)
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
// hfetch completeness gate (serveartifact.Verify); without one it detects facts and
// warns that the artifact was not re-verified.
func resolveFacts(modelDir, repoTree string, stderr interface{ Write([]byte) (int, error) }) (serving.ArtifactFacts, error) {
	if repoTree != "" {
		files, err := loadRepoTree(repoTree)
		if err != nil {
			return serving.ArtifactFacts{}, err
		}
		return serveartifact.Verify(files, modelDir)
	}
	fmt.Fprintf(stderr, "warning: emitting without --repo-tree; trusting that %q was completeness-gated at hfetch pull time\n", modelDir)
	return serveartifact.DetectFacts(modelDir)
}

func loadRepoTree(path string) ([]hub.ModelFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading repo tree: %w", err)
	}
	var files []hub.ModelFile
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

func parseMounts(mounts []string) ([]servespec.Mount, error) {
	var out []servespec.Mount
	for _, m := range mounts {
		host, container, ok := strings.Cut(m, ":")
		if !ok || host == "" || container == "" {
			return nil, fmt.Errorf("invalid --mount %q, want host:container", m)
		}
		// Resolve the host path against the operator's cwd HERE, while it is still
		// authoritative. The emitted spec is stored and run from elsewhere (XDG
		// state), so a relative host path would otherwise resolve against the
		// wrong directory.
		absHost, err := filepath.Abs(host)
		if err != nil {
			return nil, fmt.Errorf("resolving --mount host %q: %w", host, err)
		}
		out = append(out, servespec.Mount{Host: absHost, Container: container})
	}
	return out, nil
}

// validateBudgetFlags rejects an explicitly-typed 0 for either budget lever. The
// resolver treats a zero value as "unset" (the omitted-flag sentinel), and that
// is the one case it structurally cannot tell apart from an operator typing 0 —
// only cobra's Changed() sees the difference. Without this guard, `--max-num-seqs
// 0` / `--gpu-mem-util 0` would silently fall through to a default instead of
// failing closed, which is both a fail-open and incoherent (an explicit -1 is
// rejected while an explicit 0 is not). Every other out-of-domain value
// (negative, > 1, NaN) is still caught by the resolver, the single range
// authority for all callers including the budgeter.
func validateBudgetFlags(cmd *cobra.Command) error {
	f := cmd.Flags()
	if f.Changed("gpu-mem-util") {
		if v, _ := f.GetFloat64("gpu-mem-util"); v == 0 {
			return fmt.Errorf("--gpu-mem-util 0 is not a fraction in (0, 1]; omit the flag to use the default")
		}
	}
	if f.Changed("max-num-seqs") {
		if v, _ := f.GetInt("max-num-seqs"); v == 0 {
			return fmt.Errorf("--max-num-seqs 0 is not a positive count; omit the flag to use the default")
		}
	}
	return nil
}

func parseTarget(target string) (servespec.Target, error) {
	t := servespec.Target(target)
	if slices.Contains(servespec.Targets(), t) {
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
