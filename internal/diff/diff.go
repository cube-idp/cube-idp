// Package diff computes what a re-run of `up` would change, without mutating
// anything: kernel objects via SSA dry-run, pack content via cube.lock
// rendered hashes, orphans via the inventory.
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

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/engine"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

// ensureTimeout bounds the connect step, mirroring status/get: no infinite
// spinner if a configured "existing" cluster is unreachable.
const ensureTimeout = 3 * time.Minute

// Run loads cfgPath, connects to the cube's cluster (read-only — it never
// creates a cluster), and reports what a real `cube-idp up` would change:
// kernel objects (registry + engine install + pack delivery objects) via SSA
// server-side dry-run, pack content drift via cube.lock rendered hashes, and
// orphaned inventory entries. changed is true iff a re-run of `up` would do
// anything at all.
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
	eng, err := enginefactory.New(cube.Spec.Engine.Type)
	if err != nil {
		return false, err
	}

	// Desired kernel set: registry + engine install + per-pack delivery
	// objects, assembled the same way up.Run does (gateway pack prepended,
	// Fetch -> Render -> Deliver — Deliver is pure, so no push happens here).
	desired, packEntries, err := desiredState(ctx, cube, eng)
	if err != nil {
		return false, err
	}

	changed := false
	changes, err := a.Diff(ctx, desired)
	if err != nil {
		return false, err
	}
	fmt.Fprintln(out, "KERNEL OBJECTS")
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
	fmt.Fprintln(out, "PACK CONTENT")
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

	// Orphans: inventory entries no longer in the desired set.
	inv, err := a.LoadInventory(ctx)
	if err != nil {
		return false, err
	}
	orphans := orphanRefs(inv, desired)
	if len(orphans) > 0 {
		changed = true
		fmt.Fprintln(out, "ORPHANS (in inventory, no longer desired)")
		for _, ref := range orphans {
			fmt.Fprintf(out, "  orphaned    %s\n", ref)
		}
	}
	return changed, nil
}

// desiredState re-fetches and re-renders every pack (gateway pack first,
// exactly as up.Run orders it) and returns the full kernel object set plus
// one lock.Entry{Name, RenderedHash} per pack for content-drift comparison.
func desiredState(ctx context.Context, cube *config.Cube, eng engine.Engine) ([]*unstructured.Unstructured, []lock.Entry, error) {
	var desired []*unstructured.Unstructured

	regObjs, err := registry.Manifests()
	if err != nil {
		return nil, nil, err
	}
	desired = append(desired, regObjs...)

	installObjs, err := eng.InstallManifests()
	if err != nil {
		return nil, nil, err
	}
	desired = append(desired, installObjs...)

	dir, err := pack.DefaultCacheDir()
	if err != nil {
		return nil, nil, err
	}

	// Gateway pack goes first, mirroring up.Run — everything else depends on
	// ingress existing.
	refs := append([]config.PackRef{{Ref: cube.Spec.Gateway.PackRef()}}, cube.Spec.Packs...)
	var entries []lock.Entry
	for _, pr := range refs {
		p, err := pack.Fetch(ctx, pr.Ref, dir)
		if err != nil {
			return nil, nil, err
		}
		rendered, err := p.Render(pr.Values)
		if err != nil {
			return nil, nil, err
		}
		rh, err := lock.RenderedHash(rendered.Objects)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, lock.Entry{Name: rendered.Name, RenderedHash: rh})

		// Deliver is pure (no push): the ArtifactRef mirrors the repo/tag
		// up.Run pushes to, but nothing is pushed here.
		artifact := engine.ArtifactRef{Repo: "packs/" + rendered.Name, Tag: rendered.Version}
		deliverObjs, err := eng.Deliver(ctx, rendered, artifact)
		if err != nil {
			return nil, nil, err
		}
		desired = append(desired, deliverObjs...)
	}

	return desired, entries, nil
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
