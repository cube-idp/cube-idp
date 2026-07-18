package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cluster/k3dp"
	"github.com/cube-idp/cube-idp/internal/cluster/kindp"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
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
			// render-cluster stays pure and file-free: no certs.d/zot-mirror
			// staging here (kindp.CertsD{}/k3dp.ZotMirror{} below are the zero
			// values, so RenderConfig omits the containerd certs.d bind mount /
			// registries.yaml zot entry entirely). `up` stages the real certs.d
			// directory (kind) or zot mirror host (k3d) and injects that at
			// create-time (internal/cluster/kindp/kind.go's certsD,
			// internal/cluster/k3dp/k3d.go's Ensure, D6 canonical hostname) —
			// this rendering is therefore not byte-identical to what `up`
			// actually hands the provider, and the gap is called out on stderr
			// below rather than left as a silent difference: stdout stays pure
			// YAML so `cube-idp config render-cluster --file cube.yaml | kind
			// create cluster --config -` keeps working.
			var out []byte
			switch cube.Spec.Cluster.Provider {
			case "kind":
				out, _, err = kindp.RenderConfig(c.Context(), cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway, kindp.CertsD{})
			case "k3d":
				out, err = k3dp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway, k3dp.ZotMirror{})
			default:
				return diag.New(diag.CodeProviderMiss,
					fmt.Sprintf("render-cluster applies to cluster-creating providers (kind, k3d), not %q", cube.Spec.Cluster.Provider),
					"provider: existing has no provider config to render")
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(c.ErrOrStderr(),
				"note: `up` also injects a containerd certs.d bind mount (kind) or registries.yaml zot mirror entry (k3d) for the local CA trust root (D6) — this rendering omits it")
			fmt.Fprint(c.OutOrStdout(), string(out))
			return nil
		},
	}
	// `cube-idp config render-engine` — render-cluster's engine twin (GT1,
	// U3): prints the engine install manifests exactly as `up` would SSA
	// them, i.e. with spec.engine.tuning already patched in. Inspectability
	// is the point — the tuned result is visible before any cluster exists.
	// Unlike render-cluster there is no up-time injection gap: stdout IS
	// the full object stream, so it stays pure YAML (pipeable into kubectl).
	renderEngine := &cobra.Command{
		Use:   "render-engine",
		Short: "Print the tuned engine install manifests that `up` would apply (GT1)",
		RunE: func(c *cobra.Command, _ []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			eng, err := enginefactory.New(cube.Spec.Engine)
			if err != nil {
				return err
			}
			objs, err := eng.InstallManifests()
			if err != nil {
				return err
			}
			for i, o := range objs {
				b, err := yaml.Marshal(o.Object)
				if err != nil {
					return err
				}
				if i > 0 {
					fmt.Fprintln(c.OutOrStdout(), "---")
				}
				fmt.Fprint(c.OutOrStdout(), string(b))
			}
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
	cfg.AddCommand(renderEngine)
	cfg.AddCommand(schema)
	return cfg
}
