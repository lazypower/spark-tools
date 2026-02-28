// Package llmrun provides a unified Go API for managing llama.cpp inference.
//
// It wraps the sub-packages (engine, resolver, hardware, profiles) into a
// single Engine type that llm-bench and other consumers can import directly.
package llmrun

import (
	"context"
	"fmt"

	hfconfig "github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
	"github.com/lazypower/spark-tools/pkg/llmrun/profiles"
	"github.com/lazypower/spark-tools/pkg/llmrun/resolver"
)

// Re-export commonly used types so consumers only need to import llmrun.
type (
	RunConfig     = engine.RunConfig
	Capabilities  = engine.Capabilities
	Process       = engine.Process
	NumaStrategy  = engine.NumaStrategy
	HealthStatus  = engine.HealthStatus
	ProcessStats  = engine.ProcessStats
	HardwareInfo  = hardware.HardwareInfo
	GPUInfo       = hardware.GPUInfo
	Profile       = profiles.Profile
	ProfileStore  = profiles.ProfileStore
	ResolvedModel = resolver.ResolvedModel
)

// Re-export constants.
const (
	NumaDisabled   = engine.NumaDisabled
	NumaDistribute = engine.NumaDistribute
	NumaIsolate    = engine.NumaIsolate
)

// Option configures an Engine.
type Option func(*engineOptions)

type engineOptions struct {
	llamaDir string
	configDir string
	dataDir  string
	hfDataDir string
}

// WithLlamaDir sets the llama.cpp binary directory.
func WithLlamaDir(dir string) Option {
	return func(o *engineOptions) { o.llamaDir = dir }
}

// WithConfigDir sets the llm-run config directory.
func WithConfigDir(dir string) Option {
	return func(o *engineOptions) { o.configDir = dir }
}

// WithDataDir sets the llm-run data directory.
func WithDataDir(dir string) Option {
	return func(o *engineOptions) { o.dataDir = dir }
}

// WithHFDataDir sets the hfetch data directory for model resolution.
func WithHFDataDir(dir string) Option {
	return func(o *engineOptions) { o.hfDataDir = dir }
}

// Engine manages llama.cpp processes, model resolution, and hardware detection.
type Engine struct {
	caps     *Capabilities
	hw       *HardwareInfo
	resolver *resolver.Resolver
	profiles *ProfileStore
	dirs     config.DirConfig
	dataDir  string
}

// NewEngine creates a new Engine, detecting llama.cpp and hardware.
func NewEngine(opts ...Option) (*Engine, error) {
	o := &engineOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// Resolve directories.
	dirs := config.Dirs()
	if o.configDir != "" {
		dirs.Config = o.configDir
	}
	if o.dataDir != "" {
		dirs.Data = o.dataDir
	}

	hfDataDir := o.hfDataDir
	if hfDataDir == "" {
		hfDataDir = hfconfig.Dirs().Data
	}

	// Detect llama.cpp.
	llamaDir := o.llamaDir
	if llamaDir == "" {
		gcfg := config.LoadGlobalConfig()
		llamaDir = gcfg.LlamaDir
	}
	caps, err := engine.DetectBinaries(llamaDir)
	if err != nil {
		return nil, fmt.Errorf("llama.cpp not found: %w", err)
	}

	// Detect hardware.
	hw, _ := hardware.DetectHardware()

	return &Engine{
		caps:     caps,
		hw:       hw,
		resolver: resolver.NewResolver(dirs.Config, hfDataDir),
		profiles: profiles.NewProfileStore(dirs.Config),
		dirs:     dirs,
		dataDir:  dirs.Data,
	}, nil
}

// Launch starts an inference process with the given config.
func (e *Engine) Launch(ctx context.Context, cfg RunConfig) (*Process, error) {
	return engine.Launch(ctx, cfg, *e.caps, e.dataDir)
}

// ResolveModel resolves a model reference to a structured result.
func (e *Engine) ResolveModel(ctx context.Context, ref string) (*ResolvedModel, error) {
	return e.resolver.ResolveModel(ctx, ref)
}

// DetectCapabilities returns the detected llama.cpp capabilities.
func (e *Engine) DetectCapabilities() *Capabilities {
	return e.caps
}

// DetectHardware returns information about the current system.
func DetectHardware() (*HardwareInfo, error) {
	return hardware.DetectHardware()
}

// BuildCommand translates a RunConfig into a command line, gated by capabilities.
func BuildCommand(cfg RunConfig, caps Capabilities) (cmd []string, warnings []string, err error) {
	return engine.BuildCommand(cfg, caps)
}

// Recommend returns a RunConfig with smart defaults for the given hardware.
func Recommend(hw *HardwareInfo) RunConfig {
	return hardware.RecommendConfig(hw, nil)
}

// Hardware returns the detected hardware info, or nil if detection failed.
func (e *Engine) Hardware() *HardwareInfo {
	return e.hw
}

// Profiles returns the profile store.
func (e *Engine) Profiles() *ProfileStore {
	return e.profiles
}

// NewProfileStore creates a new profile store for the given config directory.
func NewProfileStore(configDir string) *ProfileStore {
	return profiles.NewProfileStore(configDir)
}
