package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// spoke: declarative hub/spoke registration (Phase 5 spec §5, decision 9).
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
		Short: "List spokes declared in cube.yaml",
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
			for _, s := range cube.Spec.Spokes {
				ctx := s.Cluster.Context
				if ctx == "" {
					ctx = "-"
				}
				fmt.Fprintf(out, "%-20s %-10s %s\n", s.Name, s.Cluster.Provider, ctx)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
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

// spokeDeleteCluster deletes a kind spoke's cluster after consent. The
// provider call arrives in S3; until then the consent path is real and the
// deletion reports a clear not-yet error so --delete-cluster is never a
// silent no-op.
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
	return diag.New(diag.CodeSpokeProviderUnsupported,
		"spoke cluster deletion ships in a later task of this plan (S3)",
		"delete manually: kind delete cluster --name "+cubeName+"-spoke-"+s.Name)
}
