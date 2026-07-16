package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/ui"
	"github.com/cube-idp/cube-idp/internal/up"
)

func newUpCmd() *cobra.Command {
	var file string
	var bundlePath string
	c := &cobra.Command{
		Use:   "up",
		Short: "Create/ensure the cluster, install the engine, deliver all packs, exit",
		// RunPipeline owns the event pipeline for the resolved mode (plain /
		// live / JSON) and guarantees the terminal is released and no
		// goroutine survives before it returns (Task 14b, design doc §4.2).
		RunE: func(c *cobra.Command, _ []string) error {
			return ui.RunPipeline(c.Context(), "up", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					return up.Run(ctx, up.Options{ConfigPath: file, Bundle: bundlePath, Con: con})
				})
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().StringVar(&bundlePath, "bundle", "", "install fully offline from a cube-idp vendor bundle")
	return c
}
