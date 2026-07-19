package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/cfgload"
	"github.com/cube-idp/cube-idp/internal/trust"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// seams for tests — the OS trust store must never be touched by `go test`.
var (
	trustInstall   = trust.InstallOS
	trustUninstall = trust.UninstallOS
	trustDir       = trust.Dir
)

func newTrustCmd() *cobra.Command {
	var yes, uninstall bool
	var file string
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
				// Name the CONFIGURED gateway.host (same -f/--file
				// convention every other command uses) rather than a
				// hardcoded default that's wrong the moment a user sets a
				// different host. No cube.yaml loadable yet (e.g. `trust`
				// run before `init`) falls back to generic wording instead
				// of asserting a host cube-idp can't back up.
				subject := "your cube-idp gateway's HTTPS"
				if cube, cerr := cfgload.Load(c.Context(), file); cerr == nil {
					subject = "https://*." + cube.Spec.Gateway.Host
				}
				desc := "This adds the cube-idp local CA to your OS trust stores so browsers accept\n" +
					subject + " without warnings (mkcert mechanism).\n" +
					"It is fully removed by `cube-idp trust --uninstall` or `cube-idp down`."
				if ui.PromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
					ok, err := ui.Confirm(c.InOrStdin(), c.OutOrStdout(),
						ui.ConfirmOpts{Title: "Trust the cube-idp local CA?", Description: desc})
					if err != nil {
						return err
					}
					if !ok {
						fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
						return nil
					}
					fmt.Fprintln(c.OutOrStdout(), "  hint: cube-idp trust --yes") // flag-twin hint (spec Decision 4)
				} else {
					// non-TTY fallback: byte-identical to the pre-migration prompt
					fmt.Fprint(c.OutOrStdout(), desc+"\nProceed? [y/N] ")
					line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
					if strings.ToLower(strings.TrimSpace(line)) != "y" {
						fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
						return nil
					}
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
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
