package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/plugin"
	"github.com/cube-idp/cube-idp/internal/registry"
	"github.com/cube-idp/cube-idp/internal/trust"
	"github.com/cube-idp/cube-idp/internal/ui"
)

func NewRootCmd() *cobra.Command {
	var plain bool
	var progress string
	root := &cobra.Command{
		Use:           "cube-idp",
		Short:         "cube-idp stands up an internal developer platform on Kubernetes and gets out of the way",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Resolves the output Mode once, before any subcommand's RunE, so
		// every Printer/renderer built downstream sees the resolved choice
		// without threading a bool through every orchestrator signature
		// (Task 13.8, extended by Task 14b's §6 resolve ladder). Stage B
		// (Task 14c) activates the --progress flag: rungs 1–3 of the ladder go
		// live, --plain stays a permanent alias (rung 4).
		PersistentPreRunE: func(*cobra.Command, []string) error {
			// Resolve the mode first — an unrecognized --progress value already
			// falls through the ladder (Resolve matches only json/plain/live),
			// so SetMode still reflects the real environment (TTY/CI/NO_COLOR).
			// The bad-value error then renders in the right mode (plain when
			// piped or in CI), instead of a styled panel on a machine pipe.
			_, noColor := os.LookupEnv("NO_COLOR")
			ui.SetMode(ui.Resolve(ui.Request{
				ProgressFlag: progress,
				PlainFlag:    plain,
				EnvProgress:  os.Getenv("CUBE_IDP_PROGRESS"),
				IsTTY:        ui.IsTerminal(os.Stdout),
				CIEnv:        os.Getenv("CI"),
				NoColor:      noColor,
				Term:         os.Getenv("TERM"),
			}))
			return validateProgressFlag(progress)
		},
	}
	root.PersistentFlags().BoolVar(&plain, "plain", false,
		"force plain, non-styled output (permanent alias for --progress=plain)")
	root.PersistentFlags().StringVar(&progress, "progress", "auto",
		"output style: auto|plain|live|json (json is EXPERIMENTAL until the config v1 freeze)")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newUpCmd())
	root.AddCommand(newDownCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newUpgradeCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newGetCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newPackCmd())
	root.AddCommand(newTrustCmd())
	root.AddCommand(newCnoeCmd())
	root.AddCommand(newPluginCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newRepoCmd())
	root.AddCommand(newVendorCmd())
	return root
}

// Execute runs the root command to completion using ctx for cancellation —
// main.go wires this to a SIGINT-cancelable context so long-running steps
// like `up` can unwind cleanly on Ctrl-C. Before dispatching to cobra, it
// intercepts a first argument that isn't a built-in command (spec §4.4 tier
// 2, exec-plugin fallthrough, krew model): a `cube-idp-<name>` binary on
// PATH or in plugin.InstallDir() runs in place of a "command not found"
// error. This only ever inspects os.Args[1] — a flag-shaped first argument
// (leading "-") is left to cobra untouched, and the --plain
// PersistentPreRunE hook only matters for the built-in-command path since a
// plugin invocation never runs through NewRootCmd's own RunE machinery.
func Execute(ctx context.Context) error {
	root := NewRootCmd()
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		if _, _, err := root.Find(os.Args[1:]); err != nil { // not a built-in command
			if path, ok := plugin.Lookup(os.Args[1]); ok {
				env, cleanup := pluginEnv(ctx)
				defer cleanup()
				return plugin.Exec(ctx, path, os.Args[2:], env)
			}
			return diag.New(diag.CodePluginNotFound,
				fmt.Sprintf("unknown command %q and no cube-idp-%s plugin found on PATH", os.Args[1], os.Args[1]),
				"run `cube-idp plugin list` to see discovered plugins, or `cube-idp --help` for built-in commands")
		}
	}
	return root.ExecuteContext(ctx)
}

// pluginEnv assembles the exec-plugin env contract (spec §4.4). It is
// best-effort BY DESIGN — plugins must run even with no cube.yaml or
// cluster around, so every step here degrades to an empty field instead of
// an error; a plugin that requires a field must detect and report its own
// absence. The returned cleanup removes the temp kubeconfig file (if one
// was written) once the plugin process has exited; callers must defer it.
func pluginEnv(ctx context.Context) (plugin.Env, func()) {
	noop := func() {}
	env := plugin.Env{}

	cube, err := config.Load("cube.yaml")
	if err != nil {
		return env, noop
	}
	env.CubeName = cube.Metadata.Name

	if dir, err := trust.Dir(); err == nil { // read-only: never creates a CA as a side effect of running a plugin
		caPath := filepath.Join(dir, "ca.crt")
		if _, err := os.Stat(caPath); err == nil {
			env.CA = caPath
		}
	}

	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return env, noop
	}
	exists, err := prov.Exists(ctx, cube.Metadata.Name)
	if err != nil || !exists {
		return env, noop
	}
	kubeconfig, err := prov.Kubeconfig(ctx, cube.Metadata.Name)
	if err != nil {
		return env, noop
	}
	f, err := os.CreateTemp("", "cube-idp-kubeconfig-*.yaml")
	if err != nil {
		return env, noop
	}
	cleanup := func() { os.Remove(f.Name()) }
	if err := os.Chmod(f.Name(), 0o600); err != nil { // explicit: CreateTemp's 0600 is "before umask" per its own doc
		f.Close()
		return env, cleanup
	}
	if _, err := f.Write(kubeconfig); err != nil {
		f.Close()
		return env, cleanup
	}
	f.Close()

	env.Kubeconfig = f.Name()
	// The plugin reaches zot through its own port-forward or, on a host
	// where the gateway hostname resolves, https://registry.<gateway.host>
	// (internal/registry.GatewayRoute) with CUBE_IDP_CA as the trust
	// anchor — documented in README's plugin section (Task 13).
	env.Registry = registry.InClusterURL
	return env, cleanup
}

// requireClusterExists guards read-only commands (status, get, down
// --keep-cluster) against side-effect cluster creation: both cluster-creating
// providers' (kind, k3d) Ensure CREATE a missing cluster, so any command that
// must never mutate calls this before Ensure. For provider "existing" Ensure
// was always read-only, so nothing is checked.
func requireClusterExists(ctx context.Context, prov cluster.Provider, provider, name string) error {
	if provider != "kind" && provider != "k3d" {
		return nil
	}
	exists, err := prov.Exists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return diag.New(diag.CodeClusterNotExists,
			fmt.Sprintf("%s cluster %q does not exist", provider, name),
			"run `cube-idp up` first")
	}
	return nil
}
