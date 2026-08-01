# Changelog

## Unreleased

M3 cluster domain (design: `docs/archived/design/2026-07-29-cluster-domain.md`):

- `spec.cluster` API sub-struct: typed `provider` (defaults to `kind`) plus
  opaque `forProvider` passthrough, validated at load time.
- `internal/cluster` domain: `Provisioner` driver seam with an exported
  conformance suite, kubeconfig machinery (`ContextName`/`Rebrand`/`Merge` —
  no client-go), the `Init` operation, and the `CUBE-CLU-*` code catalog.
- kind provider (`sigs.k8s.io/kind v0.32.0`, confined to
  `internal/cluster/kind`); real-cluster conformance is opt-in via
  `make test-e2e`, never part of the green gate.
- CLI: `cube-idp init` creates the cluster and installs a cube-owned
  kubeconfig context (`cube-idp.dev/<name>`), with `--kubeconfig` and
  `--kubeconfig-context-name` overrides.

v0 greenfield baseline (2026-07-27 reset; design:
`docs/archived/design/2026-07-27-back-to-basics-structure.md`):

- `Config` API `cube-idp.dev/v1alpha1`: CRD-ready types (real apimachinery
  `TypeMeta`/`ObjectMeta`), controller-gen deepcopy, name validation.
- Strict loading pipeline: decode (unknown fields rejected) → default →
  validate, with aggregated path-qualified errors.
- Coded error model `CUBE-<TAG>-NNN`: machinery in `internal/cubeerr`,
  code catalogs owned per domain.
- CLI: `cube-idp config validate` and `cube-idp config show`
  (exit 0 valid, 2 config error).
- Gates: `make build/test/lint/generate`; funlen 50 and 300-line file
  limit enforced in CI.

History from before the reset (the previous full implementation) is
preserved in git history on `main`.
