// gateway-api-crds — the embedded Gateway API standard-channel CRDs (M11).
// The payload is the upstream v1.6.1 release asset standard-install.yaml,
// vendored byte-identical and pinned by version constant and sha256 in
// packs.go; regenerate only via `make gateway-api-manifests` — data, not a
// dependency. Version is the repo's clean SemVer spelling; upstream tags it
// v1.6.1. Its own prerequisite pack, never folded into the gateway pack: a
// future engine may ship its own Gateway API CRDs (docs/domains/gateway.md,
// D1). No #Values and no namespace: CRDs are cluster-scoped and the payload
// renders exactly as written.
name:     "gateway-api-crds"
version:  "1.6.1"
type:     "raw"
category: "gateway"
