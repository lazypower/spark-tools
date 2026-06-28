package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/paths"
	"github.com/lazypower/spark-tools/pkg/llmserve"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
)

// dirs resolves the XDG state layout: manifests, emitted specs, and the watchdog
// script all live under one root, overridable via LLM_SERVE_HOME.
func dirs() (stateDir, specDir, watchdogDir string) {
	// POLICY (llm-serve-owned): LLM_SERVE_HOME overrides; the state root is an
	// XDG_STATE_HOME app dir; specs/ and watchdog/ live under the root. Only the
	// XDG-state arithmetic is delegated to the shared mechanism.
	root := os.Getenv("LLM_SERVE_HOME")
	if root == "" {
		root = paths.XDGState("llm-serve")
	}
	return root, filepath.Join(root, "specs"), filepath.Join(root, "watchdog")
}

func upCmd() *cobra.Command {
	var (
		modelDir, name, served, image, accelerator, target, repoTree string
		caps, mounts                                                 []string
		ctx, port                                                    int
		timeout                                                      time.Duration
	)
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Bring an instance up to confirmed serving (resolve → emit → apply → verify)",
		Long: "Resolve a verified model + capabilities into a validated launch, apply it via the\n" +
			"host runtime, and wait until it is CONFIRMED serving (identity + health + a warmup\n" +
			"against the served model + watchdog). Fail-closed: a bring-up that does not confirm\n" +
			"is torn down (or kept as a recovery handle if teardown can't be confirmed).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if target != "" && target != "compose" {
				return fmt.Errorf("B1 drives compose only; --target %q not supported", target)
			}
			capList, err := parseCaps(caps)
			if err != nil {
				return err
			}
			mountList, err := parseMounts(mounts)
			if err != nil {
				return err
			}
			facts, err := resolveFacts(modelDir, repoTree, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			stateDir, specDir, watchdogDir := dirs()
			if _, err := llmserve.EnsureWatchdogScript(watchdogDir); err != nil {
				return fmt.Errorf("installing watchdog script: %w", err)
			}

			plan, resolved, err := llmserve.BuildPlan(llmserve.PlanRequest{
				Name:         name,
				ServedName:   served,
				Facts:        facts,
				Capabilities: capList,
				ContextLen:   ctx,
				Image:        image,
				Accelerator:  accelerator,
				Port:         port,
				Mounts:       mountList,
				WatchdogDir:  watchdogDir,
			})
			if err != nil {
				return err
			}
			for _, w := range resolved.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}

			orch := llmserve.NewOrchestrator(stateDir, specDir)
			orch.BootTimeout = timeout // 0 ⇒ the orchestrator's generous default
			fmt.Fprintf(cmd.ErrOrStderr(), "bringing up %q (waiting for confirmed serving; large models cold-start in minutes, fail-fast on crash)...\n", name)
			res, err := orch.Up(context.Background(), plan)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s — %s\n", name, res.Status, res.Reason)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&modelDir, "model-dir", "", "path to the hfetch-verified model directory (required)")
	f.StringVar(&name, "name", "", "instance name / served alias (required)")
	f.StringVar(&served, "served-name", "", "served model name (default: --name)")
	f.StringSliceVar(&caps, "cap", nil, "requested capability (repeatable)")
	f.IntVar(&ctx, "ctx", 0, "max model length (tokens)")
	f.StringVar(&image, "image", "", "engine image, e.g. vllm/vllm-openai@v0.23.0 (required)")
	f.StringVar(&accelerator, "accelerator", "nvidia:gb10:sm121", "target accelerator fingerprint")
	f.IntVar(&port, "port", 8000, "host port to map to container :8000")
	f.StringArrayVar(&mounts, "mount", nil, "read-only model mount host:container (repeatable)")
	f.StringVar(&target, "target", "compose", "render target (B1: compose only)")
	f.StringVar(&repoTree, "repo-tree", "", "saved hfetch tree listing (JSON) to run the completeness gate")
	f.DurationVar(&timeout, "timeout", 0, "ceiling for reaching confirmed serving (0 = default 20m; a crashed container fails fast regardless)")
	_ = cmd.MarkFlagRequired("model-dir")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down <name>",
		Short: "Tear an instance down (confirmed teardown removes it; unconfirmed keeps a recovery handle)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, specDir, _ := dirs()
			return llmserve.NewOrchestrator(stateDir, specDir).Down(context.Background(), args[0])
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [name]",
		Short: "Reconcile desired vs actual (pure read; never reports a state it can't confirm)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, specDir, _ := dirs()
			orch := llmserve.NewOrchestrator(stateDir, specDir)
			out := cmd.OutOrStdout()
			if len(args) == 1 {
				st, err := orch.Status(context.Background(), args[0])
				if err != nil {
					return err
				}
				printStatus(out, st)
				return nil
			}
			list, err := orch.List(context.Background())
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintln(out, "no managed instances")
				return nil
			}
			for _, st := range list {
				printStatus(out, st)
			}
			return nil
		},
	}
}

func printStatus(out interface{ Write([]byte) (int, error) }, st lifecycle.InstanceStatus) {
	phase := "steady"
	if st.Instance.Operation != nil {
		phase = string(st.Instance.Operation.Phase)
	}
	fmt.Fprintf(out, "%-20s %-12s %-14s %s\n", st.Instance.Desired.Name, st.Status, phase, st.Reason)
}

func recoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recover",
		Short: "Resolve in-flight/half-done mutations against the runtime (under the host lock)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			stateDir, specDir, _ := dirs()
			return llmserve.NewOrchestrator(stateDir, specDir).Recover(context.Background())
		},
	}
}

func forgetCmd() *cobra.Command {
	var acceptOrphan bool
	cmd := &cobra.Command{
		Use:   "forget <name>",
		Short: "Abandon a manifest stuck in cleanup_required (prefers confirmed absence)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, specDir, _ := dirs()
			return llmserve.NewOrchestrator(stateDir, specDir).Forget(context.Background(), args[0], acceptOrphan)
		},
	}
	cmd.Flags().BoolVar(&acceptOrphan, "accept-orphan", false, "abandon even if a possibly-running stack can't be confirmed gone")
	return cmd
}
