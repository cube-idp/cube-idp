package cluster

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// RunClusterConformance asserts the behavioral contract every Provisioner
// must satisfy: absent→ensure→exists round-trip, idempotent Ensure (a
// second Ensure against the existing cluster must succeed and leave the
// kubeconfig byte-identical — the seam's stability guarantee) and Delete,
// and a parseable kubeconfig only while the cluster exists. Drivers run
// it from their own test packages.
func RunClusterConformance(t *testing.T, factory func() Provisioner) {
	t.Helper()
	const name = "cube-conformance"
	ctx := t.Context()
	p := factory()
	// Go cancels t.Context() before cleanup functions run, so the
	// safety-net Delete gets a context detached from the test lifecycle —
	// bounded, so a hung backend cannot stall the run indefinitely.
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if err := p.Delete(cctx, name); err != nil {
			t.Errorf("cleanup: delete cluster %s: %v", name, err)
		}
	})

	assertSpecValidation(t, p, name)

	assertExists(t, ctx, p, name, false, "before Ensure")
	if err := p.Ensure(ctx, Spec{Name: name}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	assertExists(t, ctx, p, name, true, "after Ensure")

	raw, err := p.Kubeconfig(ctx, name)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	assertKubeconfigHasContexts(t, raw)

	assertEnsureIdempotent(t, ctx, p, name, raw)

	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertExists(t, ctx, p, name, false, "after Delete")
	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete (second, must no-op): %v", err)
	}
	if _, err := p.Kubeconfig(ctx, name); err == nil {
		t.Fatal("Kubeconfig after Delete: want error, got nil")
	}
}

// assertExists fails the test unless Exists reports want.
func assertExists(t *testing.T, ctx context.Context, p Provisioner, name string, want bool, when string) {
	t.Helper()
	got, err := p.Exists(ctx, name)
	if err != nil {
		t.Fatalf("Exists %s: %v", when, err)
	}
	if got != want {
		t.Fatalf("Exists %s = %v, want %v", when, got, want)
	}
}

// assertEnsureIdempotent re-runs Ensure against the already-existing
// cluster and checks the strongest idempotency signal the seam exposes:
// the cluster still exists and its kubeconfig is byte-identical to the
// one fetched before the call. That relies on the seam's stability
// guarantee — Kubeconfig returns identical bytes while the cluster is
// untouched — and catches a driver whose Ensure recreates the cluster
// (new certificates, new endpoint). Mutations that never surface in the
// kubeconfig are beyond what the seam can observe.
func assertEnsureIdempotent(t *testing.T, ctx context.Context, p Provisioner, name string, before []byte) {
	t.Helper()
	if err := p.Ensure(ctx, Spec{Name: name}); err != nil {
		t.Fatalf("Ensure (second, must no-op): %v", err)
	}
	assertExists(t, ctx, p, name, true, "after second Ensure")
	after, err := p.Kubeconfig(ctx, name)
	if err != nil {
		t.Fatalf("Kubeconfig after second Ensure: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Ensure on an existing cluster must no-op: kubeconfig changed after the second Ensure (cluster recreated or mutated)")
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
