---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 39. Gateway Token Substitution and Render-Derived Gateway Edges

## Context and Problem Statement

Packs ship charts, manifests and expose URLs that must reference the cube's ingress
gateway, but the gateway host, port and pack name are cube-level configuration that a
pack author cannot know. Packs therefore need a placeholder vocabulary that renders to
concrete values at install time, and it must resolve identically on all three render
paths (helm values, raw `manifests/*.yaml`, and kustomize builds) or the same pack
produces different URLs depending on how it happens to be packaged.

A second, related problem is ordering. Packs that contain Gateway API objects
(`Gateway`, `HTTPRoute`, …) cannot be applied before the gateway pack has installed the
corresponding CRDs. The naive fix — making every pack depend on the gateway — serializes
installs that have no such requirement. Ordering constraints should follow from what a
pack actually renders. `up` itself also applies one Gateway API object outside any pack
(the registry HTTPRoute), which needs its own readiness gate.

## Decision

Pack rendering substitutes three gateway tokens: `${GATEWAY_HOST}` (host with `:port`,
port omitted at 443), `${GATEWAY_FQDN}` (the bare gateway host) and `${GATEWAY_PACK}`
(the gateway pack's name, also its namespace). Substitution is applied across expose
URLs, chart values, pack manifest bytes and `packs[].extraManifests`, and runs *after*
the defaults merge so tokens resolve regardless of which side of the merge contributed
them. The kustomize render path applies the same substitution to built YAML bytes before
parsing, so it matches the manifests and helm paths. Substitution is a no-op for a zero
gateway spec (`gw.Host == ""`), which is how the gateway-less `RenderDir` path preserves
literal tokens.

Ordering dependencies on the gateway are derived from render output rather than declared,
which is why substitution and the graph pass both run after every pack is rendered. See
ADR-0005 for the authoritative statement of the implicit gateway dependency edge and the
delivery-ordering algorithm.

`up` additionally waits for the `httproutes` CRD to become Established, with a deadline
typed CUBE-5005, before applying the registry HTTPRoute. This
`waitCRDEstablished(httpRouteCRD)` gate stays in `up.Run` because it guards a route `up`
applies itself, outside any pack.

## Consequences

* Good, because a pack authored once renders identically whether it is delivered as a
  chart, raw manifests or a kustomization — the substitution is a single shared function.
* Good, because packs with no Gateway API content are not forced to queue behind the
  gateway pack, so installs parallelize as far as the real constraints allow.
* Good, because the CRD wait is bounded and typed (CUBE-5005) rather than an unbounded
  spin, so a stuck gateway controller surfaces as a diagnosable error.
* Bad, because token substitution is textual: it happens on raw bytes and string leaves,
  so a literal `${GATEWAY_HOST}` a pack wanted to keep cannot be escaped.
* Bad, because the gateway edge depends on render output, meaning ordering cannot be
  computed from pack metadata alone — every pack must be rendered before the graph is
  resolvable.
* Bad, because one Gateway API consumer (the registry HTTPRoute) sits outside the pack
  graph and needs its own hand-written gate, so the readiness logic exists in two places.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Pack rendering substitutes `${GATEWAY_HOST}`, `${GATEWAY_FQDN}` and `${GATEWAY_PACK}` across expose URLs, chart values, pack manifest bytes and `extraManifests`, applied after the defaults merge, with the kustomize path substituting built YAML bytes before parsing; a zero gateway spec is a no-op. | `internal/pack/expose.go:58-66`; `internal/pack/helm.go:142`; `internal/pack/render.go:83,140`; `internal/pack/kustomize.go:34` |
| `up` waits for the `httproutes` CRD to become Established with a CUBE-5005-typed deadline before applying the registry HTTPRoute, and this gate is retained in `up.Run` because it guards a route outside any pack. | `internal/up/up.go:442`; `internal/diag/codes.go:123` |
| *(superseded)* Gitea is never itself repo-delivered, is hard-ordered immediately after the gateway, and repo delivery is gated on gitea API readiness. | `internal/up/up.go:1163-1170`, `internal/up/up.go:1179-1190` |

### Verification

- [ ] `internal/pack/expose.go:58-66` — `substitute` replaces all three tokens and is a
      no-op when `gw.Host == ""`.
- [ ] `internal/pack/helm.go:142` — `substituteValues` is called on
      `mergeValues(ref.Values, values)`, i.e. after the defaults merge, not before.
- [ ] `internal/pack/render.go:83` and `internal/pack/kustomize.go:34` — both call
      `substitute` on raw/built YAML bytes before `apply.ParseMultiDoc`.
- [ ] `internal/pack/render.go:140` — `substitute` is applied to
      `packs[].extraManifests` before `apply.ParseMultiDoc`.
- [ ] `internal/pack/kustomize.go:42` — `RenderDir` calls `RenderDirFor` with
      `config.GatewaySpec{}`, the gateway-less path the no-op preserves tokens for.
- [ ] `internal/up/up.go:442` — `waitCRDEstablished(ctx, a, con, httpRouteCRD,
      gatewayCRDTimeout)` precedes the registry HTTPRoute apply; `httpRouteCRD` is
      defined at `internal/up/up.go:68`.
- [ ] `internal/diag/codes.go:123` — `CodeRegistryRouteCRDTimeout` is `CUBE-5005`.

## History

An earlier decision hard-coded Gitea's position in the delivery order: Gitea was never
itself delivered via repo delivery, was hard-ordered immediately after the gateway, and
repo delivery was gated on Gitea API readiness. Two of those three sub-claims still
hold — gitea can never be repo-delivered, and repo delivery is still gated on gitea API
readiness by `giteaSession`'s bounded poll returning CUBE-7301
(`internal/up/up.go:1179-1190`). The hard ordering is gone: `orderPackRefs`
(`internal/up/up.go:1168-1170`) now only prepends the gateway, and the gitea hoist moved
to `pack.ResolveOrder`'s implicit repo→gitea graph edge with a declared-order tie-break
(`internal/up/up.go:1163-1167`). This is consistent with the wider move to deriving
ordering edges from rendered content rather than hard-coding them.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2278` — gateway token substitution
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:458` — render-derived gateway edge
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:256` — httproutes CRD gate
- `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:54` — superseded gitea hard ordering
