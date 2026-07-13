package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/diag"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cube-idp",
		Short:         "cube-idp stands up an internal developer platform on Kubernetes and gets out of the way",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newUpCmd())
	root.AddCommand(newDownCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newGetCmd())
	root.AddCommand(newInitCmd())
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
