// Package liveness is llm-serve's runtime-liveness authority (B2): it answers "is
// this model artifact protected from eviction?" for other tools (the future
// llm-tidy interlock) — fail-closed, and WITHOUT a persisted ledger, daemon, or
// heartbeat. Liveness is derived LIVE, the same way B1 derives "serving": an
// artifact is protected if it is used by a running llm-serve-managed container OR
// named by an existing desired manifest (intent-to-serve protects the weights).
// Any doubt — docker unreachable, a managed container we can't map, a read error
// — resolves to PROTECTED. The cardinal sin is a false "evictable" (deleting the
// weights of a live model), so the design never infers evictable from absence.
package liveness

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
	"github.com/lazypower/spark-tools/pkg/llmserve/runtime"
)

// Liveness derives eviction protection from the manifest store (intent) and the
// runtime (live containers).
type Liveness struct {
	Store   *instance.Store
	Runtime runtime.Runtime
}

// New builds a Liveness over a manifest store and a runtime.
func New(store *instance.Store, rt runtime.Runtime) *Liveness {
	return &Liveness{Store: store, Runtime: rt}
}

// Report is the protected-artifact set for one query. AllProtected ⇒ the query
// could not be fully evaluated (fail-closed) and EVERY artifact must be treated
// as protected; Protected is the explicit set otherwise. Reason explains a
// fail-closed verdict. Unmanaged lists running containers that are NOT
// llm-serve-managed but bind-mount host dirs — a consumer (llm-tidy) should
// COMPLAIN about these: an unlabeled container is using the filesystem and we
// cannot reason precisely about what it serves, so pruning under its mounts is
// blocked. Label or migrate it.
type Report struct {
	Protected    map[string]bool
	AllProtected bool
	Reason       string
	Unmanaged    []UnmanagedMount
}

// UnmanagedMount is a running non-llm-serve container with bind mounts: the
// coexistence signal (run.sh / Ollama / hand-launched) a consumer complains about.
type UnmanagedMount struct {
	Container string
	Mounts    []string // canonical host paths
}

// canonicalize reduces a host path to the single key both this authority and a
// consumer compare on: resolve symlinks, absolutize, clean. resolved is false
// when EvalSymlinks fails (the path does not exist / is unreadable) — in which
// case the key is only a best-effort abs+clean of the INPUT, which can desync
// from a consumer that holds the resolved target. A recorded path that does not
// resolve is therefore a DOUBT and must fail closed, not silently mis-key.
func canonicalize(path string) (key string, resolved bool) {
	if path == "" {
		return "", false
	}
	p := path
	resolved = true
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	} else {
		resolved = false
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p), resolved
}

// Canonical is the best-effort comparison key (resolves symlinks when possible).
// Exposed for diagnostics; the protected-set logic uses canonicalize's resolved
// flag to fail closed on an unresolvable recorded path.
func Canonical(path string) string {
	k, _ := canonicalize(path)
	return k
}

// Protected computes the protected-artifact set: the union of every desired
// manifest's model dir (intent) and every RUNNING managed container's
// artifact-host-path label (live use). It is fail-closed: any error, or a running
// managed container whose artifact cannot be determined, yields AllProtected.
func (l *Liveness) Protected(ctx context.Context) Report {
	r := Report{Protected: map[string]bool{}}

	// Intent half: every manifest's model dir protects its weights, until the
	// operator down/forgets it. The store is local and authoritative; a read error
	// is fail-closed.
	manifests, err := l.Store.List()
	if err != nil {
		return Report{AllProtected: true, Reason: "manifest read error: " + err.Error()}
	}
	for i := range manifests {
		dir := manifests[i].Desired.ModelDir
		if dir == "" {
			continue
		}
		if !l.addRoot(&r, dir) {
			return r // unresolvable recorded path → fail closed
		}
	}

	// Live half: REALITY-based. Every RUNNING container protects what it uses:
	//   - a MANAGED (llm-serve) container protects its PRECISE artifact (the
	//     host-path label), so B3 can still prune OTHER unused models on this host.
	//   - a FOREIGN container (run.sh, Ollama, hand-launched — no llm-serve labels)
	//     protects every host dir it BIND-MOUNTS, since we can't know which subdir
	//     it serves. Broad but safe (the coexistence guard).
	containers, err := l.Runtime.ListRunning(ctx)
	if err != nil {
		return Report{AllProtected: true, Reason: "runtime unreachable: " + err.Error()}
	}
	for _, c := range containers {
		if !c.Running {
			continue // ListRunning should pre-filter, but never protect on a dead container
		}
		if c.Labels[lifecycle.LabelManagedBy] == lifecycle.ManagedByValue {
			// Labeled (managed) → trust the label: protect the PRECISE artifact, so
			// B3 can still prune other unused models. (Operator decision: a label
			// applied is reasonable to trust.)
			host := c.Labels[lifecycle.LabelArtifactHostPath]
			if host == "" {
				return Report{AllProtected: true,
					Reason: "a running managed container has no " + lifecycle.LabelArtifactHostPath + " label; cannot determine its artifact"}
			}
			if !l.addRoot(&r, host) {
				return r
			}
			continue
		}
		// Unlabeled container WITH bind mounts → we can't know what it serves, so
		// protect everything under its mounts AND record it so a consumer
		// complains. (Operator decision: unlabeled + a mount ⇒ no model under it is
		// safe to remove.) An unlabeled container with no bind mounts is irrelevant.
		if len(c.Mounts) == 0 {
			continue
		}
		um := UnmanagedMount{Container: c.Name}
		for _, src := range c.Mounts {
			key, ok := canonicalize(src)
			if !ok {
				return Report{AllProtected: true,
					Reason: "an unlabeled running container's mount does not resolve (" + src + ")"}
			}
			r.Protected[key] = true
			um.Mounts = append(um.Mounts, key)
		}
		r.Unmanaged = append(r.Unmanaged, um)
	}
	return r
}

// addRoot canonicalizes a protected host path into the report; an unresolvable
// path makes the report fail-closed (AllProtected) and returns false.
func (l *Liveness) addRoot(r *Report, path string) bool {
	key, ok := canonicalize(path)
	if !ok {
		*r = Report{AllProtected: true, Reason: "a protected artifact path does not resolve (" + path + ")"}
		return false
	}
	if r.Protected == nil {
		r.Protected = map[string]bool{}
	}
	r.Protected[key] = true
	return true
}

// IsProtected reports whether a host artifact path must not be evicted. It is the
// API a consumer (B3's tidy interlock) calls; it canonicalizes the argument the
// same way as the protected set, and fails closed (an empty/unevaluable path, or
// an AllProtected report, returns true).
func (l *Liveness) IsProtected(ctx context.Context, artifactPath string) bool {
	r := l.Protected(ctx)
	if r.AllProtected {
		return true
	}
	key, ok := canonicalize(artifactPath)
	if artifactPath == "" || !ok {
		return true // can't evaluate the candidate → protect (fail closed)
	}
	// Protected if the candidate OVERLAPS any protected root: deleting it would
	// affect a protected path, or a protected path lives under it. Exact match,
	// candidate-under-root (artifact inside a foreign mount), and root-under-
	// candidate (evicting a parent of a served artifact) all count.
	for root := range r.Protected {
		if pathsOverlap(key, root) {
			return true
		}
	}
	return false
}

// pathsOverlap reports whether two canonical paths are equal or one is an
// ancestor of the other.
func pathsOverlap(a, b string) bool {
	return a == b || isUnder(a, b) || isUnder(b, a)
}

// isUnder reports whether p is a strict descendant of ancestor.
func isUnder(p, ancestor string) bool {
	rel, err := filepath.Rel(ancestor, p)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ProtectedArtifacts returns the sorted canonical keys of protected artifacts, and
// allProtected=true when the query is fail-closed (treat everything as protected).
func (l *Liveness) ProtectedArtifacts(ctx context.Context) (keys []string, allProtected bool) {
	r := l.Protected(ctx)
	if r.AllProtected {
		return nil, true
	}
	keys = make([]string, 0, len(r.Protected))
	for k := range r.Protected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, false
}

// InstanceLiveness is the per-instance view for the CLI.
type InstanceLiveness struct {
	Name        string
	Running     bool
	HasManifest bool
	Protected   bool
	Reason      string
}

// Instance reports liveness for one named instance: whether it has a desired
// manifest, whether a managed container for it is running, and the resulting
// protected verdict (protected if either holds, fail-closed on a runtime error).
func (l *Liveness) Instance(ctx context.Context, name string) (InstanceLiveness, error) {
	out := InstanceLiveness{Name: name}
	_, err := l.Store.Load(name)
	switch {
	case err == nil:
		out.HasManifest = true
	case errors.Is(err, instance.ErrNotFound):
		// no manifest — may still have a running orphan container
	default:
		return out, err
	}

	// The set-level report is the authority: if it is fail-closed (AllProtected),
	// THIS instance is protected too, regardless of its own running/manifest state
	// — otherwise the per-instance verdict could contradict the authority.
	report := l.Protected(ctx)

	containers, err := l.Runtime.ListRunning(ctx)
	if err != nil {
		out.Protected = true // fail-closed
		out.Reason = "runtime unreachable: " + err.Error()
		return out, nil
	}
	for _, c := range containers {
		if c.Labels[lifecycle.LabelManagedBy] == lifecycle.ManagedByValue && c.Labels[lifecycle.LabelInstance] == name {
			out.Running = true
			break
		}
	}
	out.Protected = out.HasManifest || out.Running || report.AllProtected
	out.Reason = instanceReason(out, report)
	return out, nil
}

func instanceReason(l InstanceLiveness, report Report) string {
	switch {
	case l.Running && l.HasManifest:
		return "running and desired"
	case l.Running:
		return "running (no manifest — orphan stack)"
	case l.HasManifest:
		return "desired (manifest present; not currently running)"
	case report.AllProtected:
		return "fail-closed: " + report.Reason
	default:
		return "not protected"
	}
}
