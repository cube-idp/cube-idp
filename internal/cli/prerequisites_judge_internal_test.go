package cli

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// TestGatewayUnitJudge walks what the composed judge must do: pass the
// cube-authored Gateway (its readiness is not gated in M11), delegate the CR
// pair to the domain predicate, and let a genuine unrecognized object keep
// its CUBE-GWY-003 rather than be swallowed.
func TestGatewayUnitJudge(t *testing.T) {
	t.Parallel()
	notReady := crObject("helm.toolkit.fluxcd.io/v2", "HelmRelease", map[string]any{
		"conditions": []any{map[string]any{"type": "Ready", "status": "False", "reason": "InstallFailed"}},
	})
	stale := crObject("source.toolkit.fluxcd.io/v1", "OCIRepository", map[string]any{
		"conditions":         []any{map[string]any{"type": "Ready", "status": "True"}},
		"observedGeneration": int64(1),
	})
	stale.SetGeneration(2)
	cases := []struct {
		name    string
		obj     *unstructured.Unstructured
		wantOK  bool
		wantErr bool
	}{
		{name: "the cube-authored Gateway passes ungated", obj: gateway.GatewayObject(testDomain), wantOK: true},
		{name: "a not-ready HelmRelease delegates", obj: notReady},
		{name: "a stale OCIRepository delegates", obj: stale},
		{name: "an unrelated kind keeps its coded error",
			obj: crObject("apps/v1", "Deployment", nil), wantErr: true},
		// The Gateway is excused by identity, not by kind: a Gateway some
		// other unit put in this one would otherwise be waved through a
		// judgment that never looked at it.
		{name: "a foreign Gateway of the same kind delegates",
			obj: foreignGateway(func(o *unstructured.Unstructured) { o.SetName("someone-elses") }), wantErr: true},
		{name: "a Gateway in another namespace delegates",
			obj: foreignGateway(func(o *unstructured.Unstructured) { o.SetNamespace("other-system") }), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, reason, err := gatewayUnitJudge(tc.obj)
			if tc.wantErr {
				assertCode(t, err, gateway.CodeUnrecognizedObject)
				return
			}
			if err != nil {
				t.Fatalf("judge returned %v, want no error", err)
			}
			if ok != tc.wantOK {
				t.Errorf("judge = %v (%q), want %v", ok, reason, tc.wantOK)
			}
			if !ok && reason == "" {
				t.Error("a not-ready verdict carries no reason; the timeout diagnostics need one")
			}
		})
	}
}

// crObject builds a judgeable object with the given status fields.
func crObject(apiVersion, kind string, status map[string]any) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": "x", "namespace": "gateway-system"},
	}}
	if status != nil {
		o.Object["status"] = status
	}
	return o
}

// assertCode asserts err carries exactly the expected cube code. Identity is
// the code, never the message.
func assertCode(t *testing.T, err error, want cubeerr.Code) {
	t.Helper()
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("err = %v, want a *cubeerr.Coded %s", err, want)
	}
	if coded.Code != want {
		t.Fatalf("code = %s, want %s", coded.Code, want)
	}
}

// foreignGateway builds a Gateway that is not this cube's, differing from it
// only by the mutation applied.
func foreignGateway(mutate func(*unstructured.Unstructured)) *unstructured.Unstructured {
	o := gateway.GatewayObject("dev.cube.test")
	mutate(o)
	return o
}
