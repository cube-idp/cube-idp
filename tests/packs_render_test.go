// Package tests holds cross-package smoke tests that don't belong to any
// single internal package: this file renders every starter pack shipped in
// packs/ end-to-end (pack.Fetch -> Render), the same path cube-idp's `up`
// orchestration exercises for a real cluster.
package tests

import (
	"context"
	"testing"

	"github.com/rafpe/cube-idp/internal/pack"
)

// namespacedKinds are kinds we know are namespace-scoped and that appear
// in the starter packs. cube-idp's delivery path (rendered objects -> OCI
// artifact -> Flux Kustomization with no targetNamespace) applies objects
// exactly as rendered, so any of these missing metadata.namespace would
// silently land in `default` — the bug class this guards against is
// vendoring an upstream bundle (like argo-cd's install.yaml) that assumes
// `kubectl apply -n <ns>` supplies the namespace externally.
var namespacedKinds = map[string]bool{
	"Deployment":     true,
	"StatefulSet":    true,
	"Service":        true,
	"ConfigMap":      true,
	"Secret":         true,
	"ServiceAccount": true,
	"Role":           true,
	"RoleBinding":    true,
	"NetworkPolicy":  true,
	"HTTPRoute":      true,
}

// TestStarterPacksRender is network-gated (helm chart pulls +
// gateway-api/argo-cd manifest parsing): it renders each starter pack with
// no user-supplied values (nil), matching how `cube-idp up` invokes packs
// with only their pack.cue defaults, and asserts each produces at least
// one object and that every known-namespaced object carries an explicit
// metadata.namespace. This is deliberately a smoke test, not a golden test
// — starter pack manifests are vendored upstream content that changes on
// every chart bump, so pinning exact output would make routine version
// bumps fail CI.
func TestStarterPacksRender(t *testing.T) {
	if testing.Short() {
		t.Skip("helm renders hit the network")
	}
	for _, dir := range []string{"../packs/traefik", "../packs/gitea", "../packs/argocd"} {
		p, err := pack.Fetch(context.Background(), dir, t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		r, err := p.Render(nil)
		if err != nil {
			t.Fatalf("%s render: %v", dir, err)
		}
		if len(r.Objects) == 0 {
			t.Fatalf("%s rendered zero objects", dir)
		}
		for _, o := range r.Objects {
			if namespacedKinds[o.GetKind()] && o.GetNamespace() == "" {
				t.Errorf("%s: %s %q has no metadata.namespace — it would land in `default` when applied",
					dir, o.GetKind(), o.GetName())
			}
		}
	}
}
