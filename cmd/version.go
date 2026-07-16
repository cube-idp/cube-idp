package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version, Commit and Date are stamped at release time via
// -ldflags "-X github.com/cube-idp/cube-idp/cmd.Version=… -X github.com/cube-idp/cube-idp/cmd.Commit=… -X github.com/cube-idp/cube-idp/cmd.Date=…"
// (.goreleaser.yaml). Defaults describe a plain `go build`.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the cube-idp version",
		RunE: func(c *cobra.Command, _ []string) error {
			fmt.Fprintf(c.OutOrStdout(), "cube-idp version %s (commit %s, built %s)\n", Version, Commit, Date)
			return nil
		},
	}
}
