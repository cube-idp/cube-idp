package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cnoe"
	"github.com/cube-idp/cube-idp/internal/config"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/registry"
	"github.com/cube-idp/cube-idp/internal/ui"
)

const cnoeClusterTimeout = 3 * time.Minute
const cnoeApplyTimeout = 2 * time.Minute

func newCnoeCmd() *cobra.Command {
	var file string
	parent := &cobra.Command{Use: "cnoe", Short: "Compatibility tools for CNOE/idpbuilder setups"}

	imp := &cobra.Command{
		Use:   "import <dir>",
		Short: "Ingest idpbuilder Argo Application/ApplicationSet YAMLs into the running cube (cnoe:// paths become OCI pushes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			apps, err := cnoe.Load(args[0])
			if err != nil {
				return err
			}
			prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			// import is read-only with respect to cluster lifecycle: Ensure
			// would CREATE a missing kind cluster, but `cnoe import` should
			// only ever target an already-`up` cube (status/get pattern).
			if err := requireClusterExists(c.Context(), prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(c.Context(), cnoeClusterTimeout)
			conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster)
			cancel()
			if err != nil {
				return err
			}
			a, err := apply.New(conn.REST, cube.Metadata.Name)
			if err != nil {
				return err
			}
			eng, err := enginefactory.New(cube.Spec.Engine.Type)
			if err != nil {
				return err
			}
			tunnelAddr, stop, err := registry.PortForward(c.Context(), conn.REST)
			if err != nil {
				return err
			}
			defer stop()
			cacheDir, err := pack.DefaultCacheDir()
			if err != nil {
				return err
			}
			p := ui.NewFor(c.OutOrStdout())
			for _, app := range apps {
				rendered, err := app.Render(c.Context(), cacheDir)
				if err != nil {
					return err
				}
				artifact, err := oci.PushRendered(c.Context(), rendered, tunnelAddr)
				if err != nil {
					return err
				}
				deliverObjs, err := eng.Deliver(c.Context(), rendered, artifact)
				if err != nil {
					return err
				}
				if err := a.Apply(c.Context(), deliverObjs, false, cnoeApplyTimeout); err != nil {
					return err
				}
				if err := a.RecordInventory(c.Context(), deliverObjs); err != nil {
					return err
				}
				p.Step("cnoe", "%s imported as %s@%s", app.Name, rendered.Name, rendered.Version)
			}
			fmt.Fprintf(c.OutOrStdout(), "%s %d application(s) imported — `cube-idp status` tracks their health\n", p.Glyph(ui.GlyphOK), len(apps))
			return nil
		},
	}
	parent.PersistentFlags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	parent.AddCommand(imp)
	return parent
}
