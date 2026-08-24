package pack

import (
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// object is one Kubernetes object as a pack author writes it.
func object(kind, name, namespace string) string {
	doc := "apiVersion: v1\nkind: " + kind + "\nmetadata:\n  name: " + name + "\n"
	if namespace != "" {
		doc += "  namespace: " + namespace + "\n"
	}
	return doc
}

// loadRawPack loads a raw pack holding one ConfigMap, optionally forcing a
// namespace. It is the pack half of every composition case: the interesting
// part is what the setup entry adds around it.
func loadRawPack(t *testing.T, namespace string) *Pack {
	t.Helper()
	cue := "name: \"p\"\nversion: \"1\"\ntype: \"raw\"\n"
	if namespace != "" {
		cue += "namespace: \"" + namespace + "\"\n"
	}
	files := fstest.MapFS{
		"pack.cue":            &fstest.MapFile{Data: []byte(cue)},
		"manifests/own.yaml":  &fstest.MapFile{Data: []byte(object("ConfigMap", "own", ""))},
		"manifests/more.yaml": &fstest.MapFile{Data: []byte(object("ConfigMap", "second", ""))},
	}
	p, err := Load(t.Context(), files, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	return p
}

// planNames renders "kind/namespace/name" per object of each group, which
// captures the grouping, the order, and the namespace transform at once.
func planNames(plan RenderPlan) (pre, objects []string) {
	for _, obj := range plan.Prerequisites {
		pre = append(pre, obj.GetKind()+"/"+obj.GetNamespace()+"/"+obj.GetName())
	}
	for _, obj := range plan.Objects {
		objects = append(objects, obj.GetKind()+"/"+obj.GetNamespace()+"/"+obj.GetName())
	}
	return pre, objects
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// external manifests reach the plan grouped by lifecycle: prerequisites of
// their own, and everything else after the pack's own objects, in declaration
// order.
func TestRenderInstanceGroupsExternalManifests(t *testing.T) {
	resolver := &stubResolver{docs: map[string]string{
		"./gateway.yaml": object("Service", "gateway", ""),
		"./crd.yaml":     object("ConfigMap", "crd", ""),
	}}
	spec := v1alpha1.PackSpec{ExternalManifests: []v1alpha1.ExternalManifest{
		{Ref: "./crd.yaml", Lifecycle: v1alpha1.LifecyclePre},
		{Ref: "./gateway.yaml", Lifecycle: v1alpha1.LifecycleWith},
		{
			Manifest:  &runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"inline"}}`)},
			Lifecycle: v1alpha1.LifecyclePre,
		},
		// An entry with no lifecycle is delivered with the pack.
		{Manifest: &runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"beside"}}`)}},
	}}

	plan, err := renderInstance(t.Context(), loadRawPack(t, ""), spec, resolver.resolve)
	if err != nil {
		t.Fatalf("renderInstance() = error %v, want a plan", err)
	}

	pre, objects := planNames(plan)
	wantPre := []string{"ConfigMap//crd", "Secret//inline"}
	wantObjects := []string{"ConfigMap//second", "ConfigMap//own", "Service//gateway", "Secret//beside"}
	if !equal(pre, wantPre) {
		t.Errorf("Prerequisites = %v, want %v", pre, wantPre)
	}
	if !equal(objects, wantObjects) {
		t.Errorf("Objects = %v, want %v", objects, wantObjects)
	}
}

// A pack that forces a namespace forces it over everything the instance
// delivers, external manifests included — and an object insisting on another
// namespace is the same conflict it would be inside the pack.
func TestRenderInstanceNamespaceCoversExternalManifests(t *testing.T) {
	resolver := &stubResolver{docs: map[string]string{
		"./own-ns.yaml":  object("Service", "elsewhere", "other"),
		"./no-ns.yaml":   object("Service", "gateway", ""),
		"./cluster.yaml": object("ClusterRole", "reader", ""),
	}}

	t.Run("namespaced external objects join the pack's namespace", func(t *testing.T) {
		spec := v1alpha1.PackSpec{ExternalManifests: []v1alpha1.ExternalManifest{
			{Ref: "./no-ns.yaml", Lifecycle: v1alpha1.LifecyclePre},
			{Ref: "./cluster.yaml"},
		}}
		plan, err := renderInstance(t.Context(), loadRawPack(t, "platform"), spec, resolver.resolve)
		if err != nil {
			t.Fatalf("renderInstance() = error %v, want a plan", err)
		}
		pre, objects := planNames(plan)
		if want := []string{"Service/platform/gateway"}; !equal(pre, want) {
			t.Errorf("Prerequisites = %v, want %v", pre, want)
		}
		if want := []string{"ConfigMap/platform/second", "ConfigMap/platform/own", "ClusterRole//reader"}; !equal(objects, want) {
			t.Errorf("Objects = %v, want %v", objects, want)
		}
	})

	t.Run("an external object in another namespace is a conflict", func(t *testing.T) {
		spec := v1alpha1.PackSpec{ExternalManifests: []v1alpha1.ExternalManifest{{Ref: "./own-ns.yaml"}}}
		_, err := renderInstance(t.Context(), loadRawPack(t, "platform"), spec, resolver.resolve)
		wantCoded(t, err, CodeNamespaceConflict)
	})
}

// A resolved ref must be exactly one Kubernetes object. Several documents are
// several entries — which is what makes per-object lifecycle expressible.
func TestExternalManifestErrors(t *testing.T) {
	tests := []struct {
		name  string
		entry v1alpha1.ExternalManifest
		want  cubeerr.Code
	}{
		{
			name:  "several documents",
			entry: v1alpha1.ExternalManifest{Ref: "./multi.yaml"},
			want:  CodeExternalManifest,
		},
		{
			name:  "no document at all",
			entry: v1alpha1.ExternalManifest{Ref: "./empty.yaml"},
			want:  CodeExternalManifest,
		},
		{
			name:  "not a mapping",
			entry: v1alpha1.ExternalManifest{Ref: "./list.yaml"},
			want:  CodeExternalManifest,
		},
		{
			name:  "a mapping that is not a Kubernetes object",
			entry: v1alpha1.ExternalManifest{Ref: "./values.yaml"},
			want:  CodeExternalManifest,
		},
		{
			name:  "a Kubernetes object with no kind",
			entry: v1alpha1.ExternalManifest{Ref: "./kindless.yaml"},
			want:  CodeExternalManifest,
		},
		{
			name:  "an inline manifest that is not JSON",
			entry: v1alpha1.ExternalManifest{Manifest: &runtime.RawExtension{Raw: []byte("{oops")}},
			want:  CodeManifestParse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubResolver{docs: map[string]string{
				"./multi.yaml":    object("Service", "a", "") + "---\n" + object("Service", "b", ""),
				"./empty.yaml":    "# nothing here\n",
				"./list.yaml":     "- a\n- b\n",
				"./values.yaml":   "image: nginx\n",
				"./kindless.yaml": "apiVersion: v1\nmetadata:\n  name: a\n",
			}}
			spec := v1alpha1.PackSpec{ExternalManifests: []v1alpha1.ExternalManifest{tt.entry}}

			_, err := renderInstance(t.Context(), loadRawPack(t, ""), spec, resolver.resolve)
			wantCoded(t, err, tt.want)
		})
	}
}

// A raw pack has no values surface, and its type is declared — so the refusal
// comes before anything is fetched, and it does not depend on what the values
// turn out to be.
func TestRenderInstanceValuesOnRawPack(t *testing.T) {
	tests := []struct {
		name string
		spec v1alpha1.PackSpec
	}{
		{
			name: "a valuesRef is refused without resolving it",
			spec: instanceSpec("./values.yaml", ""),
		},
		{
			name: "inline values are refused",
			spec: instanceSpec("", `{"image":"nginx"}`),
		},
		{
			// The merged map would be empty here, so a check made after the
			// merge would see no values and let this through.
			name: "a valuesRef holding an empty mapping is still refused",
			spec: instanceSpec("./empty.yaml", ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubResolver{docs: map[string]string{
				"./values.yaml": "image: nginx\n",
				"./empty.yaml":  "{}\n",
			}}

			_, err := renderInstance(t.Context(), loadRawPack(t, ""), tt.spec, resolver.resolve)
			wantCoded(t, err, CodeValuesOnRawPack)
			if len(resolver.calls) != 0 {
				t.Errorf("resolved %v, want nothing resolved before the refusal", resolver.calls)
			}
		})
	}
}

// Both values sources decode through JSON, where every number is a float — so
// an integer written by an author must still satisfy an `int` field. Rendering
// without a values error is the proof that #Values accepted them.
func TestRenderInstanceNumbersSatisfyValuesDefinition(t *testing.T) {
	files := fstest.MapFS{
		"pack.cue": &fstest.MapFile{Data: []byte("name: \"v\"\nversion: \"1\"\ntype: \"helm\"\n" +
			"chart: {kind: \"repo\", url: \"https://c.example.com\", name: \"c\", version: \"1.0.0\"}\n" +
			"#Values: {\n\treplicas: int | *1\n\tratio:    number | *1.0\n}\n")},
	}
	p, err := Load(t.Context(), files, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}

	tests := []struct {
		name string
		spec v1alpha1.PackSpec
	}{
		{
			name: "an integer from a valuesRef satisfies an int field",
			spec: instanceSpec("./values.yaml", ""),
		},
		{
			name: "an integer from inline values satisfies an int field",
			spec: instanceSpec("", `{"replicas":4}`),
		},
		{
			name: "a fractional value still satisfies a number field",
			spec: instanceSpec("", `{"ratio":1.5}`),
		},
		{
			name: "an integer merged over a valuesRef satisfies an int field",
			spec: instanceSpec("./values.yaml", `{"replicas":5}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubResolver{docs: map[string]string{"./values.yaml": "replicas: 3\n"}}

			_, err := renderInstance(t.Context(), p, tt.spec, resolver.resolve)
			if err != nil {
				t.Errorf("renderInstance(%s) = error %v, want a plan", tt.name, err)
			}
		})
	}
}

// A raw pack with no values renders, external manifests and all: the refusal
// above is about values, not about setup entries in general.
func TestRenderInstanceRawPackWithoutValues(t *testing.T) {
	resolver := &stubResolver{docs: map[string]string{"./svc.yaml": object("Service", "gateway", "")}}
	spec := v1alpha1.PackSpec{ExternalManifests: []v1alpha1.ExternalManifest{{Ref: "./svc.yaml"}}}

	plan, err := renderInstance(t.Context(), loadRawPack(t, ""), spec, resolver.resolve)
	if err != nil {
		t.Fatalf("renderInstance() = error %v, want a plan", err)
	}
	if _, objects := planNames(plan); len(objects) != 3 {
		t.Errorf("Objects = %v, want the pack's two objects plus the external one", objects)
	}
}
