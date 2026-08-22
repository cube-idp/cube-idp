package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// helm is the one type this build cannot render, and it still validates: its
// metadata and payload are sound, and only the render backend is missing.
func TestPackValidateUnrenderableType(t *testing.T) {
	dir := writePack(t, "name: \"h\"\nversion: \"1\"\ntype: \"helm\"\n",
		map[string]string{"Chart.yaml": "name: h\n"})

	code, stdout, stderr := run(t, "pack", "validate", dir)
	if code != 0 {
		t.Fatalf("pack validate (helm) exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Errorf("pack validate (helm) stdout = %q, want a validity confirmation", stdout)
	}
}

// kustomizeDir writes a kustomize pack whose build yields one ConfigMap
// carrying a ${VAR} reference resolved from a #Values default.
func kustomizeDir(t *testing.T) string {
	t.Helper()
	return writePack(t,
		"name: \"k\"\nversion: \"1\"\ntype: \"kustomize\"\n#Values: {\n\tTIER: string | *\"backend\"\n}\n",
		map[string]string{
			"kustomization.yaml": "resources:\n- cm.yaml\nnamePrefix: k-\n",
			"cm.yaml":            "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\ndata:\n  tier: ${TIER}\n",
		})
}

// A kustomize pack renders through the CLI: transformers apply, ${VAR} is
// substituted from the #Values default, and stdout stays pure YAML.
func TestPackRenderKustomize(t *testing.T) {
	code, stdout, stderr := run(t, "pack", "render", kustomizeDir(t))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("pack render wrote %q to stderr, want nothing on success", stderr)
	}
	for _, want := range []string{"name: k-c", "tier: backend"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("rendered output missing %q; got:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "${TIER}") {
		t.Errorf("rendered output still carries an unsubstituted reference:\n%s", stdout)
	}
}

// Rendering a kustomize pack twice must produce identical bytes.
func TestPackRenderKustomizeIsReproducible(t *testing.T) {
	dir := kustomizeDir(t)

	_, first, _ := run(t, "pack", "render", dir)
	for i := range 3 {
		if _, got, _ := run(t, "pack", "render", dir); got != first {
			t.Fatalf("kustomize render run %d differs from the first:\ngot:\n%s\nwant:\n%s", i, got, first)
		}
	}
}

// validate renders, so it reports kustomize-stage problems too.
func TestPackValidateKustomize(t *testing.T) {
	code, stdout, stderr := run(t, "pack", "validate", kustomizeDir(t))
	if code != 0 {
		t.Fatalf("pack validate (kustomize) exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Errorf("pack validate (kustomize) stdout = %q, want a validity confirmation", stdout)
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
