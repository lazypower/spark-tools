package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// containerEngineFlagUsage documents that an unset engine is detected, mirroring
// how --accelerator behaves.
const containerEngineFlagUsage = "container engine the spec will be run with: docker or podman; " +
	"unset detects it (podman when present, else docker). Only affects how an AMD GPU is passed in"

// Container engine names this CLI accepts.
const (
	engineDocker = "docker"
	enginePodman = "podman"
)

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
		if e != engineDocker && e != enginePodman {
			if warn != nil {
				fmt.Fprintf(warn, "warning: unknown --container-engine %q; rendering for docker.\n", flag)
			}
			return engineDocker
		}
		return e
	}
	return detectContainerEngine()
}

// probeContainerEngine reports which engine this host actually uses.
//
// It decides on the PRESENCE of podman, and deliberately does not probe the
// docker daemon. Asking `docker info` looks like the more precise test and is
// actively harmful on a CoreOS-style host: docker.socket is socket-activated, so
// any docker command from a caller who can reach the socket STARTS a daemon the
// operator chose not to run. Worse, it is self-defeating -- having started
// dockerd, the probe then sees a reachable daemon and answers "docker", which
// renders the group form that yields a GPU-less container on exactly the host
// that needed podman. A detector must not change the machine it is inspecting.
//
// Presence is the right discriminator for the two real environments: the DGX
// Spark ships docker-ce and no podman, so this falls through to docker; a
// CoreOS-style Strix host ships podman (daemonless, enabled by default)
// alongside docker-cli, and podman is what is actually used.
//
// The tie-break when both exist favors podman because the two failure modes are
// not symmetric. Choosing podman for a docker user is LOUD -- the spec says
// `podman run` and carries podman-only flags, so it is wrong in the first
// second and --container-engine docker fixes it. Choosing docker for a podman
// user is SILENT: the container starts and runs the engine with no GPU. Prefer
// the mistake that announces itself.
func probeContainerEngine() string {
	if _, err := exec.LookPath("podman"); err == nil {
		return enginePodman
	}
	return engineDocker
}
