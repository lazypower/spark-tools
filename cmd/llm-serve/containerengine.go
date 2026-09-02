package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// containerEngineFlagUsage documents that an unset engine is detected, mirroring
// how --accelerator behaves.
const containerEngineFlagUsage = "container engine the spec will be run with: docker or podman; " +
	"unset detects it (podman when no reachable docker daemon). Only affects how an AMD GPU is passed in"

// detectContainerEngine is a seam so the choice is testable without a host.
var detectContainerEngine = probeContainerEngine

// resolveContainerEngine picks the engine a spec is rendered for: an explicit
// flag first, then detection.
//
// This matters only on AMD, where the two engines need different GPU-access
// flags and the wrong one fails silently. Detecting it rather than defaulting
// keeps the common case correct without the operator having to know that the
// difference exists.
func resolveContainerEngine(flag string, warn io.Writer) string {
	if e := strings.ToLower(strings.TrimSpace(flag)); e != "" {
		if e != "docker" && e != "podman" {
			if warn != nil {
				fmt.Fprintf(warn, "warning: unknown --container-engine %q; rendering for docker.\n", flag)
			}
			return "docker"
		}
		return e
	}
	return detectContainerEngine()
}

// probeContainerEngine reports which engine is actually usable here.
//
// Presence alone cannot decide it, because both real target environments have
// docker on PATH:
//
//   - DGX Spark (GB10) ships docker-ce with a running daemon and no podman at
//     all, so docker is both available and correct.
//   - A CoreOS-style Strix host ships docker-cli alongside podman, but runs no
//     docker daemon — podman is daemonless and enabled by default, so it is the
//     engine actually in use.
//
// What separates them is the daemon, so a short `docker info` is the honest
// test: it succeeds on the GB10 and fails fast on the daemonless box, which is
// precisely the machine where podman is the right answer.
//
// Falls back to docker when neither probe resolves, preserving the historical
// default rather than inventing one.
func probeContainerEngine() string {
	if dockerDaemonReachable() {
		return "docker"
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}
	return "docker"
}

func dockerDaemonReachable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	// Bounded: a spec render must not hang on an unreachable daemon.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}
