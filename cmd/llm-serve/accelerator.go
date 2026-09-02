package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
)

// acceleratorFlagUsage documents that an unset --accelerator is detected rather
// than assumed, and names the assumption made when detection finds nothing.
const acceleratorFlagUsage = "target accelerator fingerprint (vendor:arch:compute); " +
	"unset detects the local accelerator, falling back to " + hardware.FallbackAccelerator

// detectAccelerator is a seam so the fallback path is testable on a box that
// does have an accelerator.
var detectAccelerator = hardware.DetectAccelerator

// resolveAccelerator decides which accelerator identity an artifact is stamped
// with: an explicit flag first, then the detected local accelerator, and only
// then the historical default.
//
// The stamp is the anchor of the anti-fossil staleness check -- a contract's
// flags are trustworthy only while the target environment matches the one they
// were authored against -- so a hardcoded default is worse than no default. It
// does not fail; it silently asserts the box is a GB10, and every drift check
// downstream then compares against an identity the machine never had. Detecting
// it makes the check mean what it says on a non-NVIDIA box.
//
// The fallback is announced rather than applied quietly, because that is
// precisely the case where the stamp may be fiction.
func resolveAccelerator(flag string, warn io.Writer) string {
	if a := strings.TrimSpace(flag); a != "" {
		return a
	}
	if a := detectAccelerator(); a != "" {
		return a
	}
	if warn != nil {
		fmt.Fprintf(warn, "warning: no accelerator detected; stamping %s. Pass --accelerator to set it explicitly.\n",
			hardware.FallbackAccelerator)
	}
	return hardware.FallbackAccelerator
}
