package diff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/registry"
)

// writeEngineFixture materializes a minimal on-disk engine pack (named
// cube-engine-flux, one Namespace manifest) and returns its dir. Engine-as-pack
// (T9): desiredState now fetches+renders the engine pack via
// EngineSpec.PackRef(), so every test cube must point Spec.Engine.Ref at a
// resolvable pack — the published 0.1.0 default does not exist until T15.
func writeEngineFixture(t *testing.T) string {
	t.Helper()
	pd := filepath.Join(t.TempDir(), "cube-engine-flux")
	if err := os.MkdirAll(filepath.Join(pd, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "pack.cue"), []byte("name: \"cube-engine-flux\"\nversion: \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "manifests", "ns.yaml"), []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: flux-system\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return pd
}

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
			"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "OCIRepository",
			"metadata": map[string]any{"name": "cube-idp-" + r.Name, "namespace": "engine-system"},
		}},
		{Object: map[string]any{
			"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": "Kustomization",
			"metadata": map[string]any{"name": "cube-idp-" + r.Name, "namespace": "engine-system"},
		}},
	}, nil
}

// DeliverGit mirrors the flux shape truthfully: a distinct source kind
// (GitRepository, never OCIRepository) plus the shared Kustomization —
// TestDesiredStateRepoDeliveredPack relies on the kinds differing exactly
// the way the real engines' do.
func (fakeEngine) DeliverGit(_ context.Context, name string, _ engine.GitSource, _ []string) ([]*unstructured.Unstructured, error) {
	return []*unstructured.Unstructured{
		{Object: map[string]any{
			"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "GitRepository",
			"metadata": map[string]any{"name": "cube-idp-" + name, "namespace": "engine-system"},
		}},
		{Object: map[string]any{
			"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": "Kustomization",
			"metadata": map[string]any{"name": "cube-idp-" + name, "namespace": "engine-system"},
		}},
	}, nil
}

// DeliverSelf mirrors the flux engine self-source shape truthfully:
// the plain cube-engine names (never cube-idp-<pack>) are exactly what
// diff.desiredState's orphan stubs must reproduce.
func (fakeEngine) DeliverSelf(context.Context, engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return []*unstructured.Unstructured{
		{Object: map[string]any{
			"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "OCIRepository",
			"metadata": map[string]any{"name": "cube-engine", "namespace": "engine-system"},
		}},
		{Object: map[string]any{
			"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": "Kustomization",
			"metadata": map[string]any{"name": "cube-engine", "namespace": "engine-system"},
		}},
	}, nil
}

func (fakeEngine) Poke(context.Context, *apply.Applier, string) error { return nil }

func (fakeEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	return nil, nil
}

func (fakeEngine) Uninstall(context.Context, *apply.Applier, time.Duration) error { return nil }

// OrdersDeliveries: this fake mirrors flux's shapes (GitRepository +
// Kustomization) throughout, so it answers true — desiredState's
// DependsOn-threading is exercised by the real engines' own tests
// (internal/engine/flux, internal/engine/argocd), not here.
func (fakeEngine) OrdersDeliveries() bool { return true }

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
	enginePack := writeEngineFixture(t)
	cube := &config.Cube{
		Metadata: config.Metadata{Name: "test"},
		Spec: config.Spec{
			Engine:  config.EngineSpec{Type: "flux", Ref: enginePack},
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
	// Engine-as-pack (T9): the engine install is the rendered engine pack, so
	// mirror desiredState's FetchRenderEngine call for the want-set — the fake
	// engine's InstallManifests no longer feeds desiredState.
	engineDir, err := pack.DefaultCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	enginePk, engineRendered, err := pack.FetchRenderEngine(context.Background(), cube.Spec.Engine, cube.Spec.Gateway, cube.Spec.Engine.PackRef(), engineDir)
	if err != nil {
		t.Fatal(err)
	}
	installObjs := engineRendered.Objects
	route := registry.GatewayRoute(cube.Spec.Gateway.Host, cube.Spec.Gateway.Pack)
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
		// customized/delivery mirror up.Run's record-writer
		// expressions; only the record's identity is compared below, but
		// stay truthful.
		wantPackRecords = append(wantPackRecords, pack.PackObject(p, cube.Spec.Gateway, false,
			len(pr.Values) > 0 || pr.ExtraManifests != "", pr.Delivery, nil))
	}
	// Engine-as-pack (T9): up.Run also writes the engine's own D11 Pack record
	// (delivery "engine"); desiredState mirrors its identity in orphanOnly.
	wantPackRecords = append(wantPackRecords, pack.PackObject(enginePk, cube.Spec.Gateway, false,
		len(cube.Spec.Engine.Values) > 0, "engine", nil))

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

// TestDesiredStateRepoDeliveredPack pins the diff mirror for repo delivery: a
// delivery: repo pack contributes NO OCI delivery objects to the dry-run
// diff set — up applies engine git-source objects instead, whose spec
// embeds live-derived state (the gitea admin owner in the clone URL), so
// re-rendering them here would fabricate fields. Their identities go to
// orphanOnly (the Pack-record reasoning), keeping a converged
// repo-delivered cube free of false orphans AND of phantom OCI-source
// drift. The gateway pack (always OCI) keeps its full-spec diff.
func TestDesiredStateRepoDeliveredPack(t *testing.T) {
	// pack.ResolveOrder (p6 DEP1/DEP2) requires a "gitea" pack whenever any
	// delivery: repo pack is declared (gitea stays an optional pack but is
	// mandatory whenever one is, CUBE-4018 otherwise) —
	// desiredState now validates the graph, so the test cube must satisfy
	// that same rule config.Load already enforces in production.
	cube := &config.Cube{
		Metadata: config.Metadata{Name: "test"},
		Spec: config.Spec{
			Engine:  config.EngineSpec{Type: "flux", Ref: writeEngineFixture(t)},
			Gateway: config.GatewaySpec{Pack: "demo", Host: "cube-idp.localtest.me", Port: 8443, Ref: "../pack/testdata/demo"},
			Packs: []config.PackRef{
				{Ref: "../pack/testdata/demo-kustomize", Delivery: "repo"},
				{Ref: "../pack/testdata/gitea"},
			},
		},
	}

	desired, orphanOnly, entries, err := desiredState(context.Background(), cube, fakeEngine{})
	if err != nil {
		t.Fatalf("desiredState: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("repo-delivered packs still get lock entries: %d", len(entries))
	}

	desiredSet := identitySet(desired)
	orphanSet := identitySet(orphanOnly)
	const ns = "engine-system"
	ociSrc := refKey("source.toolkit.fluxcd.io", "OCIRepository", ns, "cube-idp-demo-kustomize")
	gitSrc := refKey("source.toolkit.fluxcd.io", "GitRepository", ns, "cube-idp-demo-kustomize")
	kust := refKey("kustomize.toolkit.fluxcd.io", "Kustomization", ns, "cube-idp-demo-kustomize")
	if desiredSet[ociSrc] || desiredSet[gitSrc] || desiredSet[kust] {
		t.Fatalf("repo-delivered pack must contribute no full-spec delivery objects to the diff set:\n%v", sortedKeys(desiredSet))
	}
	if !orphanSet[gitSrc] || !orphanSet[kust] {
		t.Fatalf("repo-delivered pack's git-source identities must be orphan-tracked:\n%v", sortedKeys(orphanSet))
	}
	if orphanSet[ociSrc] {
		t.Fatalf("no OCI source identity belongs to a repo-delivered pack:\n%v", sortedKeys(orphanSet))
	}
	// The gateway pack stays a full-spec OCI diff.
	if !desiredSet[refKey("source.toolkit.fluxcd.io", "OCIRepository", ns, "cube-idp-demo")] {
		t.Fatalf("gateway pack must keep its OCI delivery objects:\n%v", sortedKeys(desiredSet))
	}
}

// TestDesiredStateSelfManagedEngine pins the diff mirror for engine self-management: with
// spec.engine.selfManage on, up.Run additionally applies + inventories the
// engine's cube-engine self-source objects, whose SOURCE carries a fresh
// reconcile-now annotation per render — so desiredState must track their
// identities in orphanOnly (never full-spec through a.Diff, the perpetual
// "changed" trap) and contribute nothing when selfManage is off.
func TestDesiredStateSelfManagedEngine(t *testing.T) {
	cube := &config.Cube{
		Metadata: config.Metadata{Name: "test"},
		Spec: config.Spec{
			Engine:  config.EngineSpec{Type: "flux", SelfManage: true, Ref: writeEngineFixture(t)},
			Gateway: config.GatewaySpec{Pack: "demo", Host: "cube-idp.localtest.me", Port: 8443, Ref: "../pack/testdata/demo"},
		},
	}

	desired, orphanOnly, _, err := desiredState(context.Background(), cube, fakeEngine{})
	if err != nil {
		t.Fatalf("desiredState: %v", err)
	}
	desiredSet := identitySet(desired)
	orphanSet := identitySet(orphanOnly)
	const ns = "engine-system"
	selfSrc := refKey("source.toolkit.fluxcd.io", "OCIRepository", ns, "cube-engine")
	selfKust := refKey("kustomize.toolkit.fluxcd.io", "Kustomization", ns, "cube-engine")
	if !orphanSet[selfSrc] || !orphanSet[selfKust] {
		t.Fatalf("self-source identities must be orphan-tracked on a selfManage cube:\n%v", sortedKeys(orphanSet))
	}
	if desiredSet[selfSrc] || desiredSet[selfKust] {
		t.Fatalf("self-source objects must never enter the full-spec diff set:\n%v", sortedKeys(desiredSet))
	}

	// selfManage off: no self identities anywhere (the sets a cube without
	// engine self-management produces, exactly).
	cube.Spec.Engine.SelfManage = false
	desired, orphanOnly, _, err = desiredState(context.Background(), cube, fakeEngine{})
	if err != nil {
		t.Fatalf("desiredState: %v", err)
	}
	off := identitySet(desired, orphanOnly)
	if off[selfSrc] || off[selfKust] {
		t.Fatalf("selfManage off must contribute no self-source identities:\n%v", sortedKeys(off))
	}
}

// TestDesiredStateFailsOnDepCycle pins p6 DEP2's diff-side wiring: desiredState
// now calls pack.ResolveOrder (mirroring up.Run's pass 2) over the fetched+
// rendered pack set, so a cube whose packs.dependsOn forms a cycle must fail
// desiredState itself with CUBE-4019 — a `cube-idp diff` on such a cube must
// surface the cycle instead of silently rendering a stale/partial diff.
func TestDesiredStateFailsOnDepCycle(t *testing.T) {
	cube := &config.Cube{
		Metadata: config.Metadata{Name: "test"},
		Spec: config.Spec{
			Engine:  config.EngineSpec{Type: "flux", Ref: writeEngineFixture(t)},
			Gateway: config.GatewaySpec{Pack: "demo", Host: "cube-idp.localtest.me", Port: 8443, Ref: "../pack/testdata/demo"},
			Packs: []config.PackRef{
				{Ref: "../pack/testdata/demo-kustomize", DependsOn: []string{"demo-helm"}},
				{Ref: "../pack/testdata/demo-helm", DependsOn: []string{"demo-kustomize"}},
			},
		},
	}

	_, _, _, err := desiredState(context.Background(), cube, fakeEngine{})
	if err == nil {
		t.Fatal("want an error for a cube whose packs form a dependency cycle")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("want a *diag.Error, got %v (%T)", err, err)
	}
	if de.Code != diag.CodePackDepCycle {
		t.Fatalf("want CUBE-4019, got %v", de.Code)
	}
}
