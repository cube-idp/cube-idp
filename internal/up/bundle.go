package up

import (
	"fmt"
	"path"
	"strings"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/lock"
)

// resolveBundleRefs rewrites every cube pack ref to point at that pack's
// source directory inside an opened vendor bundle — the pure heart of offline
// mode. It never reaches the network: a ref that maps to no bundled pack is a
// hard CUBE-7004, not a fallthrough to `pack.Fetch`'s online path.
//
// Name resolution follows the bundle's own keying, as `cube-idp vendor` wrote
// it: the embedded
// cube.lock records each pack's Name alongside the Ref it was locked from, so
// the preferred lookup matches a cube ref against lk.Packs by Ref equality
// and resolves through packDir(entry.Name). Local-dir refs the lock records
// verbatim (no oci:// tag) fall back to their last path segment — the name
// the bundle keyed the pack under. lookup is bundle.Opened.PackDirLookup:
// name -> (dir, present).
func resolveBundleRefs(refs []config.PackRef, lk *lock.File, lookup func(name string) (string, bool)) ([]config.PackRef, error) {
	out := make([]config.PackRef, len(refs))
	for i, ref := range refs {
		name := bundlePackName(ref.Ref, lk)
		dir, ok := lookup(name)
		if !ok {
			return nil, diag.New(diag.CodeVendorIncomplete,
				fmt.Sprintf("bundle has no pack for ref %q (resolved name %q)", ref.Ref, name),
				"re-run `cube-idp vendor` on a connected machine so the bundle carries every pack this cube references, then retry")
		}
		// Only the SOURCE is rewritten — the ref's install-shaping fields
		// (values, extraManifests, delivery) carry over so a bundle
		// install renders and delivers exactly like the online one (repo
		// delivery is in-cluster and works air-gapped).
		out[i] = config.PackRef{Ref: dir, Values: ref.Values, ExtraManifests: ref.ExtraManifests, Delivery: ref.Delivery}
	}
	return out, nil
}

// bundlePackName maps a cube pack ref to the name the bundle keyed it under.
// Preferred: an exact Ref match in the lock yields that entry's Name. Fallback
// (local-dir refs the lock stores verbatim, and any ref absent from the lock):
// the last path segment with any :tag stripped.
func bundlePackName(ref string, lk *lock.File) string {
	if lk != nil {
		for _, e := range lk.Packs {
			if e.Ref == ref {
				return e.Name
			}
		}
	}
	return refBaseName(ref)
}

// refBaseName extracts a pack name from a raw ref by taking its last
// slash-separated segment and dropping a trailing :tag (an oci:// scheme, if
// present, leaves no bare colon in the final segment once the tag is removed).
func refBaseName(ref string) string {
	base := path.Base(strings.TrimSuffix(ref, "/"))
	if i := strings.LastIndex(base, ":"); i >= 0 {
		base = base[:i]
	}
	return base
}
