package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/syncer"
)

const syncClusterTimeout = 3 * time.Minute

func newSyncCmd() *cobra.Command {
	var file string
	var watch bool
	c := &cobra.Command{
		Use:   "sync <dir>",
		Short: "Render a directory as a pack and deliver it to the running cube, once (D7)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			// The flag surface ships now so scripts/docs written against it
			// don't need to change when Task 11 implements it — only the
			// behavior is stubbed.
			if watch {
				return diag.New(diag.CodeSyncWatchNotBuilt,
					"watch mode lands in the next task of this plan",
					"run without --watch")
			}
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			// sync targets an already-`up` cube: Ensure would CREATE a
			// missing kind cluster, which sync must never do as a side
			// effect (status/get/cnoe-import pattern).
			if err := requireClusterExists(c.Context(), prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(c.Context(), syncClusterTimeout)
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
			deps := syncer.Deps{Applier: a, Engine: eng, REST: conn.REST, Out: c.OutOrStdout()}
			result, err := syncer.SyncOnce(c.Context(), deps, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "✔ synced %s@%s — engine reconciling\n", result.Pack, result.Version)
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&watch, "watch", false, "watch dir and re-sync on change (lands in a later task)")
	return c
}
