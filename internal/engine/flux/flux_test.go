package flux_test

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/flux"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
)

// TestConformance runs the shared engine suite against the real driver —
// the suite's first runner, hermetic because the seam is pure. The Want
// objects are hand-authored goldens pinning the sync wiring's
// source-derived content (url, ref, path, interval); no SpecValidator
// fixtures — flux deliberately does not implement the capability.
func TestConformance(t *testing.T) {
	engine.RunEngineConformance(t, func() (engine.Provider, engine.Fixtures) {
		return flux.New(), engine.Fixtures{
			NoSource: v1alpha1.EngineSpec{Provider: v1alpha1.EngineProviderFlux},
			Sources: []engine.SourceFixture{
				{Name: "git", Spec: gitSpec(), Want: wantGit()},
				{Name: "oci", Spec: ociSpec(), Want: wantOCI()},
			},
			Statuses: engine.StatusFixtures{
				Ready:         gitRepoWithStatus(2, 2, "True", "Succeeded", "stored artifact"),
				NotReady:      gitRepoWithStatus(2, 2, "False", "GitOperationFailed", "auth required"),
				Stale:         gitRepoWithStatus(2, 1, "True", "Succeeded", "stored artifact"),
				UnknownStatus: bareKustomization(),
				Unrecognized:  configMap(),
			},
		}
	})
}

// TestSourceObjectsUnknownKind is the driver's defensive guard (config
// validation is the primary gate): an unmappable source kind is the
// coded CUBE-ENG-006, superseding CUBE-BST-007.
func TestSourceObjectsUnknownKind(t *testing.T) {
	spec := v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{Kind: "svn", URL: "https://x"}}
	_, err := flux.New().SourceObjects(t.Context(), spec)
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("SourceObjects(kind=svn) = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != engine.CodeUnsupportedSourceKind {
		t.Fatalf("code = %s, want %s", coded.Code, engine.CodeUnsupportedSourceKind)
	}
}

// TestEngineNamespaceIsSubstrate pins the degenerate fact the shared
// suite only checks for non-emptiness: flux's engine namespace IS the
// substrate namespace.
func TestEngineNamespaceIsSubstrate(t *testing.T) {
	if got := flux.New().EngineNamespace(); got != substrate.Namespace {
		t.Fatalf("EngineNamespace() = %q, want the substrate namespace %q", got, substrate.Namespace)
	}
}

// TestReconciledReasons pins the diagnostic quality of the pending
// reasons that feed CUBE-BST-009: they must carry what a human acts on.
func TestReconciledReasons(t *testing.T) {
	noObserved := gitRepoWithStatus(2, 2, "True", "Succeeded", "stored artifact")
	unstructured.RemoveNestedField(noObserved.Object, "status", "observedGeneration")
	cases := []struct {
		name string
		obj  *unstructured.Unstructured
		want []string
	}{
		{"not ready carries the CR's own diagnosis",
			gitRepoWithStatus(2, 2, "False", "GitOperationFailed", "auth required"),
			[]string{"Ready=False", "GitOperationFailed", "auth required"}},
		{"stale names both generations",
			gitRepoWithStatus(2, 1, "True", "Succeeded", "stored artifact"),
			[]string{"observedGeneration 1", "generation 2"}},
		{"missing observedGeneration is named",
			noObserved,
			[]string{"observedGeneration"}},
		{"no status names the missing condition",
			bareKustomization(),
			[]string{"no Ready condition"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason, err := flux.New().Reconciled(tc.obj)
			if err != nil || ok {
				t.Fatalf("Reconciled() = (%v, %q, %v), want not reconciled without error", ok, reason, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(reason, want) {
					t.Errorf("reason %q missing %q", reason, want)
				}
			}
		})
	}
}

// gitSpec is a defaulted git engine spec, the values api defaulting
// would produce.
func gitSpec() v1alpha1.EngineSpec {
	return v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
		Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/fleet",
		Ref: "main", Path: "./", Interval: "10m",
	}}
}

// ociSpec is a defaulted oci engine spec.
func ociSpec() v1alpha1.EngineSpec {
	return v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
		Kind: v1alpha1.EngineSourceOCI, URL: "oci://ghcr.io/org/fleet",
		Ref: "latest", Path: "./", Interval: "10m",
	}}
}

// wantGit is the golden sync wiring for gitSpec: the GitRepository +
// Kustomization pair with every source-derived field pinned.
func wantGit() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		wiringObject("source.toolkit.fluxcd.io/v1", "GitRepository", map[string]any{
			"interval": "10m",
			"url":      "https://github.com/org/fleet",
			"ref":      map[string]any{"branch": "main"},
		}),
		wiringObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", map[string]any{
			"interval":  "10m",
			"path":      "./",
			"prune":     true,
			"sourceRef": map[string]any{"kind": "GitRepository", "name": "flux-system"},
		}),
	}
}

// wantOCI is the golden sync wiring for ociSpec.
func wantOCI() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		wiringObject("source.toolkit.fluxcd.io/v1", "OCIRepository", map[string]any{
			"interval": "10m",
			"url":      "oci://ghcr.io/org/fleet",
			"ref":      map[string]any{"tag": "latest"},
			"provider": "generic",
		}),
		wiringObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", map[string]any{
			"interval":  "10m",
			"path":      "./",
			"prune":     true,
			"sourceRef": map[string]any{"kind": "OCIRepository", "name": "flux-system"},
		}),
	}
}

// wiringObject builds one golden wiring object: Flux's shared
// name/namespace convention plus the given spec.
func wiringObject(apiVersion, kind string, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      "flux-system",
			"namespace": substrate.Namespace,
		},
		"spec": spec,
	}}
}

// gitRepoWithStatus builds a GitRepository at generation gen whose Ready
// condition and observedGeneration read as given.
func gitRepoWithStatus(gen, observed int64, ready, reason, message string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       "GitRepository",
		"metadata": map[string]any{
			"name": "flux-system", "namespace": substrate.Namespace,
			"generation": gen,
		},
		"status": map[string]any{
			"observedGeneration": observed,
			"conditions": []any{map[string]any{
				"type": "Ready", "status": ready, "reason": reason, "message": message,
			}},
		},
	}}
}

// bareKustomization is a recognized object whose status is absent — the
// unknown-status fixture.
func bareKustomization() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": "flux-system", "namespace": substrate.Namespace},
	}}
}

// configMap is an object outside the driver's coverage — the
// unrecognized fixture.
func configMap() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "whatever", "namespace": "default"},
	}}
}
