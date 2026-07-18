package engine

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
)

func deployment(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": name, "namespace": "x"},
		"spec": map[string]any{
			"replicas": int64(1),
			"template": map[string]any{"spec": map[string]any{
				"containers": []any{map[string]any{"name": "main"}},
			}},
		},
	}}
}

func intp(i int) *int { return &i }

func TestApplyTuningPatchesReplicasAndResources(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("kustomize-controller"), deployment("source-controller")}
	v := &config.EngineTuning{Components: map[string]config.ComponentTuning{
		"kustomize-controller": {
			Replicas:  intp(2),
			Resources: map[string]any{"limits": map[string]any{"memory": "512Mi"}},
		},
	}}
	if err := ApplyTuning(objs, v); err != nil {
		t.Fatal(err)
	}
	rep, _, _ := unstructured.NestedInt64(objs[0].Object, "spec", "replicas")
	if rep != 2 {
		t.Fatalf("replicas = %d, want 2", rep)
	}
	cs, _, _ := unstructured.NestedSlice(objs[0].Object, "spec", "template", "spec", "containers")
	res := cs[0].(map[string]any)["resources"].(map[string]any)
	if res["limits"].(map[string]any)["memory"] != "512Mi" {
		t.Fatalf("resources not patched: %v", res)
	}
	// Untouched deployment stays untouched.
	rep2, _, _ := unstructured.NestedInt64(objs[1].Object, "spec", "replicas")
	if rep2 != 1 {
		t.Fatalf("source-controller must be untouched, replicas=%d", rep2)
	}
}

func TestApplyTuningUnknownComponentIsCube3009(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("source-controller")}
	v := &config.EngineTuning{Components: map[string]config.ComponentTuning{"nope": {Replicas: intp(2)}}}
	err := ApplyTuning(objs, v)
	if err == nil || !strings.Contains(err.Error(), "CUBE-3009") || !strings.Contains(err.Error(), "source-controller") {
		t.Fatalf("want CUBE-3009 naming valid components, got: %v", err)
	}
}

func TestApplyTuningNilIsNoop(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("a")}
	if err := ApplyTuning(objs, nil); err != nil {
		t.Fatal(err)
	}
}
