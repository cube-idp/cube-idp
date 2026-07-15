package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/bundle"
	"github.com/rafpe/cube-idp/internal/ui"
)

func newVendorCmd() *cobra.Command {
	var lockPath, out, platform string
	c := &cobra.Command{
		Use:   "vendor",
		Short: "Bundle every artifact and image pinned in cube.lock for air-gapped installs",
		Long: "Reads cube.lock and pulls every pinned pack source and container image into\n" +
			"one self-contained tar.gz bundle (spec §4.1). Pure lock consumer: no cluster\n" +
			"access, no config mutation. A bundle is complete or an error — any pull\n" +
			"failure aborts the whole run rather than shipping a partial bundle.",
		Args: cobra.NoArgs,
		// RunPipeline owns the event pipeline for the resolved mode (plain /
		// live / JSON) — vendor is the one R3 command that keeps the live
		// step-tree (long-running, per-pack/per-image progress; Task R3).
		// RunStarted.Cube is deliberately empty: vendor is a pure lock
		// consumer with no cube.yaml.
		RunE: func(c *cobra.Command, _ []string) error {
			return ui.RunPipeline(c.Context(), "vendor", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("vendor", "")
					return bundle.Vendor(ctx, lockPath, out, platform, con)
				})
		},
	}
	c.Flags().StringVar(&lockPath, "lock", "cube.lock", "path to cube.lock")
	c.Flags().StringVarP(&out, "output", "o", "cube-bundle.tar.gz", "bundle output path")
	c.Flags().StringVar(&platform, "platform", "", "image platform os/arch (default: linux/<host-arch>)")
	return c
}
