package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/up"
)

func newUpCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "up",
		Short: "Create/ensure the cluster, install the engine, deliver all packs, exit",
		RunE: func(c *cobra.Command, _ []string) error {
			return up.Run(c.Context(), file, c.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
