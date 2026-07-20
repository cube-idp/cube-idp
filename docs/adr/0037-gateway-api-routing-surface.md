---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 37. Gateway API as the Routing Surface with a Swappable Gateway Pack

## Context and Problem Statement

cube-idp delivers packs into a cluster and must expose the services they contain
under a single, predictable hostname. Kubernetes offers two competing routing
APIs: the legacy `Ingress` object, whose behaviour is defined largely by
controller-specific annotations, and Gateway API, whose `Gateway` and
`HTTPRoute` resources are portable across implementations. Choosing one binds
every pack author: a pack must know which kind of routing resource to emit, and
that choice cannot be renegotiated per pack without fragmenting the ecosystem.

The routing implementation itself is a separate question. ingress-nginx is
end-of-life, so a default had to be picked that would not have to be re-picked
under duress — and picking one must not make the others unreachable. Users who
run a different data plane need to swap it without maintaining a fork.

Three practical hazards follow from this. Routing must exist before anything
that needs routing, so ordering matters. The gateway's source location is read
by several commands (`up`, `diff`, `upgrade`, `doctor`), so a resolution rule
duplicated per command will drift. And a config with two fields naming the
gateway — a pack name and an OCI ref — invites an incoherent pair where the ref
points at one implementation and the name at another.

## Decision

Gateway API is the canonical routing surface for cube-idp. The gateway pack
vendors the pinned standard-channel Gateway API CRDs, and services are exposed
through Gateway API rather than Ingress. Traefik is the default gateway pack,
and swapping the implementation is a configuration change, not a code fork.

The gateway pack is always delivered first, ahead of every user-declared pack.
Gateway pack ref resolution — `gw.Ref` when set, otherwise `packs/<gw.Pack>` —
is centralised in the shared helper `config.GatewaySpec.PackRef()`, used
identically by `up`, `diff` and `upgrade`.

`cube-idp init` always derives `gateway.ref` from the finally selected gateway
pack, so a mismatched pack/ref pair can never be authored.

## Consequences

* Good, because pack authors target one portable routing API; an `HTTPRoute`
  written for a pack works under any conformant gateway implementation.
* Good, because the gateway implementation is a config line. `traefik` and
  `envoy-gateway` are both selectable and neither is privileged in code.
* Good, because one resolution helper means `up`, `diff`, `upgrade` and `doctor`
  cannot disagree about where the gateway pack comes from.
* Good, because deriving `gateway.ref` from the chosen pack at init time makes
  the incoherent-pair failure unrepresentable rather than merely documented.
* Bad, because Gateway API CRDs must be established before any HTTPRoute is
  applied, adding a mandatory CRD wait to the delivery path.
* Bad, because there is no Ingress escape hatch: an environment that can only
  offer Ingress cannot be served.
* Bad, because the `packs/<pack>` fallback only resolves when cube-idp runs from
  the root of a packs checkout — a documented v0.1.0 caveat, not a general path.
* Bad, because the gateway-first rule fixes one edge of delivery order outside
  the general dependency graph.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Gateway API resources are the canonical routing surface; the gateway pack vendors the pinned Gateway API standard-channel CRDs, so services are exposed through Gateway API rather than Ingress. | `internal/config/types.go:248` |
| Traefik is the default gateway pack (ingress-nginx being end-of-life), and swapping the gateway implementation is a config line rather than a code fork. | `internal/config/types.go:248`; `cmd/init.go:38` |
| The gateway pack is always delivered first, ahead of all user-declared packs. | `internal/up/up.go:1168-1170` |
| Gateway pack ref resolution (`gw.Ref` when set, else `packs/<gw.Pack>`) lives in one shared helper used by `up`, `diff` and `upgrade` alike; the repo-relative fallback only resolves from inside a repo checkout. | `internal/config/types.go:178-183` |
| `cube-idp init` always derives `gateway.ref` from the finally selected gateway pack, so a mismatched pack/ref pair can never be authored. | `cmd/init.go:136-146` |

### Verification

- [ ] `internal/config/types.go:248` — `config.Default` sets `Gateway{Pack: "traefik", Ref: "oci://ghcr.io/cube-idp/packs/traefik:0.2.0"}`.
- [ ] `cmd/init.go:38` — `gatewayPacks` lists exactly `{traefik, envoy-gateway}`; no nginx option exists.
- [ ] `internal/config/types.go:178-183` — `GatewaySpec.PackRef()` returns `g.Ref` when non-empty, else `"packs/" + g.Pack`.
- [ ] `PackRef()` is the single resolution point: called from `internal/up/up.go:299`, `internal/diff/diff.go:226`, `internal/upgrade/plan.go:50` and `internal/doctor/doctor.go:504`.
- [ ] `internal/up/up.go:1168-1170` — `orderPackRefs` prepends the gateway ref and does nothing else.
- [ ] `cmd/init.go:136-146` — `gateway.Ref` is assigned after the wizard sets `gateway.Pack`, via `filepath.Join(localAbs, "packs", Pack)` in `--local` mode or `publishedGatewayRef(Pack)` (`cmd/init.go:48-50`) otherwise.
- [ ] Gateway API is on the delivery path: `internal/up/up.go:68` names `httproutes.gateway.networking.k8s.io` as the CRD every gateway pack must establish, and `up.go:442` waits for it before applying an HTTPRoute.
- [ ] `internal/pack/expose.go:53-54` — `${GATEWAY_PACK}` substitution exists so pack HTTPRoutes parent to the Gateway in the gateway pack's own namespace rather than a hardcoded `traefik`.
- [ ] No Ingress code path exists: `grep -rn "networking.k8s.io/v1.*Ingress" --include='*.go' .` returns nothing.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`)
during the 2026-07-20 documentation audit; the underlying statements were
validated against the code before this record was written.

Member origins:

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:32` — Gateway API as the routing surface.
- `docs/archive/superpowers/research/2026-07-13-cube-idp-brainstorm/proposals.md:88` — Traefik as the default, swap-by-config.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:208` — gateway-first delivery order.
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:1507` — single shared `PackRef()` helper.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2982` — init's pack/ref coherence rule.

One note on drift between the recorded statement and the code: the original
wording said `init` writes `gateway.ref` only in `--local` mode. It in fact
writes it in both modes (`cmd/init.go:142-146`); the substance — a mismatched
pack/ref pair cannot be authored — holds either way, and the Decision above
reflects the implemented behaviour.

No rationale for the specific `0.2.0` pin was recorded in the source material.
