package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/ui"
)

func NewRootCmd() *cobra.Command {
	var plain bool
	root := &cobra.Command{
		Use:           "cube-idp",
		Short:         "cube-idp stands up an internal developer platform on Kubernetes and gets out of the way",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Sets ui.PlainFlag once, before any subcommand's RunE, so every
		// Printer built downstream (internal/up's step(), cmd/cnoe.go, ...)
		// sees the resolved --plain choice without threading a bool through
		// every orchestrator signature (Task 13.8).
		PersistentPreRunE: func(*cobra.Command, []string) error {
			ui.PlainFlag = plain
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&plain, "plain", false,
		"force plain, non-styled output (always on when $CI is set or stdout isn't a terminal)")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newUpCmd())
	root.AddCommand(newDownCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newUpgradeCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newGetCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newPackCmd())
	root.AddCommand(newTrustCmd())
	root.AddCommand(newCnoeCmd())
	return root
}

// Execute runs the root command to completion using ctx for cancellation —
// main.go wires this to a SIGINT-cancelable context so long-running steps
// like `up` can unwind cleanly on Ctrl-C.
func Execute(ctx context.Context) error { return NewRootCmd().ExecuteContext(ctx) }

// requireClusterExists guards read-only commands (status, get, down
// --keep-cluster) against side-effect cluster creation: the kind provider's
// Ensure CREATES a missing cluster, so any command that must never mutate
// calls this before Ensure. For provider "existing" Ensure was always
// read-only, so nothing is checked.
func requireClusterExists(ctx context.Context, prov cluster.Provider, provider, name string) error {
	if provider != "kind" {
		return nil
	}
	exists, err := prov.Exists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return diag.New(diag.CodeClusterNotExists,
			fmt.Sprintf("kind cluster %q does not exist", name),
			"run `cube-idp up` first")
	}
	return nil
}
