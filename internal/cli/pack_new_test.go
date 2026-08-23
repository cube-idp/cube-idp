package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// targetDir is a path inside the test's own directory that does not exist yet.
func targetDir(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// A created pack renders immediately — the command's whole promise — and the
// confirmation names what was made and how to look at it.
func TestPackNewRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "raw is the default", args: nil, want: "(raw)"},
		{name: "kustomize", args: []string{"--type", "kustomize"}, want: "(kustomize)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := targetDir(t, "hello")

			code, stdout, stderr := run(t, append([]string{"pack", "new", dir}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
			}
			if !strings.Contains(stdout, tt.want) || !strings.Contains(stdout, "created pack hello") {
				t.Errorf("stdout = %q, want it to confirm a created pack %s", stdout, tt.want)
			}

			code, rendered, stderr := run(t, "pack", "render", dir)
			if code != 0 {
				t.Fatalf("render exit = %d, want 0 (stderr: %s)", code, stderr)
			}
			if !strings.Contains(rendered, "kind: ConfigMap") {
				t.Errorf("the new pack rendered %q, want a ConfigMap", rendered)
			}
		})
	}
}

// The scaffolded pack also passes validate, which renders and discards — so
// nothing about it is merely well-formed on the surface.
func TestPackNewValidates(t *testing.T) {
	dir := targetDir(t, "hello")
	if code, _, stderr := run(t, "pack", "new", dir); code != 0 {
		t.Fatalf("new exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	code, stdout, stderr := run(t, "pack", "validate", dir)
	if code != 0 {
		t.Fatalf("validate exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Errorf("stdout = %q, want validate to confirm the pack", stdout)
	}
}

// --name overrides the directory's base name.
func TestPackNewName(t *testing.T) {
	dir := targetDir(t, "traefik")

	code, stdout, stderr := run(t, "pack", "new", dir, "--name", "gateway")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "created pack gateway") {
		t.Errorf("stdout = %q, want the pack named gateway", stdout)
	}
}

// An existing target is refused with the pack domain's own code.
func TestPackNewExistingTarget(t *testing.T) {
	dir := t.TempDir()

	code, stdout, stderr := run(t, "pack", "new", dir)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing when the target is refused", stdout)
	}
	if !strings.Contains(stderr, "CUBE-PKG-022") {
		t.Errorf("stderr %q should carry CUBE-PKG-022", stderr)
	}
}

// --from forks an existing pack: the copy keeps the source's type and payload,
// takes the new name, and renders.
func TestPackNewFrom(t *testing.T) {
	source := targetDir(t, "source")
	if code, _, stderr := run(t, "pack", "new", source, "--type", "kustomize"); code != 0 {
		t.Fatalf("new source exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	forked := targetDir(t, "forked")
	code, stdout, stderr := run(t, "pack", "new", forked, "--from", "file://"+filepath.ToSlash(source), "--name", "forked")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "created pack forked") || !strings.Contains(stdout, "(kustomize)") {
		t.Errorf("stdout = %q, want a forked kustomize pack", stdout)
	}
	if _, err := os.Stat(filepath.Join(forked, "kustomization.yaml")); err != nil {
		t.Errorf("Stat(kustomization.yaml) = %v, want the payload copied", err)
	}

	if code, rendered, _ := run(t, "pack", "render", forked); code != 0 || !strings.Contains(rendered, "kind: ConfigMap") {
		t.Errorf("the fork rendered %q at exit %d, want a ConfigMap", rendered, code)
	}
}

// A backend this build does not implement keeps the reference leaf's code, so
// the error names the backend and where it lands rather than looking like a
// broken path.
func TestPackNewFromUnimplementedBackend(t *testing.T) {
	code, stdout, stderr := run(t, "pack", "new", targetDir(t, "x"), "--from", "oci://example.com/pack:1")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing when the source cannot be resolved", stdout)
	}
	if !strings.Contains(stderr, "CUBE-REF-008") {
		t.Errorf("stderr %q should carry CUBE-REF-008", stderr)
	}
}

// A fork keeps the type its source declares, so asking for a different one is
// not a conversion this command could perform.
func TestPackNewFromRejectsType(t *testing.T) {
	source := targetDir(t, "source")
	if code, _, stderr := run(t, "pack", "new", source); code != 0 {
		t.Fatalf("new source exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	code, stdout, stderr := run(t, "pack", "new", targetDir(t, "x"),
		"--from", "file://"+filepath.ToSlash(source), "--type", "kustomize")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing when the flags conflict", stdout)
	}
	if !strings.Contains(stderr, "--type") || !strings.Contains(stderr, "--from") {
		t.Errorf("stderr = %q, want it to name both flags", stderr)
	}
}
