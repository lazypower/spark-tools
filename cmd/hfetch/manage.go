package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

func rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <model_id> [filename]",
		Short: "Remove a downloaded model",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			reg := registry.New(dirs.Data)
			if err := reg.Load(); err != nil {
				return err
			}

			var filenames []string
			if len(args) > 1 {
				filenames = args[1:]
			}

			if err := reg.Remove(args[0], filenames...); err != nil {
				return err
			}
			if err := reg.Save(); err != nil {
				return err
			}

			if len(filenames) > 0 {
				fmt.Printf("  Removed %s from %s\n", filenames[0], args[0])
			} else {
				fmt.Printf("  Removed %s\n", args[0])
			}
			return nil
		},
	}
}

func gcCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Remove partial downloads and orphaned files",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := config.Dirs()
			reg := registry.New(dirs.Data)
			if err := reg.Load(); err != nil {
				return err
			}

			freed, err := reg.GC()
			if err != nil {
				return err
			}

			if freed == 0 {
				fmt.Println("  Nothing to clean up.")
			} else {
				fmt.Printf("  Freed %s\n", formatSize(freed))
			}
			return nil
		},
	}
}
