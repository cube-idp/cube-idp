package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/plugin"
)

// isolatePluginEnv points PATH/HOME/XDG_* at fresh temp dirs so plugin
// discovery and the trust store never see the real machine's state, and
// returns the PATH dir for tests to drop fake plugin binaries into.
func isolatePluginEnv(t *testing.T) (pathDir string) {
	t.Helper()
	pathDir = t.TempDir()
	t.Setenv("PATH", pathDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	return pathDir
}

func writeFakePlugin(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("exec-plugin tests are unix-only")
	}
	p := filepath.Join(dir, "cube-idp-"+name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPluginListReportsDiscoveredAndTrustState(t *testing.T) {
	dir := isolatePluginEnv(t)
	p := writeFakePlugin(t, dir, "hello")
	if err := plugin.Trust("hello", p); err != nil {
		t.Fatal(err)
	}
	writeFakePlugin(t, dir, "untrusted")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.Contains(got, "NAME") || !strings.Contains(got, "TRUSTED") {
		t.Fatalf("expected a NAME/TRUSTED table header, got:\n%s", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "untrusted") {
		t.Fatalf("expected both discovered plugins listed, got:\n%s", got)
	}
}

func TestPluginListEmptyReportsNoPlugins(t *testing.T) {
	isolatePluginEnv(t)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no plugins found") {
		t.Fatalf("expected an empty-state notice, got:\n%s", out.String())
	}
}

func TestPluginTrustRecordsHashAndUnblocksExec(t *testing.T) {
	dir := isolatePluginEnv(t)
	writeFakePlugin(t, dir, "hello")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "trust", "hello"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "trusted") {
		t.Fatalf("expected a trust confirmation, got:\n%s", out.String())
	}

	// EnsureTrusted must now pass non-interactively — `plugin trust`
	// recorded exactly the hash Exec's own EnsureTrusted call will check.
	path, ok := plugin.Lookup("hello")
	if !ok {
		t.Fatal("plugin should still be discoverable after trust")
	}
	if err := plugin.EnsureTrusted("hello", path, false); err != nil {
		t.Fatalf("plugin trust hello should have unblocked EnsureTrusted: %v", err)
	}
}

func TestPluginTrustUnknownPluginReportsNotFound(t *testing.T) {
	isolatePluginEnv(t)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "trust", "nosuch"})
	err := root.Execute()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginNotFound {
		t.Fatalf("want CUBE-7101, got %v", err)
	}
}

// TestExecuteFallsThroughToTrustedPlugin covers Execute's own fallthrough
// (root.go), not just the `plugin` built-in commands above: a first
// argument that Find() doesn't recognize as a built-in must run the
// matching cube-idp-<name> binary. Execute reads os.Args directly (per the
// brief's exact fallthrough shape, matching cobra's own default when
// SetArgs was never called), so this test swaps it out for the duration of
// the call.
func TestExecuteFallsThroughToTrustedPlugin(t *testing.T) {
	dir := isolatePluginEnv(t)
	p := writeFakePlugin(t, dir, "hello")
	if err := plugin.Trust("hello", p); err != nil {
		t.Fatal(err)
	}

	restoreArgs := os.Args
	os.Args = []string{"cube-idp", "hello"}
	defer func() { os.Args = restoreArgs }()

	if err := Execute(context.Background()); err != nil {
		t.Fatalf("expected the trusted plugin to run cleanly, got %v", err)
	}
}

// TestExecuteUnknownCommandNoPluginReportsCUBE7101 covers the "neither a
// built-in command nor a discoverable plugin" case.
func TestExecuteUnknownCommandNoPluginReportsCUBE7101(t *testing.T) {
	isolatePluginEnv(t)

	restoreArgs := os.Args
	os.Args = []string{"cube-idp", "nosuchcommand"}
	defer func() { os.Args = restoreArgs }()

	err := Execute(context.Background())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginNotFound {
		t.Fatalf("want CUBE-7101, got %v", err)
	}
}

// TestPluginInstallWithoutIndexReportsCUBE7102 covers OWNER DECISION #8:
// there is no default index, so `plugin install <name>` without --index
// must fail fast with CUBE-7102 rather than reaching for a repo that does
// not exist.
func TestPluginInstallWithoutIndexReportsCUBE7102(t *testing.T) {
	isolatePluginEnv(t)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "install", "hello"})
	err := root.Execute()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginTrustIO {
		t.Fatalf("want CUBE-7102, got %v", err)
	}
}

// TestExecuteBuiltinCommandsStillDispatch guards against the fallthrough
// swallowing real built-in commands (e.g. because Find's error handling
// changed) — a known command must still run through cobra normally.
func TestExecuteBuiltinCommandsStillDispatch(t *testing.T) {
	restoreArgs := os.Args
	os.Args = []string{"cube-idp", "version"}
	defer func() { os.Args = restoreArgs }()

	if err := Execute(context.Background()); err != nil {
		t.Fatalf("built-in `version` command must still dispatch: %v", err)
	}
}
