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

`config.EngineSpec` exposes exactly `Type`, `Ref`, `Values` and `SelfManage`, and the
cube.yaml engine schema accepts only those four keys. Engine customization is expressed as
real Helm chart values under `engine.values`, joining the fixed configuration triad in
which `values` renders Helm — for packs and the engine alike — and `extraManifests`
appends objects. Ad-hoc patch DSLs remain prohibited.

Because values are Helm-only, `engine.values` is valid only for `type: argocd`. Setting it
with `type: flux`, or setting `values`/`valuesRef` on any chartless pack, is the typed
error CUBE-4016 at render time.

The `tuning` noun is retired rather than deprecated: a present `engine.tuning` key is
rejected at config load with a typed migration diagnostic (CUBE-0012) pointing at
`engine.values` and the `cube-engine-<type>` pack README. No dual code path is kept.

Argo CD also ships as a UI-only pack for flux users, so listing the argocd pack while
`engine.type` is `argocd` is a validation error (CUBE-0005). `cube-idp init` writes a
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
| `config.EngineSpec` exposes exactly Type, Ref, Values and SelfManage; the schema accepts only those keys, and `EngineTuning`/`ComponentTuning` no longer exist. | `internal/config/types.go:91`, `internal/config/schema.cue:21` |
| The configuration vocabulary is a fixed triad in which `values` renders Helm (for packs and the engine alike) and `extraManifests` appends objects. | `internal/config/types.go:99`, `internal/config/types.go:196` |
| Engine customization is expressed as real Helm chart values under `engine.values`; the `tuning` noun is retired from the product vocabulary while the prohibition on ad-hoc patch DSLs still stands. | `internal/config/types.go:99` |
| `engine.tuning` is removed rather than deprecated: config load rejects a present tuning key with a typed migration diagnostic (CUBE-0012) and no dual code path is kept. | `internal/config/load.go:96` |
| `values:` and `valuesRef:` are Helm values only: setting either on a chartless pack, or `spec.engine.values` with engine type flux, is the typed error CUBE-4016 at render time. | `internal/pack/render.go:130` |
| `engine.values` is therefore supported only for `type: argocd`; the chartless `cube-engine-flux` pack routes through the same render check. | `internal/pack/enginepack.go:24` |
| Argo CD ships as a UI-only pack for flux users; listing the argocd pack while `engine.type` is argocd is a validation error (CUBE-0005). | `internal/config/load.go:196` |
| `cube-idp init` writes a kind + flux + traefik + gitea + argocd default profile and accepts `--engine` (flux\|argocd, default flux), dropping the redundant argocd pack when argocd is selected. | `cmd/init.go:169`, `cmd/init.go:107` |
| `cube-idp init --local` derives `spec.engine.ref` from the local packs checkout; published mode leaves `engine.ref` unset so the default published pin applies. | `cmd/init.go:153` |

### Verification

- [ ] `internal/config/types.go` `EngineSpec` has exactly four fields: Type, Ref, Values, SelfManage.
- [ ] `internal/config/schema.cue` engine block accepts only `type`, `ref?`, `values?`, `selfManage?` — no `tuning`.
- [ ] `grep -rn "EngineTuning\|ComponentTuning" --include='*.go'` matches only diagnostic code definitions, never a type declaration.
- [ ] `internal/config/load.go` returns `diag.CodeEngineTuningRemoved` (CUBE-0012) when raw YAML carries `spec.engine.tuning`; covered by `TestEngineTuningRemovedIsCube0012` (`internal/config/load_test.go:373`).
- [ ] `internal/pack/render.go` `RenderWith` returns `diag.CodePackValuesChartless` (CUBE-4016) when `len(values) > 0 && !p.HasChart()`.
- [ ] `internal/pack/enginepack.go` `FetchRenderEngine` calls `pk.RenderWith(spec.Values, ...)`, so engine values reach the same guard.
- [ ] `internal/config/load.go` cross-validation returns `diag.CodeArgoPackRedun` (CUBE-0005) for an argocd pack under `engine.type: argocd`; covered by `TestLoadRejectsArgoPackWithArgoEngine` (`internal/config/load_test.go:116`).
- [ ] `cmd/init.go` registers `--engine` defaulting to `"flux"` and omits the argocd pack when `engineType == "argocd"`.
- [ ] `cmd/init.go` sets `Spec.Engine.Ref` only when `--local` is given; `EngineSpec.PackRef()` otherwise falls back to `defaultEngineRefs` (`internal/config/types.go:116`).

## History

Under the previous design, `engine.tuning` entries were applied as in-memory patches over
pre-rendered embedded engine manifests before server-side apply, because the engine install
path had no Helm chart to merge values into. With the engine shipped as a pack,
`spec.engine.values` became genuine chart values of the `cube-engine-<type>` pack rendered
by Helm. The `EngineTuning`/`ComponentTuning` types, `internal/engine/tune.go` and the
schema.cue tuning block were deleted, and the `tuning` key now produces a migration
diagnostic. The old per-component diagnostic CUBE-3009
(`internal/diag/codes.go:84`) is retained only as a retired code and has not been emitted
since.

## More Information

Origin: mined from the archived planning corpus (`docs/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:236` — engine joins the `values` vocabulary, `tuning` retired.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:223` — removal rather than deprecation, with a migration diagnostic.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:368` — `engine.values` for `type: argocd` only.
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md:518` — the four-field `EngineSpec` and closed engine schema.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:37` — Argo CD as a UI-only pack for flux users (CUBE-0005).
