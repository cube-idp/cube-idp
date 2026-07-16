package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
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

// TestExecScrubsStaleContractEnvWhenFieldsOmitted covers the review's
// IMPORTANT finding: "omitted" contract fields must mean the plugin does
// not see the key AT ALL — a stale CUBE_IDP_* variable sitting in the
// operator's shell must not leak through os.Environ() into the child. The
// fake plugin exits 9 if any contract key other than the one deliberately
// set (CUBE_IDP_CUBE_NAME) is present in its environment; the `+x`
// expansion detects set-but-empty too.
func TestExecScrubsStaleContractEnvWhenFieldsOmitted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("plugin exec tests are unix-only")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "cube-idp-scrub")
	script := `#!/bin/sh
[ -n "${CUBE_IDP_KUBECONFIG+x}" ] && exit 9
[ -n "${CUBE_IDP_REGISTRY+x}" ] && exit 9
[ -n "${CUBE_IDP_CA+x}" ] && exit 9
[ "$CUBE_IDP_CUBE_NAME" = "dev" ] || exit 8
exit 0
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	// Stale contract vars in the parent environment, as if exported by the
	// operator's shell or a previous tool run.
	t.Setenv("CUBE_IDP_KUBECONFIG", "/stale/kubeconfig")
	t.Setenv("CUBE_IDP_REGISTRY", "stale.example:5000")
	t.Setenv("CUBE_IDP_CA", "/stale/ca.crt")
	if err := Trust("scrub", p); err != nil {
		t.Fatal(err)
	}
	if err := Exec(context.Background(), p, nil, Env{CubeName: "dev"}); err != nil {
		t.Fatalf("stale CUBE_IDP_* env leaked into the plugin (exit 9) or the set field was lost (exit 8): %v", err)
	}
}

// TestTrustKeyCanonicalization: recording trust through a symlinked or
// relative path and checking through the resolved absolute path (or vice
// versa) must agree — the store keys on Abs+EvalSymlinks canonical paths.
func TestTrustKeyCanonicalization(t *testing.T) {
	// store isolation — RESOLVED (Task 0): existing plugin tests inline this
	// exact triple (no shared helper); mirror it verbatim:
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	dir := t.TempDir()
	real := filepath.Join(dir, "cube-idp-demo")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link-to-demo")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Trust("demo", link); err != nil { // record via the symlink
		t.Fatal(err)
	}
	if !isTrusted(real) { // look up via the real path
		t.Fatal("trust recorded via a symlink must be visible via the canonical path")
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
