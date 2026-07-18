package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/trust"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// seams for tests — the decline/consent paths need a TTY, so tests override
// these (the trust.go trustInstall pattern) instead of faking a terminal.
var (
	downPromptsAllowed = ui.PromptsAllowed
	downConfirmName    = ui.InputExact
)

func newDownCmd() *cobra.Command {
	var file string
	var keepCluster bool
	var yes bool
	var confirmName string
	c := &cobra.Command{
		Use:   "down",
		Short: "Delete everything cube-idp created (inventory-driven cascade), then the cluster",
		// RunPipeline (Task 14b): down joins the event stream — the step
		// lines below are NEW, additive plain output (design doc §8: no test
		// pins down's full plain output; the substring assertions keep
		// passing because revertTrust's lines are byte-identical Notes).
		RunE: func(c *cobra.Command, _ []string) error {
			// Consent gate (TE-3, ratified R3) runs BEFORE ui.RunPipeline —
			// a prompt and the pipeline must never share the terminal (spec
			// Decision 5).
			if !yes {
				cube, err := config.Load(file)
				if err != nil {
					return err
				}
				if confirmName != "" {
					if confirmName != cube.Metadata.Name {
						return diag.New(diag.CodeConfirmRequired,
							fmt.Sprintf("--confirm=%q does not match cube %q", confirmName, cube.Metadata.Name),
							fmt.Sprintf("pass --confirm=%s (or --yes)", cube.Metadata.Name))
					}
				} else if downPromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
					printDownPreview(c.OutOrStdout(), cube, keepCluster) // TE-3.1
					ok, err := downConfirmName(c.InOrStdin(), c.OutOrStdout(),
						"Type the cube name to confirm:", cube.Metadata.Name)
					if err != nil {
						return err
					}
					if !ok {
						fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed") // TE-3.3, trust.go's exact wording
						return nil
					}
					fmt.Fprintln(c.OutOrStdout(), "  hint: cube-idp down --yes")
				} else {
					return diag.New(diag.CodeConfirmRequired, // TE-3.4 / R3
						fmt.Sprintf("destroying cube %q requires confirmation", cube.Metadata.Name),
						"re-run with --yes (or --confirm=<cube-name>) — non-interactive runs never destroy silently")
				}
			}
			return ui.RunPipeline(c.Context(), "down", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					return runDown(ctx, con, file, keepCluster)
				})
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&keepCluster, "keep-cluster", false, "delete cube-idp resources but keep the cluster")
	c.Flags().BoolVar(&yes, "yes", false, "skip the consent prompt (required on non-TTY runs)")
	c.Flags().StringVar(&confirmName, "confirm", "", "confirm by cube name instead of the interactive prompt")
	return c
}

// printDownPreview enumerates the REAL deletion set for the active config
// branch (TE-3.1) — it mirrors runDown's actual paths: kind/k3d delete the
// whole cluster (registry volume and TLS certs go with it); provider
// "existing"/--keep-cluster uninstall the engine, revert CoreDNS, and
// cascade-delete the inventory. The OS trust-store bullet appears only when
// trust.LoadState reports an installed CA. Bullet rows carry theme styles
// with plain-text content; the golden test compares ANSI-stripped bytes.
func printDownPreview(out io.Writer, cube *config.Cube, keepCluster bool) {
	fmt.Fprintln(out, th.Section.Render(fmt.Sprintf("Destroying cube %q will delete:", cube.Metadata.Name)))
	bullet := func(format string, a ...any) {
		fmt.Fprintf(out, "  %s %s\n", th.Err.Render("•"), fmt.Sprintf(format, a...))
	}
	if cube.Spec.Cluster.Provider == "existing" || keepCluster {
		bullet("%s engine install + CoreDNS rewrite (reverted; cluster kept)", cube.Spec.Engine.Type)
		bullet("all cube-idp inventory objects (cascade delete)")
	} else {
		bullet("%s cluster + kubeconfig context %s-%s",
			cube.Spec.Cluster.Provider, cube.Spec.Cluster.Provider, cube.Metadata.Name)
		bullet("zot registry volume, generated TLS certs")
	}
	bullet("%d installed packs", len(cube.Spec.Packs))
	// S3 (spec §5): declared spokes are part of the deletion set — one line
	// each, mirroring downSpokes' real paths. Spoke-less cubes print nothing
	// here, keeping the TE-3.1 golden byte-identical (GT13).
	for _, s := range cube.Spec.Spokes {
		switch {
		case s.Cluster.Provider == "kind" && !keepCluster:
			bullet("spoke %s (kind) — cluster will be deleted", s.Name)
		case s.Cluster.Provider == "kind":
			bullet("spoke %s (kind) — cluster kept; hub registration removed", s.Name)
		default:
			bullet("spoke %s (existing) — cluster left untouched; hub registration removed", s.Name)
		}
	}
	if dir, err := trustDir(); err == nil {
		if st, serr := trust.LoadState(dir); serr == nil && st.Installed {
			bullet("OS trust-store entry for the cube-idp local CA (reverted)")
		}
	}
}

// downSpokes cascades `down` onto declared spokes AFTER the hub teardown
// succeeded (spec §5): kind spoke clusters are deleted (best-effort — a
// failure warns with CUBE-8004 and never fails down, which must not strand
// the finished hub teardown on a half-dead spoke); existing spokes are
// never touched — the note hands the operator the manual RBAC removal.
// Hub-side registration secrets need no work here: they died with the hub
// cluster (kind/k3d path) or were cascade-deleted from inventory
// (existing/--keep-cluster path). --keep-cluster keeps spoke clusters too.
func downSpokes(ctx context.Context, con *ui.Console, cube *config.Cube, keepCluster bool) {
	for _, s := range cube.Spec.Spokes {
		switch s.Cluster.Provider {
		case "kind":
			name := cube.Metadata.Name + "-spoke-" + s.Name
			if keepCluster {
				con.Note("spoke %s: kind cluster %s kept (--keep-cluster)", s.Name, name)
				continue
			}
			if err := spokeClusterDelete(ctx, s, name); err != nil {
				con.Warn("%v", diag.Wrap(err, diag.CodeSpokeEnsureFailed,
					fmt.Sprintf("spoke %s: kind cluster %s deletion failed (continuing)", s.Name, name),
					"delete manually: kind delete cluster --name "+name))
				continue
			}
			con.Step("spoke", "kind cluster %s deleted", name)
		case "existing":
			con.Note("spoke %s: existing cluster left untouched — cube-idp-%s RBAC remains; remove with kubectl delete ns cube-idp-system && kubectl delete clusterrolebinding cube-idp-%s-admin --context %s",
				s.Name, cube.Spec.Engine.Type, cube.Spec.Engine.Type, s.Cluster.Context)
		}
	}
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
		eng, err := enginefactory.New(cube.Spec.Engine)
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
		downSpokes(ctx, con, cube, keepCluster) // hub teardown done — cascade to spokes (S3)
		return revertTrust(con)
	}
	// local providers (kind, k3d): deleting the cluster IS the cascade
	pr := con.Progress("cluster", "deleting "+cube.Spec.Cluster.Provider+" cluster")
	if err := prov.Delete(ctx, cube.Metadata.Name); err != nil {
		pr.Stop()
		return err
	}
	pr.Done("%s cluster deleted", cube.Spec.Cluster.Provider)
	downSpokes(ctx, con, cube, keepCluster) // spoke clusters are separate kind clusters — the hub delete does not take them down
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
