package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// spoke: declarative hub/spoke registration — see
// docs/adr/0013-spoke-clusters.md for the registration-only scope decision.
// These commands only edit cube.yaml — `up` bootstraps and registers spokes
// (S2/S3), `down` cascades. cube-idp registers spokes with the hub engine
// and gets out of the way; delivering workloads to them is engine content.
func newSpokeCmd() *cobra.Command {
	parent := &cobra.Command{Use: "spoke", Short: "Manage spoke clusters registered with this cube's engine"}
	parent.AddCommand(newSpokeAddCmd(), newSpokeListCmd(), newSpokeRemoveCmd())
	return parent
}

func newSpokeAddCmd() *cobra.Command {
	var file, provider, kubeContext string
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Declare a spoke in cube.yaml (applied on the next `cube-idp up`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			for _, s := range cube.Spec.Spokes {
				if s.Name == name {
					return diag.New(diag.CodeSpokeProviderUnsupported,
						fmt.Sprintf("spoke %q already declared", name),
						"pick another name or `cube-idp spoke remove` it first")
				}
			}
			cube.Spec.Spokes = append(cube.Spec.Spokes, config.SpokeSpec{
				Name:    name,
				Cluster: config.ClusterSpec{Provider: provider, Context: kubeContext},
			})
			if err := config.SaveValidated(file, cube); err != nil {
				return err
			}
			p := ui.NewFor(c.OutOrStdout())
			p.Step("spoke", "%q declared (provider %s) — run `cube-idp up` to bootstrap and register it", name, provider)
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().StringVar(&provider, "provider", "kind", "spoke cluster provider (kind|existing)")
	c.Flags().StringVar(&kubeContext, "context", "", "kubeconfig context (required for --provider existing)")
	return c
}

func newSpokeListCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "list",
		Short: "List spokes declared in cube.yaml (with live hub state when reachable)",
		RunE: func(c *cobra.Command, args []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if len(cube.Spec.Spokes) == 0 {
				fmt.Fprintln(out, "no spokes declared")
				return nil
			}
			// S4: live Registered/Reachable columns from the same collector
			// `status` uses (the statusConnect seam). Graceful degradation:
			// any failure — missing cluster, dead apiserver — falls back to
			// the declared-config-only table with a trailing note, never an
			// error. `spoke list` must always answer.
			live, ok := spokeListLive(c.Context(), file)
			p := ui.NewFor(out)
			for _, s := range cube.Spec.Spokes {
				kctx := s.Cluster.Context
				if kctx == "" {
					kctx = "-"
				}
				if !ok {
					fmt.Fprintf(out, "%-20s %-10s %s\n", s.Name, s.Cluster.Provider, kctx)
					continue
				}
				st := live[s.Name]
				fmt.Fprintf(out, "%-20s %-10s %-20s %s  %s\n", s.Name, s.Cluster.Provider, kctx,
					spokeStateCell(p, st.Registered, "registered", "unregistered"),
					spokeStateCell(p, st.Reachable, "reachable", "unreachable"))
			}
			if !ok {
				fmt.Fprintln(out, "hub unreachable — showing declared config only")
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}

// spokeListLive attempts one status collection through the statusConnect
// seam and indexes the spoke rows by name. ok=false on any failure — the
// caller degrades to declared config, it never errors.
func spokeListLive(ctx context.Context, file string) (map[string]spokeStatus, bool) {
	_, collect, err := statusConnect(ctx, file, false)
	if err != nil {
		return nil, false
	}
	snap, err := collect(ctx)
	if err != nil {
		return nil, false
	}
	live := make(map[string]spokeStatus, len(snap.Spokes))
	for _, s := range snap.Spokes {
		live[s.Name] = s
	}
	return live, true
}

func newSpokeRemoveCmd() *cobra.Command {
	var file string
	var deleteCluster, yes bool
	c := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a spoke declaration (hub registration prunes on next `up`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			idx := -1
			for i, s := range cube.Spec.Spokes {
				if s.Name == name {
					idx = i
					break
				}
			}
			if idx < 0 {
				return diag.New(diag.CodeSpokeProviderUnsupported,
					fmt.Sprintf("spoke %q is not declared", name),
					"`cube-idp spoke list` shows declared spokes")
			}
			spoke := cube.Spec.Spokes[idx]
			cube.Spec.Spokes = append(cube.Spec.Spokes[:idx], cube.Spec.Spokes[idx+1:]...)
			if err := config.SaveValidated(file, cube); err != nil {
				return err
			}
			p := ui.NewFor(c.OutOrStdout())
			p.Step("spoke", "%q removed — the hub registration secret prunes on the next `cube-idp up`", name)
			if deleteCluster && spoke.Cluster.Provider == "kind" {
				return spokeDeleteCluster(c, cube.Metadata.Name, spoke, yes)
			}
			if spoke.Cluster.Provider == "kind" {
				p.Warn("kind cluster %s-spoke-%s left running — delete with `cube-idp spoke remove --delete-cluster` or `kind delete cluster --name %s-spoke-%s`",
					cube.Metadata.Name, name, cube.Metadata.Name, name)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&deleteCluster, "delete-cluster", false, "also delete a kind spoke cluster now")
	c.Flags().BoolVar(&yes, "yes", false, "skip the delete confirmation (required non-interactively with --delete-cluster)")
	return c
}

// spokeClusterDelete deletes one kind spoke's cluster — the single seam
// `spoke remove --delete-cluster` and `down`'s cascade share; tests stub it
// (the trust.go trustInstall pattern) to observe deletions without a
// container runtime. A zero GatewaySpec: spoke clusters map no host ports.
var spokeClusterDelete = func(ctx context.Context, sp config.SpokeSpec, clusterName string) error {
	prov, err := cluster.New(sp.Cluster, config.GatewaySpec{})
	if err != nil {
		return err
	}
	return prov.Delete(ctx, clusterName)
}

// spokeDeleteCluster deletes a kind spoke's cluster after consent (S3 wired
// the real provider call; S1 shipped the consent path).
func spokeDeleteCluster(c *cobra.Command, cubeName string, s config.SpokeSpec, yes bool) error {
	if !yes {
		// Prompt doctrine: Confirm's Default (false) is returned verbatim on
		// a non-TTY, so bare --delete-cluster in a script refuses with the
		// CUBE-0010 flag twin instead of hanging.
		ok, err := ui.Confirm(c.InOrStdin(), c.OutOrStdout(), ui.ConfirmOpts{
			Title: fmt.Sprintf("delete kind cluster %s-spoke-%s?", cubeName, s.Name),
		})
		if err != nil {
			return err
		}
		if !ok {
			return diag.New(diag.CodeConfirmRequired, "spoke cluster deletion not confirmed", "re-run with --yes to skip the prompt")
		}
	}
	name := cubeName + "-spoke-" + s.Name
	if err := spokeClusterDelete(c.Context(), s, name); err != nil {
		return diag.Wrap(err, diag.CodeSpokeEnsureFailed,
			fmt.Sprintf("kind cluster %s deletion failed", name),
			"retry, or delete manually: kind delete cluster --name "+name)
	}
	p := ui.NewFor(c.OutOrStdout())
	p.Step("spoke", "kind cluster %s deleted", name)
	return nil
}
