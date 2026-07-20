package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// Env is the plugin process's env contract — the CUBE_IDP_* variables an
// exec plugin may rely on, plus the cube-idp CA.
// for CA). Empty fields are omitted from the child's environment entirely —
// a plugin that requires one must detect and report its own absence; no
// cube-idp error is raised here, so cluster-independent plugins keep
// working with no cube.yaml/cluster around.
type Env struct{ Kubeconfig, CubeName, Registry, CA string }

// Exec replaces cube-idp's process semantics with the plugin's: it inherits
// stdio, receives the env contract, and its exit code is propagated
// verbatim. Refuses untrusted plugins (CUBE-7104) unless the trust store
// already approves the current binary or the user confirms interactively.
func Exec(ctx context.Context, path string, args []string, env Env) error {
	name := strings.TrimPrefix(filepath.Base(path), pluginPrefix)
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	if err := EnsureTrusted(name, path, interactive); err != nil {
		return err
	}

	contract := map[string]string{
		"CUBE_IDP_KUBECONFIG": env.Kubeconfig,
		"CUBE_IDP_CUBE_NAME":  env.CubeName,
		"CUBE_IDP_REGISTRY":   env.Registry,
		"CUBE_IDP_CA":         env.CA,
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Inherit the parent environment MINUS every contract key: an omitted
	// field must mean the plugin does not see the key at all, so a stale
	// CUBE_IDP_* exported in the operator's shell (or by a previous tool
	// run) can never leak through as if cube-idp had set it.
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, isContract := contract[name]; isContract {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	for k, v := range contract {
		if v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return err // the plugin's own failure: propagate the exit code verbatim, do NOT wrap — its own output is its diagnosis
		}
		return diag.Wrap(err, diag.CodePluginExecFail, fmt.Sprintf("plugin %q failed to execute", name),
			"check that the plugin binary is executable and built for this platform")
	}
	return nil
}
