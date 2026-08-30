package gateway_test

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// TestReconciled walks the no-stale-success doctrine over this domain's
// own CR vocabulary: Ready true alone is not enough, the status must also
// describe the current generation, and an object outside the domain's
// coverage is a coded error rather than a false negative.
func TestReconciled(t *testing.T) {
	noObserved := helmRelease(2, 2, "True", "Succeeded", "install succeeded")
	unstructured.RemoveNestedField(noObserved.Object, "status", "observedGeneration")
	cases := []struct {
		name       string
		obj        *unstructured.Unstructured
		want       bool
		wantReason []string
		wantErr    bool
	}{
		{name: "ready HelmRelease", obj: helmRelease(2, 2, "True", "Succeeded", "install succeeded"), want: true},
		{name: "ready OCIRepository", obj: ociRepository(1, 1, "True", "Succeeded", "stored artifact"), want: true},
		{
			name:       "not ready carries the CR's own diagnosis",
			obj:        helmRelease(2, 2, "False", "InstallFailed", "chart pull failed"),
			wantReason: []string{"Ready=False", "InstallFailed", "chart pull failed"},
		},
		{
			// The contract's Testing clause lists an unknown fixture
			// alongside ready/not-ready/stale: a controller that has
			// observed the object but reached no verdict is pending, not
			// reconciled, and the diagnostic must say which.
			name:       "Ready=Unknown is pending, not reconciled",
			obj:        helmRelease(2, 2, "Unknown", "Progressing", "reconciliation in progress"),
			wantReason: []string{"Ready=Unknown", "Progressing", "reconciliation in progress"},
		},
		{
			name:       "stale names both generations",
			obj:        helmRelease(3, 2, "True", "Succeeded", "install succeeded"),
			wantReason: []string{"observedGeneration 2", "generation 3"},
		},
		{
			name:       "missing observedGeneration is named",
			obj:        noObserved,
			wantReason: []string{"observedGeneration"},
		},
		{
			name:       "no conditions names the missing condition",
			obj:        bareRelease(),
			wantReason: []string{"no Ready condition"},
		},
		{name: "unrecognized object", obj: configMap(), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason, err := gateway.Reconciled(tc.obj)
			if tc.wantErr {
				assertUnrecognized(t, err)
				return
			}
			if err != nil {
				t.Fatalf("Reconciled: %v", err)
			}
			if ok != tc.want {
				t.Errorf("reconciled = %v, want %v (reason %q)", ok, tc.want, reason)
			}
			for _, want := range tc.wantReason {
				if !strings.Contains(reason, want) {
					t.Errorf("reason %q does not carry %q", reason, want)
				}
			}
			if tc.want && reason != "" {
				t.Errorf("reconciled object carries reason %q, want none", reason)
			}
		})
	}
}

// TestReconciledCoversTheEmittedPair ties the predicate to content: every
// object HelmPairObjects emits must be recognized, so a change to the pair
// cannot leave the wait judging an object it refuses.
func TestReconciledCoversTheEmittedPair(t *testing.T) {
	for _, obj := range gateway.HelmPairObjects() {
		if _, _, err := gateway.Reconciled(obj); err != nil {
			t.Errorf("Reconciled(%s %s) = %v, want the emitted pair to be recognized",
				obj.GetAPIVersion(), obj.GetKind(), err)
		}
	}
}

// assertUnrecognized asserts err is this domain's coverage error, by code.
func assertUnrecognized(t *testing.T, err error) {
	t.Helper()
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != gateway.CodeUnrecognizedObject {
		t.Fatalf("code = %s, want %s", coded.Code, gateway.CodeUnrecognizedObject)
	}
}

// helmRelease builds a HelmRelease at the given generations carrying one
// Ready condition.
func helmRelease(generation, observed int64, status, reason, message string) *unstructured.Unstructured {
	return withStatus("helm.toolkit.fluxcd.io/v2", "HelmRelease", generation, observed, status, reason, message)
}

// ociRepository builds an OCIRepository at the given generations carrying
// one Ready condition.
func ociRepository(generation, observed int64, status, reason, message string) *unstructured.Unstructured {
	return withStatus("source.toolkit.fluxcd.io/v1", "OCIRepository", generation, observed, status, reason, message)
}

// withStatus builds one of the domain's CRs with a Ready condition and an
// observedGeneration.
func withStatus(apiVersion, kind string, generation, observed int64, status, reason, message string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": "traefik-gateway", "generation": generation},
		"status": map[string]any{
			"observedGeneration": observed,
			"conditions": []any{map[string]any{
				"type": "Ready", "status": status, "reason": reason, "message": message,
			}},
		},
	}}
}

// bareRelease builds a HelmRelease the controller has not reported on yet.
func bareRelease() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "helm.toolkit.fluxcd.io/v2",
		"kind":       "HelmRelease",
		"metadata":   map[string]any{"name": "traefik-gateway", "generation": int64(1)},
	}}
}

// configMap is an object outside the domain's declared coverage.
func configMap() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "coredns", "namespace": "kube-system"},
	}}
}
