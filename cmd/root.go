package cmd

import (
	"context"

	"github.com/spf13/cobra"
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
	root.AddCommand(newInitCmd())
	return root
}

// Execute runs the root command to completion using ctx for cancellation —
// main.go wires this to a SIGINT-cancelable context so long-running steps
// like `up` can unwind cleanly on Ctrl-C.
func Execute(ctx context.Context) error { return NewRootCmd().ExecuteContext(ctx) }
