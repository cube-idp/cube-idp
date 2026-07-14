package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/trust"
)

// seams for tests — the OS trust store must never be touched by `go test`.
var (
	trustInstall   = trust.InstallOS
	trustUninstall = trust.UninstallOS
	trustDir       = trust.Dir
)

func newTrustCmd() *cobra.Command {
	var yes, uninstall bool
	c := &cobra.Command{
		Use:   "trust",
		Short: "Add (or remove, --uninstall) the cube-idp local CA to your OS trust stores — opt-in, fully reverted by `down` (D6)",
		RunE: func(c *cobra.Command, _ []string) error {
			dir, err := trust.Dir()
			if err != nil {
				return err
			}
			if uninstall {
				if err := trustUninstall(dir); err != nil {
					return err
				}
				fmt.Fprintln(c.OutOrStdout(), "cube-idp CA removed from OS trust stores")
				return nil
			}
			if !yes {
				fmt.Fprint(c.OutOrStdout(),
					"This adds the cube-idp local CA to your OS trust stores so browsers accept\n"+
						"https://*."+"cube-idp.localtest.me without warnings (mkcert mechanism).\n"+
						"It is fully removed by `cube-idp trust --uninstall` or `cube-idp down`.\nProceed? [y/N] ")
				line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
				if strings.ToLower(strings.TrimSpace(line)) != "y" {
					fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
					return nil
				}
			}
			if err := trustInstall(dir); err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), "cube-idp CA is now trusted by this machine")
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "skip the consent prompt")
	c.Flags().BoolVar(&uninstall, "uninstall", false, "remove the CA from the OS trust stores")
	return c
}
