// Package bootstrap is the micro-bootstrap applier (M7): it SSA-applies the
// embedded, pinned Flux install manifests plus the source/sync CRs derived
// from spec.engine, waits on the bootstrap kind-set, records an inventory,
// then hands steady-state ownership to the engine. It runs against injected
// client-go interfaces and never imports internal/kube (domains never import
// each other). Contract: docs/domains/bootstrap.md.
package bootstrap

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// FluxVersion is the pinned Flux release whose install manifests are
// embedded in this binary. Regenerate the vendored asset with
// `make flux-manifests` (which needs the flux CLI at this version) and
// update fluxManifestsSHA256 to match; the provenance test enforces both.
const FluxVersion = "v2.9.2"

//go:embed assets/flux.yaml
var fluxManifests []byte

// fluxManifestsSHA256 pins the vendored asset's content. A mismatch means
// the embedded manifests drifted from their recorded provenance.
const fluxManifestsSHA256 = "f229fa8ace1655b04ccb2566fa3169681061b48c8e1b7a4e72b7d2413915bb84"

// Manifests returns a copy of the embedded, pinned Flux install manifests
// (multi-document YAML) after verifying they match the recorded sha256
// provenance. A mismatch is an asset-integrity error (CUBE-BST-001): the
// binary carries a vendored asset that no longer matches its pin.
func Manifests() ([]byte, error) {
	sum := sha256.Sum256(fluxManifests)
	if got := hex.EncodeToString(sum[:]); got != fluxManifestsSHA256 {
		return nil, newAssetIntegrityError(got)
	}
	out := make([]byte, len(fluxManifests))
	copy(out, fluxManifests)
	return out, nil
}
