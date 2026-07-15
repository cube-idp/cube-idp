package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/syncer"
)

const (
	syncClusterTimeout = 3 * time.Minute
	syncWatchDebounce  = 300 * time.Millisecond
)

func newSyncCmd() *cobra.Command {
	var file string
	var watch bool
	c := &cobra.Command{
		Use:   "sync <dir>",
		Short: "Render a directory as a pack and deliver it to the running cube (D7)",
		Long: `Render a directory as a pack and deliver it to the running cube (D7).

Without --watch, sync runs once and exits. With --watch, sync runs once
immediately and then re-syncs on every debounced filesystem change under
dir until interrupted (Ctrl-C); a sync failure while watching is printed
in full and does not stop the watch — fix the file and save again.

sync pushes OCI artifacts directly to the cube's registry; it is not a
git-push-based deployment flow. That flow is provided separately by the
gitea pack ('cube-idp repo create').`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
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

			if watch {
				// Watch is the sanctioned long-running FOREGROUND mode, not
				// a daemon: it blocks until c.Context() is cancelled
				// (Ctrl-C, via main.go's signal.NotifyContext flowing
				// through Execute(ctx)) and returns nil.
				return syncer.Watch(c.Context(), deps, args[0], syncWatchDebounce)
			}

			// SyncOnce's own printer.Step already emits the "delivered —
			// engine reconciling" line (internal/syncer/syncer.go:118) via the
			// ui seam; a second raw "✔ synced ..." line here would both
			// bypass ui and duplicate that line.
			if _, err := syncer.SyncOnce(c.Context(), deps, args[0]); err != nil {
				return err
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&watch, "watch", false, "watch dir and re-sync on every debounced change until Ctrl-C")
	return c
}
