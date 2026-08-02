# Domain: config

Living contract of the config domain (`internal/config` +
`api/config/v1alpha1`). Cross-cutting rules: `docs/ARCHITECTURE.md`.

## Purpose

Load, default, and validate the single `Config` document
(`cube-idp.dev/v1alpha1`, kind `Config`) that describes a cube. A non-nil
`*Config` returned by this domain is always complete and valid. The domain
also owns scaffolding: it is the only writer of the config document —
other domains never write config.

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

## Scaffolding (M4)

`ScaffoldFile(path, name)` creates a new config document from a fixed
hand-written template (not `yaml.Marshal` of a `Config`, which emits
`creationTimestamp: null` noise):

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: <name>
spec:
  cluster: {}
```

`spec.cluster: {}` (present-and-empty) already means "default kind
cluster" per the cluster contract — the scaffold provisions out of the box.

- Before writing, the rendered document runs through the standard load
  pipeline in memory (strict decode → `Default()` → `Validate()`); an
  invalid name surfaces as `CUBE-CFG-003` and nothing is written. No name
  validation is duplicated outside the pipeline.
- The file is created with `O_CREATE|O_EXCL`: scaffolding can never
  clobber an existing document, even in a race (`CUBE-CFG-006`); other
  scaffold I/O failures are `CUBE-CFG-007`, and a partial file is
  removed on a failed write.
- Name generation: `GenerateName()` returns a docker-style
  `<adjective>-<noun>` name. It lives in `internal/config` beside the
  loader — NOT in `api/` (which stays logic-free). A unit test asserts
  every adjective×noun combination matches the api name regex (max 31
  chars).
- Deciding *when* to scaffold is the CLI edge's job (`init` composes
  scaffold-if-absent → load → provision); this domain only provides the
  pieces.

## Error codes (`CUBE-CFG-*`)

| Code | Meaning |
|---|---|
| `CUBE-CFG-001` | unsupported apiVersion/kind |
| `CUBE-CFG-002` | unknown fields in config |
| `CUBE-CFG-003` | invalid config (validation errors) |
| `CUBE-CFG-004` | config file missing/unreadable |
| `CUBE-CFG-005` | `--name` conflicts with existing document's `metadata.name` (M4) |
| `CUBE-CFG-006` | config already exists — scaffold never overwrites (M4) |
| `CUBE-CFG-007` | scaffold I/O failure (M4) |

`CUBE-CFG-005` fires only on *mismatch*: a `--name` equal to the
document's `metadata.name` is a no-op and proceeds (keeps
`init --name <x>` idempotent). Its remediation: edit `metadata.name` in
the file instead — flags never mutate an existing document. The
constructor is exported (like cluster's) because the CLI edge raises it.

`CFG` codes map to exit code 2.

## CLI surface

`config validate` — exit 0 when valid, 2 for document errors, and (from
M4) 1 for provider-payload errors — and `config show` (round-trips the
loaded, defaulted config as YAML; byte-exact golden test).

From M4 `config validate` also surfaces provider-side `forProvider`
validation, composed at the CLI edge via the cluster seam's optional
validation capability (see `docs/domains/cluster.md`) — `internal/config`
still never imports `internal/cluster`. Provider-payload failures carry
the provider domain's code (`CUBE-CLU-003`, exit 1), not a `CFG` code.
