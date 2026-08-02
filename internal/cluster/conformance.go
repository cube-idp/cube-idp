package cluster

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// RunClusterConformance asserts the behavioral contract every Provisioner
// must satisfy: absent→ensure→exists round-trip, idempotent Ensure and
// Delete, and a parseable kubeconfig only while the cluster exists.
// Drivers run it from their own test packages.
func RunClusterConformance(t *testing.T, factory func() Provisioner) {
	t.Helper()
	const name = "cube-conformance"
	ctx := context.Background()
	p := factory()
	t.Cleanup(func() { _ = p.Delete(ctx, name) })

	assertExists := func(want bool, when string) {
		t.Helper()
		got, err := p.Exists(ctx, name)
		if err != nil {
			t.Fatalf("Exists %s: %v", when, err)
		}
		if got != want {
			t.Fatalf("Exists %s = %v, want %v", when, got, want)
		}
	}

	assertSpecValidation(t, p, name)

	assertExists(false, "before Ensure")
	if err := p.Ensure(ctx, Spec{Name: name}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	assertExists(true, "after Ensure")
	if err := p.Ensure(ctx, Spec{Name: name}); err != nil {
		t.Fatalf("Ensure (second, must no-op): %v", err)
	}

	raw, err := p.Kubeconfig(ctx, name)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	assertKubeconfigHasContexts(t, raw)

	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertExists(false, "after Delete")
	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete (second, must no-op): %v", err)
	}
	if _, err := p.Kubeconfig(ctx, name); err == nil {
		t.Fatal("Kubeconfig after Delete: want error, got nil")
	}
}

// assertSpecValidation exercises the optional SpecValidator capability:
// an empty payload must pass, and a payload no provider can decode (a
// YAML array where a config object belongs) must yield the domain's
// coded invalid-forProvider error. No-op for drivers without the
// capability.
func assertSpecValidation(t *testing.T, p Provisioner, name string) {
	t.Helper()
	v, ok := p.(SpecValidator)
	if !ok {
		return
	}
	if err := v.ValidateSpec(Spec{Name: name}); err != nil {
		t.Fatalf("ValidateSpec (no payload): %v", err)
	}
	err := v.ValidateSpec(Spec{Name: name,
		ForProvider: &runtime.RawExtension{Raw: []byte(`[1]`)}})
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("ValidateSpec (invalid payload) = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != CodeInvalidForProvider {
		t.Fatalf("code = %s, want %s", coded.Code, CodeInvalidForProvider)
	}
}

// assertKubeconfigHasContexts checks the raw kubeconfig is parseable YAML
// with at least one context entry.
func assertKubeconfigHasContexts(t *testing.T, raw []byte) {
	t.Helper()
	var kc struct {
		Contexts []struct {
			Name string `json:"name"`
		} `json:"contexts"`
	}
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		t.Fatalf("Kubeconfig not parseable YAML: %v", err)
	}
	if len(kc.Contexts) == 0 {
		t.Fatal("Kubeconfig has no contexts")
	}
}
