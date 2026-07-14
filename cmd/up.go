package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/ui"
	"github.com/rafpe/cube-idp/internal/up"
)

func newUpCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "up",
		Short: "Create/ensure the cluster, install the engine, deliver all packs, exit",
		// RunPipeline owns the event pipeline for the resolved mode (plain /
		// live / JSON) and guarantees the terminal is released and no
		// goroutine survives before it returns (Task 14b, design doc §4.2).
		RunE: func(c *cobra.Command, _ []string) error {
			return ui.RunPipeline(c.Context(), "up", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					return up.Run(ctx, file, con)
				})
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
