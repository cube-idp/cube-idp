---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 32. cube.yaml Is a KRM Document Authored in Plain YAML

## Context and Problem Statement

`cube.yaml` is the single declarative input to `cube-idp`: it names the cluster, the
GitOps engine, the gateway, and the packs to install. Two questions had to be settled
before the shape could stabilise.

First, what does the author actually write? The file is validated against an embedded CUE
schema, and it would have been easy to let that leak — asking operators to learn CUE, or
to write engine configuration in a schema-constrained dialect. Kubernetes users already
know `apiVersion`/`kind`/`metadata`/`spec`, so the file should read like a Kubernetes
resource and nothing more.

Second, how much of the engine's configuration surface should the schema own? Attempting
to enumerate every engine tuning knob in CUE means the schema must be revised for every
upstream chart change, and operators are blocked on a cube-idp release to set a value
their engine already supports.

A closed CUE schema also has a failure mode: a key it does not know about is rejected with
a generic "invalid config" error. For keys that were deliberately *removed*, that error is
actively unhelpful — it tells the operator nothing about where the setting went.

## Decision

`cube.yaml` is a KRM-shaped document at `apiVersion: cube-idp.dev/v1alpha1`, `kind: Cube`.
Users author plain YAML; CUE is an internal-only validation implementation detail and is
never presented as the authoring surface.

The engine never appears in `spec.packs`: its pack source is resolved from `spec.engine`
(an explicit `ref`, otherwise the published default pinned in Go). Engine configuration is
a free-form open `values?: {...}` block passed to the engine pack's chart, not a fixed
tuning knob set owned by the schema. See ADR-0019 for the authoritative statement of the
full `spec.engine` key set.

Optional nil-able config and lock fields (slices, maps, optional strings) carry
`omitempty`, so an unset value marshals as an absent key rather than an explicit null or
empty string. Non-pointer struct fields such as `ClusterSpec.Registry` are the known
exception and always marshal, which is why `schema.cue` declares `registry?` permissively.

Removed config keys are rejected at load time with a remediation pointing at their
replacement. Legacy-shape guards such as `engine.tuning` are probed pre-CUE from the raw
YAML bytes so the closed schema cannot swallow them as a generic `CUBE-0002`.

## Consequences

* Good, because operators author a file that looks like every other Kubernetes manifest
  they already read; no CUE knowledge is required to use cube-idp.
* Good, because the open `values?: {...}` block means engine chart options the schema has
  never heard of work immediately — value validation is the chart's job, not CUE's.
* Good, because `omitempty` discipline makes config and lock files round-trip: a loaded
  document re-serialises to something the schema still accepts, and diffs stay small.
* Good, because removed keys produce a named diagnostic with a migration recipe rather
  than an opaque schema rejection.
* Bad, because free-form `values` means a typo in a chart value path is not caught at
  config load time — it surfaces later, at render or reconcile.
* Bad, because every removed key needs a hand-written pre-CUE probe to keep its
  diagnostic; the guards are manual and must be added deliberately.
* Bad, because the engine living outside `spec.packs` is a special case: engine fetch and
  render follow a separate code path from the ordinary pack loop.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `cube.yaml` uses `apiVersion: cube-idp.dev/v1alpha1` and `kind: Cube`; users author plain YAML while CUE is an internal-only implementation detail, never the authoring surface. | `internal/config/schema.cue:3-5` |
| The literal `apiVersion`/`kind` pinning is non-optional and non-defaulted, so any other value is rejected at validation. | `internal/config/schema.cue:3-5` |
| The engine pack source is resolved from `spec.engine` (explicit `ref`, else the published default) and is never listed in `spec.packs`. | `internal/config/types.go:90-131`; `internal/up/up.go:244-254` |
| `schema.cue` has no `engine.tuning` block and declares an open `values?: {...}` field, replacing the fixed tuning knob set with free-form engine pack values. | `internal/config/schema.cue:21-31`; `internal/config/types.go:99-104` |
| The `engine.tuning` migration guard is probed pre-CUE from the raw YAML so the closed schema cannot swallow it as a generic `CUBE-0002`. | `internal/config/load.go:85-100` |
| Optional nil-able config and lock fields carry `yaml`/`json` `omitempty` so an unset value marshals as an absent key, matching `PackRef.Values` nil-map round-trip discipline. | `internal/config/types.go:196-224` |
| Non-pointer struct fields are the recorded exception: `ClusterSpec.Registry` carries no `omitempty` (the tag is a no-op there) and always marshals, which `schema.cue`'s `registry?` accepts. | `internal/config/types.go:48-54` |

### Verification

- [ ] `internal/config/schema.cue:3-5` pins `apiVersion: "cube-idp.dev/v1alpha1"` and
      `kind: "Cube"` as literal constraints, and `config.Default` in
      `internal/config/types.go` writes exactly those values for `cube-idp init`.
- [ ] `grep -c 'tuning' internal/config/schema.cue` prints `0`, and
      `grep -n 'values?' internal/config/schema.cue` shows the `values?: {...}` open struct
      inside the `engine` block at `internal/config/schema.cue:21-31`.
- [ ] `internal/config/load.go:85-100` unmarshals a `legacyTuning` struct from the raw
      YAML bytes *before* the cuecontext unify that follows it, returning
      `diag.CodeEngineTuningRemoved` (`CUBE-0012`) with a migration remediation.
- [ ] `CUBE-0012` is registered in `internal/diag/codes.go` and `internal/diag/registry.go`
      with an `engine.tuning was removed` summary.
- [ ] `internal/up/up.go:244-254` resolves the engine through
      `cube.Spec.Engine.PackRef()` and `pack.FetchRenderEngine`, outside the `spec.Packs`
      loop (which starts at `internal/up/up.go:299` via `orderPackRefs`).
- [ ] Every optional `PackRef` field in `internal/config/types.go:196-224` carries both
      `yaml:",omitempty"` and `json:",omitempty"`; the same holds for `EngineLock` fields
      in `internal/lock/lock.go:30-35` (`Type` at line 29 is required and carries none).
- [ ] `cmd/config.go:111` prints `config.Schema()` and nothing else.
- [ ] No other command renders or accepts CUE; the `CUBE-0002`/`CUBE-0012` remediations in
      `internal/config/load.go` only *reference* the `config schema` command.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member origins:

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:194` — plain
  YAML authoring surface, CUE internal-only.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-prerequisites-packs-design.md:24` —
  `apiVersion`/`kind` pinning.
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md:144` — engine referenced
  via `spec.engine.ref`, never `spec.packs`.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:111` —
  `engine.tuning` replaced by open `values?: {...}`.
- `docs/archive/superpowers/plans/2026-07-19-valuesref-remote-config.md:17` — `omitempty`
  round-trip discipline.

Rationale for the merge of these statements into one record was not recorded in the source
material.
