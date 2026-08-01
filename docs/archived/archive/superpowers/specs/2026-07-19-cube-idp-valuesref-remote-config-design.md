# cube-idp: `valuesRef` / `tuningRef` remote values + remote `-f` config — design

Date: 2026-07-19
Status: PROPOSED
Depends on: `2026-07-18-cluster-forprovider-design.md` (the `providerConfigRef`
precedent — implemented in `internal/cluster/compose`), phase-5 F12 (pack ref
grammar), p6 (cube.lock / dependsOn).

## 1. Problem

Pack values and engine tuning can only be declared inline in `cube.yaml`.
Platform teams that publish shared bases (org-wide Traefik values, standard
flux tuning) have no way to reference them; every cube copies the YAML.
Separately, `-f` only reads `cube.yaml` from local disk, so a cube definition
published to git/OCI/S3 must be manually downloaded before any command runs.

The cluster spec already solved the first problem for provider configs:
`providerConfigRef` fetches a single YAML via the pack ref grammar
(`pack.FetchFile`) and merge-patches inline `forProvider` on top. This design
generalizes that pattern to pack values, engine tuning, and the `-f` flag.

## 2. Goals / non-goals

Goals:

- G1 — `packs[].valuesRef`: a single ref fetching a base values YAML, with
  inline `values:` merged on top.
- G2 — `engine.tuningRef`: same shape for the closed engine tuning knob set.
- G3 — Reproducibility parity with pack refs: git refs must be pinned,
  everything resolves to a pin recorded in `cube.lock`; `upgrade --plan`
  attributes "values source changed" separately from "chart changed".
- G4 — Remote `-f`: any command's `-f` accepts the same ref grammar,
  read-only.
- G5 — One shared resolver so `providerConfigRef`, `valuesRef`, `tuningRef`,
  and remote `-f` ride the same grammar, cache, auth, and guard machinery.

Non-goals:

- Multiple layered valuesRefs (`[refA, refB]`). The field is a scalar string;
  a future widening to `string | [...string]` is backward-compatible.
- Write-back to remote config sources (git commit / s3 put / oci push).
- Engine Helm values. GT15 stands: `values:` are Helm values only; the engine
  keeps its closed `tuning:` knob set. `tuningRef` fetches *tuning*, not
  values.
- A cube-level global valuesRef shared by all packs.
- Recording remote `-f` origin in `cube.lock` (an info line is printed
  instead; see §7.3).

## 3. cube.yaml surface

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
spec:
  cluster:
    provider: kind
    providerConfigRef: oci://ghcr.io/acme/cluster-bases/kind:1.0.0  # existing
  engine:
    type: flux
    tuningRef: git.company.com/platform/cube-tuning//flux@v1.2.0    # NEW
    tuning:                       # inline — merged ON TOP of fetched
      components:
        source-controller: { replicas: 2 }
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/traefik:0.2.0
      valuesRef: s3::https://s3.eu-west-1.amazonaws.com/acme-platform/traefik-values.yaml  # NEW
      values:                     # inline — merged ON TOP of fetched
        deployment: { replicas: 3 }
```

Schema (`internal/config/schema.cue`):

- pack entry gains `valuesRef?: string & !=""`
- engine gains `tuningRef?: string & !=""`

Go types (`internal/config/types.go`):

- `PackRef` gains `ValuesRef string \`yaml:"valuesRef,omitempty" json:"valuesRef,omitempty"\``
- `EngineSpec` gains `TuningRef string \`yaml:"tuningRef,omitempty" json:"tuningRef,omitempty"\``

Both fields are optional and independent of their inline counterpart: ref
only, inline only, both, or neither are all valid shapes at config level.

Cross-validation: `valuesRef` obeys the same GT15 rule as `values` — set on a
chartless pack it fails at render with `CUBE-4016` (the `RenderWith` guard in
`internal/pack/render.go` extends its condition to `len(values) > 0 ||
valuesRef != ""`). This stays a render-time check (like today's `values`
guard) because chartlessness is only known after the pack is fetched.

### 3.1 Accepted ref grammar (identical to pack refs, spec §4.4)

| form | example | pin recorded |
| --- | --- | --- |
| local path | `./values/traefik.yaml`, `values/base` | `dir:<dirhash>` |
| OCI | `oci://ghcr.io/acme/values/traefik:1.2.0` (or `@sha256:…`) | `oci:<manifest-digest>` |
| bare git | `github.com/acme/values//traefik@v1.2.0` | `git+<full-sha>` — pin **required**, else `CUBE-4007` |
| explicit go-getter | `git::…`, `s3::…`, `http://`, `https://` | `dir:<dirhash>` |
| other `://` scheme | — | rejected, `CUBE-4001` |

File-targeting semantics come from `pack.FetchFile`: a direct-file URL
(http/s3 object) fetches that file; a directory-shaped ref (local dir, git
`//subdir`, OCI artifact) must contain **exactly one** top-level
`*.yaml`/`*.yml`. This is `FetchFile`'s existing `singleYAML` contract,
unchanged.

## 4. Shared resolver — `internal/refval`

New package with a single entry point:

```go
// Resolve fetches one YAML document via the pack ref grammar and returns
// its decoded map plus the reproducibility pin (oci:<digest> / git+<sha> /
// dir:<dirhash>).
func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error)
```

Implementation: `pack.FetchFile` + `sigs.k8s.io/yaml` unmarshal into
`map[string]any`. An empty fetched document decodes to an empty (non-nil)
map. A document that is valid YAML but not a mapping (e.g. a bare list or
scalar) is a resolve error — values and tuning are both object-shaped.

### 4.1 `pack.FetchFile` gains pin output

```go
// before
func FetchFile(ctx context.Context, ref, cacheDir string) ([]byte, error)
// after
func FetchFile(ctx context.Context, ref, cacheDir string) ([]byte, string, error)
```

`FetchFile` already routes through the machinery that computes pins for
`pack.Fetch` (`oci:` via ORAS manifest digest, `git+` via resolved sha,
`dir:` via `dirhash.Hash1`); the change surfaces the pin instead of
discarding it. Exactly one production call site updates:
`internal/cluster/compose/compose.go:29`.

### 4.2 `compose` migrates onto `refval`

`compose.Resolve` becomes a thin wrapper over `refval.Resolve` (keeping its
`CUBE-1005` error wrapping and YAML→JSON map behavior). `Compose` gains the
pin as a return value so `up` can record it (§6). No behavior change for
`forProvider` merge semantics.

### 4.3 Fetch mechanics inherited unchanged

Auth: ambient docker credential chain for OCI (anonymous fallback,
plain-HTTP only for localhost/zot tunnel), git CLI + ambient credentials for
`git::`, AWS environment/profile chain for `s3::`. Safety: forked go-getter
`DisableSymlinks`, `GuardTree` path-traversal guard (`CUBE-4014`), atomic
tmp-dir + rename, digest-keyed OCI cache under
`$HOME/.cache/cube-idp/packs`.

## 5. Merge semantics and resolution site

### 5.1 Pack values ladder (bottom → top)

1. `chart.yaml` defaults (pack author's base — existing)
2. `valuesRef` fetched map (NEW)
3. inline `packs[].values` (existing)

Layers 2+3 combine first via **RFC 7386 merge-patch** (`jsonpatch.MergePatch`,
inline patched over fetched) — the identical algorithm and precedence
direction as `forProvider` over `providerConfigRef`. RFC 7386 consequences,
accepted deliberately for consistency: `null` in the inline layer **deletes**
the key from the fetched base; arrays replace wholesale (Helm behaves the
same way between values files).

The merged result (`effective`) then enters the existing pipeline exactly
where inline `values` enters today: `normalizePackValues`-style int64→int
normalization is applied to the fetched map right after decode (Helm
consumer), then `RenderWith(effective, …)` → `mergeValues(chartDefaults,
effective)` deep-merge → `${GATEWAY_*}` substitution → `#Values` validation
(`CUBE-4002`). Steps 1-and-beyond are untouched; the feature is purely a
pre-step producing `effective`.

### 5.2 Engine tuning ladder

1. `tuningRef` fetched YAML — **strict-decoded** into `config.EngineTuning`
   (unknown fields fail the decode; this is `CUBE-3012`, not silent drop)
2. inline `engine.tuning` — RFC 7386 merge-patched on top (patch computed on
   the map forms, result re-decoded strictly)

Numeric leaves keep int64 (the consumer is unstructured SSA `ApplyTuning`,
not Helm — existing convention). Unknown component names still fail with
`CUBE-3009` at apply time, exactly as inline tuning does today.

### 5.3 Where resolution happens

At **desired-state build time**, alongside the existing `pack.Fetch` calls in
the shared `up`/`diff` path (`internal/up/up.go` two-pass fetch/render and
the `diff` desired-state builder) — for the engine, at engine-install
assembly before `ApplyTuning`. Explicitly **not** in `config.Load`:
`status`, `doctor`, `get`, and every other read-only command stay
offline-capable and network-silent. Consequences:

- `diff` resolves refs, so a changed remote values file shows up as manifest
  drift (and its pin change is attributable, §6).
- `up` on an air-gapped host works iff the refs are cached or local —
  identical posture to pack refs today.

## 6. Pinning, `cube.lock`, `upgrade --plan`

`cube.lock` is the KRM-shaped `CubeLock` document (`internal/lock/lock.go`,
`apiVersion: cube-idp.dev/v1alpha1, kind: CubeLock`) — a file, not an
in-cluster CRD, so these changes are additive struct fields only:

```go
type Entry struct {            // per-pack
    // … existing fields …
    ValuesRef string `yaml:"valuesRef,omitempty" json:"valuesRef,omitempty"`
    ValuesPin string `yaml:"valuesPin,omitempty" json:"valuesPin,omitempty"`
}
type EngineLock struct {
    Type      string `yaml:"type" json:"type"`
    TuningRef string `yaml:"tuningRef,omitempty" json:"tuningRef,omitempty"`
    TuningPin string `yaml:"tuningPin,omitempty" json:"tuningPin,omitempty"`
}
type File struct {
    // … existing fields …
    Cluster *ClusterLock `yaml:"cluster,omitempty" json:"cluster,omitempty"`
}
type ClusterLock struct {      // providerConfigRef pin — falls out of §4.2
    ProviderConfigRef string `yaml:"providerConfigRef,omitempty" json:"providerConfigRef,omitempty"`
    ProviderConfigPin string `yaml:"providerConfigPin,omitempty" json:"providerConfigPin,omitempty"`
}
```

Omitted-when-empty keeps locks for ref-less cubes byte-identical to today
(the p6 "stock records unchanged" precedent).

Pin rules are pack-ref rules verbatim: bare-git refs without `@rev` fail
with `CUBE-4007` **before** any fetch; OCI tags resolve to manifest digests;
http/s3/local hash fetched content (`dir:<dirhash>`).

`upgrade --plan` extends its existing `pack.ResolveRemote` probe to each
`valuesRef`/`tuningRef`/`providerConfigRef`: compare would-be pin against the
locked pin and report a distinct line item — `values source changed
(traefik): dir:abc… → dir:def…` — never conflated with chart/pack changes.
`ResolveRemote` already computes exactly these pin forms without pulling.

## 7. Remote `-f`

### 7.1 Dispatch — one change point, zero flag changes

All `-f` handling funnels through `config.Load(path)`. `Load` gains a
dispatch prelude:

1. `os.Stat(path)` succeeds → local file, today's path, byte-for-byte
   identical behavior. Local always wins (this also disambiguates paths like
   `configs.d/cube.yaml` that would otherwise parse as bare-git refs).
2. Otherwise, if `path` matches the remote ref grammar (`oci://`, contains
   `::`, `http(s)://`, or bare-git form per `pack.isGitRef`) →
   `refval`-fetch the bytes (via `pack.FetchFile`; same single-YAML
   contract), then parse through a new `loadBytes(raw []byte)` — the
   existing CUE pipeline (`yaml.Unmarshal` → probes → Unify/Validate/Decode
   → normalize → crossValidate) refactored to operate on bytes; `Load` for
   local files becomes `os.ReadFile` + `loadBytes`.
3. Otherwise → `CUBE-0001` (unchanged: missing local file).

Fetch or single-YAML failure in step 2 → `CUBE-0013`. Pinning rules apply
(git `-f` refs need `@rev`, `CUBE-4007`).

No cobra flags change: `-f` keeps its name, shorthand, and `cube.yaml`
default on every command, so `cmd/testdata/clitree.golden` is untouched and
the F1 CLI freeze holds.

### 7.2 Origin tracking and the read-only guard

`Cube` gains non-serialized origin metadata:

```go
// set by Load, never marshaled
type Origin struct { Ref, Pin string; Remote bool }
func (c *Cube) Origin() Origin
```

`config.SaveValidated` checks it first: remote origin → `CUBE-0012`
("remote config is read-only — fetch it locally to edit, e.g. `curl`/`git
clone`, then re-run"). Current mutating call sites, all covered by this one
guard: `cmd/pack.go:368`, `cmd/spoke.go:48`, `cmd/spoke.go:154`. Read-side
commands (`up`, `diff`, `down`, `status`, `doctor`, `sync`, `get`, `trust`,
`upgrade`, …) work with remote `-f` transparently.

### 7.3 Lock path and UX

`lock.PathFor(cfgPath)` returns `dir(cfgPath)/cube.lock` — meaningless for a
ref. When origin is remote, lock reads/writes use `./cube.lock` in the
current working directory: `PathFor` keeps its signature; the `up`/`diff`
call sites branch on `cube.Origin().Remote` and pass `"."` as the base. On every remote load, one info line prints through the existing UI
writer: `using remote config <ref> (<pin>)` — making non-reproducible tag
refs at least visible in logs.

## 8. Error handling — diag codes

New codes (numbers verified free as of this design; re-verify against
`internal/diag/codes.go` at implementation — `3010/3011/4018-4020` are taken):

| code | domain | meaning | fix hint |
| --- | --- | --- | --- |
| `CUBE-0012` | config | remote config is read-only (SaveValidated on remote origin) | fetch locally to edit |
| `CUBE-0013` | config | remote `-f` fetch/single-YAML failure | check ref, network, credentials |
| `CUBE-3012` | engine | `tuningRef` fetch, non-map document, or strict-decode failure | check the ref and the tuning file's schema |
| `CUBE-4021` | pack | `valuesRef` fetch, non-map document, or merge failure | check the ref and that the file is a YAML mapping |

Reused codes: `CUBE-4001` (bad scheme), `CUBE-4006`/`CUBE-4012` (transport
failures, wrapped by the new codes' context), `CUBE-4007` (unpinned git),
`CUBE-4014` (guard trip), `CUBE-4016` (chartless pack with values/valuesRef),
`CUBE-4002` (post-merge `#Values` validation), `CUBE-3009` (unknown tuning
component), `CUBE-1005` (providerConfigRef fetch — unchanged). All new codes
get `internal/diag/registry.go` summaries.

## 9. Testing

Unit:

- `refval`: ref-form dispatch table (local file, local dir, oci, bare git
  pinned/unpinned, `s3::`, `http`, bad scheme); non-map document rejection;
  pin propagation for each form (against local fixtures/httptest, mirroring
  existing `pack` fetch tests).
- Merge precedence: fetched⊕inline for values (override, deep-merge nesting,
  `null` deletion, array replacement, int normalization) and tuning (strict
  decode, unknown-field rejection, int64 preservation).
- `RenderWith` chartless guard extended to `valuesRef`.
- Lock: entries with/without values/tuning/cluster pins; ref-less cube locks
  byte-identical to current goldens.
- `config.Load` dispatch: existing-local wins over ref-shaped name; missing
  local non-ref → `CUBE-0001`; remote parse via `loadBytes` equals local
  parse of same bytes; `SaveValidated` on remote origin → `CUBE-0012`.
- `upgrade --plan`: values-pin drift reported as its own line item.

E2E (existing kind harness; local gitea + zot already run in it — remember
`CUBE_IDP_E2E_GATEWAY_PORT=18443` locally):

- Publish a values YAML to the e2e gitea (or zot as OCI artifact); cube with
  `valuesRef` + inline override → `up` → assert rendered manifest reflects
  fetched base with inline override applied, and `cube.lock` carries
  `valuesRef`+`valuesPin`.
- Push `cube.yaml` to gitea; `up -f <git-ref>@<sha>` succeeds read-only,
  `cube.lock` lands in CWD; a mutating command against the same `-f` fails
  `CUBE-0012`.
- `tuningRef` leg: fetched replicas tuning visible on the engine Deployment.

Frozen surfaces: `TestCommandTreeGolden` must pass with **no** golden update
(no flag changes). Pack record output (DEP4 columns) untouched.

## 10. Implementation order (suggested lanes)

1. **RV1** — `pack.FetchFile` pin return + `internal/refval` + `compose`
   migration (pure refactor + new package; no user-visible change).
2. **RV2** — `valuesRef`: schema/types, desired-state resolution + merge,
   render guard, lock `ValuesRef/ValuesPin`, `CUBE-4021`.
3. **RV3** — `tuningRef`: schema/types, engine assembly resolution + strict
   merge, lock `TuningRef/TuningPin`, `CUBE-3012`.
4. **RV4** — remote `-f`: `loadBytes` refactor, dispatch, origin metadata,
   `SaveValidated` guard, CWD lock path, `CUBE-0012`/`CUBE-0013`.
5. **RV5** — `upgrade --plan` attribution + `ClusterLock` pin + docs
   (README, machine-readable-output if lock schema is documented there) +
   e2e legs.

Each lane is independently shippable; RV1 gates the rest.
