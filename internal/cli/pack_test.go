package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePack materialises a pack on disk and returns its directory. The CLI
// edge is the only layer that touches the OS filesystem, so its tests are the
// only ones that need real files.
func writePack(t *testing.T, cue string, manifests map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.cue"), []byte(cue), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range manifests {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const helloPackCUE = `name:    "hello"
version: "0.1.0"
type:    "raw"
`

func helloManifests() map[string]string {
	return map[string]string{
		"manifests/a-namespace.yaml": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: hello\n",
		"manifests/b-config.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: hello\n" +
			"  namespace: hello\ndata:\n  greeting: hi\n",
	}
}

// TestPackRenderGolden is the byte-exact check of render's stdout: object
// order, document separators, key order, and the final newline are all part
// of the contract, because the output is piped into kubectl.
func TestPackRenderGolden(t *testing.T) {
	dir := writePack(t, helloPackCUE, helloManifests())

	code, stdout, stderr := run(t, "pack", "render", dir)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("pack render wrote %q to stderr, want nothing on success", stderr)
	}

	want, err := os.ReadFile(filepath.Join("testdata", "pack-render.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(want) {
		t.Errorf("pack render output differs from testdata/pack-render.golden:\ngot:\n%s\nwant:\n%s", stdout, want)
	}
}

// Rendering the same pack twice must produce identical bytes.
func TestPackRenderIsReproducible(t *testing.T) {
	dir := writePack(t, helloPackCUE, helloManifests())

	_, first, _ := run(t, "pack", "render", dir)
	for i := range 3 {
		if _, got, _ := run(t, "pack", "render", dir); got != first {
			t.Fatalf("pack render run %d differs from the first:\ngot:\n%s\nwant:\n%s", i, got, first)
		}
	}
}

// A failed render must leave stdout completely untouched — a partial stream
// piped into kubectl would apply half a pack.
func TestPackRenderNoPartialStdoutOnFailure(t *testing.T) {
	tests := []struct {
		name      string
		cue       string
		manifests map[string]string
		wantCode  int
		wantErr   string
	}{
		{
			name: "one good manifest and one unparseable",
			cue:  helloPackCUE,
			manifests: map[string]string{
				"manifests/a-good.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: good\n",
				"manifests/b-bad.yaml":  "key: value\n  bad: indent\n",
			},
			wantCode: 1,
			wantErr:  "CUBE-PKG-005",
		},
		{
			name: "namespace conflict after objects already parsed",
			cue:  "name: \"hello\"\nversion: \"0.1.0\"\ntype: \"raw\"\nnamespace: \"team\"\n",
			manifests: map[string]string{
				"manifests/a.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n",
				"manifests/b.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n  namespace: other\n",
			},
			wantCode: 1,
			wantErr:  "CUBE-PKG-008",
		},
		{
			name:      "pack renders nothing",
			cue:       helloPackCUE,
			manifests: map[string]string{"manifests/README.md": "not a manifest"},
			wantCode:  1,
			wantErr:   "CUBE-PKG-007",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writePack(t, tt.cue, tt.manifests)
			code, stdout, stderr := run(t, "pack", "render", dir)
			if code != tt.wantCode {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, tt.wantCode, stderr)
			}
			if stdout != "" {
				t.Errorf("pack render wrote %q to stdout on failure, want nothing", stdout)
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("stderr %q should carry %s", stderr, tt.wantErr)
			}
		})
	}
}

// Every user-reaching failure is a coded error on stderr with exit 1: pack
// problems are never config-document problems, so they never map to exit 2.
func TestPackErrorsAreCodedOnStderr(t *testing.T) {
	tests := []struct {
		name string
		verb string
		// ref, when set, is used verbatim. Otherwise a pack is written from
		// packCUE + files, or — when packCUE is empty too — an empty
		// directory stands in.
		ref     string
		packCUE string
		files   map[string]string
		wantErr string
	}{
		{
			name:    "remote scheme is not resolvable in this build",
			verb:    "render",
			ref:     "oci://registry.example/monitoring:2.1.0",
			wantErr: "CUBE-PKG-001",
		},
		{
			name:    "directory without a pack.cue",
			verb:    "render",
			wantErr: "CUBE-PKG-001",
		},
		{
			name:    "helm render is not implemented in this build",
			verb:    "render",
			packCUE: "name: \"h\"\nversion: \"1\"\ntype: \"helm\"\n",
			files:   map[string]string{"Chart.yaml": "name: h\n"},
			wantErr: "CUBE-PKG-020",
		},
		{
			// kustomize resolves remote references over the network and offers
			// no switch to stop it, so the payload is scanned and rejected
			// first — this row reaches no network.
			name:    "kustomize payload referencing a remote base",
			verb:    "render",
			packCUE: "name: \"k\"\nversion: \"1\"\ntype: \"kustomize\"\n",
			files:   map[string]string{"kustomization.yaml": "resources:\n- github.com/org/repo//base\n"},
			wantErr: "CUBE-PKG-021",
		},
		{
			name:    "kustomize build failure",
			verb:    "render",
			packCUE: "name: \"k\"\nversion: \"1\"\ntype: \"kustomize\"\n",
			files:   map[string]string{"kustomization.yaml": "resources:\n- missing.yaml\n"},
			wantErr: "CUBE-PKG-006",
		},
		{
			name:    "pack.cue that does not compile",
			verb:    "validate",
			packCUE: "name: \"unterminated\nversion: \"1\"\n",
			wantErr: "CUBE-PKG-002",
		},
		{
			name:    "pack.cue with an undeclared field",
			verb:    "validate",
			packCUE: "name: \"x\"\nversion: \"1\"\ntype: \"raw\"\nuuid: \"b2c1\"\n",
			wantErr: "CUBE-PKG-003",
		},
		{
			// validate renders, so it reports render-time problems too.
			name:    "validate reports an unparseable manifest",
			verb:    "validate",
			packCUE: helloPackCUE,
			files:   map[string]string{"manifests/bad.yaml": "key: value\n  bad: indent\n"},
			wantErr: "CUBE-PKG-005",
		},
		{
			name:    "validate reports a pack that renders nothing",
			verb:    "validate",
			packCUE: helloPackCUE,
			files:   map[string]string{"manifests/README.md": "not a manifest"},
			wantErr: "CUBE-PKG-007",
		},
		{
			name:    "validate reports a namespace conflict",
			verb:    "validate",
			packCUE: "name: \"hello\"\nversion: \"0.1.0\"\ntype: \"raw\"\nnamespace: \"team\"\n",
			files: map[string]string{
				"manifests/a.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  namespace: other\n",
			},
			wantErr: "CUBE-PKG-008",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := tt.ref
			switch {
			case ref != "":
			case tt.packCUE != "":
				ref = writePack(t, tt.packCUE, tt.files)
			default:
				ref = t.TempDir()
			}

			code, stdout, stderr := run(t, "pack", tt.verb, ref)
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
			}
			if stdout != "" {
				t.Errorf("wrote %q to stdout on failure, want nothing", stdout)
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("stderr %q should carry %s", stderr, tt.wantErr)
			}
			if !strings.Contains(stderr, "→") {
				t.Errorf("stderr %q should carry a remediation line", stderr)
			}
		})
	}
}
