package cmd

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cube-idp",
		Short:         "cube-idp stands up an internal developer platform on Kubernetes and gets out of the way",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	return root
}

func Execute() error { return NewRootCmd().Execute() }
