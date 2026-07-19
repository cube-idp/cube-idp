package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cfgload"
	"github.com/cube-idp/cube-idp/internal/cluster"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/syncer"
	"github.com/cube-idp/cube-idp/internal/ui"
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
			if watch {
				// Watch stays OUTSIDE the event pipeline (Task R3, spec
				// §5.3 — a ratified deferral): it's the sanctioned
				// long-running FOREGROUND mode, not a daemon, and its own
				// loop already routes through the ui seam
				// (internal/syncer/watch.go). This branch is byte-identical
				// to the pre-R3 body: its own config.Load/cluster/Deps
				// setup, never touching RunPipelineStatic.
				cube, err := cfgload.Load(c.Context(), file)
				if err != nil {
					return err
				}
				prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
				if err != nil {
					return err
				}
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
				eng, err := enginefactory.New(cube.Spec.Engine)
				if err != nil {
					return err
				}
				deps := syncer.Deps{Applier: a, Engine: eng, REST: conn.REST, Out: c.OutOrStdout()}
				// Watch is the sanctioned long-running FOREGROUND mode, not
				// a daemon: it blocks until c.Context() is cancelled
				// (Ctrl-C, via main.go's signal.NotifyContext flowing
				// through Execute(ctx)) and returns nil.
				return syncer.Watch(c.Context(), deps, args[0], syncWatchDebounce)
			}

			// The one-shot path is fully on the event stream (Task R3):
			// RunPipelineStatic owns the whole RunE body so a failed
			// config.Load returns before con.Start ever fires (the
			// RunStarted-skip rule, G6) — machine consumers must tolerate a
			// stream that is only run_done+diagnosis.
			return ui.RunPipelineStatic(c.Context(), "sync", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					cube, err := cfgload.Load(ctx, file)
					if err != nil {
						return err
					}
					con.Start("sync", cube.Metadata.Name)

					prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
					if err != nil {
						return err
					}
					// sync targets an already-`up` cube: Ensure would
					// CREATE a missing kind cluster, which sync must never
					// do as a side effect (status/get/cnoe-import pattern).
					if err := requireClusterExists(ctx, prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
						return err
					}
					ensureCtx, cancel := context.WithTimeout(ctx, syncClusterTimeout)
					conn, err := prov.Ensure(ensureCtx, cube.Metadata.Name, cube.Spec.Cluster)
					cancel()
					if err != nil {
						return err
					}
					a, err := apply.New(conn.REST, cube.Metadata.Name)
					if err != nil {
						return err
					}
					eng, err := enginefactory.New(cube.Spec.Engine)
					if err != nil {
						return err
					}
					deps := syncer.Deps{Applier: a, Engine: eng, REST: conn.REST, Out: c.OutOrStdout(), Steps: con}

					// SyncOnce's own Step calls already emit the "delivered
					// — engine reconciling" line (internal/syncer/syncer.go)
					// via deps.Steps; a second raw line here would both
					// bypass the event stream and duplicate that line.
					_, err = syncer.SyncOnce(ctx, deps, args[0])
					return err
				})
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&watch, "watch", false, "watch dir and re-sync on every debounced change until Ctrl-C")
	return c
}
