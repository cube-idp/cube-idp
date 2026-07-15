package cmd

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/plugin"
	"github.com/rafpe/cube-idp/internal/ui"
)

// pluginNameRe is the charset every plugin name must satisfy on `plugin
// trust`/`plugin install`: lowercase letters, digits, and hyphens, matching
// the cube-idp-<name> binary naming convention. Guards against
// option-shaped ("-flag") or path-shaped ("../evil") names reaching
// plugin.Lookup/Trust/Install.
var pluginNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validatePluginName refuses any name outside pluginNameRe before it is
// ever handed to a lookup, a trust-store write, or an index clone/exec —
// closing the `../`-shaped-name path-escape (self-inflicted only, still
// worth closing).
func validatePluginName(name string) error {
	if !pluginNameRe.MatchString(name) {
		return diag.New(diag.CodePluginNameInvalid,
			fmt.Sprintf("invalid plugin name %q", name),
			"plugin names are lowercase letters, digits, and hyphens (cube-idp-<name> binaries)")
	}
	return nil
}

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
		// RunPipelineStatic owns the whole RunE body (Task R3): plugin list
		// is a short static command, never a live step-tree.
		RunE: func(c *cobra.Command, _ []string) error {
			return ui.RunPipelineStatic(c.Context(), "plugin", c.OutOrStdout(),
				func(_ context.Context, con *ui.Console) error {
					con.Start("plugin", "")
					descs := plugin.List()
					if len(descs) == 0 {
						con.Warn("no plugins found — install a cube-idp-<name> binary on PATH")
						return nil
					}
					var buf bytes.Buffer
					w := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)
					fmt.Fprint(w, "NAME\tPATH\tTRUSTED\n")
					for _, d := range descs {
						trusted := "no"
						if d.Trusted {
							trusted = "yes"
						}
						fmt.Fprintf(w, "%s\t%s\t%s\n", d.Name, d.Path, trusted)
					}
					if err := w.Flush(); err != nil {
						return err
					}
					con.Note("%s", strings.TrimRight(buf.String(), "\n"))
					return nil
				})
		},
	}
}

func newPluginTrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust <name>",
		Short: "Trust a discovered plugin: record its current sha256 so it runs without prompting",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return ui.RunPipelineStatic(c.Context(), "plugin", c.OutOrStdout(),
				func(_ context.Context, con *ui.Console) error {
					con.Start("plugin", "")
					name := args[0]
					if err := validatePluginName(name); err != nil {
						return err
					}
					path, ok := plugin.Lookup(name)
					if !ok {
						return diag.New(diag.CodePluginNotFound,
							fmt.Sprintf("no cube-idp-%s plugin found on PATH or in the plugin install dir", name),
							"install the plugin binary, or run `cube-idp plugin list` to see what's discovered")
					}
					if err := plugin.Trust(name, path); err != nil {
						return err
					}
					con.Note("✔ plugin %q (%s) is now trusted", name, path)
					return nil
				})
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
			return ui.RunPipelineStatic(c.Context(), "plugin", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("plugin", "")
					name := args[0]
					if err := validatePluginName(name); err != nil {
						return err
					}
					if index == "" {
						return diag.New(diag.CodePluginTrustIO, "no plugin index configured",
							"pass --index <git-url>[@commit]; a default public index is planned but not yet published")
					}
					if err := plugin.Install(ctx, index, name); err != nil {
						return err
					}
					con.Note("✔ plugin %q installed and trusted", name)
					return nil
				})
		},
	}
	install.Flags().StringVar(&index, "index", "", "git URL of the plugin index (optionally @commit-pinned)")
	return install
}
