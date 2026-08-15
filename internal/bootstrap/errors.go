package bootstrap

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The bootstrap domain owns the CUBE-BST-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors stay unexported — no other package
// raises BST errors. The catalog grows with the domain: later M7 tasks
// (SSA apply/wait, source/sync, inventory) add their codes beside the
// constructors that raise them.
const (
	// CodeAssetIntegrity reports embedded Flux manifests whose content no
	// longer matches the recorded sha256 provenance (a build-time drift).
	CodeAssetIntegrity cubeerr.Code = "CUBE-BST-001"
)

func newAssetIntegrityError(got string) error {
	return cubeerr.Wrap(CodeAssetIntegrity,
		fmt.Sprintf("embedded Flux manifests failed provenance (sha256 %s does not match the pin)", got),
		"rebuild from a clean checkout; if you regenerated the asset, run `make flux-manifests` and update fluxManifestsSHA256",
		nil)
}
