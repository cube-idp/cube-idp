// Package cli holds ALL cobra wiring and user-facing rendering for
// cube-idp. Commands only map flags and call domain packages — business
// logic never lives here.
package cli

import (
	"github.com/spf13/cobra"
)

func newRootCmd(factory provisionerFactory) *cobra.Command {
	root := &cobra.Command{
		Use:           "cube-idp",
		Short:         "cube-idp — internal developer platform, from a single config",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringP("config", "f", "cube.yaml", "path to the Config document")
	root.AddCommand(newConfigCmd(), newInitCmd(factory))
	return root
}
