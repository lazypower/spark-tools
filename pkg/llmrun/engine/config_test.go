package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupFakeBinaries creates fake llama-server and llama-cli executables in a temp dir.
func setupFakeBinaries(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"llama-server", "llama-cli", "llama-bench"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("creating fake binary %s: %v", name, err)
		}
	}
	return dir
}

// fullCaps returns a Capabilities struct with all features enabled.
func fullCaps(binDir string) Capabilities {
	return Capabilities{
		Version:        "b1234",
		Backend:        "cuda",
		FlashAttention: true,
		NUMA:           true,
		MMap:           true,
		MLock:          true,
		ServerMode:     true,
		BenchMode:      true,
		BinaryDir:      binDir,
		BinaryPath:     filepath.Join(binDir, "llama-server"),
	}
}

// cpuOnlyCaps returns a Capabilities struct for a CPU-only build.
func cpuOnlyCaps(binDir string) Capabilities {
	return Capabilities{
		Version:    "b1234",
		Backend:    "cpu",
		MMap:       true,
		ServerMode: true,
		BenchMode:  false,
		BinaryDir:  binDir,
		BinaryPath: filepath.Join(binDir, "llama-server"),
	}
}

func TestBuildCommand_ServerMode(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		Host:       "0.0.0.0",
		Port:       9090,
		Threads:    8,
		GPULayers:  -1,
		ContextSize: 4096,
	}

	cmd, warnings, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	cmdStr := strings.Join(cmd, " ")

	// Should use llama-server binary.
	if !strings.Contains(cmd[0], "llama-server") {
		t.Errorf("expected llama-server binary, got %q", cmd[0])
	}

	// Check required flags.
	assertContainsFlag(t, cmdStr, "--model", "/models/test.gguf")
	assertContainsFlag(t, cmdStr, "--host", "0.0.0.0")
	assertContainsFlag(t, cmdStr, "--port", "9090")
	assertContainsFlag(t, cmdStr, "--threads", "8")
	assertContainsFlag(t, cmdStr, "--n-gpu-layers", "-1")
	assertContainsFlag(t, cmdStr, "--ctx-size", "4096")
}

func TestBuildCommand_CLIMode(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode: false,
		ModelPath:  "/models/test.gguf",
		Threads:    4,
		GPULayers:  32,
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use llama-cli binary.
	if !strings.Contains(cmd[0], "llama-cli") {
		t.Errorf("expected llama-cli binary, got %q", cmd[0])
	}
}

func TestBuildCommand_FlashAttentionDegradation(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := cpuOnlyCaps(binDir)

	cfg := RunConfig{
		ServerMode:     true,
		ModelPath:      "/models/test.gguf",
		FlashAttention: true,
	}

	cmd, warnings, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")

	// --flash-attn should NOT be in command.
	if strings.Contains(cmdStr, "--flash-attn") {
		t.Error("--flash-attn should be omitted when not supported")
	}

	// Should have a warning.
	if len(warnings) == 0 {
		t.Error("expected warning for unsupported flash attention")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "flash attention") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected flash attention warning, got: %v", warnings)
	}
}

func TestBuildCommand_MLockDegradation(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := cpuOnlyCaps(binDir)

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		MLock:      true,
	}

	cmd, warnings, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	if strings.Contains(cmdStr, "--mlock") {
		t.Error("--mlock should be omitted when not supported")
	}
	if len(warnings) == 0 {
		t.Error("expected warning for unsupported mlock")
	}
}

func TestBuildCommand_NUMADegradation(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := cpuOnlyCaps(binDir)

	cfg := RunConfig{
		ServerMode:   true,
		ModelPath:    "/models/test.gguf",
		NumaStrategy: NumaDistribute,
	}

	cmd, warnings, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	if strings.Contains(cmdStr, "--numa") {
		t.Error("--numa should be omitted when not supported")
	}
	if len(warnings) == 0 {
		t.Error("expected warning for unsupported NUMA")
	}
}

func TestBuildCommand_NUMAWithSupport(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:   true,
		ModelPath:    "/models/test.gguf",
		NumaStrategy: NumaDistribute,
	}

	cmd, warnings, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--numa", "distribute")
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestBuildCommand_HardError_GPUOnCPU(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := cpuOnlyCaps(binDir)

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		GPULayers:  -1, // All layers on GPU.
	}

	_, _, err := BuildCommand(cfg, caps)
	if err == nil {
		t.Fatal("expected error for GPU layers on CPU-only build")
	}
	if !strings.Contains(err.Error(), "GPU support") {
		t.Errorf("expected GPU support error, got: %v", err)
	}
}

func TestBuildCommand_HardError_ServerModeWithoutBinary(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)
	caps.ServerMode = false // No llama-server.

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
	}

	_, _, err := BuildCommand(cfg, caps)
	if err == nil {
		t.Fatal("expected error for server mode without llama-server")
	}
	if !strings.Contains(err.Error(), "llama-server not found") {
		t.Errorf("expected llama-server not found error, got: %v", err)
	}
}

func TestBuildCommand_ExtraArgs(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		ExtraArgs:  []string{"--verbose", "--log-prefix"},
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	if !strings.Contains(cmdStr, "--verbose") {
		t.Error("expected --verbose in extra args")
	}
	if !strings.Contains(cmdStr, "--log-prefix") {
		t.Error("expected --log-prefix in extra args")
	}
}

func TestBuildCommand_GenerationParams(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:    true,
		ModelPath:     "/models/test.gguf",
		Temperature:   0.3,
		TopP:          0.9,
		TopK:          40,
		RepeatPenalty: 1.1,
		Seed:          42,
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--temp", "0.3")
	assertContainsFlag(t, cmdStr, "--top-p", "0.9")
	assertContainsFlag(t, cmdStr, "--top-k", "40")
	assertContainsFlag(t, cmdStr, "--repeat-penalty", "1.1")
	assertContainsFlag(t, cmdStr, "--seed", "42")
}

func TestBuildCommand_BatchSizes(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		BatchSize:  512,
		UBatchSize: 128,
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--batch-size", "512")
	assertContainsFlag(t, cmdStr, "--ubatch-size", "128")
}

func TestBuildCommand_AllAdvancedFeaturesEnabled(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:     true,
		ModelPath:      "/models/test.gguf",
		FlashAttention: true,
		MMap:           true,
		MLock:          true,
		NumaStrategy:   NumaIsolate,
	}

	cmd, warnings, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	cmdStr := strings.Join(cmd, " ")
	if !strings.Contains(cmdStr, "--flash-attn") {
		t.Error("expected --flash-attn")
	}
	if !strings.Contains(cmdStr, "--mmap") {
		t.Error("expected --mmap")
	}
	if !strings.Contains(cmdStr, "--mlock") {
		t.Error("expected --mlock")
	}
	assertContainsFlag(t, cmdStr, "--numa", "isolate")
}

func TestBuildCommand_ZeroValuesOmitted(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		// All numeric fields left at zero defaults.
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	// Zero-value fields should not produce flags.
	for _, flag := range []string{"--threads", "--ctx-size", "--batch-size", "--temp", "--top-p", "--top-k", "--seed"} {
		if strings.Contains(cmdStr, flag) {
			t.Errorf("zero-value field should not produce flag %s, got: %s", flag, cmdStr)
		}
	}
}

func TestBuildCommand_ServerParallelAndAPIKey(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		Host:       "127.0.0.1",
		Port:       8080,
		Parallel:   4,
		APIKey:     "sk-test-key",
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--parallel", "4")
	assertContainsFlag(t, cmdStr, "--api-key", "sk-test-key")
}

func TestBuildCommand_CLIModeNoServerFlags(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode: false,
		ModelPath:  "/models/test.gguf",
		Host:       "127.0.0.1",
		Port:       8080,
		Parallel:   4,
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	// Server-specific flags should not be present in CLI mode.
	if strings.Contains(cmdStr, "--host") {
		t.Error("--host should not be present in CLI mode")
	}
	if strings.Contains(cmdStr, "--port") {
		t.Error("--port should not be present in CLI mode")
	}
	if strings.Contains(cmdStr, "--parallel") {
		t.Error("--parallel should not be present in CLI mode")
	}
}

func TestBuildCommand_MMapDegradation(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := cpuOnlyCaps(binDir)
	caps.MMap = false // Override: no mmap support.

	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		MMap:       true,
	}

	cmd, warnings, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	if strings.Contains(cmdStr, "--mmap") {
		t.Error("--mmap should be omitted when not supported")
	}
	if len(warnings) == 0 {
		t.Error("expected warning for unsupported mmap")
	}
}

func TestBuildCommand_SystemPrompt(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:   true,
		ModelPath:    "/models/test.gguf",
		SystemPrompt: "You are a helpful assistant.",
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--system-prompt", "You are a helpful assistant.")
}

func TestBuildCommand_GPULayersPositiveOnCPU(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := cpuOnlyCaps(binDir)

	// Positive GPU layers (not -1) on CPU build should still work -
	// llama.cpp will just ignore them or error itself.
	cfg := RunConfig{
		ServerMode: true,
		ModelPath:  "/models/test.gguf",
		GPULayers:  10,
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--n-gpu-layers", "10")
}

func TestBuildCommand_ReasoningBudgetZero(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:      true,
		ModelPath:       "/models/test.gguf",
		ReasoningBudget: 0, // Disable thinking.
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--reasoning-budget", "0")
}

func TestBuildCommand_ReasoningBudgetPositive(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:      true,
		ModelPath:       "/models/test.gguf",
		ReasoningBudget: 1024,
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--reasoning-budget", "1024")
}

func TestBuildCommand_ReasoningBudgetNegativeOmitted(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:      true,
		ModelPath:       "/models/test.gguf",
		ReasoningBudget: -1, // Default: omit flag entirely.
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	if strings.Contains(cmdStr, "--reasoning-budget") {
		t.Errorf("--reasoning-budget should be omitted when set to -1, got: %s", cmdStr)
	}
}

func TestBuildCommand_ChatTemplate(t *testing.T) {
	binDir := setupFakeBinaries(t)
	caps := fullCaps(binDir)

	cfg := RunConfig{
		ServerMode:   true,
		ModelPath:    "/models/test.gguf",
		ChatTemplate: "chatml",
	}

	cmd, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdStr := strings.Join(cmd, " ")
	assertContainsFlag(t, cmdStr, "--chat-template", "chatml")
}

// assertContainsFlag checks that a flag and its value appear in the command string.
func assertContainsFlag(t *testing.T, cmdStr, flag, value string) {
	t.Helper()
	expected := flag + " " + value
	if !strings.Contains(cmdStr, expected) {
		t.Errorf("expected %q in command, got: %s", expected, cmdStr)
	}
}
