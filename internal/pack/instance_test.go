package pack_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ref"
)

// writeRef writes a document into the test's own directory and returns the
// reference that resolves it. file:// is the absolute spelling of a local
// path, so this exercises the production resolver — internal/ref — rather than
// a stand-in for it.
func writeRef(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) = error %v", path, err)
	}
	return "file://" + path
}

const instanceCUE = `name:    "web"
version: "1"
type:    "kustomize"
#Values: {
	IMAGE!: string
	TIER:   string | *"backend"
}
`

// The whole instance surface through the production resolver: a valuesRef on
// disk, an inline patch over it, the merged values reaching the pack's
// #Values and its ${VAR} substitution, and an external manifest joining the
// plan behind the pack's own objects.
func TestRenderInstance(t *testing.T) {
	fsys := fstest.MapFS{
		"pack.cue":           &fstest.MapFile{Data: []byte(instanceCUE)},
		"kustomization.yaml": &fstest.MapFile{Data: []byte("resources:\n- deploy.yaml\n")},
		"deploy.yaml": &fstest.MapFile{Data: []byte(
			"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\n" +
				"  labels:\n    tier: ${TIER}\nspec:\n  template:\n    spec:\n" +
				"      containers:\n      - name: app\n        image: ${IMAGE}\n")},
	}
	p, err := pack.Load(t.Context(), fsys, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}

	spec := v1alpha1.PackSpec{
		ValuesRef: writeRef(t, "values.yaml", "IMAGE: nginx:1.26\nTIER: frontend\n"),
		Values:    &runtime.RawExtension{Raw: []byte(`{"IMAGE":"nginx:1.27"}`)},
		ExternalManifests: []v1alpha1.ExternalManifest{
			{
				Ref:       writeRef(t, "crd.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: before\n"),
				Lifecycle: v1alpha1.LifecyclePre,
			},
			{Ref: writeRef(t, "svc.yaml", "apiVersion: v1\nkind: Service\nmetadata:\n  name: beside\n")},
		},
	}

	plan, err := pack.RenderInstance(t.Context(), p, spec)
	if err != nil {
		t.Fatalf("RenderInstance() = error %v, want a plan", err)
	}

	if got, want := len(plan.Prerequisites), 1; got != want {
		t.Fatalf("len(Prerequisites) = %d, want %d", got, want)
	}
	if got, want := plan.Prerequisites[0].GetName(), "before"; got != want {
		t.Errorf("Prerequisites[0] = %q, want %q", got, want)
	}
	if got, want := len(plan.Objects), 2; got != want {
		t.Fatalf("len(Objects) = %d, want %d: the Deployment plus the external Service", got, want)
	}
	if got, want := plan.Objects[1].GetName(), "beside"; got != want {
		t.Errorf("Objects[1] = %q, want the external manifest %q", got, want)
	}

	// The inline patch won over the valuesRef document; the field it did not
	// mention kept the resolved value rather than the pack's default.
	deployment := plan.Objects[0]
	if got, want := imageOf(t, deployment), "nginx:1.27"; got != want {
		t.Errorf("substituted image = %q, want the inline value %q", got, want)
	}
	if got, want := deployment.GetLabels()["tier"], "frontend"; got != want {
		t.Errorf("substituted tier = %q, want the valuesRef value %q", got, want)
	}
}

// A reference that resolves to nothing surfaces as the resolver's own code:
// pack wraps ref errors and never re-tags them.
func TestRenderInstanceRefFailureKeepsRefCode(t *testing.T) {
	p, err := pack.Load(t.Context(), rawPack(), "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	spec := v1alpha1.PackSpec{ExternalManifests: []v1alpha1.ExternalManifest{
		{Ref: "file://" + filepath.Join(t.TempDir(), "absent.yaml")},
	}}

	_, err = pack.RenderInstance(t.Context(), p, spec)
	wantCode(t, err, ref.CodeFetchFailed)
}

// imageOf digs the container image out of a rendered Deployment.
func imageOf(t *testing.T, obj *unstructured.Unstructured) string {
	t.Helper()
	containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) == 0 {
		t.Fatalf("rendered object has no containers: found=%v err=%v", found, err)
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		t.Fatalf("container is %T, want a mapping", containers[0])
	}
	image, _ := container["image"].(string)
	return image
}
