# Changelog

## Unreleased

v0 greenfield baseline (2026-07-27 reset; design:
`docs/design/2026-07-27-back-to-basics-structure.md`):

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
