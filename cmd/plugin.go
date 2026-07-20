package cmd

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/plugin"
	"github.com/cube-idp/cube-idp/internal/ui"
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

// newPluginCmd groups the exec-plugin discovery commands (the krew-style
// second tier of the CLI surface, below the built-in commands):
// 2): `plugin list` shows every cube-idp-<name> binary found on $PATH or in
// plugin.InstallDir(), `plugin trust <name>` records the current sha256 of
// a discovered plugin so it runs without an interactive prompt, and
// `plugin install <name>` fetches one from a sha256-pinned git index
// index. Running an unknown top-level command (`cube-idp <name>`)
// itself is handled by Execute's fallthrough in root.go, not here.
func newPluginCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "plugin",
		Short: "Discover and manage cube-idp exec-plugins",
	}
	root.AddCommand(newPluginListCmd())
	root.AddCommand(newPluginTrustCmd())
	root.AddCommand(newPluginInstallCmd())
	root.AddCommand(newPluginSearchCmd())
	return root
}

func newPluginListCmd() *cobra.Command {
	var available bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List every cube-idp-<name> plugin discovered on PATH or in the plugin install dir",
		// RunPipelineStatic owns the whole RunE body (Task R3): plugin list
		// is a short static command, never a live step-tree.
		RunE: func(c *cobra.Command, _ []string) error {
			return ui.RunPipelineStatic(c.Context(), "plugin", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("plugin", "")
					// --available reads the official index (P10) instead of the
					// local filesystem: the two are orthogonal (what's published
					// vs. what's installed).
					if available {
						return renderAvailable(ctx, con, "")
					}
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
	c.Flags().BoolVar(&available, "available", false, "list every plugin published in the official index")
	return c
}

// newPluginSearchCmd filters the official index by a substring of the plugin
// name or description — the discovery twin of `pack search`.
func newPluginSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <term>",
		Short: "Search the official plugin index by name or description",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return ui.RunPipelineStatic(c.Context(), "plugin", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("plugin", "")
					return renderAvailable(ctx, con, args[0])
				})
		},
	}
}

// renderAvailable fetches the official index and prints a NAME/VERSION/
// DESCRIPTION table, optionally filtered to rows whose name or description
// contains term (case-insensitive). Shared by `plugin list --available` and
// `plugin search`.
func renderAvailable(ctx context.Context, con *ui.Console, term string) error {
	idx, err := plugin.FetchPluginIndex(ctx)
	if err != nil {
		return err
	}
	term = strings.ToLower(term)
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)
	fmt.Fprint(w, "NAME\tVERSION\tDESCRIPTION\n")
	rows := 0
	for _, p := range idx.Plugins {
		if term != "" && !strings.Contains(strings.ToLower(p.Name), term) &&
			!strings.Contains(strings.ToLower(p.Description), term) {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Version, p.Description)
		rows++
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if rows == 0 {
		con.Warn("no plugins match %q in the official index", term)
		return nil
	}
	con.Note("%s", strings.TrimRight(buf.String(), "\n"))
	return nil
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

// newPluginInstallCmd installs a plugin. By default (P10) it resolves the
// official OCI index (oci://ghcr.io/cube-idp/plugins/index:latest, override
// via CUBE_IDP_PLUGIN_INDEX), pulls the current-platform binary BY DIGEST,
// and hands off to the sha256 trust-consent flow. Passing --index keeps the
// original sha256-pinned git-index path working unchanged.
func newPluginInstallCmd() *cobra.Command {
	var index string
	var yes bool
	install := &cobra.Command{
		Use:   "install <name>[@version]",
		Short: "Install a plugin from the official index (or a sha256-pinned git index with --index)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return ui.RunPipelineStatic(c.Context(), "plugin", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("plugin", "")
					// Split an optional @version off the name; the git-index
					// path has no versions so it ignores any suffix (kept whole
					// as the name for that path's own charset guard).
					name, version, _ := strings.Cut(args[0], "@")
					if index != "" {
						// git-index path: names carry no @version.
						if err := validatePluginName(args[0]); err != nil {
							return err
						}
						if err := plugin.Install(ctx, index, args[0]); err != nil {
							return err
						}
						con.Note("✔ plugin %q installed and trusted", args[0])
						return nil
					}
					if err := validatePluginName(name); err != nil {
						return err
					}
					// The official-index install writes the binary then hands
					// off to the trust-consent seam: --yes records trust
					// directly (flag consent), else prompt (TTY) / refuse
					// CUBE-7104 (non-TTY). PromptsAllowed keys the same gate
					// every other prompt-owning command uses.
					interactive := ui.PromptsAllowed(c.InOrStdin(), c.OutOrStdout())
					if err := plugin.InstallFromIndex(ctx, name, version, yes, interactive); err != nil {
						return err
					}
					con.Note("✔ plugin %q installed and trusted", name)
					return nil
				})
		},
	}
	install.Flags().StringVar(&index, "index", "", "install from this sha256-pinned git index instead of the official OCI index")
	install.Flags().BoolVar(&yes, "yes", false, "trust the installed plugin without prompting (official-index installs only)")
	return install
}
