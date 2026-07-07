package main

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A bare repo id must route to `pull` — cobra needs ArbitraryArgs on the root or
// it rejects the id as an unknown subcommand before RunE runs (the shorthand was
// dead code). The shorthand must also supply the default profile.
func TestRootShorthand_RoutesRepoIDToPull(t *testing.T) {
	orig := runPullFn
	t.Cleanup(func() { runPullFn = orig })

	var gotID, gotProfile string
	called := false
	runPullFn = func(_ *cobra.Command, id string, f pullFlags) error {
		called = true
		gotID, gotProfile = id, f.profile
		return nil
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"org/model"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bare repo id should route to pull, got: %v", err)
	}
	if !called {
		t.Fatal("bare repo id never reached pull (cobra rejected it before RunE)")
	}
	if gotID != "org/model" {
		t.Errorf("pull got id %q, want org/model", gotID)
	}
	if gotProfile != defaultPullProfile {
		t.Errorf("shorthand must default the profile, got %q want %q", gotProfile, defaultPullProfile)
	}
}

// A single non-repo arg is a typo of a subcommand, not a pull target — it must
// keep cobra's unknown-command behavior rather than silently showing help.
func TestRootShorthand_NonRepoArgIsUnknownCommand(t *testing.T) {
	orig := runPullFn
	t.Cleanup(func() { runPullFn = orig })
	runPullFn = func(*cobra.Command, string, pullFlags) error {
		t.Fatal("a non-repo arg must not route to pull")
		return nil
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"searchh"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("a typo'd subcommand should be an unknown-command error, got: %v", err)
	}
}

// A real subcommand must still dispatch even though the root now takes args.
func TestRootShorthand_SubcommandStillDispatches(t *testing.T) {
	orig := runPullFn
	t.Cleanup(func() { runPullFn = orig })
	runPullFn = func(*cobra.Command, string, pullFlags) error {
		t.Fatal("a subcommand invocation must not route through the shorthand")
		return nil
	}
	t.Setenv("HFETCH_DATA_DIR", t.TempDir())

	cmd := rootCmd()
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list subcommand should still dispatch: %v", err)
	}
}
