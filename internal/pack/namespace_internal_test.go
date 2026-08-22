package pack

import (
	"testing"
	"testing/fstest"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// crdDoc is a CustomResourceDefinition as a pack ships one: only the fields the
// scope index reads, since everything else is the API server's business.
func crdDoc(group, kind, scope string) string {
	doc := "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\n" +
		"metadata:\n  name: " + kind + "s." + group + "\nspec:\n  group: " + group + "\n" +
		"  names:\n    kind: " + kind + "\n"
	if scope != "" {
		doc += "  scope: " + scope + "\n"
	}
	return doc
}

// crDoc is one custom resource of a definition's kind.
func crDoc(apiVersion, kind, name string) string {
	return "apiVersion: " + apiVersion + "\nkind: " + kind + "\nmetadata:\n  name: " + name + "\n"
}

// renderRawNames renders a raw pack forcing namespace "team" and returns
// "kind/namespace/name" per object — the namespace is what every case here is
// about, and the order is the lexical file order renderRaw walks.
func renderRawNames(t *testing.T, manifests map[string]string) []string {
	t.Helper()
	files := fstest.MapFS{
		"pack.cue": &fstest.MapFile{Data: []byte("name: \"p\"\nversion: \"1\"\ntype: \"raw\"\nnamespace: \"team\"\n")},
	}
	for name, body := range manifests {
		files["manifests/"+name] = &fstest.MapFile{Data: []byte(body)}
	}

	p, err := Load(t.Context(), files, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	plan, err := p.Render(t.Context(), RenderOptions{})
	if err != nil {
		t.Fatalf("Render() = error %v, want a plan", err)
	}

	names := make([]string, 0, len(plan.Objects))
	for _, obj := range plan.Objects {
		names = append(names, obj.GetKind()+"/"+obj.GetNamespace()+"/"+obj.GetName())
	}
	return names
}

// A pack that bundles a definition ships the authoritative answer to its own
// resources' scope, so the namespace transform reads it instead of guessing.
func TestNamespaceUsesBundledCRDScope(t *testing.T) {
	tests := []struct {
		name      string
		manifests map[string]string
		want      []string
	}{
		{
			name: "a bundled cluster-scoped definition keeps its resources out of the namespace",
			manifests: map[string]string{
				"a-crd.yaml": crdDoc("example.com", "Widget", scopeCluster),
				"b-cr.yaml":  crDoc("example.com/v1", "Widget", "w"),
			},
			want: []string{"CustomResourceDefinition//Widgets.example.com", "Widget//w"},
		},
		{
			name: "a bundled namespaced definition puts its resources in the namespace",
			manifests: map[string]string{
				"a-crd.yaml": crdDoc("example.com", "Widget", scopeNamespaced),
				"b-cr.yaml":  crDoc("example.com/v1", "Widget", "w"),
			},
			want: []string{"CustomResourceDefinition//Widgets.example.com", "Widget/team/w"},
		},
		{
			// The foreign case: the definition lives elsewhere, so nothing
			// offline can say what the scope is. This is the remaining sharp
			// edge, and it stays on the documented default.
			name: "a resource whose definition the pack does not bundle falls to the default",
			manifests: map[string]string{
				"b-cr.yaml": crDoc("example.com/v1", "Widget", "w"),
			},
			want: []string{"Widget/team/w"},
		},
		{
			name: "a definition declaring no scope leaves its resources on the default",
			manifests: map[string]string{
				"a-crd.yaml": crdDoc("example.com", "Widget", ""),
				"b-cr.yaml":  crDoc("example.com/v1", "Widget", "w"),
			},
			want: []string{"CustomResourceDefinition//Widgets.example.com", "Widget/team/w"},
		},
		{
			name: "a definition declaring an unrecognised scope leaves its resources on the default",
			manifests: map[string]string{
				"a-crd.yaml": crdDoc("example.com", "Widget", "Clusterr"),
				"b-cr.yaml":  crDoc("example.com/v1", "Widget", "w"),
			},
			want: []string{"CustomResourceDefinition//Widgets.example.com", "Widget/team/w"},
		},
		{
			// Two groups may define the same kind, so the group is half the
			// identity: a definition only governs its own group's resources.
			name: "a definition governs its own group only",
			manifests: map[string]string{
				"a-crd.yaml": crdDoc("example.com", "Widget", scopeCluster),
				"b-cr.yaml":  crDoc("other.com/v1", "Widget", "w"),
			},
			want: []string{"CustomResourceDefinition//Widgets.example.com", "Widget/team/w"},
		},
		{
			name: "built-in kinds are still decided by the static set",
			manifests: map[string]string{
				"a-crd.yaml": crdDoc("example.com", "Widget", scopeCluster),
				"b-ns.yaml":  crDoc("v1", "Namespace", "team"),
				"c-cm.yaml":  crDoc("v1", "ConfigMap", "settings"),
			},
			want: []string{
				"CustomResourceDefinition//Widgets.example.com",
				"Namespace//team",
				"ConfigMap/team/settings",
			},
		},
		{
			name: "a definition that is not a Kubernetes CRD is not a definition",
			manifests: map[string]string{
				"a-crd.yaml": crDoc("example.com/v1", "CustomResourceDefinition", "impostor"),
				"b-cr.yaml":  crDoc("example.com/v1", "Widget", "w"),
			},
			want: []string{"CustomResourceDefinition//impostor", "Widget/team/w"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderRawNames(t, tt.manifests)
			if !equal(got, tt.want) {
				t.Errorf("Render() = %v, want %v", got, tt.want)
			}
		})
	}
}

// External manifests are judged by the same index the pack's own objects were,
// so an instance delivers one consistent answer about what is namespaced.
func TestNamespaceUsesBundledCRDScopeForExternalManifests(t *testing.T) {
	files := fstest.MapFS{
		"pack.cue": &fstest.MapFile{Data: []byte(
			"name: \"p\"\nversion: \"1\"\ntype: \"raw\"\nnamespace: \"team\"\n")},
		"manifests/crd.yaml": &fstest.MapFile{Data: []byte(crdDoc("example.com", "Widget", scopeCluster))},
	}
	p, err := Load(t.Context(), files, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}

	resolver := &stubResolver{docs: map[string]string{
		"./widget.yaml": crDoc("example.com/v1", "Widget", "external"),
		"./gadget.yaml": crDoc("other.com/v1", "Gadget", "foreign"),
	}}
	spec := v1alpha1.PackSpec{ExternalManifests: []v1alpha1.ExternalManifest{
		{Ref: "./widget.yaml", Lifecycle: v1alpha1.LifecyclePre},
		{Ref: "./gadget.yaml"},
	}}

	plan, err := renderInstance(t.Context(), p, spec, resolver.resolve)
	if err != nil {
		t.Fatalf("renderInstance() = error %v, want a plan", err)
	}

	if got, want := plan.Prerequisites[0].GetNamespace(), ""; got != want {
		t.Errorf("external Widget namespace = %q, want %q — the pack bundles its cluster-scoped definition", got, want)
	}
	if got, want := plan.Objects[1].GetNamespace(), "team"; got != want {
		t.Errorf("external Gadget namespace = %q, want %q — no bundled definition, so the default applies", got, want)
	}
}
