package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

func newInitCmd() *cobra.Command {
	var name string
	var local string
	var engineType string
	c := &cobra.Command{
		Use:   "init",
		Short: "Write the default cube.yaml (kind + flux + traefik + gitea + argocd, D9)",
		RunE: func(c *cobra.Command, _ []string) error {
			if _, err := os.Stat("cube.yaml"); err == nil {
				return diag.New(diag.CodeInitExists, "cube.yaml already exists — refusing to overwrite",
					"remove or rename the existing cube.yaml, or edit it directly and re-run `cube-idp up`")
			}
			cube := config.Default(name)
			cube.Spec.Engine.Type = engineType
			// engine.type: argocd installs Argo CD itself (UI included), so
			// the argocd pack would trip CUBE-0005 (redundant pack).
			if engineType == "argocd" {
				cube.Spec.Packs = []config.PackRef{
					{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"},
				}
			}
			if local != "" {
				abs, err := filepath.Abs(local)
				if err != nil {
					return fmt.Errorf("resolving --local %q: %w", local, err)
				}
				// Point at the checkout's own packs/ dir instead of the
				// released OCI refs, so `up` works against uncommitted or
				// unpublished pack changes.
				cube.Spec.Gateway.Ref = filepath.Join(abs, "packs", "traefik")
				cube.Spec.Packs = []config.PackRef{
					{Ref: filepath.Join(abs, "packs", "gitea")},
				}
				if engineType != "argocd" {
					cube.Spec.Packs = append(cube.Spec.Packs, config.PackRef{Ref: filepath.Join(abs, "packs", "argocd")})
				}
			}
			out, err := yaml.Marshal(cube)
			if err != nil {
				return err
			}
			if err := os.WriteFile("cube.yaml", out, 0o644); err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), "wrote cube.yaml — run `cube-idp up`")
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "dev", "cube name")
	c.Flags().StringVar(&local, "local", "", "path to a cube-idp repo checkout; writes local packs/ paths instead of released OCI refs")
	c.Flags().StringVar(&engineType, "engine", "flux", "gitops engine: flux | argocd")
	return c
}
