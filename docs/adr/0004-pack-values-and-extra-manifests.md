---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 4. Pack Values, Helm Value Merge Order, and extraManifests Layering

## Context and Problem Statement

A cube.yaml lists packs, and users need two distinct kinds of customization: tuning a
pack's Helm chart, and adding arbitrary Kubernetes objects alongside it. Packs are not
uniform — some ship a `chart.yaml`, others are plain `manifests/` directories — so a
single `values:` field cannot mean the same thing everywhere. Whether a pack has a chart
is not knowable from cube.yaml alone; it is only discoverable after the pack ref is
fetched. At the same time, once several sources of values exist (chart defaults, pack
defaults, user input, gateway token substitution), the order in which they combine has to
be pinned or renders become non-deterministic. Finally, an operator inspecting a cluster
needs to see at a glance which packs were installed stock and which were customized.

## Decision

`values:` in cube.yaml means Helm values only: they are validated against the pack's
optional `#Values` CUE schema and then merged into the pack's `chart.yaml` Helm render; a
pack without `chart.yaml` cannot accept them. There is no fetched or remote middle layer.
Supplying `values:` for a pack without `chart.yaml` is the typed error CUBE-4016, raised at
render time — chartlessness is only known after the pack is fetched — and before `#Values`
schema validation, so a chartless pack with values never reaches that schema. This ADR is
the authoritative definition of CUBE-4016; `spec.engine.values` flows through the same
`RenderWith` entry point (`internal/pack/enginepack.go`) and therefore inherits this
rule — see ADR-0019 for the engine-specific consequences.

Value merge order is fixed in three steps: `chart.yaml` defaults (`ref.Values`), then
inline user `values:`, then `${GATEWAY_*}` substitution, with CUE `int64` decodes
normalized to `int` at load time.

`packs[].extraManifests` is the uniform extras mechanism for every pack kind: a non-empty
multi-doc YAML string that is parsed, `${GATEWAY_*}`-substituted, appended to the pack's
rendered objects, and inventoried. Invalid YAML is reported as CUBE-4017, and a cleared
field marshals as an absent key.

Repo-delivered packs honor `values` and `extraManifests` exactly like OCI-delivered ones.
A pack with non-empty inline values or extraManifests is marked CUSTOMIZED on its Pack
record and shown as a CUSTOMIZED printer column in `kubectl get packs`.

## Consequences

* Good, because `values:` has exactly one meaning, so a user cannot silently expect raw
 manifests to be templated by it — the chartless case is a loud typed error.
* Good, because `extraManifests` gives every pack kind, chart-backed or not, one uniform
 escape hatch that rides the same delivery path as the pack's own objects.
* Good, because a fixed merge order plus number normalization makes renders reproducible
 and comparable against ordinary Go int literals in tests.
* Good, because customization is visible to operators directly in `kubectl get packs`
 rather than requiring a diff against cube.yaml.
* Bad, because the chartless-values error can only fire after a network fetch, so the
 failure arrives later than a pure load-time validation would.
* Bad, because with no remote values layer, values shared across several clusters must be
 duplicated inline in each cube.yaml.
* Bad, because `extraManifests` is an unstructured YAML string, so mistakes surface only
 as a parse error (CUBE-4017) rather than schema-level feedback.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `values:` means Helm values only: validated against the pack's optional `#Values` CUE schema and merged into the `chart.yaml` render; values on a chartless pack is CUBE-4016, raised before that `#Values` validation. | `internal/pack/render.go`, `internal/pack/pack.go` |
| `spec.engine.values` travels the same `RenderWith` entry point as pack values, so the chartless rule applies to it identically (see ADR-0019). | `internal/pack/enginepack.go` |
| The chartless-values error is enforced at render time rather than load time, because chartlessness is only knowable after the ref is fetched. | `internal/pack/render.go` |
| `packs[].extraManifests` is the uniform extras mechanism for every pack kind: a non-empty multi-doc YAML string that is parsed, `${GATEWAY_*}`-substituted, appended to the pack's objects and inventoried; invalid YAML is CUBE-4017 and a cleared field marshals as an absent key. | `internal/pack/render.go` |
| Additional user-supplied objects for any pack kind arrive via `packs[].extraManifests`, `${GATEWAY_*}`-substituted and appended to the pack's rendered output. | `internal/pack/render.go` |
| Helm pack value merge order is fixed as `chart.yaml` defaults (`ref.Values`) ← inline user `values:` ← `${GATEWAY_*}` substitution, with CUE `int64` decodes normalized to `int` at load time. | `internal/pack/helm.go` and `365-387`, `internal/config/load.go` |
| Repo-delivered packs honor `values` and `extraManifests` exactly like OCI-delivered ones: cube.yaml is the source of truth and the Gitea repo is the editable working copy. | `internal/up/up.go` |
| A pack installed with non-empty `values` or `extraManifests` is marked CUSTOMIZED on its Pack record and shown as a CUSTOMIZED printer column in `kubectl get packs`. | `internal/up/up.go` |
| A pack's `customized` record field is always written as `"yes"` or `"no"` and never omitted. (Superseded in part — see History.) | `internal/pack/expose.go` |
| Pack values layer via a fetched middle map combined by RFC 7386 merge-patch. (Superseded — never built; see History.) | — no code surface; design provenance only: `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:161` |
| Setting `valuesRef` on a chartless pack fails at render time with CUBE-4016. (Superseded — never built; see History.) | — no code surface; design provenance only: `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:161` |

### Verification

- [ ] `internal/pack/render.go` returns `diag.CodePackValuesChartless` when
      `len(values) > 0 && !p.HasChart()`, and this precedes the `RenderFor` call on
      line 135 where `validateValues`/`#Values` runs (`internal/pack/render.go`).
- [ ] `internal/diag/codes.go` defines `CodePackValuesChartless = "CUBE-4016"` and
      `CodePackExtraManifests = "CUBE-4017"`.
- [ ] `internal/pack/render.go` parses `extraManifests` with
      `apply.ParseMultiDoc([]byte(substitute(extraManifests, gw)))`, wraps failures as
      CUBE-4017, and appends the result to `r.Objects`.
- [ ] `internal/config/schema.cue` declares `extraManifests?: string & !=""` and
      `internal/config/types.go` carries the `omitempty` tag on `ExtraManifests`.
- [ ] `internal/pack/helm.go` is `substituteValues(mergeValues(ref.Values, values), gw)`
      and `mergeValues` (`internal/pack/helm.go`) deep-merges with override winning.
- [ ] `internal/config/load.go` normalizes CUE `int64` decodes to `int` for both
      `Spec.Packs[i].Values` and `Spec.Engine.Values`.
- [ ] `internal/up/up.go` calls `pk.RenderWith(...)` once per pack before any delivery
      branch, so repo- and OCI-delivered packs render identically.
- [ ] `internal/up/up.go` computes
      `customized := len(refs[i].Values) > 0 || refs[i].ExtraManifests != ""`.
- [ ] `internal/pack/expose.go` writes `spec.customized` as `"yes"`/`"no"`
      unconditionally, and `internal/pack/manifests/pack-crd.yaml` declares the
      `CUSTOMIZED` printer column; `internal/pack/discovery_test.go` pins both.
- [ ] `grep -rn "valuesRef\|ValuesRef" internal/ cmd/` returns nothing.

## History

An earlier design added a remote `valuesRef` field. Under it, values would have layered in
three tiers — chart defaults, a fetched `valuesRef` map, then inline values — combined via
RFC 7386 merge-patch; CUBE-4016 would also have guarded `valuesRef` on chartless packs; and
a `valuesRef` would have counted toward the `customized` record. `valuesRef` was never
built and has no code surface: it is absent from `internal/config/schema.cue`, and a
repo-wide grep for `valuesRef`/`ValuesRef` in `internal/` and `cmd/` returns nothing. RFC
7386 merge-patch is implemented only for the cluster ladder
(`internal/cluster/compose/compose.go`), not for pack values.

The shipped model is two-layer (chart defaults plus inline `packs[].values`), CUBE-4016
guards inline values only, and `customized` is computed from inline values and
`extraManifests` alone. The unconditional `"yes"`/`"no"` writing of `spec.customized`
survives from the earlier design and remains binding.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2139` — `values:` are helm values
 only; chartless packs with values are CUBE-4016.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2007` — `extraManifests` as the
 uniform extras mechanism.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2283` — fixed value merge order.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:224` — CUSTOMIZED marking and
 printer column.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:161` — the
 superseded three-tier `valuesRef` layering.
