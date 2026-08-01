# Domain: config

Living contract of the config domain (`internal/config` +
`api/config/v1alpha1`). Cross-cutting rules: `docs/ARCHITECTURE.md`.

## Purpose

Load, default, and validate the single `Config` document
(`cube-idp.dev/v1alpha1`, kind `Config`) that describes a cube. A non-nil
`*Config` returned by this domain is always complete and valid.

## API (`api/config/v1alpha1`)

- `Config{TypeMeta, ObjectMeta, Spec ConfigSpec}` — only `metadata.name`
  (the cube identity, DNS-label-like, max 31 chars) is honored from
  ObjectMeta; server-populated fields are accepted and ignored.
- `ConfigSpec` holds one typed sub-struct per component (`Cluster`
  today). Sub-structs bring their own `Default()` additions and
  `Validate()` rules; optional sub-structs are pointers ("unset vs zero
  must differ").
- deepcopy is generated (`make generate`) and committed.

## Loading pipeline (`internal/config`)

`Load(fs.FS, path)` / `LoadFile(path)`: GVK pre-parse → strict decode
(unknown fields rejected) → `Default()` → `Validate()` (aggregated
`field.ErrorList` — user sees every problem in one run). No
`runtime.Scheme` until a second served version needs it; `v1alpha1` is the
hub.

## Error codes (`CUBE-CFG-*`)

| Code | Meaning |
|---|---|
| `CUBE-CFG-001` | unsupported apiVersion/kind |
| `CUBE-CFG-002` | unknown fields in config |
| `CUBE-CFG-003` | invalid config (validation errors) |
| `CUBE-CFG-004` | config file missing/unreadable |

`CFG` codes map to exit code 2.

## CLI surface

`config validate` (exit 0/2) and `config show` (round-trips the loaded,
defaulted config as YAML; byte-exact golden test).

## Pending

- `config validate` covering provider-owned payloads
  (`spec.cluster.forProvider`) via edge-surfaced provider validation —
  without `internal/config` ever importing `internal/cluster`.
