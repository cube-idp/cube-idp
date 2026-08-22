package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// A type this build cannot render still validates: its metadata and payload
// are sound, and only the render backend is missing.
func TestPackValidateUnrenderableTypes(t *testing.T) {
	tests := []struct {
		name    string
		packCUE string
		files   map[string]string
	}{
		{
			name:    "helm",
			packCUE: "name: \"h\"\nversion: \"1\"\ntype: \"helm\"\n",
			files:   map[string]string{"Chart.yaml": "name: h\n"},
		},
		{
			name:    "kustomize",
			packCUE: "name: \"k\"\nversion: \"1\"\ntype: \"kustomize\"\n",
			files:   map[string]string{"kustomization.yaml": "resources: []\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writePack(t, tt.packCUE, tt.files)
			code, stdout, stderr := run(t, "pack", "validate", dir)
			if code != 0 {
				t.Fatalf("pack validate (%s) exit = %d, want 0 (stderr: %s)", tt.name, code, stderr)
			}
			if !strings.Contains(stdout, "is valid") {
				t.Errorf("pack validate (%s) stdout = %q, want a validity confirmation", tt.name, stdout)
			}
		})
	}
}

// The reference is positional and local paths in every spelling resolve to
// the same pack, so the grammar the command exposes is stable before the
// reference resolver lands.
func TestPackRefLocalForms(t *testing.T) {
	dir := writePack(t, helloPackCUE, helloManifests())

	for _, ref := range []string{dir, "file://" + filepath.ToSlash(dir)} {
		t.Run(ref, func(t *testing.T) {
			code, stdout, stderr := run(t, "pack", "validate", ref)
			if code != 0 {
				t.Fatalf("pack validate %s exit = %d, want 0 (stderr: %s)", ref, code, stderr)
			}
			if !strings.Contains(stdout, "hello 0.1.0 (raw) is valid") {
				t.Errorf("pack validate %s stdout = %q, want the pack identity", ref, stdout)
			}
		})
	}
}

// pack render and pack validate each take exactly one reference.
func TestPackArgCount(t *testing.T) {
	for _, verb := range []string{"render", "validate"} {
		t.Run(verb, func(t *testing.T) {
			if code, _, _ := run(t, "pack", verb); code == 0 {
				t.Errorf("pack %s with no reference exited 0, want a usage failure", verb)
			}
			if code, _, _ := run(t, "pack", verb, "a", "b"); code == 0 {
				t.Errorf("pack %s with two references exited 0, want a usage failure", verb)
			}
		})
	}
}
