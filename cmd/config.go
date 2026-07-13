package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/cluster/kindp"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
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
				return diag.New(diag.CodeProviderMiss,
					fmt.Sprintf("render-cluster applies to provider: kind (got %q)", cube.Spec.Cluster.Provider),
					"provider: existing creates no cluster, so there is no provider config to render")
			}
			// render-cluster stays pure and file-free: no certs.d staging here.
			out, err := kindp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway, kindp.CertsD{})
			if err != nil {
				return err
			}
			fmt.Fprint(c.OutOrStdout(), string(out))
			return nil
		},
	}
	// `cube-idp config schema` — the command every CUBE-0002 remediation
	// points at: prints the embedded CUE schema cube.yaml is validated
	// against. Needs no cube.yaml to exist.
	schema := &cobra.Command{
		Use:   "schema",
		Short: "Print the CUE schema cube.yaml is validated against",
		RunE: func(c *cobra.Command, _ []string) error {
			fmt.Fprint(c.OutOrStdout(), config.Schema())
			return nil
		},
	}

	cfg.PersistentFlags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	cfg.AddCommand(render)
	cfg.AddCommand(schema)
	return cfg
}
