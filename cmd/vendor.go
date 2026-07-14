package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/bundle"
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
		RunE: func(c *cobra.Command, _ []string) error {
			return bundle.Vendor(c.Context(), lockPath, out, platform, c.OutOrStdout())
		},
	}
	c.Flags().StringVar(&lockPath, "lock", "cube.lock", "path to cube.lock")
	c.Flags().StringVarP(&out, "output", "o", "cube-bundle.tar.gz", "bundle output path")
	c.Flags().StringVar(&platform, "platform", "", "image platform os/arch (default: linux/<host-arch>)")
	return c
}
