package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	huh "charm.land/huh/v2"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/ui"
)

// cubeNameRe mirrors internal/config/schema.cue's metadata.name pattern —
// the wizard validates interactively against the same rule Load() enforces,
// so a name accepted by the wizard is never rejected later by `up`.
var cubeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

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

			includeGitea := true
			if wizardApplicable(c) {
				if err := runInitWizard(c, &name, &engineType, &includeGitea); err != nil {
					return err
				}
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
			// includeGitea only ever turns false via the wizard's confirm
			// field (there is no --include-gitea flag, so non-interactive
			// runs — every existing flag-driven test, the e2e suite, CI —
			// keep today's D9 default profile unchanged); applied last so it
			// strips the gitea pack regardless of --local/--engine shape.
			if !includeGitea {
				cube.Spec.Packs = withoutGiteaPack(cube.Spec.Packs)
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

// withoutGiteaPack drops the gitea pack ref (OCI or --local path — both
// contain "gitea" as their pack directory/image name) from packs.
func withoutGiteaPack(packs []config.PackRef) []config.PackRef {
	kept := make([]config.PackRef, 0, len(packs))
	for _, p := range packs {
		if strings.Contains(p.Ref, "gitea") {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// wizardApplicable reports whether it is safe and appropriate to prompt
// interactively: both stdin and stdout must be real terminals (never true
// in CI, in `go test`, or when init's output is piped — the e2e suite pipes
// this command, so it must never block), and neither --name nor --engine
// was explicitly passed (flags always win over the wizard).
func wizardApplicable(c *cobra.Command) bool {
	if c.Flags().Changed("name") || c.Flags().Changed("engine") {
		return false
	}
	return ui.IsTerminal(c.InOrStdin()) && ui.IsTerminal(c.OutOrStdout())
}

// runInitWizard runs a single huh form — three fields (name, engine,
// include-gitea) — prefilled with the current (D9 default profile) values,
// and writes the answers back into *name/*engineType/*includeGitea.
func runInitWizard(c *cobra.Command, name, engineType *string, includeGitea *bool) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Cube name").
				Value(name).
				Validate(func(v string) error {
					if !cubeNameRe.MatchString(v) {
						return fmt.Errorf("must match %s", cubeNameRe.String())
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("GitOps engine").
				Options(
					huh.NewOption("flux", "flux"),
					huh.NewOption("argocd", "argocd"),
				).
				Value(engineType),
			huh.NewConfirm().
				Title("Include the gitea pack?").
				Affirmative("yes").
				Negative("no").
				Value(includeGitea),
		),
	).WithOutput(c.OutOrStdout()).WithInput(c.InOrStdin())
	return form.Run()
}
