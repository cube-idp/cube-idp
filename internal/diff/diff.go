// Package diff computes what a re-run of `up` would change, without mutating
// anything: kernel objects via SSA dry-run, pack content via cube.lock
// rendered hashes, orphans via the inventory. Not modeled: the CoreDNS
// Corefile rewrite (internal/trust.EnsureCoreDNSRewrite) that `up` applies
// for the D6 canonical hostname — it lives in kube-system's coredns
// ConfigMap/Deployment, outside every object this package's desiredState
// assembles or diffs, so drift there (e.g. a manual CoreDNS edit, or a
// host change since the last `up`) is invisible to `diff`.
package diff

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/fluxcd/cli-utils/pkg/object"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/engine"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/lock"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/registry"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// ensureTimeout bounds the connect step, mirroring status/get: no infinite
// spinner if a configured "existing" cluster is unreachable.
const ensureTimeout = 3 * time.Minute

// Run loads cfgPath, connects to the cube's cluster (read-only — it never
// creates a cluster), and reports what a real `cube-idp up` would change:
// kernel objects (registry + engine install + pack delivery objects) via SSA
// server-side dry-run, pack content drift via cube.lock rendered hashes, and
// orphaned inventory entries. changed is true iff any of THOSE would do
// anything at all — it does not cover the CoreDNS rewrite (see the package
// doc), so a re-run of `up` could still have DNS work to do even when Run
// reports changed=false.
func Run(ctx context.Context, cfgPath string, out io.Writer) (bool, error) {
	cube, err := config.Load(cfgPath)
	if err != nil {
		return false, err
	}
	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return false, err
	}
	// diff is read-only: Ensure would CREATE a missing kind cluster, so check
	// existence first rather than calling Ensure unconditionally (the
	// requireClusterExists pattern used by status/get, CUBE-1004) — but
	// unlike those commands, a missing cluster is not an error here: it just
	// means `up` would create everything.
	exists, err := prov.Exists(ctx, cube.Metadata.Name)
	if err != nil {
		return false, err
	}
	if !exists {
		fmt.Fprintf(out, "cluster %q does not exist — `cube-idp up` would create everything\n", cube.Metadata.Name)
		return true, nil
	}

	ensureCtx, cancel := context.WithTimeout(ctx, ensureTimeout)
	conn, err := prov.Ensure(ensureCtx, cube.Metadata.Name, cube.Spec.Cluster)
	cancel()
	if err != nil {
		return false, err
	}
	a, err := apply.New(conn.REST, cube.Metadata.Name)
	if err != nil {
		return false, err
	}
	eng, err := enginefactory.New(cube.Spec.Engine)
	if err != nil {
		return false, err
	}

	// Desired kernel set: registry + Pack CRD + engine install + per-pack
	// delivery objects + the registry gateway route, assembled the same way
	// up.Run does (gateway pack prepended, Fetch -> Render -> Deliver —
	// Deliver is pure, so no push happens here). orphanOnly carries a few
	// more objects up.Run also applies/inventories but that desired cannot
	// safely reproduce byte-for-byte for a dry-run diff (see desiredState);
	// it widens only the orphan check, never the printed kernel diff.
	desired, orphanOnly, packEntries, err := desiredState(ctx, cube, eng)
	if err != nil {
		return false, err
	}

	p := ui.NewFor(out)
	changed := false
	changes, err := a.Diff(ctx, desired)
	if err != nil {
		return false, err
	}
	p.Section("KERNEL OBJECTS")
	for _, c := range changes {
		if c.Action != "unchanged" {
			changed = true
		}
		fmt.Fprintf(out, "  %-11s %s\n", c.Action, c.Ref)
	}

	// Pack content drift: compare fresh rendered hashes against cube.lock.
	prev, err := lock.Read(lock.PathFor(cfgPath))
	if err != nil {
		return false, err
	}
	p.Section("PACK CONTENT")
	for _, e := range packEntries {
		old := lockEntryFor(prev, e.Name)
		switch {
		case old == nil:
			changed = true
			fmt.Fprintf(out, "  new         %s (no cube.lock entry — first delivery)\n", e.Name)
		case old.RenderedHash != e.RenderedHash:
			changed = true
			fmt.Fprintf(out, "  changed     %s (%s -> %s)\n", e.Name, short(old.RenderedHash), short(e.RenderedHash))
		default:
			fmt.Fprintf(out, "  unchanged   %s\n", e.Name)
		}
	}

	// Orphans: inventory entries no longer in the desired set. orphanOnly
	// widens the set beyond what a.Diff saw above (see desiredState).
	inv, err := a.LoadInventory(ctx)
	if err != nil {
		return false, err
	}
	orphans := orphanRefs(inv, append(desired, orphanOnly...))
	if len(orphans) > 0 {
		changed = true
		p.Section("ORPHANS (in inventory, no longer desired)")
		for _, ref := range orphans {
			fmt.Fprintf(out, "  orphaned    %s\n", ref)
		}
	}
	return changed, nil
}

// desiredState re-fetches and re-renders every pack (gateway pack first,
// exactly as up.Run orders it) and returns:
//
//   - desired: the kernel object set safe to SSA dry-run diff — registry,
//     the D11 Pack CRD, engine install, per-pack delivery objects, and the
//     D6 registry gateway route. Every one of these is pure/deterministic
//     given cube.yaml alone, so re-rendering them here and diffing against
//     live state is accurate.
//   - orphanOnly: identity-only stubs (kind/namespace/name, no spec) for a
//     few more objects up.Run also applies and inventories, but that this
//     function must NOT feed through a.Diff: the gateway TLS Namespace and
//     Secret (ensureGatewayTLS deliberately reuses the live secret's cert
//     rather than reissuing one on every `up`/`diff` — reissuing here would
//     fabricate fresh random cert bytes and misreport a stable secret as
//     "changed" on every single diff) and each pack's D11 Pack
//     discoverability record (whose `ready` field tracks live engine health
//     at write time, not something a re-render should perturb). Sending a
//     partial stub through a.Diff would apply-patch it under the SAME field
//     manager as `up`'s full object, which SSA reads as "the fields I no
//     longer mention are no longer wanted" — the same footgun documented on
//     the argocd-cmd-params-cm ConfigMap in internal/engine/argocd. Identity
//     alone is enough for orphanRefs, which only compares
//     group/kind/namespace/name.
//   - one lock.Entry{Name, RenderedHash} per pack, for content-drift
//     comparison.
//
// Without orphanOnly, every one of these objects would show up as a false
// "orphaned" entry on every converged cube, because they're written by
// up.Run outside the per-pack Deliver loop this function otherwise mirrors.
func desiredState(ctx context.Context, cube *config.Cube, eng engine.Engine) (desired, orphanOnly []*unstructured.Unstructured, entries []lock.Entry, err error) {
	regObjs, err := registry.Manifests()
	if err != nil {
		return nil, nil, nil, err
	}
	desired = append(desired, regObjs...)

	// D11: applied by up.Run's "packs-crd" step, before the engine and the
	// pack loop below; pure (embedded YAML, no live-state dependency).
	crd, err := pack.CRD()
	if err != nil {
		return nil, nil, nil, err
	}
	desired = append(desired, crd)

	dir, err := pack.DefaultCacheDir()
	if err != nil {
		return nil, nil, nil, err
	}

	// Engine-as-pack: mirror up.Run — the engine install is the rendered
	// engine pack (warm cache in the common case).
	enginePk, engineRendered, err := pack.FetchRenderEngine(ctx, cube.Spec.Engine, cube.Spec.Gateway, cube.Spec.Engine.PackRef(), dir)
	if err != nil {
		return nil, nil, nil, err
	}
	desired = append(desired, engineRendered.Objects...)

	// A self-managed engine (spec.engine.selfManage) additionally carries the cube-engine
	// self-source objects (flux OCIRepository + Kustomization / argocd
	// Application), applied and inventoried by up.Run's selfManage block.
	// Identity-only, like the repo-delivery sources below: the source object
	// carries a fresh reconcile-now annotation per render (the self-source poke),
	// so re-rendering it here would fabricate a perpetual "changed" — and
	// identity is all orphanRefs needs. A placeholder ArtifactRef suffices:
	// the names are deterministic (cube-engine).
	if cube.Spec.Engine.SelfManage {
		selfObjs, err := eng.DeliverSelf(ctx, engine.ArtifactRef{})
		if err != nil {
			return nil, nil, nil, err
		}
		for _, o := range selfObjs {
			orphanOnly = append(orphanOnly, identityStub(o.GroupVersionKind(), o.GetNamespace(), o.GetName()))
		}
	}

	// Gateway pack goes first, mirroring up.Run — everything else depends on
	// ingress existing. Fetch+render every pack first (declared order), same
	// as up.Run's pass 1, accumulating dPacks/dRenders alongside refs so
	// ResolveOrder can validate the graph after the loop.
	refs := append([]config.PackRef{{Ref: cube.Spec.Gateway.PackRef()}}, cube.Spec.Packs...)
	var dPacks []*pack.Pack
	var dRenders []*pack.Rendered
	for _, pr := range refs {
		p, err := pack.Fetch(ctx, pr.Ref, dir)
		if err != nil {
			return nil, nil, nil, err
		}
		// Mirror up.Run — RenderWith enforces the values stone (values: are helm
		// values only, never engine tuning; see docs/adr/0004-pack-values-and-extra-manifests.md)
		// and appends extraManifests, so diff previews exactly what up
		// would deliver (including CUBE-4016/4017 failures).
		rendered, err := p.RenderWith(pr.Values, pr.ExtraManifests, cube.Spec.Gateway)
		if err != nil {
			return nil, nil, nil, err
		}
		rh, err := lock.RenderedHash(rendered.Objects)
		if err != nil {
			return nil, nil, nil, err
		}
		entries = append(entries, lock.Entry{Name: rendered.Name, RenderedHash: rh})
		dPacks = append(dPacks, p)
		dRenders = append(dRenders, rendered)
	}

	// Mirror up.Run's pass 2: validate the dependency graph over the
	// fetched+rendered set. diff does not deliver, so `order` is discarded —
	// but packDeps is threaded into the Deliver calls below, exactly as
	// up.Run's deliver pass does, so diff previews byte-identical flux
	// Kustomization dependsOn / argocd annotation objects. A graph error
	// surfaces from diff exactly as CUBE-4016/4017 already do above, before
	// any object is added to desired/orphanOnly.
	_, packDeps, err := pack.ResolveOrder(dPacks, refs, dRenders)
	if err != nil {
		return nil, nil, nil, err
	}

	for i, pr := range refs {
		rendered := dRenders[i]
		// p6 DEP3: stamp this pack's resolved deps before Deliver/DeliverGit
		// so the preview matches what up.Run's deliver pass would produce.
		rendered.DependsOn = packDeps[rendered.Name]

		if pr.Delivery == "repo" {
			// With delivery: repo, up delivers this pack as an engine git source over the
			// in-cluster Gitea repo (deliverPackRepo), whose spec embeds
			// live-derived state — the gitea admin owner in the clone URL —
			// so re-rendering it here for a dry-run diff would fabricate
			// fields (the Pack-record reasoning above). Identity is enough
			// for orphan accounting; a placeholder GitSource yields the
			// engine-native identities (names are deterministic:
			// cube-idp-<pack>).
			gitObjs, err := eng.DeliverGit(ctx, rendered.Name, engine.GitSource{}, rendered.DependsOn)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, o := range gitObjs {
				orphanOnly = append(orphanOnly, identityStub(o.GroupVersionKind(), o.GetNamespace(), o.GetName()))
			}
		} else {
			// Deliver is pure (no push): the ArtifactRef mirrors the
			// repo/tag up.Run pushes to, but nothing is pushed here.
			artifact := engine.ArtifactRef{Repo: "packs/" + rendered.Name, Tag: rendered.Version}
			deliverObjs, err := eng.Deliver(ctx, rendered, artifact)
			if err != nil {
				return nil, nil, nil, err
			}
			desired = append(desired, deliverObjs...)
		}

		// D11 Pack record identity (see the orphanOnly doc above for why
		// only identity, not the full spec, belongs here).
		orphanOnly = append(orphanOnly, identityStub(packGVK, "", rendered.Name))
	}

	// Engine-as-pack §3.3.7: up.Run also appends the engine's own D11 Pack
	// record row (delivery "engine"); its identity belongs here for the same
	// reason as the per-pack records above.
	orphanOnly = append(orphanOnly, identityStub(packGVK, "", enginePk.Name))

	// D6: applied by up.Run right after the pack loop; pure given the
	// gateway host alone.
	desired = append(desired, registry.GatewayRoute(cube.Spec.Gateway.Host, cube.Spec.Gateway.Pack))

	// Gateway TLS Namespace + Secret identities (see the orphanOnly doc
	// above). Namespace equals the gateway pack name by convention
	// (internal/up/tls.go's gatewayTLSObjects).
	orphanOnly = append(orphanOnly,
		identityStub(namespaceGVK, "", cube.Spec.Gateway.Pack),
		identityStub(secretGVK, cube.Spec.Gateway.Pack, "cube-idp-gateway-tls"))

	return desired, orphanOnly, entries, nil
}

var (
	packGVK      = schema.GroupVersionKind{Group: "cube-idp.dev", Version: "v1alpha1", Kind: "Pack"}
	namespaceGVK = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	secretGVK    = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
)

// identityStub builds a minimal unstructured object carrying only
// GVK/namespace/name — never fed through a.Diff (see desiredState), only
// used so orphanRefs (which reads exactly these fields) recognizes the
// object as desired.
func identityStub(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name},
	}}
	if namespace != "" {
		o.SetNamespace(namespace)
	}
	o.SetGroupVersionKind(gvk)
	return o
}

// lockEntryFor returns the cube.lock entry named name, or nil if f is nil
// (no lock yet) or name has no entry (first delivery).
func lockEntryFor(f *lock.File, name string) *lock.Entry {
	if f == nil {
		return nil
	}
	for i := range f.Packs {
		if f.Packs[i].Name == name {
			return &f.Packs[i]
		}
	}
	return nil
}

// short returns the first 12 hex characters of h after its "sha256:" prefix,
// for compact before/after display.
func short(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// refKey formats a group/kind/namespace/name identity the same way
// apply.Change.Ref does, so orphan output lines up with the KERNEL OBJECTS
// section.
func refKey(group, kind, ns, name string) string {
	return group + "/" + kind + "/" + ns + "/" + name
}

// orphanRefs set-subtracts desired from inv (by group/kind/namespace/name)
// and returns the leftover refs, sorted for stable output.
func orphanRefs(inv []object.ObjMetadata, desired []*unstructured.Unstructured) []string {
	want := make(map[string]bool, len(desired))
	for _, o := range desired {
		gvk := o.GroupVersionKind()
		want[refKey(gvk.Group, gvk.Kind, o.GetNamespace(), o.GetName())] = true
	}
	var orphans []string
	for _, ref := range inv {
		key := refKey(ref.GroupKind.Group, ref.GroupKind.Kind, ref.Namespace, ref.Name)
		if !want[key] {
			orphans = append(orphans, key)
		}
	}
	sort.Strings(orphans)
	return orphans
}
