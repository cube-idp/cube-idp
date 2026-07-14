package diff

import (
	"context"
	"sort"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

// fakeEngine is a minimal engine.Engine double: InstallManifests and Deliver
// are the only methods desiredState calls, and their output only needs to be
// deterministic and distinct per pack — the point of this test isn't engine
// behavior (that's internal/engine/{flux,argocd}'s contract suite), it's
// whether diff.desiredState's book-keeping (desired vs orphanOnly) accounts
// for every object up.Run itself applies/inventories.
type fakeEngine struct{}

func (fakeEngine) Install(context.Context, *apply.Applier, time.Duration) error { return nil }

func (fakeEngine) InstallManifests() ([]*unstructured.Unstructured, error) {
	return []*unstructured.Unstructured{
		{Object: map[string]any{
			"apiVersion": "apps/v1", "kind": "Deployment",
			"metadata": map[string]any{"name": "engine-controller", "namespace": "engine-system"},
		}},
	}, nil
}

func (fakeEngine) Deliver(_ context.Context, r *pack.Rendered, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return []*unstructured.Unstructured{
		{Object: map[string]any{
			"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": "Kustomization",
			"metadata": map[string]any{"name": "cube-idp-" + r.Name, "namespace": "engine-system"},
		}},
	}, nil
}

func (fakeEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	return nil, nil
}

func (fakeEngine) Uninstall(context.Context, *apply.Applier, time.Duration) error { return nil }

// identityKey mirrors diff.go's refKey, keyed off group/kind/namespace/name —
// exactly what orphanRefs (and, in production, the live inventory) compares
// on.
func identityKey(o *unstructured.Unstructured) string {
	gvk := o.GroupVersionKind()
	return refKey(gvk.Group, gvk.Kind, o.GetNamespace(), o.GetName())
}

func identitySet(objs ...[]*unstructured.Unstructured) map[string]bool {
	set := make(map[string]bool)
	for _, list := range objs {
		for _, o := range list {
			set[identityKey(o)] = true
		}
	}
	return set
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestDesiredStateMatchesUpAppliedSet is the false-orphan regression net:
// desiredState's desired+orphanOnly identities must cover EXACTLY what
// up.Run applies/inventories for the same cube (registry + Pack CRD +
// engine install + per-pack deliver objects + the D6 gateway route + the D11
// Pack records + the gateway TLS Namespace/Secret — see up.go and
// up/tls.go's gatewayTLSObjects). Anything up.Run applies that this test
// doesn't also expect here would show up as a false "orphaned" entry on
// every converged cube; anything expected here that up.Run doesn't actually
// apply would silently hide a real orphan.
func TestDesiredStateMatchesUpAppliedSet(t *testing.T) {
	cube := &config.Cube{
		Metadata: config.Metadata{Name: "test"},
		Spec: config.Spec{
			Engine:  config.EngineSpec{Type: "flux"},
			Gateway: config.GatewaySpec{Pack: "demo", Host: "cube-idp.localtest.me", Port: 8443, Ref: "../pack/testdata/demo"},
			Packs:   []config.PackRef{{Ref: "../pack/testdata/demo-kustomize"}},
		},
	}

	desired, orphanOnly, entries, err := desiredState(context.Background(), cube, fakeEngine{})
	if err != nil {
		t.Fatalf("desiredState: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 lock entries (gateway + 1 pack), got %d", len(entries))
	}

	// Independently assemble what up.Run applies/inventories, reusing the
	// same pure/exported helpers up.go itself calls (registry.Manifests,
	// pack.CRD, registry.GatewayRoute, pack.PackObject) so this test tracks
	// their real signatures rather than re-describing them by hand. The
	// gateway TLS Namespace/Secret can't be reused directly (up/tls.go's
	// gatewayTLSObjects needs a live CA) so their identity is pinned here
	// against the documented convention (namespace == gateway pack name,
	// secret name "cube-idp-gateway-tls") — if that convention ever changes,
	// this expectation (and diff.go's orphanOnly) must change together.
	regObjs, err := registry.Manifests()
	if err != nil {
		t.Fatal(err)
	}
	crd, err := pack.CRD()
	if err != nil {
		t.Fatal(err)
	}
	eng := fakeEngine{}
	installObjs, err := eng.InstallManifests()
	if err != nil {
		t.Fatal(err)
	}
	route := registry.GatewayRoute(cube.Spec.Gateway.Host)
	tlsNamespace := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": cube.Spec.Gateway.Pack},
	}}
	tlsSecret := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "cube-idp-gateway-tls", "namespace": cube.Spec.Gateway.Pack},
	}}

	var wantDeliver []*unstructured.Unstructured
	var wantPackRecords []*unstructured.Unstructured
	refs := append([]config.PackRef{{Ref: cube.Spec.Gateway.PackRef()}}, cube.Spec.Packs...)
	dir, err := pack.DefaultCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, pr := range refs {
		p, err := pack.Fetch(context.Background(), pr.Ref, dir)
		if err != nil {
			t.Fatalf("Fetch(%s): %v", pr.Ref, err)
		}
		rendered, err := p.Render(pr.Values)
		if err != nil {
			t.Fatalf("Render(%s): %v", pr.Ref, err)
		}
		deliverObjs, err := eng.Deliver(context.Background(), rendered, engine.ArtifactRef{Repo: "packs/" + rendered.Name, Tag: rendered.Version})
		if err != nil {
			t.Fatal(err)
		}
		wantDeliver = append(wantDeliver, deliverObjs...)
		wantPackRecords = append(wantPackRecords, pack.PackObject(p, cube.Spec.Gateway, false))
	}

	wantApplied := identitySet(regObjs, []*unstructured.Unstructured{crd}, installObjs,
		[]*unstructured.Unstructured{tlsNamespace, tlsSecret}, wantDeliver, wantPackRecords,
		[]*unstructured.Unstructured{route})

	gotCovered := identitySet(desired, orphanOnly)

	if len(wantApplied) != len(gotCovered) {
		t.Fatalf("identity set size mismatch: up.Run applies %d distinct objects, desiredState covers %d\napplied: %v\ncovered: %v",
			len(wantApplied), len(gotCovered), sortedKeys(wantApplied), sortedKeys(gotCovered))
	}
	for key := range wantApplied {
		if !gotCovered[key] {
			t.Errorf("up.Run applies %s but desiredState's desired+orphanOnly never mentions it — would show up as a false orphan on every `cube-idp diff`", key)
		}
	}
	for key := range gotCovered {
		if !wantApplied[key] {
			t.Errorf("desiredState claims %s but up.Run never applies it — would silently mask a real orphan of that identity", key)
		}
	}
}
