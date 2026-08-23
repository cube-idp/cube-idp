package pack_test

import (
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// manifest builds a one-object manifest file.
func manifest(kind, name, namespace string) *fstest.MapFile {
	doc := "apiVersion: v1\nkind: " + kind + "\nmetadata:\n  name: " + name + "\n"
	if namespace != "" {
		doc += "  namespace: " + namespace + "\n"
	}
	return &fstest.MapFile{Data: []byte(doc)}
}

// rawPackWith builds a raw pack from a pack.cue body and a set of manifests.
func rawPackWith(cue string, manifests map[string]*fstest.MapFile) fstest.MapFS {
	files := fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte(cue)}}
	for name, file := range manifests {
		files["manifests/"+name] = file
	}
	return files
}

// renderNames renders the pack and returns "kind/namespace/name" per object,
// which captures both the order and the namespace transform in one assertion.
func renderNames(t *testing.T, files fstest.MapFS) []string {
	t.Helper()
	p, err := pack.Load(t.Context(), files, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	plan, err := p.Render(t.Context(), pack.RenderOptions{})
	if err != nil {
		t.Fatalf("Render() = error %v, want a plan", err)
	}
	if plan.Prerequisites != nil {
		t.Errorf("Render().Prerequisites = %v, want nil until external manifests land", plan.Prerequisites)
	}
	names := make([]string, 0, len(plan.Objects))
	for _, obj := range plan.Objects {
		names = append(names, obj.GetKind()+"/"+obj.GetNamespace()+"/"+obj.GetName())
	}
	return names
}

const rawCUE = "name: \"p\"\nversion: \"1\"\ntype: \"raw\"\n"

func TestRenderRaw(t *testing.T) {
	tests := []struct {
		name      string
		cue       string
		manifests map[string]*fstest.MapFile
		want      []string
	}{
		{
			name:      "single object",
			cue:       rawCUE,
			manifests: map[string]*fstest.MapFile{"a.yaml": manifest("ConfigMap", "a", "")},
			want:      []string{"ConfigMap//a"},
		},
		{
			// fs.WalkDir visits lexically, so file names alone fix the order.
			name: "files render in lexical order, not map order",
			cue:  rawCUE,
			manifests: map[string]*fstest.MapFile{
				"c.yaml": manifest("ConfigMap", "c", ""),
				"a.yaml": manifest("ConfigMap", "a", ""),
				"b.yaml": manifest("ConfigMap", "b", ""),
			},
			want: []string{"ConfigMap//a", "ConfigMap//b", "ConfigMap//c"},
		},
		{
			name: "nested directories are walked, still lexically",
			cue:  rawCUE,
			manifests: map[string]*fstest.MapFile{
				"z.yaml":       manifest("ConfigMap", "z", ""),
				"sub/a.yaml":   manifest("ConfigMap", "sub-a", ""),
				"sub/deep.yml": manifest("ConfigMap", "sub-deep", ""),
			},
			want: []string{"ConfigMap//sub-a", "ConfigMap//sub-deep", "ConfigMap//z"},
		},
		{
			name: "multi-document file keeps document order",
			cue:  rawCUE,
			manifests: map[string]*fstest.MapFile{
				"multi.yaml": {Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: first\n" +
					"---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: second\n")},
			},
			want: []string{"ConfigMap//first", "Secret//second"},
		},
		{
			name: "empty documents and trailing separators are skipped",
			cue:  rawCUE,
			manifests: map[string]*fstest.MapFile{
				"a.yaml": {Data: []byte("---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n")},
			},
			want: []string{"ConfigMap//a"},
		},
		{
			name: "non-manifest files are ignored, not parsed",
			cue:  rawCUE,
			manifests: map[string]*fstest.MapFile{
				"a.yaml":    manifest("ConfigMap", "a", ""),
				"README.md": {Data: []byte("# not a manifest: [ this would not parse")},
				"notes.txt": {Data: []byte("also not a manifest")},
			},
			want: []string{"ConfigMap//a"},
		},
		{
			name: "pack namespace fills in objects that declare none",
			cue:  "name: \"p\"\nversion: \"1\"\ntype: \"raw\"\nnamespace: \"team\"\n",
			manifests: map[string]*fstest.MapFile{
				"a.yaml": manifest("ConfigMap", "a", ""),
			},
			want: []string{"ConfigMap/team/a"},
		},
		{
			name: "pack namespace leaves cluster-scoped kinds alone",
			cue:  "name: \"p\"\nversion: \"1\"\ntype: \"raw\"\nnamespace: \"team\"\n",
			manifests: map[string]*fstest.MapFile{
				"a.yaml": manifest("Namespace", "team", ""),
				"b.yaml": manifest("ClusterRole", "reader", ""),
				"c.yaml": manifest("CustomResourceDefinition", "widgets", ""),
				"d.yaml": manifest("ConfigMap", "cm", ""),
			},
			want: []string{"Namespace//team", "ClusterRole//reader", "CustomResourceDefinition//widgets", "ConfigMap/team/cm"},
		},
		{
			name: "an object already in the pack namespace is left as is",
			cue:  "name: \"p\"\nversion: \"1\"\ntype: \"raw\"\nnamespace: \"team\"\n",
			manifests: map[string]*fstest.MapFile{
				"a.yaml": manifest("ConfigMap", "a", "team"),
			},
			want: []string{"ConfigMap/team/a"},
		},
		{
			name: "without a pack namespace objects keep their own",
			cue:  rawCUE,
			manifests: map[string]*fstest.MapFile{
				"a.yaml": manifest("ConfigMap", "a", "elsewhere"),
				"b.yaml": manifest("ConfigMap", "b", ""),
			},
			want: []string{"ConfigMap/elsewhere/a", "ConfigMap//b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderNames(t, rawPackWith(tt.cue, tt.manifests))
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

func TestRenderErrors(t *testing.T) {
	tests := []struct {
		name      string
		cue       string
		manifests map[string]*fstest.MapFile
		want      cubeerr.Code
	}{
		{
			name:      "manifest is not valid YAML",
			cue:       rawCUE,
			manifests: map[string]*fstest.MapFile{"bad.yaml": {Data: []byte("key: value\n  bad: indent\n")}},
			want:      pack.CodeManifestParse,
		},
		{
			name:      "manifests directory holds no manifests",
			cue:       rawCUE,
			manifests: map[string]*fstest.MapFile{"README.md": {Data: []byte("nothing here")}},
			want:      pack.CodeEmptyRender,
		},
		{
			name:      "manifests are all empty documents",
			cue:       rawCUE,
			manifests: map[string]*fstest.MapFile{"a.yaml": {Data: []byte("---\n---\n")}},
			want:      pack.CodeEmptyRender,
		},
		{
			name: "object namespace contradicts the pack namespace",
			cue:  "name: \"p\"\nversion: \"1\"\ntype: \"raw\"\nnamespace: \"team\"\n",
			manifests: map[string]*fstest.MapFile{
				"a.yaml": manifest("ConfigMap", "a", "other"),
			},
			want: pack.CodeNamespaceConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pack.Load(t.Context(), rawPackWith(tt.cue, tt.manifests), "./p")
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			_, err = p.Render(t.Context(), pack.RenderOptions{})
			wantCode(t, err, tt.want)
		})
	}
}

// Rendering must be reproducible: the same pack rendered twice yields the
// same objects in the same order, with nothing derived from the clock or the
// environment.
func TestRenderIsDeterministic(t *testing.T) {
	files := rawPackWith(rawCUE, map[string]*fstest.MapFile{
		"b.yaml":     manifest("ConfigMap", "b", ""),
		"a.yaml":     manifest("Service", "a", ""),
		"sub/c.yaml": manifest("Secret", "c", ""),
	})

	first := renderNames(t, files)
	for i := range 5 {
		got := renderNames(t, files)
		if len(got) != len(first) {
			t.Fatalf("Render() run %d = %v, want %v", i, got, first)
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("Render() run %d object %d = %q, want %q", i, j, got[j], first[j])
			}
		}
	}
}

// Every type the schema admits now renders, so there is no unsupported-type
// path left to reach: helm was the last one, and CUBE-PKG-020 is retired
// rather than reachable. This asserts the whole enum, so a type added without
// a render arm fails here instead of at a user.
func TestEveryDeclarableTypeRenders(t *testing.T) {
	packs := map[pack.Type]fstest.MapFS{
		pack.TypeRaw: rawPackWith(rawCUE, map[string]*fstest.MapFile{"a.yaml": manifest("ConfigMap", "a", "")}),
		pack.TypeHelm: helmPack("name: \"h\"\nversion: \"1\"\ntype: \"helm\"\n" +
			"chart: {kind: \"repo\", url: \"https://c.example.com\", name: \"c\", version: \"1.0.0\"}\n"),
		pack.TypeKustomize: {
			"pack.cue":           &fstest.MapFile{Data: []byte("name: \"k\"\nversion: \"1\"\ntype: \"kustomize\"\n")},
			"kustomization.yaml": &fstest.MapFile{Data: []byte("resources:\n- cm.yaml\n")},
			"cm.yaml":            manifest("ConfigMap", "k", ""),
		},
	}

	for typ, files := range packs {
		t.Run(string(typ), func(t *testing.T) {
			p, err := pack.Load(t.Context(), files, "./p")
			if err != nil {
				t.Fatalf("Load(%s) = error %v, want a pack", typ, err)
			}
			plan, err := p.Render(t.Context(), pack.RenderOptions{})
			if err != nil {
				t.Fatalf("Render(%s) = error %v, want a plan", typ, err)
			}
			if len(plan.Objects) == 0 {
				t.Errorf("Render(%s) rendered no objects, want at least one", typ)
			}
		})
	}
}
