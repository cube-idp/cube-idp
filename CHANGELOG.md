# Changelog

## v0.1.0 (unreleased)

First release. Private distribution via GitHub Releases (`gh release download`).

### Phase 1 — MVP (2026-07-13)
- `cube-idp init | up | down | status | doctor | get secrets | diff | config`:
  one static binary drives kind cluster + Flux/Argo CD + zot registry +
  traefik gateway + gitea/argocd packs from a single `cube.yaml`
  (`cube-idp.dev/v1alpha1`).
- Typed CUBE-xxxx error model with remediations; byte-stable plain output.

### Phase 2 — Trust, sources, day-2 (2026-07-14)
- Local CA + OS trust store (`cube-idp trust`), HTTPS gateway (NodePort 30443),
  CoreDNS `*.<host>` in-cluster resolution, registry certs.d wiring.
- Pack sources: OCI, bare-git grammar, go-getter refs; `cube.lock` pins
  (`oci:sha256:…`, `git+<sha>`, `dir:h1:…`); `upgrade --plan`; pack
  discoverability records (`kubectl get packs`); cnoe-compat import.

### Phase 3 — Providers, air-gap, delivery (2026-07-14/15)
- k3d provider + shared provider contract suite.
- Air-gap: `cube-idp vendor [--platform]` → `up --bundle` (per-image OCI tars).
- Exec-plugins with sha256-pinned index (`plugin list|trust|install`).
- `sync [--watch]`, `repo create [--deploy]`, pack catalog
  (backstage, cert-manager, external-secrets, turnkey envoy-gateway),
  `pack push --also-tag`.
- One-console UX: typed event stream, plain/live/JSON renderers,
  `--progress`, JSON documents for status/doctor/get secrets.

### Phase 4 — First release hardening (2026-07-15)
- Release pipeline (goreleaser, 4 platforms, checksums) + version stamping.
- Bundle integrity: content-hashed manifest v2, extraction caps.
- Event stream covers vendor/sync/repo/plugin/pack push (`--progress=json`).
- Diag taxonomy sweep; plugin trust/index hardening; kustomize
  substitution; gateway pack/ref coherence + envoy in-cluster CoreDNS fix.

### Phase 5 — In progress (2026-07-18)
- BREAKING: `spec.cluster.providerConfig` (string) is replaced by
  `providerConfigRef` (fetchable ref: local path, oci://, git, s3, http) and
  `forProvider` (inline provider-native fields, RFC 7386-merged over the ref).
  A file path migrates to `providerConfigRef: <path>`; an inline blob becomes
  structured `forProvider:` fields. Load fails with CUBE-0011 and this recipe.
  Core injections (gateway ports, node image, certs.d/zot) now override
  conflicting user fields with a warning (CUBE-1206/CUBE-1306) instead of
  erroring; user-vs-user conflicts still error (CUBE-1201/CUBE-1301).
