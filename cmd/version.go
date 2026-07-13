package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set via -ldflags "-X github.com/rafpe/cube-idp/cmd.Version=v0.1.0".
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the cube-idp version",
		RunE: func(c *cobra.Command, _ []string) error {
			fmt.Fprintf(c.OutOrStdout(), "cube-idp version %s\n", Version)
			return nil
		},
	}
}
