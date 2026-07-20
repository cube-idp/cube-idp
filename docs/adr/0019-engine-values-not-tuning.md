---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 19. Engine Configuration Is Helm Values, Not a Tuning DSL

## Context and Problem Statement

The GitOps engine (Flux or Argo CD) needs to be customizable — replica counts, resource
requests, image overrides. Earlier the engine was installed from manifests embedded in the
binary, so there was no chart to merge values into; customization was expressed as a
bespoke `engine.tuning` block whose entries were applied as in-memory patches over those
manifests before server-side apply. That created a second, engine-only configuration
vocabulary alongside the `values` / `extraManifests` pair every pack already used, and a
patch DSL whose surface grew with every knob a user wanted.

Once the engine itself shipped as a pack, that asymmetry had no justification: the engine
pack renders like any other pack, so it can take real Helm chart values. The open question
was whether to keep `tuning` working alongside `values` for compatibility, and what to do
about packs that have no chart at all — where "values" cannot mean anything.

A second, related problem: `engine.type: argocd` installs Argo CD *including its UI*,
while flux users who want the Argo CD UI need it as a standalone pack. Both cannot be
listed at once without installing Argo CD twice.

## Decision

`config.EngineSpec` exposes exactly `Type`, `Ref`, `Values` and `SelfManage`; the cube.yaml
engine schema declares only those four keys and CUE closure rejects any other. `tuning`
specifically is intercepted before CUE, so it gets the migration diagnostic below rather
than a generic unknown-field error. Engine customization is expressed as real Helm chart
values under `engine.values`, joining the same configuration pair every pack already uses:
`values` renders Helm — for packs and the engine alike — and `extraManifests` appends
objects. Ad-hoc patch DSLs remain prohibited.

Because values are Helm-only and the flux engine pack is chartless, `engine.values` is
valid only for `type: argocd`; setting it with `type: flux` hits the same chartless-values
render error, CUBE-4016. See ADR-0004 for the authoritative statement of CUBE-4016 and the
general `values`/`extraManifests` rule; this ADR decides only the engine-specific corollary.

The `tuning` noun is retired rather than deprecated: a present `engine.tuning` key is
rejected at config load with a typed migration diagnostic (CUBE-0012) pointing at
`engine.values` and the `cube-engine-<type>` pack README. No dual code path is kept.

Argo CD also ships as a UI-only pack for flux users, so listing a pack whose ref contains
`packs/argocd` while `engine.type` is `argocd` is a validation error (CUBE-0005) — a
substring convention over the ref, not pack-identity resolution. `cube-idp init` writes a
kind/flux/traefik default profile, and its `--engine` flag (`flux|argocd`, default `flux`)
drops the redundant argocd pack when argocd is chosen.

## Consequences

* Good, because there is one customization vocabulary to learn and to document: `values`
 means Helm values everywhere, `extraManifests` means raw objects everywhere.
* Good, because engine customization inherits Helm's own merge and validation semantics
 instead of a hand-written patcher that had to be extended per knob.
* Good, because the failure modes are typed and specific — CUBE-0012 carries a migration
 recipe, CUBE-4016 names the chartless-pack cause, CUBE-0005 names the redundant pack.
* Good, because deleting the tuning path removed the `EngineTuning`/`ComponentTuning`
 types, `internal/engine/tune.go` and the schema.cue tuning block outright.
* Bad, because existing cube.yaml files using `engine.tuning` break at load rather than
 degrading; the migration is manual, guided only by the diagnostic.
* Bad, because `engine.values` is silently useless for `type: flux` until render time —
 the chartless flux engine pack rejects values structurally, so the constraint is not
 visible in the schema.
* Bad, because the engine and packs now share a failure mechanism that is structural
 (has-chart) rather than a named check, so the flux-specific error message is generic.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `config.EngineSpec` exposes exactly Type, Ref, Values and SelfManage; the schema's engine block declares only those keys (CUE closure rejects others), and `EngineTuning`/`ComponentTuning` no longer exist. | `internal/config/types.go`, `internal/config/schema.cue` |
| The configuration vocabulary is the same pair for packs and the engine alike: `values` renders Helm and `extraManifests` appends objects. | `internal/config/types.go` |
| Engine customization is expressed as real Helm chart values under `engine.values`, the documented replacement for the retired `tuning` noun. | `internal/config/types.go` |
| `engine.tuning` is removed rather than deprecated: config load rejects a present tuning key with a typed migration diagnostic (CUBE-0012) and no dual code path is kept. | `internal/config/load.go` |
| `values:` are Helm values only: setting them on a chartless pack — including `spec.engine.values` with engine type flux — is the typed error CUBE-4016 at render time (rule owned by ADR-0004). | `internal/pack/render.go` |
| `engine.values` is therefore supported only for `type: argocd`; the chartless `cube-engine-flux` pack routes through the same render check. | `internal/pack/enginepack.go` |
| Argo CD ships as a UI-only pack for flux users; a pack whose ref contains `packs/argocd` while `engine.type` is argocd is CUBE-0005 (substring convention over the ref, not pack-identity resolution). | `internal/config/load.go` |
| `cube-idp init` writes a kind + flux + traefik + gitea + argocd default profile (`config.Default`) and accepts `--engine` (flux\|argocd, default flux), dropping the redundant argocd pack when argocd is selected. | `internal/config/types.go`, `cmd/init.go` |
| `cube-idp init --local` derives `spec.engine.ref` from the local packs checkout; published mode leaves `engine.ref` unset so the default published pin applies. | `cmd/init.go` |

### Verification

- [ ] `internal/config/types.go` `EngineSpec` has exactly four fields: Type, Ref, Values, SelfManage.
- [ ] `internal/config/schema.cue` engine block accepts only `type`, `ref?`, `values?`, `selfManage?` — no `tuning`.
- [ ] `grep -rn "EngineTuning\|ComponentTuning" --include='*.go'` matches only diagnostic code definitions, never a type declaration.
- [ ] `internal/config/load.go` returns `diag.CodeEngineTuningRemoved` (CUBE-0012) when raw YAML carries `spec.engine.tuning`; covered by `TestEngineTuningRemovedIsCube0012` (`internal/config/load_test.go`).
- [ ] `internal/pack/render.go` `RenderWith` returns `diag.CodePackValuesChartless` (CUBE-4016) when `len(values) > 0 && !p.HasChart()`.
- [ ] `internal/pack/enginepack.go` `FetchRenderEngine` calls `pk.RenderWith(spec.Values, ...)`, so engine values reach the same guard.
- [ ] `internal/config/load.go` cross-validation returns `diag.CodeArgoPackRedun` (CUBE-0005) for an argocd pack under `engine.type: argocd`; covered by `TestLoadRejectsArgoPackWithArgoEngine` (`internal/config/load_test.go`).
- [ ] `cmd/init.go` registers `--engine` defaulting to `"flux"` and omits the argocd pack when `engineType == "argocd"`.
- [ ] `cmd/init.go` sets `Spec.Engine.Ref` only when `--local` is given; `EngineSpec.PackRef()` otherwise falls back to `defaultEngineRefs` (`internal/config/types.go`).

## History

Under the previous design, `engine.tuning` entries were applied as in-memory patches over
pre-rendered embedded engine manifests before server-side apply, because the engine install
path had no Helm chart to merge values into. With the engine shipped as a pack,
`spec.engine.values` became genuine chart values of the `cube-engine-<type>` pack rendered
by Helm. The `EngineTuning`/`ComponentTuning` types, `internal/engine/tune.go` and the
schema.cue tuning block were deleted, and the `tuning` key now produces a migration
diagnostic. The old per-component diagnostic CUBE-3009 is retained but marked retired — as
a constant (`internal/diag/codes.go`) and as its registry entry, which is what
`cube-idp explain CUBE-3009` still surfaces (`internal/diag/registry.go`) — and has not
been emitted since.

## More Information

Origin: mined from the archived planning corpus (`docs/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:236` — engine joins the `values` vocabulary, `tuning` retired.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:223` — removal rather than deprecation, with a migration diagnostic.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:368` — `engine.values` for `type: argocd` only.
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md:518` — the four-field `EngineSpec` and closed engine schema.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:37` — Argo CD as a UI-only pack for flux users (CUBE-0005).
