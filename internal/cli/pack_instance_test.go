package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileRef is the reference spelling for a path on disk. Instance mode resolves
// every reference through the reference grammar, which recognises explicit
// forms only — so a config points at a local pack as file:///abs/path.
func fileRef(path string) string {
	return "file://" + filepath.ToSlash(path)
}

// writeDoc writes one document into the test's directory and returns its path.
func writeDoc(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeConfig writes a Config document carrying the given spec.packs body.
func writeConfig(t *testing.T, packs string) string {
	t.Helper()
	return writeTemp(t, "apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\n"+
		"spec:\n  packs:\n"+packs)
}

const instancePackCUE = `name:    "web"
version: "1"
type:    "kustomize"
namespace: "team"
#Values: {
	IMAGE!: string
}
`

// instancePack writes a kustomize pack whose rendered image comes from values,
// so the output proves the configured values reached the render.
func instancePack(t *testing.T) string {
	t.Helper()
	return writePack(t, instancePackCUE, map[string]string{
		"kustomization.yaml": "resources:\n- deploy.yaml\n",
		"deploy.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\n" +
			"spec:\n  template:\n    spec:\n      containers:\n      - name: app\n        image: ${IMAGE}\n",
	})
}

// Instance mode renders what the setup configures — values merged over the
// valuesRef document, external manifests in their groups, the pack's namespace
// on everything — while artifact mode renders the same pack as authored.
func TestPackRenderInstance(t *testing.T) {
	dir := instancePack(t)
	values := writeDoc(t, "values.yaml", "IMAGE: nginx:1.26\n")
	external := writeDoc(t, "svc.yaml", "apiVersion: v1\nkind: Service\nmetadata:\n  name: gateway\n")
	cfg := writeConfig(t, "  - id: web\n    packRef: "+fileRef(dir)+"\n"+
		"    valuesRef: "+fileRef(values)+"\n"+
		"    values:\n      IMAGE: nginx:1.27\n"+
		"    externalManifests:\n    - ref: "+fileRef(external)+"\n      lifecycle: pre\n")

	code, stdout, stderr := run(t, "pack", "render", "-f", cfg, "--id", "web")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("pack render wrote %q to stderr, want nothing on success", stderr)
	}

	// Prerequisites lead the stream, so the external Service comes first.
	if !strings.HasPrefix(stdout, "apiVersion: v1\nkind: Service\n") {
		t.Errorf("stdout does not lead with the prerequisite:\n%s", stdout)
	}
	for _, want := range []string{
		"image: nginx:1.27", // the inline values won over the valuesRef document
		"namespace: team",   // the pack's namespace reached both groups
		"kind: Deployment",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q; got:\n%s", want, stdout)
		}
	}
	if strings.Count(stdout, "namespace: team") != 2 {
		t.Errorf("want the pack namespace on both the Service and the Deployment; got:\n%s", stdout)
	}
}

// The requested instance may be named by its own id or, when the pack's name
// is unambiguous in the setup, by that name.
func TestPackRenderInstanceDefaultedID(t *testing.T) {
	dir := instancePack(t)
	cfg := writeConfig(t, "  - packRef: "+fileRef(dir)+"\n    values:\n      IMAGE: nginx\n")

	code, stdout, stderr := run(t, "pack", "render", "-f", cfg, "--id", "web")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "image: nginx") {
		t.Errorf("stdout is missing the rendered image; got:\n%s", stdout)
	}
}

// A mistyped id names the identities the setup actually has, because a
// defaulted id is one the user never wrote and is the likeliest to get wrong.
func TestPackRenderInstanceUnknownID(t *testing.T) {
	dir := instancePack(t)
	cfg := writeConfig(t, "  - packRef: "+fileRef(dir)+"\n    values:\n      IMAGE: nginx\n")

	code, stdout, stderr := run(t, "pack", "render", "-f", cfg, "--id", "wrong")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing written when rendering fails", stdout)
	}
	for _, want := range []string{"wrong", "web"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q should name both the bad id and the available ones", stderr)
		}
	}
}

// Two instances of one pack cannot both take its name, and the setup layer
// says so with its own code rather than picking one.
func TestPackRenderInstanceAmbiguousName(t *testing.T) {
	dir := instancePack(t)
	entry := "  - packRef: " + fileRef(dir) + "\n    values:\n      IMAGE: nginx\n"
	cfg := writeConfig(t, entry+entry)

	code, stdout, stderr := run(t, "pack", "render", "-f", cfg, "--id", "web")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing written when rendering fails", stdout)
	}
	if !strings.Contains(stderr, "CUBE-PKG-015") {
		t.Errorf("stderr %q should carry CUBE-PKG-015", stderr)
	}
}

// Every pack in the setup is read, so an unreadable one fails the preview even
// when another instance was asked for. That follows from identity: an entry's
// effective id depends on whether any other entry's pack shares its name, so
// the names all have to be read before any id is known.
//
// The consequence is deliberate and documented, and this pins it: the error
// names the entry that could not be read, which is a reference the user did
// not ask about.
func TestPackRenderInstanceUnreadableSibling(t *testing.T) {
	dir := instancePack(t)
	missing := filepath.Join(t.TempDir(), "absent")
	cfg := writeConfig(t,
		"  - id: web\n    packRef: "+fileRef(dir)+"\n    values:\n      IMAGE: nginx\n"+
			"  - id: other\n    packRef: "+fileRef(missing)+"\n")

	code, stdout, stderr := run(t, "pack", "render", "-f", cfg, "--id", "web")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing written when a pack cannot be read", stdout)
	}
	if !strings.Contains(stderr, "CUBE-PKG-001") {
		t.Errorf("stderr %q should carry CUBE-PKG-001", stderr)
	}
	if !strings.Contains(stderr, "absent") {
		t.Errorf("stderr %q should name the reference that could not be read", stderr)
	}
}

// The two forms are exclusive, and each way of mixing them says which one to
// drop rather than quietly preferring the other.
func TestPackRenderFormsAreExclusive(t *testing.T) {
	dir := instancePack(t)
	cfg := writeConfig(t, "  - packRef: "+fileRef(dir)+"\n    values:\n      IMAGE: nginx\n")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a reference and an id",
			args: []string{"pack", "render", dir, "--id", "web"},
			want: "not both",
		},
		{
			name: "a reference and a config document",
			args: []string{"pack", "render", dir, "-f", cfg},
			want: "not both",
		},
		{
			name: "an id with no config document",
			args: []string{"pack", "render", "--id", "web"},
			want: "add -f",
		},
		{
			name: "a config document with no id",
			args: []string{"pack", "render", "-f", cfg},
			want: "add --id",
		},
		{
			name: "neither form",
			args: []string{"pack", "render"},
			want: "pack render",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing written when the form is wrong", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.want)
			}
		})
	}
}

// A config problem stays a config problem: instance mode loads the same
// document `config validate` does, so it keeps that domain's code and its
// exit status.
func TestPackRenderInstanceConfigError(t *testing.T) {
	cfg := writeTemp(t, "apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: \"\"\nspec: {}\n")

	code, stdout, stderr := run(t, "pack", "render", "-f", cfg, "--id", "web")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a config error (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing written when the config is invalid", stdout)
	}
	if !strings.Contains(stderr, "CUBE-CFG-003") {
		t.Errorf("stderr %q should carry CUBE-CFG-003", stderr)
	}
}
