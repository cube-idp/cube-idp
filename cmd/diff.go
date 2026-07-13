package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/diff"
)

func newDiffCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "diff",
		Short: "Show what a re-run of `up` would change; exit 1 if anything would",
		RunE: func(c *cobra.Command, _ []string) error {
			changed, err := diff.Run(c.Context(), file, c.OutOrStdout())
			if err != nil {
				return err
			}
			if changed {
				os.Exit(1) // kubectl-diff convention; output is already flushed
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
