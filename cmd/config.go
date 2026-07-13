package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/cluster/kindp"
	"github.com/rafpe/cube-idp/internal/config"
)

// newConfigCmd exposes read-only inspection of the loaded cube.yaml, e.g.
// `cube-idp config render-cluster` for the D10 provider-config merge.
func newConfigCmd() *cobra.Command {
	var file string
	cfg := &cobra.Command{Use: "config", Short: "Inspect cube-idp configuration"}

	render := &cobra.Command{
		Use:   "render-cluster",
		Short: "Print the final merged provider config that `up` would create (D10)",
		RunE: func(c *cobra.Command, _ []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			if cube.Spec.Cluster.Provider != "kind" {
				return fmt.Errorf("render-cluster applies to provider: kind (got %q)", cube.Spec.Cluster.Provider)
			}
			out, err := kindp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			fmt.Fprint(c.OutOrStdout(), string(out))
			return nil
		},
	}
	cfg.PersistentFlags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	cfg.AddCommand(render)
	return cfg
}
