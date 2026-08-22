package pack_test

import (
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

const kustomizeCUE = "name: \"web\"\nversion: \"1\"\ntype: \"kustomize\"\n"

// kPack builds a kustomize pack from a pack.cue body, a kustomization, and
// any extra payload files.
func kPack(cue, kustomization string, files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{
		"pack.cue":           &fstest.MapFile{Data: []byte(cue)},
		"kustomization.yaml": &fstest.MapFile{Data: []byte(kustomization)},
	}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

const deployYAML = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\n"

// renderK loads and renders a kustomize pack, returning "kind/namespace/name"
// per object.
func renderK(t *testing.T, fsys fstest.MapFS, values map[string]any) []string {
	t.Helper()
	p, err := pack.Load(t.Context(), fsys, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	plan, err := p.Render(t.Context(), pack.RenderOptions{Values: values})
	if err != nil {
		t.Fatalf("Render() = error %v, want a plan", err)
	}
	names := make([]string, 0, len(plan.Objects))
	for _, obj := range plan.Objects {
		names = append(names, obj.GetKind()+"/"+obj.GetNamespace()+"/"+obj.GetName())
	}
	return names
}

func TestRenderKustomize(t *testing.T) {
	tests := []struct {
		name          string
		cue           string
		kustomization string
		files         map[string]string
		want          []string
	}{
		{
			name:          "kustomize transformers are applied",
			cue:           kustomizeCUE,
			kustomization: "resources:\n- deploy.yaml\nnamePrefix: web-\n",
			files:         map[string]string{"deploy.yaml": deployYAML},
			want:          []string{"Deployment//web-app"},
		},
		{
			// The resource order is the kustomization's own, so it is a
			// function of the payload rather than of map iteration.
			name:          "resource order follows the kustomization",
			cue:           kustomizeCUE,
			kustomization: "resources:\n- b.yaml\n- a.yaml\n",
			files: map[string]string{
				"a.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n",
				"b.yaml": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: b\n",
			},
			want: []string{"Secret//b", "ConfigMap//a"},
		},
		{
			name:          "a base in a subdirectory builds",
			cue:           kustomizeCUE,
			kustomization: "resources:\n- base\n",
			files: map[string]string{
				"base/kustomization.yaml": "resources:\n- cm.yaml\n",
				"base/cm.yaml":            "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: from-base\n",
			},
			want: []string{"ConfigMap//from-base"},
		},
		{
			// The same transform raw packs get, so the two types agree.
			name:          "pack namespace is applied to the built output",
			cue:           "name: \"web\"\nversion: \"1\"\ntype: \"kustomize\"\nnamespace: \"team\"\n",
			kustomization: "resources:\n- deploy.yaml\n- ns.yaml\n",
			files: map[string]string{
				"deploy.yaml": deployYAML,
				"ns.yaml":     "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: team\n",
			},
			want: []string{"Deployment/team/app", "Namespace//team"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderK(t, kPack(tt.cue, tt.kustomization, tt.files), nil)
			if len(got) != len(tt.want) {
				t.Fatalf("Render() objects = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Render() object %d = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestRenderKustomizeErrors(t *testing.T) {
	tests := []struct {
		name          string
		kustomization string
		files         map[string]string
		want          cubeerr.Code
	}{
		{
			name:          "resource does not exist",
			kustomization: "resources:\n- missing.yaml\n",
			want:          pack.CodeKustomizeBuild,
		},
		{
			name:          "kustomization is not valid YAML",
			kustomization: "resources:\n  - a\n bad: indent\n",
			want:          pack.CodeKustomizeBuild,
		},
		{
			name:          "resource is not a Kubernetes object",
			kustomization: "resources:\n- junk.yaml\n",
			files:         map[string]string{"junk.yaml": "just: a mapping\n"},
			want:          pack.CodeKustomizeBuild,
		},
		{
			name:          "build produces no objects",
			kustomization: "resources: []\n",
			want:          pack.CodeEmptyRender,
		},
		{
			name:          "namespace conflict in the built output",
			kustomization: "resources:\n- cm.yaml\n",
			files:         map[string]string{"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n  namespace: other\n"},
			want:          pack.CodeNamespaceConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cue := kustomizeCUE
			if tt.want == pack.CodeNamespaceConflict {
				cue = "name: \"web\"\nversion: \"1\"\ntype: \"kustomize\"\nnamespace: \"team\"\n"
			}
			p, err := pack.Load(t.Context(), kPack(cue, tt.kustomization, tt.files), "./p")
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			_, err = p.Render(t.Context(), pack.RenderOptions{})
			wantCode(t, err, tt.want)
		})
	}
}

// Rendering never reaches the network. kustomize resolves remote references
// unconditionally — krusty has no option to forbid it — so the payload is
// scanned and rejected first. These rows therefore make no network call,
// which is what keeps the gate hermetic.
func TestRenderKustomizeRejectsRemoteRefs(t *testing.T) {
	remote := []string{
		"https://raw.githubusercontent.com/org/repo/main/cm.yaml",
		"http://example.com/cm.yaml",
		"github.com/kubernetes-sigs/kustomize/examples/helloWorld?ref=v1.0.6",
		"github.com/org/repo//overlays/prod",
		"git@github.com:org/repo.git",
		"git::https://example.com/org/repo",
		"oci://registry.example/base:1.0",
		"ssh://git@example.com/org/repo",
		"https://example.com/repo.git//base",
	}
	for _, ref := range remote {
		t.Run("remote "+ref, func(t *testing.T) {
			fsys := kPack(kustomizeCUE, "resources:\n- "+ref+"\n", nil)
			p, err := pack.Load(t.Context(), fsys, "./p")
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			_, err = p.Render(t.Context(), pack.RenderOptions{})
			wantCode(t, err, pack.CodeRemoteRef)
		})
	}
}

// The scan must not reject ordinary local references — a false positive is a
// blocked pack, so the local forms are pinned too.
func TestRenderKustomizeAcceptsLocalRefs(t *testing.T) {
	const cm = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n"

	tests := []struct {
		name  string
		ref   string
		files map[string]string
	}{
		{name: "bare file name", ref: "cm.yaml", files: map[string]string{"cm.yaml": cm}},
		{name: "explicitly relative", ref: "./cm.yaml", files: map[string]string{"cm.yaml": cm}},
		{name: "in a subdirectory", ref: "sub/cm.yaml", files: map[string]string{"sub/cm.yaml": cm}},
		{name: "deeply nested .yml", ref: "nested/deep/cm.yml", files: map[string]string{"nested/deep/cm.yml": cm}},
		{name: "a directory holding a kustomization", ref: "base", files: map[string]string{
			"base/kustomization.yaml": "resources:\n- cm.yaml\n",
			"base/cm.yaml":            cm,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderK(t, kPack(kustomizeCUE, "resources:\n- "+tt.ref+"\n", tt.files), nil)
			if len(got) != 1 {
				t.Fatalf("Render() with local ref %q gave objects %v, want exactly one", tt.ref, got)
			}
		})
	}
}

// Every field the scan collects gets a row: an unscanned field is exactly the
// false negative a fail-closed check exists to prevent. bases is deprecated in
// kustomize's API but still honoured at build time, so it is scanned too.
func TestRenderKustomizeRejectsRemoteInEveryScannedField(t *testing.T) {
	const remote = "github.com/org/repo//base"

	kustomizations := map[string]string{
		"resources":      "resources:\n- " + remote + "\n",
		"bases":          "bases:\n- " + remote + "\n",
		"components":     "components:\n- " + remote + "\n",
		"crds":           "crds:\n- " + remote + "\n",
		"configurations": "configurations:\n- " + remote + "\n",
		"patches path":   "patches:\n- path: " + remote + "\n",
	}

	for field, kustomization := range kustomizations {
		t.Run(field, func(t *testing.T) {
			p, err := pack.Load(t.Context(), kPack(kustomizeCUE, kustomization, nil), "./p")
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			_, err = p.Render(t.Context(), pack.RenderOptions{})
			wantCode(t, err, pack.CodeRemoteRef)
		})
	}
}

// A remote reference in a nested base is rejected too — the scan walks the
// whole payload, not just the root kustomization.
func TestRenderKustomizeScansNestedKustomizations(t *testing.T) {
	fsys := kPack(kustomizeCUE, "resources:\n- base\n", map[string]string{
		"base/kustomization.yaml": "resources:\n- https://example.com/cm.yaml\n",
	})
	p, err := pack.Load(t.Context(), fsys, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	_, err = p.Render(t.Context(), pack.RenderOptions{})
	wantCode(t, err, pack.CodeRemoteRef)
}
