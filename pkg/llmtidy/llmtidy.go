// Package llmtidy is the top-level facade for declarative model inventory
// management. Sub-packages handle the manifest, the per-backend inventory,
// the diff/plan/apply engine, and the Ollama REST client.
package llmtidy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lazypower/spark-tools/pkg/hfetch"
	hfconfig "github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
	"github.com/lazypower/spark-tools/pkg/llmtidy/interlock"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/manifest"
	"github.com/lazypower/spark-tools/pkg/llmtidy/ollama"
	"github.com/lazypower/spark-tools/pkg/llmtidy/reconcile"
)

// Re-exports keep callers from importing every sub-package.
type (
	Manifest        = manifest.Manifest
	OllamaModelSpec = manifest.OllamaModelSpec
	GGUFModelSpec   = manifest.GGUFModelSpec
	InstalledModel  = inventory.InstalledModel
	Backend         = inventory.ModelBackend
	DiffResult      = reconcile.DiffResult
	ModelSpec       = reconcile.ModelSpec
)

const (
	BackendOllama = inventory.BackendOllama
	BackendGGUF   = inventory.BackendGGUF
)

// ErrManifestNotFound is re-exported so callers can detect the
// "run llm-tidy init" remediation without importing manifest.
var ErrManifestNotFound = manifest.ErrNotFound

// Option configures Tidy at construction.
type Option func(*config)

type config struct {
	manifestPath string
	ollamaHost   string
	hfetchClient *hfetch.Client
	checker      interlock.Checker
}

// WithManifestPath sets an explicit manifest path, overriding env/XDG.
func WithManifestPath(path string) Option {
	return func(c *config) { c.manifestPath = path }
}

// WithOllamaHost sets an explicit Ollama base URL.
func WithOllamaHost(host string) Option {
	return func(c *config) { c.ollamaHost = host }
}

// WithHfetchClient injects an hfetch client; used in tests to avoid network.
// WithChecker overrides the eviction-interlock liveness checker (default: shell
// out to `llm-serve liveness`). Tests inject a fake; a consumer that runs no
// llm-serve can disable it with a checker returning interlock.ErrLLMServeAbsent.
func WithChecker(c interlock.Checker) Option {
	return func(cfg *config) { cfg.checker = c }
}

func WithHfetchClient(client *hfetch.Client) Option {
	return func(c *config) { c.hfetchClient = client }
}

// Tidy is the entry point for the library.
type Tidy struct {
	manifestPath string
	provider     *inventory.Provider
	hfetch       *hfetch.Client
	checker      interlock.Checker
}

// New builds a Tidy configured per the options.
func New(opts ...Option) (*Tidy, error) {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}

	path, err := manifest.Resolve(cfg.manifestPath)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest path: %w", err)
	}

	var oc *ollama.Client
	if cfg.ollamaHost != "" {
		oc = ollama.New(cfg.ollamaHost)
	} else {
		oc = ollama.NewFromEnv()
	}

	dirs := hfconfig.Dirs()
	reg := registry.New(dirs.Data)

	checker := cfg.checker
	if checker == nil {
		checker = interlock.LLMServeChecker("") // default: shell out to llm-serve
	}

	return &Tidy{
		manifestPath: path,
		provider:     &inventory.Provider{Ollama: oc, GGUF: reg, VLLM: reg},
		hfetch:       cfg.hfetchClient,
		checker:      checker,
	}, nil
}

// ManifestPath returns the resolved manifest path used by this Tidy.
func (t *Tidy) ManifestPath() string { return t.manifestPath }

// Provider exposes the inventory provider for callers that need to call
// per-backend operations directly (e.g. the status command).
func (t *Tidy) Provider() *inventory.Provider { return t.provider }

// LoadManifest reads and validates the manifest from disk.
func (t *Tidy) LoadManifest() (*Manifest, error) {
	m, err := manifest.Load(t.manifestPath)
	if err != nil {
		return nil, err
	}
	if err := manifest.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// SaveManifest writes the manifest to disk after validating it.
func (t *Tidy) SaveManifest(m *Manifest) error {
	if err := manifest.Validate(m); err != nil {
		return err
	}
	return manifest.Save(m, t.manifestPath)
}

// Inventory returns the unified installed-model list across both backends.
func (t *Tidy) Inventory(ctx context.Context) ([]InstalledModel, error) {
	return t.provider.All(ctx)
}

// Diff loads the manifest and compares it against the live inventory.
func (t *Tidy) Diff(ctx context.Context) (*DiffResult, error) {
	m, err := t.LoadManifest()
	if err != nil {
		return nil, err
	}
	inv, err := t.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	d := reconcile.Diff(m, inv)
	return &d, nil
}

// Prune removes untracked models. When filter is non-nil it is called for
// each candidate; returning true removes the candidate. Returns the
// removed models, total bytes reclaimed, and any per-model errors joined.
func (t *Tidy) Prune(
	ctx context.Context,
	filter func(InstalledModel) bool,
) ([]InstalledModel, int64, error) {
	d, err := t.Diff(ctx)
	if err != nil {
		return nil, 0, err
	}
	plan := d.Untracked
	if filter != nil {
		filtered := make([]InstalledModel, 0, len(plan))
		for _, m := range plan {
			if filter(m) {
				filtered = append(filtered, m)
			}
		}
		plan = filtered
	}
	// Eviction interlock (B3): the gate lives HERE, at the deletion authority, so
	// EVERY library prune is protected — not just the CLI. Protected (in-use)
	// models are dropped from the plan; fail-closed if liveness can't be reached.
	plan = interlock.Apply(ctx, plan, t.checker).Keep
	return reconcile.Prune(ctx, t.provider, plan, nil)
}

// Sync pulls every missing manifest spec via the default syncer. The slice
// returned names the specs that were successfully pulled.
func (t *Tidy) Sync(ctx context.Context) ([]InstalledModel, error) {
	d, err := t.Diff(ctx)
	if err != nil {
		return nil, err
	}
	syncer, err := t.defaultSyncer()
	if err != nil {
		return nil, err
	}
	var pulled []InstalledModel
	err = reconcile.Sync(ctx, syncer, d.Missing, func(e reconcile.SyncEvent) {
		if e.Err != nil || e.Status != "ok" {
			return
		}
		pulled = append(pulled, InstalledModel{
			Name:    e.Spec.Name(),
			Backend: e.Spec.Backend,
		})
	})
	return pulled, err
}

// Promote adds a model to the manifest and saves.
func (t *Tidy) Promote(ctx context.Context, model string, backend Backend) error {
	m, err := t.LoadOrInit()
	if err != nil {
		return err
	}
	inv, err := t.Inventory(ctx)
	if err != nil {
		return err
	}

	match, err := findInstalled(inv, model, backend)
	if err != nil {
		return err
	}

	switch match.Backend {
	case inventory.BackendOllama:
		spec := manifest.OllamaModelSpec{Name: match.OllamaName}
		for _, existing := range m.Ollama {
			if existing.NormalizedName() == spec.NormalizedName() {
				return fmt.Errorf("model %q already in manifest", spec.NormalizedName())
			}
		}
		m.Ollama = append(m.Ollama, spec)
	case inventory.BackendGGUF:
		spec := manifest.GGUFModelSpec{Repo: match.Repo, Quant: match.Quant}
		for _, existing := range m.GGUF {
			if strings.EqualFold(existing.Repo, spec.Repo) && existing.Quant == spec.Quant {
				return fmt.Errorf("gguf %s %s already in manifest", spec.Repo, spec.Quant)
			}
		}
		m.GGUF = append(m.GGUF, spec)
	case inventory.BackendVLLM:
		spec := manifest.VLLMModelSpec{Repo: match.Repo}
		for _, existing := range m.VLLM {
			if strings.EqualFold(existing.Repo, spec.Repo) {
				return fmt.Errorf("vllm %s already in manifest", spec.Repo)
			}
		}
		m.VLLM = append(m.VLLM, spec)
	}

	return t.SaveManifest(m)
}

// Demote removes a model from the manifest and saves.
func (t *Tidy) Demote(_ context.Context, model string) error {
	m, err := t.LoadManifest()
	if err != nil {
		return err
	}
	normalized := manifest.NormalizeOllamaName(model)

	for i, spec := range m.Ollama {
		if spec.NormalizedName() == normalized || spec.Name == model {
			m.Ollama = append(m.Ollama[:i], m.Ollama[i+1:]...)
			return t.SaveManifest(m)
		}
	}

	repoPart, quantPart := splitRepoQuant(model)
	for i, spec := range m.GGUF {
		if !strings.EqualFold(spec.Repo, repoPart) {
			continue
		}
		if quantPart != "" && spec.Quant != quantPart {
			continue
		}
		m.GGUF = append(m.GGUF[:i], m.GGUF[i+1:]...)
		return t.SaveManifest(m)
	}
	for i, spec := range m.VLLM {
		if strings.EqualFold(spec.Repo, model) || strings.EqualFold(spec.Repo, repoPart) {
			m.VLLM = append(m.VLLM[:i], m.VLLM[i+1:]...)
			return t.SaveManifest(m)
		}
	}

	suggestions := nearestMatches(m, model)
	if len(suggestions) == 0 {
		return fmt.Errorf("model %q not in manifest", model)
	}
	return fmt.Errorf("model %q not in manifest; did you mean: %s", model, strings.Join(suggestions, ", "))
}

// Init creates a manifest from the current inventory and writes it to disk.
func (t *Tidy) Init(ctx context.Context) (*Manifest, error) {
	inv, err := t.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	m := &Manifest{Version: manifest.SchemaVersion}
	seenOllama := make(map[string]bool)
	seenGGUF := make(map[string]bool)
	seenVLLM := make(map[string]bool)
	for _, im := range inv {
		switch im.Backend {
		case inventory.BackendOllama:
			key := manifest.NormalizeOllamaName(im.OllamaName)
			if seenOllama[key] {
				continue
			}
			seenOllama[key] = true
			m.Ollama = append(m.Ollama, manifest.OllamaModelSpec{Name: im.OllamaName})
		case inventory.BackendGGUF:
			key := strings.ToLower(im.Repo) + "|" + im.Quant
			if seenGGUF[key] {
				continue
			}
			seenGGUF[key] = true
			m.GGUF = append(m.GGUF, manifest.GGUFModelSpec{Repo: im.Repo, Quant: im.Quant})
		case inventory.BackendVLLM:
			key := strings.ToLower(im.Repo)
			if seenVLLM[key] {
				continue
			}
			seenVLLM[key] = true
			m.VLLM = append(m.VLLM, manifest.VLLMModelSpec{Repo: im.Repo})
		}
	}
	if err := t.SaveManifest(m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadOrInit returns the on-disk manifest, or an empty one when the file
// is missing. Used by Promote so the first promotion bootstraps the file.
func (t *Tidy) LoadOrInit() (*Manifest, error) {
	m, err := t.LoadManifest()
	if errors.Is(err, manifest.ErrNotFound) {
		return &Manifest{Version: manifest.SchemaVersion}, nil
	}
	return m, err
}

func (t *Tidy) defaultSyncer() (reconcile.Syncer, error) {
	if t.hfetch == nil {
		client, err := hfetch.NewClient()
		if err != nil {
			return nil, fmt.Errorf("init hfetch client: %w", err)
		}
		t.hfetch = client
	}
	return &defaultSyncer{
		ollama: t.provider.Ollama,
		hfetch: t.hfetch,
	}, nil
}

type defaultSyncer struct {
	ollama *ollama.Client
	hfetch *hfetch.Client
}

func (s *defaultSyncer) PullOllama(ctx context.Context, name string, onStatus func(string)) error {
	if s.ollama == nil {
		return errors.New("ollama backend not configured")
	}
	return s.ollama.Pull(ctx, name, func(p ollama.PullProgress) {
		if onStatus != nil {
			onStatus(p.Status)
		}
	})
}

// PullGGUF picks the file matching repo+quant on the HuggingFace side and
// downloads it via hfetch. If quant is empty the call returns an error
// surfacing the v0.1 limitation; the spec's "interactive file selection"
// path is deferred to a follow-up.
func (s *defaultSyncer) PullGGUF(ctx context.Context, repo, quant string, onStatus func(string)) error {
	if s.hfetch == nil {
		return errors.New("hfetch client not initialized")
	}
	if quant == "" {
		return fmt.Errorf("cannot sync %s: GGUF spec needs an explicit quant; download manually with `hfetch pull %s`", repo, repo)
	}
	files, err := s.hfetch.ListFiles(ctx, repo)
	if err != nil {
		return fmt.Errorf("list files for %s: %w", repo, err)
	}
	var match string
	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f.Filename), ".gguf") {
			continue
		}
		if gguf.ParseQuantFromFilename(f.Filename) == quant {
			match = f.Filename
			break
		}
	}
	if match == "" {
		return fmt.Errorf("no .gguf file in %s matches quant %s", repo, quant)
	}
	if onStatus != nil {
		onStatus("downloading " + match)
	}
	_, err = s.hfetch.Pull(ctx, repo, match, hfetch.PullOptions{})
	return err
}

func findInstalled(inv []InstalledModel, model string, backend Backend) (InstalledModel, error) {
	repo, quant := splitRepoQuant(model)
	normalizedOllama := manifest.NormalizeOllamaName(model)

	var candidates []InstalledModel
	for _, im := range inv {
		if backend != inventory.BackendUnknown && im.Backend != backend {
			continue
		}
		switch im.Backend {
		case inventory.BackendOllama:
			if manifest.NormalizeOllamaName(im.OllamaName) == normalizedOllama {
				candidates = append(candidates, im)
			}
		case inventory.BackendGGUF:
			if !strings.EqualFold(im.Repo, repo) {
				continue
			}
			if quant != "" && im.Quant != quant {
				continue
			}
			candidates = append(candidates, im)
		case inventory.BackendVLLM:
			if strings.EqualFold(im.Repo, repo) || strings.EqualFold(im.Repo, model) {
				candidates = append(candidates, im)
			}
		}
	}

	switch len(candidates) {
	case 0:
		return InstalledModel{}, fmt.Errorf("model %q not found in any backend", model)
	case 1:
		return candidates[0], nil
	}

	// Multiple candidates: if backend was specified, accept the first; else ambiguous.
	if backend != inventory.BackendUnknown {
		return candidates[0], nil
	}
	return InstalledModel{}, fmt.Errorf("model %q is ambiguous across backends; pass --backend", model)
}

// splitRepoQuant splits "Org/Repo Q4_K_M" into ("Org/Repo", "Q4_K_M") or
// returns the whole string as repo when there is no quant suffix.
func splitRepoQuant(s string) (string, string) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, " "); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

func nearestMatches(m *Manifest, model string) []string {
	lower := strings.ToLower(model)
	var hits []string
	for _, spec := range m.Ollama {
		if strings.Contains(strings.ToLower(spec.Name), lower) {
			hits = append(hits, spec.NormalizedName())
		}
	}
	for _, spec := range m.GGUF {
		if strings.Contains(strings.ToLower(spec.Repo), lower) {
			if spec.Quant != "" {
				hits = append(hits, spec.Repo+" "+spec.Quant)
			} else {
				hits = append(hits, spec.Repo)
			}
		}
	}
	if len(hits) > 5 {
		hits = hits[:5]
	}
	return hits
}
