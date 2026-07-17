package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui"
	"github.com/cube-idp/cube-idp/internal/upgrade"
)

func newUpgradeCmd() *cobra.Command {
	var file string
	var plan bool
	c := &cobra.Command{
		Use:   "upgrade --plan",
		Short: "Preview available pack updates and pending changes (apply them with `cube-idp up`)",
		RunE: func(c *cobra.Command, _ []string) error {
			if !plan {
				return diag.New(diag.CodeUpgradeGuard,
					"cube-idp has no separate apply step: re-running `cube-idp up` IS the upgrade.",
					"Use `cube-idp upgrade --plan` to preview what it would change")
			}
			changed, err := upgrade.Plan(c.Context(), file, c.OutOrStdout())
			if err != nil {
				return err
			}
			if changed {
				// WP5: after the plan has reported drift on a real TTY,
				// offer to apply it — the prompt runs BEFORE any pipeline
				// (spec Decision 5) and never fires on non-TTY/plain runs,
				// whose drift-exit semantics stay exactly as they were
				// (exit-path hygiene is T08's business).
				if ui.PromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
					ok, cerr := ui.Confirm(c.InOrStdin(), c.OutOrStdout(), ui.ConfirmOpts{
						Title: "apply now (runs cube-idp up)?", Default: false})
					if cerr != nil {
						return cerr
					}
					if ok {
						fmt.Fprintln(c.OutOrStdout(), "  hint: cube-idp up") // flag-twin hint (spec Decision 4)
						return runUpPipeline(c, file, "")
					}
				}
				return errExitCode(1)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&plan, "plan", false, "show the plan (required)")
	return c
}
