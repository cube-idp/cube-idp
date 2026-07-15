# Changelog

## v0.1.0 (unreleased — cut by Phase 4 R10)

First release. Private distribution via GitHub Releases (`gh release download`).

### Phase 1 — MVP (2026-07-13)
- `cube-idp init | up | down | status | doctor | get secrets | diff | config`:
  one static binary drives kind cluster + Flux/Argo CD + zot registry +
  traefik gateway + gitea/argocd packs from a single `cube.yaml`
  (`cube-idp.dev/v1alpha1`).
- Typed CUBE-xxxx error model with remediations; byte-stable plain output.

### Phase 2 — Trust, sources, day-2 (2026-07-14)
- Local CA + OS trust store (`cube-idp trust`), HTTPS gateway (NodePort 30443),
  CoreDNS `*.<host>` in-cluster resolution, registry certs.d wiring (D12).
- Pack sources: OCI, bare-git grammar, go-getter refs; `cube.lock` pins
  (`oci:sha256:…`, `git+<sha>`, `dir:h1:…`); `upgrade --plan`; pack
  discoverability records (`kubectl get packs`, D11); cnoe-compat import.

### Phase 3 — Providers, air-gap, delivery (2026-07-14/15)
- k3d provider (D4/D10/D12) + shared provider contract suite.
- Air-gap: `cube-idp vendor [--platform]` → `up --bundle` (per-image OCI tars).
- Exec-plugins with sha256-pinned index (`plugin list|trust|install`).
- `sync [--watch]` (D7), `repo create [--deploy]`, pack catalog
  (backstage, cert-manager, external-secrets, turnkey envoy-gateway),
  `pack push --also-tag`.
- One-console UX: typed event stream, plain/live/JSON renderers,
  `--progress`, JSON documents for status/doctor/get secrets.

### Phase 4 — First release hardening (2026-07-15)
- Release pipeline (goreleaser, 4 platforms, checksums) + version stamping.
- Bundle integrity: content-hashed manifest v2, extraction caps.
- Event stream covers vendor/sync/repo/plugin/pack push (`--progress=json`).
- Diag taxonomy sweep; plugin trust/index hardening; D15 kustomize
  substitution; gateway pack/ref coherence + envoy in-cluster CoreDNS fix.
