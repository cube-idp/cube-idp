package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

// fakePlugin writes an executable cube-idp-<name> into dir that dumps its
// env and args, exiting with the given code.
func fakePlugin(t *testing.T, dir, name string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("plugin exec tests are unix-only")
	}
	p := filepath.Join(dir, "cube-idp-"+name)
	script := "#!/bin/sh\necho \"CUBE_IDP_CUBE_NAME=$CUBE_IDP_CUBE_NAME\"\nexit " +
		string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLookupFindsPathBinaries(t *testing.T) {
	dir := t.TempDir()
	fakePlugin(t, dir, "hello", 0)
	t.Setenv("PATH", dir)
	if p, ok := Lookup("hello"); !ok || p != filepath.Join(dir, "cube-idp-hello") {
		t.Fatalf("lookup: %q %v", p, ok)
	}
	if _, ok := Lookup("absent"); ok {
		t.Fatal("found a plugin that does not exist")
	}
}

func TestExecRefusesUntrustedWhenNonInteractive(t *testing.T) {
	dir := t.TempDir()
	p := fakePlugin(t, dir, "hello", 0)
	t.Setenv("HOME", t.TempDir()) // empty trust store
	t.Setenv("XDG_CONFIG_HOME", "")
	err := Exec(context.Background(), p, nil, Env{CubeName: "dev"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7104" {
		t.Fatalf("want CUBE-7104, got %v", err)
	}
}

func TestExecRunsTrustedPluginAndPropagatesExit(t *testing.T) {
	dir := t.TempDir()
	p := fakePlugin(t, dir, "boom", 3)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := Trust("boom", p); err != nil {
		t.Fatal(err)
	}
	err := Exec(context.Background(), p, nil, Env{CubeName: "dev"})
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want ExitError code 3, got %v", err)
	}
}

func TestTrustDetectsChangedBinary(t *testing.T) {
	dir := t.TempDir()
	p := fakePlugin(t, dir, "mutant", 0)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := Trust("mutant", p); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755) // binary changed after trust
	err := EnsureTrusted("mutant", p, false)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7104" {
		t.Fatalf("changed binary must re-require trust: got %v", err)
	}
}
