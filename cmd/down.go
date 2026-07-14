package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/trust"
	"github.com/rafpe/cube-idp/internal/ui"
)

func newDownCmd() *cobra.Command {
	var file string
	var keepCluster bool
	c := &cobra.Command{
		Use:   "down",
		Short: "Delete everything cube-idp created (inventory-driven cascade), then the cluster",
		// RunPipeline (Task 14b): down joins the event stream — the step
		// lines below are NEW, additive plain output (design doc §8: no test
		// pins down's full plain output; the substring assertions keep
		// passing because revertTrust's lines are byte-identical Notes).
		RunE: func(c *cobra.Command, _ []string) error {
			return ui.RunPipeline(c.Context(), "down", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					return runDown(ctx, con, file, keepCluster)
				})
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&keepCluster, "keep-cluster", false, "delete cube-idp resources but keep the cluster")
	return c
}

func runDown(ctx context.Context, con *ui.Console, file string, keepCluster bool) error {
	cube, err := config.Load(file)
	if err != nil {
		return err
	}
	con.Start("down", cube.Metadata.Name)
	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return err
	}
	// existing clusters: remove only cube-idp-managed resources (spec §4.3)
	if cube.Spec.Cluster.Provider == "existing" || keepCluster {
		// Ensure would CREATE a missing kind cluster — never as a
		// side effect of down --keep-cluster.
		if err := requireClusterExists(ctx, prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
			return err
		}
		// "no infinite spinner": an unreachable existing cluster must
		// not stall down indefinitely (mirrors status's connect timeout).
		ensureCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		conn, err := prov.Ensure(ensureCtx, cube.Metadata.Name, cube.Spec.Cluster)
		cancel()
		if err != nil {
			return err
		}
		a, err := apply.New(conn.REST, cube.Metadata.Name)
		if err != nil {
			return err
		}
		// Two-phase teardown: first the engine deletes its delivered
		// sources and waits for their prune finalizers (so flux
		// removes the workloads it delivered while its controllers
		// are still alive), then the inventory cascade removes
		// everything else — DeleteAll skips the already-gone engine
		// objects via its IsNotFound/NoMatch handling.
		eng, err := enginefactory.New(cube.Spec.Engine.Type)
		if err != nil {
			return err
		}
		pr := con.Progress("engine", "uninstalling "+cube.Spec.Engine.Type)
		if err := eng.Uninstall(ctx, a, 5*time.Minute); err != nil {
			pr.Stop()
			return err
		}
		pr.Done("%s uninstalled", cube.Spec.Engine.Type)
		// D6: revert the CoreDNS rewrite before tearing the rest down
		// — the cluster (and CoreDNS with it) survives this path, so
		// nothing else undoes it.
		pr = con.Progress("dns", "reverting CoreDNS rewrite")
		if err := trust.RemoveCoreDNSRewrite(ctx, a.Client(), 2*time.Minute); err != nil {
			pr.Stop()
			return err
		}
		pr.Done("CoreDNS rewrite reverted")
		pr = con.Progress("cascade", "deleting inventory objects")
		if err := a.DeleteAll(ctx, 5*time.Minute); err != nil {
			pr.Stop()
			return err
		}
		pr.Done("inventory objects deleted")
		return revertTrust(con)
	}
	// local providers (kind, k3d): deleting the cluster IS the cascade
	pr := con.Progress("cluster", "deleting "+cube.Spec.Cluster.Provider+" cluster")
	if err := prov.Delete(ctx, cube.Metadata.Name); err != nil {
		pr.Stop()
		return err
	}
	pr.Done("%s cluster deleted", cube.Spec.Cluster.Provider)
	return revertTrust(con)
}

// revertTrust reverts `cube-idp trust`'s OS trust-store install (D6 contract:
// `down` always undoes it, on both the kind-delete and keep-cluster paths).
// No-op if `trust` was never run.
//
// Deletion has already succeeded by the time this runs, so a broken trust
// dir/state (e.g. a corrupt trust-state.yaml, CUBE-6006) must not fail
// `down` — that would strand the user with a torn-down cluster and a
// non-zero exit. Instead we warn loudly with remediation and return nil.
//
// The messages flow as Note events (Task 14b): the plain projection adds
// exactly one trailing newline, so every line is byte-identical to the raw
// fmt.Fprintf/Fprintln output this replaced.
func revertTrust(con *ui.Console) error {
	dir, derr := trustDir()
	if derr != nil {
		con.Note("warning: could not check cube-idp trust state (%v); run `cube-idp trust --uninstall` manually if the cube-idp CA was trusted", derr)
		return nil
	}
	st, serr := trust.LoadState(dir)
	if serr != nil {
		con.Note("warning: could not read cube-idp trust state (%v); run `cube-idp trust --uninstall` manually if the cube-idp CA was trusted", serr)
		return nil
	}
	if !st.Installed {
		return nil
	}
	if err := trustUninstall(dir); err != nil {
		return err // CUBE-6003 with manual remediation
	}
	con.Step("trust", "OS trust-store install reverted")
	con.Note("reverted: cube-idp CA removed from OS trust stores")
	return nil
}
