package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/upgrade"
)

func newUpgradeCmd() *cobra.Command {
	var file string
	var plan bool
	c := &cobra.Command{
		Use:   "upgrade --plan",
		Short: "Preview available pack updates and pending changes (apply them with `cube-idp up`)",
		RunE: func(c *cobra.Command, _ []string) error {
			if !plan {
				return fmt.Errorf("cube-idp has no separate apply step: re-running `cube-idp up` IS the upgrade.\nUse `cube-idp upgrade --plan` to preview what it would change")
			}
			changed, err := upgrade.Plan(c.Context(), file, c.OutOrStdout())
			if err != nil {
				return err
			}
			if changed {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&plan, "plan", false, "show the plan (required)")
	return c
}
