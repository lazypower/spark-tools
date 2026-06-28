package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmserve"
	"github.com/lazypower/spark-tools/pkg/llmserve/liveness"
)

func livenessCmd() *cobra.Command {
	var protectedOnly, check bool
	cmd := &cobra.Command{
		Use:   "liveness [name]",
		Short: "Report which model artifacts are protected from eviction (derived live)",
		Long: "Answer 'is this artifact protected from eviction?' — the authority llm-tidy will\n" +
			"consult before pruning. Derived live from running managed containers + desired\n" +
			"manifests; fail-closed (docker unreachable / unmappable ⇒ protected). No daemon.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, _, _ := dirs()
			lv := llmserve.NewLiveness(stateDir)
			out := cmd.OutOrStdout()
			ctx := context.Background()

			// --check: the interlock contract. Read candidate paths from stdin (one
			// per line), print the PROTECTED subset to stdout, the unmanaged-container
			// complaints to stderr. The overlap is computed here (one authority), so a
			// consumer (llm-tidy) never reimplements it.
			if check {
				candidates, err := readLines(cmd.InOrStdin())
				if err != nil {
					return err
				}
				protected, report := lv.FilterProtected(ctx, candidates)
				printUnmanaged(cmd.ErrOrStderr(), report)
				for _, p := range protected {
					fmt.Fprintln(out, p)
				}
				return nil
			}

			if protectedOnly {
				keys, all := lv.ProtectedArtifacts(ctx)
				if all {
					fmt.Fprintln(out, "ALL (fail-closed: liveness could not be fully determined)")
					return nil
				}
				for _, k := range keys {
					fmt.Fprintln(out, k)
				}
				return nil
			}

			if len(args) == 1 {
				il, err := lv.Instance(ctx, args[0])
				if err != nil {
					return err
				}
				verdict := "evictable"
				if il.Protected {
					verdict = "protected"
				}
				fmt.Fprintf(out, "%-20s %-10s %s\n", il.Name, verdict, il.Reason)
				return nil
			}

			report := lv.Protected(ctx)
			printUnmanaged(cmd.ErrOrStderr(), report)
			if report.AllProtected {
				fmt.Fprintf(out, "protected: ALL (fail-closed: %s)\n", report.Reason)
				return nil
			}
			if len(report.Protected) == 0 {
				fmt.Fprintln(out, "no protected artifacts (nothing managed is running or desired)")
				return nil
			}
			fmt.Fprintln(out, "protected artifacts:")
			for _, k := range sortedKeys(report.Protected) {
				fmt.Fprintf(out, "  %s\n", k)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&protectedOnly, "protected-artifacts", false, "print only the protected artifact paths (one per line, machine-readable)")
	cmd.Flags().BoolVar(&check, "check", false, "read candidate paths from stdin, print the PROTECTED subset to stdout (the llm-tidy interlock contract)")
	return cmd
}

// readLines reads non-empty trimmed lines from r.
func readLines(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

// printUnmanaged warns about unlabeled running containers that bind-mount host
// dirs — the coexistence signal a pruner (llm-tidy) must not silently work around.
func printUnmanaged(w interface{ Write([]byte) (int, error) }, r liveness.Report) {
	for _, um := range r.Unmanaged {
		fmt.Fprintf(w, "warning: unmanaged container %q bind-mounts %v — pruning under these paths is blocked; label or migrate it to llm-serve\n",
			um.Container, um.Mounts)
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
