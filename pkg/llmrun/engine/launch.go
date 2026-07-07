package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Launch starts a llama.cpp process with the given configuration.
//
// It manages PID files in dataDir to prevent double-launch:
//   - If a PID file exists and the process is alive, returns an error.
//   - If a PID file exists but the process is dead, cleans up the stale file.
//
// The process's stderr is captured to a log file in dataDir/logs/.
func Launch(ctx context.Context, cfg RunConfig, caps Capabilities, dataDir string) (*Process, error) {
	// Build the command line.
	args, _, err := BuildCommand(cfg, caps)
	if err != nil {
		return nil, fmt.Errorf("building command: %w", err)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command after building")
	}

	// Ensure data directory exists.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	// Key the PID file by port. A single global server.pid made every launch
	// (chat/serve/run/bench) mutually exclusive even on different ports; keying
	// by port means only a genuine same-port conflict is refused, and the
	// "already running" error then names the port that is actually in use.
	pidFile := pidFilePath(dataDir, cfg)

	// Check for (and clean up) a stale PID file; error if a live server holds it.
	if err := checkPIDFile(pidFile, cfg); err != nil {
		return nil, err
	}

	// Atomically RESERVE the PID slot before spawning. Without this, two
	// concurrent same-port launches both pass checkPIDFile and both spawn; the
	// loser then fails to bind the port, and its teardown removes the winner's
	// PID file, orphaning it. O_EXCL makes the loser fail here, before spawning.
	pf, err := os.OpenFile(pidFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("server already starting on port %d", serverPort(cfg))
		}
		return nil, fmt.Errorf("reserving PID file: %w", err)
	}
	// Until the child PID is written and ownership passes to the running
	// process, remove the reservation on any early return.
	pidReserved := true
	defer func() {
		if pidReserved {
			pf.Close()
			os.Remove(pidFile)
		}
	}()

	// Ensure log directory exists.
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	// Open log file for stderr capture.
	logFile := filepath.Join(logDir, fmt.Sprintf("llama-%d.log", time.Now().Unix()))
	stderrFile, err := os.Create(logFile)
	if err != nil {
		return nil, fmt.Errorf("creating log file: %w", err)
	}

	// Spawn the process.
	cmdCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)
	cmd.Stderr = stderrFile
	cmd.Stdout = stderrFile // Capture stdout too for diagnostics.

	if err := cmd.Start(); err != nil {
		cancel()
		stderrFile.Close()
		return nil, fmt.Errorf("starting llama.cpp: %w", err)
	}

	// Record the child PID into the reserved file and hand ownership to the
	// running process (cleanup is now the caller's job via Stop).
	if _, err := pf.WriteString(strconv.Itoa(cmd.Process.Pid)); err != nil {
		cmd.Process.Kill()
		cancel()
		stderrFile.Close()
		return nil, fmt.Errorf("writing PID file: %w", err)
	}
	pf.Close()
	pidReserved = false

	// Determine endpoint for server mode.
	endpoint := ""
	if cfg.ServerMode {
		host := cfg.Host
		if host == "" {
			host = "127.0.0.1"
		}
		endpoint = fmt.Sprintf("http://%s:%d", host, serverPort(cfg))
	}

	handle := &processHandle{
		pid:    cmd.Process.Pid,
		cmd:    cmd,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	// Background reaper: immediately wait on the child so it never becomes
	// a zombie. The exit status is captured for later retrieval via Wait/Stop.
	go func() {
		handle.waitErr = cmd.Wait()
		close(handle.done)
	}()

	proc := &Process{
		Cmd:       handle,
		Config:    cfg,
		Caps:      caps,
		Endpoint:  endpoint,
		PIDFile:   pidFile,
		LogFile:   logFile,
		StartedAt: time.Now(),
	}

	return proc, nil
}

// checkPIDFile inspects an existing PID file. If the process is alive, returns
// an error. If the process is dead, cleans up the stale PID file.
func checkPIDFile(pidFile string, cfg RunConfig) error {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No PID file, proceed.
		}
		return fmt.Errorf("reading PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		// Corrupt PID file, clean up.
		os.Remove(pidFile)
		return nil
	}

	// Check if the process is alive.
	proc, err := os.FindProcess(pid)
	if err != nil {
		// Process not found, clean up stale PID file.
		os.Remove(pidFile)
		return nil
	}

	// On Unix, FindProcess always succeeds. Use signal 0 to check liveness.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process is dead, clean up stale PID file.
		os.Remove(pidFile)
		return nil
	}

	return fmt.Errorf("server already running (PID %d) on port %d", pid, serverPort(cfg))
}

// defaultServerPort is llama-server's port when the config leaves it unset.
const defaultServerPort = 8080

// serverPort resolves the effective server port — the single authority for the
// default, shared by the endpoint, the PID file name, and conflict messages.
func serverPort(cfg RunConfig) int {
	if cfg.Port == 0 {
		return defaultServerPort
	}
	return cfg.Port
}

// pidFilePath returns the per-port PID file path in dataDir. It assumes a
// server (port-bound) launch — the only kind Launch is used for today. A
// hypothetical non-server launch (ServerMode=false) would resolve to the
// default port's slot; add a distinct name here before introducing one.
func pidFilePath(dataDir string, cfg RunConfig) string {
	return filepath.Join(dataDir, fmt.Sprintf("server-%d.pid", serverPort(cfg)))
}

// Wait blocks until the process exits.
func (p *Process) Wait() error {
	if p.Cmd == nil || p.Cmd.done == nil {
		return fmt.Errorf("process not started")
	}
	<-p.Cmd.done
	p.cleanup()
	return p.Cmd.waitErr
}

// Done returns a channel that is closed when the process exits.
// Callers can select on this to detect unexpected crashes.
func (p *Process) Done() <-chan struct{} {
	if p.Cmd == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return p.Cmd.done
}

// Err returns the process exit error, or nil if still running.
// Only valid after Done() is closed.
func (p *Process) Err() error {
	return p.Cmd.waitErr
}

// CrashLog reads the last portion of the server log file. Returns an empty
// string if the log file is missing or unreadable. Useful for surfacing
// why a process exited unexpectedly.
func (p *Process) CrashLog(maxBytes int64) string {
	if p.LogFile == "" {
		return ""
	}
	f, err := os.Open(p.LogFile)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	offset := int64(0)
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
	}
	buf := make([]byte, min(info.Size(), maxBytes))
	n, err := f.ReadAt(buf, offset)
	if n == 0 {
		return ""
	}
	return string(buf[:n])
}

// Stop gracefully shuts down the process with SIGTERM, then SIGKILL after a timeout.
func (p *Process) Stop() error {
	if p.Cmd == nil {
		return fmt.Errorf("process not started")
	}

	// Already exited — just clean up.
	select {
	case <-p.Cmd.done:
		p.cleanup()
		return nil
	default:
	}

	proc, err := os.FindProcess(p.Cmd.pid)
	if err != nil {
		return fmt.Errorf("finding process: %w", err)
	}

	// Send SIGTERM for graceful shutdown.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Process may already be dead.
		p.cleanup()
		return nil
	}

	// Wait up to 10 seconds for graceful exit.
	select {
	case <-p.Cmd.done:
		p.cleanup()
		return nil
	case <-time.After(10 * time.Second):
		// Force kill.
		if err := proc.Signal(syscall.SIGKILL); err != nil {
			p.cleanup()
			return nil
		}
		<-p.Cmd.done
		p.cleanup()
		return nil
	}
}

// cleanup removes the PID file.
func (p *Process) cleanup() {
	if p.PIDFile != "" {
		os.Remove(p.PIDFile)
	}
	if p.Cmd.cancel != nil {
		p.Cmd.cancel()
	}
}

// Health queries the /health endpoint of a running llama-server.
func (p *Process) Health() (*HealthStatus, error) {
	if p.Endpoint == "" {
		return nil, fmt.Errorf("process is not running in server mode")
	}

	healthURL := p.Endpoint + "/health"
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(healthURL)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("parsing health response: %w", err)
	}

	return &status, nil
}
