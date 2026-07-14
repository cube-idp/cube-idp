package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/plugin"
	"github.com/rafpe/cube-idp/internal/ui"
)

// newPluginCmd groups the exec-plugin discovery commands (spec §4.4 tier
// 2): `plugin list` shows every cube-idp-<name> binary found on $PATH or in
// plugin.InstallDir(), `plugin trust <name>` records the current sha256 of
// a discovered plugin so it runs without an interactive prompt, and
// `plugin install <name>` fetches one from a sha256-pinned git index
// (Task 9). Running an unknown top-level command (`cube-idp <name>`)
// itself is handled by Execute's fallthrough in root.go, not here.
func newPluginCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "plugin",
		Short: "Discover and manage cube-idp exec-plugins (spec §4.4 tier 2)",
	}
	root.AddCommand(newPluginListCmd())
	root.AddCommand(newPluginTrustCmd())
	root.AddCommand(newPluginInstallCmd())
	return root
}

func newPluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every cube-idp-<name> plugin discovered on PATH or in the plugin install dir",
		RunE: func(c *cobra.Command, _ []string) error {
			descs := plugin.List()
			out := c.OutOrStdout()
			p := ui.NewFor(out)
			if len(descs) == 0 {
				p.Warn("no plugins found — install a cube-idp-<name> binary on PATH")
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
			fmt.Fprint(w, "NAME\tPATH\tTRUSTED\n")
			for _, d := range descs {
				trusted := "no"
				if d.Trusted {
					trusted = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", d.Name, d.Path, trusted)
			}
			return w.Flush()
		},
	}
}

func newPluginTrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust <name>",
		Short: "Trust a discovered plugin: record its current sha256 so it runs without prompting",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			path, ok := plugin.Lookup(name)
			if !ok {
				return diag.New(diag.CodePluginNotFound,
					fmt.Sprintf("no cube-idp-%s plugin found on PATH or in the plugin install dir", name),
					"install the plugin binary, or run `cube-idp plugin list` to see what's discovered")
			}
			if err := plugin.Trust(name, path); err != nil {
				return err
			}
			p := ui.NewFor(c.OutOrStdout())
			fmt.Fprintf(c.OutOrStdout(), "%s plugin %q (%s) is now trusted\n", p.Glyph(ui.GlyphOK), name, path)
			return nil
		},
	}
}

// newPluginInstallCmd installs a plugin from a sha256-pinned git index
// (Task 9, spec §4.4). There is deliberately no default index (RESOLVED
// 2026-07-14, Owner Decisions #8): --index is required until a first real
// index repo exists.
func newPluginInstallCmd() *cobra.Command {
	var index string
	install := &cobra.Command{
		Use:   "install <name>",
		Short: "Install a plugin from a sha256-pinned git index",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if index == "" {
				return diag.New(diag.CodePluginTrustIO, "no plugin index configured",
					"pass --index <git-url>[@commit]; a default public index is planned but not yet published")
			}
			name := args[0]
			if err := plugin.Install(c.Context(), index, name); err != nil {
				return err
			}
			p := ui.NewFor(c.OutOrStdout())
			fmt.Fprintf(c.OutOrStdout(), "%s plugin %q installed and trusted\n", p.Glyph(ui.GlyphOK), name)
			return nil
		},
	}
	install.Flags().StringVar(&index, "index", "", "git URL of the plugin index (optionally @commit-pinned)")
	return install
}
