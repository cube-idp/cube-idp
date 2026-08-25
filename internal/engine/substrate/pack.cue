// flux — the embedded tier-1 substrate pack (M10). The payload is the
// vendored `flux install --export` manifests, pinned by version constant
// and sha256 in substrate.go and regenerated only via `make
// flux-manifests` — data, not a dependency. Version is the repo's clean
// SemVer spelling; upstream tags it v2.9.2, and the substrate alone maps
// between the spellings. No #Values and no namespace: the payload
// renders as written, carrying its own Namespace object.
name:     "flux"
version:  "2.9.2"
type:     "raw"
category: "engine"
