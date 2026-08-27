# Domain: gateway

Living contract of the gateway domain (`internal/gateway`).
Cross-cutting rules: `docs/ARCHITECTURE.md`. Originating design gate:
`docs/DECISIONS.md` 2026-08-27 (M11, epic #177) — **gated ahead of
code**: this contract is the operator-approved design; the package
lands via the M11 implementation breakdown, and no code exists before
that breakdown is aligned.

## Purpose

`internal/gateway` is the **bootstrap-phase trust fabric**: the ingress
gateway, the cube's hostname convention, and the in-cluster DNS redirect
that make `https://<anything>.<domain>` reach the cube — the identity
fabric everything in steady state, including the engine's own endpoints,
presumes (founding rationale, operator 2026-08-24: "certs / hostnames /
trust / internal dns redirect and name resolutions"). Certificate
authority custody and minting are **not** this domain's — they belong to
the `ca` domain (`docs/domains/ca.md`), gated in the same M11 decision;
the gateway consumes the leaf certificate as an injected fact.

The domain is **pure** in the substrate's mold: it owns embedded
prerequisite content and emits objects, blocks, and predicates derived
from `spec.gateway`. All I/O — applying anything, reading or writing the
live CoreDNS ConfigMap — stays at the CLI/orchestrator edge, on
bootstrap's machinery or the edge's own client.

## The prerequisite model (the ordered list)

Prerequisites are an **ordered list of prerequisite packs** that
bootstrap delivers through the tier-1 substrate **before the engine** —
after the substrate's kind-set readiness, before the engine's sync
wiring and bundle. The list is plural by design and its members are
separable by design:

- **The Gateway API CRDs are their own prerequisite pack** — never
  folded into the gateway pack. Rationale (operator, D1): a future
  engine may ship its own Gateway API CRDs; keeping the CRDs a separate
  list member avoids a conflict inside one pack and lets the
  prerequisite list vary per setup. There can be more than one
  prerequisite pack, and later milestones may add members without
  reshaping the machinery.
- Each list member is applied and **waited ready before the next member
  is applied** — CRDs are Established before anything that instantiates
  them exists (the generalized bootstrap sequencing, D9;
  `docs/domains/bootstrap.md`).

M11's list, in order:

1. **`gateway-api-crds`** — an embedded **raw** pack (substrate
   discipline: `pack.cue` with `type: raw`, `category: "gateway"`,
   pinned version, recorded sha256, `make` regeneration, never fetched
   at runtime) vendoring the Gateway API **standard channel**
   `standard-install.yaml`. Verified at the gate (2026-08-27, the
   v1.6.1 release asset): 10 CRDs plus the upstream `safe-upgrades`
   admission policy, 1,170,953 bytes — the largest embedded asset in
   the binary (the Flux
   substrate asset is 232 KB); the scoping estimate of 100–300 KB is
   corrected on the record. Bootstrap SSA-applies it and waits the
   kind-set (CRD `Established`).
2. **The CA material** — not a pack (packs never carry secrets): the
   `ca` domain's minted-or-reused CA and wildcard leaf cross the edge
   as Secret objects for the gateway namespace, SSA-applied and
   inventory-recorded like any bootstrap-owned object. Ordered here
   because the gateway release mounts the leaf.
3. **`traefik-gateway`** — the gateway itself, a **thin-helm**
   prerequisite pack in the M9 delegation shape (this is deliberate
   M9 dogfooding): an embedded `type: helm` pack (`category:
   "gateway"`) whose rendered output is the `HelmRelease` + OCI source
   CR pair, reconciled in-cluster by the **substrate's own
   helm-controller**. The chart reference is **digest-pinned** (the
   chart is OCI-published — verified at the gate:
   `ghcr.io/traefik/helm/traefik`). Bootstrap SSA-applies the pair and
   runs the reconciliation wait with this domain's predicates.

Day-0 consequence, stated plainly: the thin-helm member makes bootstrap
**network-dependent** (the chart is pulled in cluster). The
**embedded-raw fallback** — vendoring the full gateway render as a raw
pack — is the documented air-gap answer, **deferred to the M12 bus
gate**, which owns the air-gap decision (D1/D10).

The prerequisite packs are **conforming packs** under the substrate's
discipline: the domain parses/emits its own content without importing
`internal/pack`, and an edge-level green-gate dogfood test asserts the
pack contract loads and renders each embedded pack to exactly the
objects the domain emits. `category: "gateway"` becomes the second
*used* well-known spelling — identification only, never a code path,
same as `"engine"`.

## Which gateway, and which routing API (D2 — verified, not asserted)

**Traefik, with Gateway API (standard channel) as the cube's routing
contract**; Ingress is tolerated for content that needs it (Traefik
serves both concurrently). The footprint claim was verified at the gate
(2026-08-27) rather than asserted:

- Traefik chart 41.3.0 (Traefik v3.7.11; OCI digest at verification
  time
  `sha256:dcae2d586d7fbda6a08150eaeeca4132e9dd042d8a4d16ada287e8c40f6ff17a`),
  rendered with `helm template t oci://ghcr.io/traefik/helm/traefik
  --version 41.3.0 --set providers.kubernetesGateway.enabled=true
  --set gateway.enabled=true`: **8 objects, exactly one Deployment** —
  the controller is the data plane. Envoy Gateway (the conformant
  alternative) runs a controller Deployment **plus** a separately
  provisioned per-Gateway Envoy Deployment and its own config CRDs —
  structurally more moving parts on a laptop cube. These are
  gate-time evidence; the implementation re-pins (chart digest, asset
  sha256) under the embedded-pack discipline at the breakdown.
- ingress-nginx is disqualified for new adoption: retirement announced,
  best-effort maintenance ended March 2026.

The implementation is **pack content, not a seam**: no gateway driver
interface exists (one implementation; the second-implementation doctrine
governs), and swapping the gateway later is a content change behind the
same prerequisite shape.

## Config surface (`spec.gateway`)

An optional typed sub-struct on `ConfigSpec`, defaults and validation
beside it in `api/config/v1alpha1`:

```go
// GatewaySpec configures the cube's trust-fabric gateway.
type GatewaySpec struct {
    // Domain is the cube's base domain: the gateway serves, and the
    // leaf certificate covers, the wildcard *.<domain>. Default:
    // <metadata.name>.cube.test, where "cube.test" is the single
    // compile-time base-domain constant.
    Domain string `json:"domain,omitempty"`
}
```

- **Absent `spec.gateway` means installed with defaults** (D8) — the
  gateway is fundamental fabric, the engine precedent: not opt-in, no
  off-switch until a gateway-less cube is a real need.
- The `cube.test` fallback is a **compile-time const by design** (D5a):
  RFC 2606-reserved (no OS/mDNS collision, the `.local` pitfall), and
  deliberately recompilable — an operator building their own binary may
  rebase every cube's default domain at the one constant; recording
  that intent is part of the decision.
- Validation: `domain` must be a valid DNS name (lowercase RFC 1123
  labels); errors are config-domain `CUBE-CFG-*` document errors
  (exit 2).

## Host reachability (D5c/D5d — cross-domain facts, recorded here)

- **The kind driver defaults to high host ports** (operator override of
  the scoped recommendation): when the user supplies no explicit
  `forProvider`, the kind driver adds `extraPortMappings` **8080→80 and
  8443→443** plus the `ingress-ready` node label — unprivileged on
  every OS, so URLs carry ports (`https://app.<domain>:8443`).
  **Explicit `forProvider` always wins** — the default never merges
  into user-supplied config. This is a *cluster-domain* default — its
  delimited gated amendment lives in `docs/domains/cluster.md`, its
  lead reviews it; two boundaries are recorded here too because they
  shape this domain:
  - **The driver cannot see `spec.gateway`** — it receives only
    `{Name, ForProvider}`; the default is therefore unconditional, not
    gateway-triggered, coherent with D8's default-on posture.
  - **Create-before-bootstrap coupling**: port mappings exist only at
    cluster *create*. A gateway wanted on a cluster created without
    them needs a recreate (or a port-forward escape hatch); a host-port
    collision (a second default cube) fails `create` loudly.
- **Exposure inside the cluster is kind's ingress-ready pattern**
  (D5d): the mapped node carries the label, and the gateway Deployment
  pins to it — `nodeSelector` on the label, hostPorts 80/443 in the
  chart values. Direct node-port→pod path; no LoadBalancer, no
  NodePort translation. Multi-node topologies inherit the
  label/selector convention.
- **No `/etc/hosts` entries in M11** (D5b): `/etc/hosts` has no
  wildcard support, no gateway-owned application endpoints exist in v0,
  and app-route hostnames arrive only with M12 delivery. M11 emits
  nothing host-resolution-related; per-host entries are M12's
  route-host-discovery work. "Wildcard just works on the host" is not
  a v0 promise.

## In-cluster DNS: the CoreDNS marker block (D6)

The same hostnames must resolve *inside* the cluster to the gateway
(the "internal dns redirect" of the founding rationale). On kind that
means editing the kubeadm-owned `kube-system/coredns` ConfigMap —
**the first sanctioned mutation of an object cube-idp does not own**,
approved with this safety envelope, all five clauses binding:

1. The domain renders a **marker-delimited rewrite block** as a pure
   function of the domain and the gateway namespace; the edge performs
   the read-modify-write. The canonical shape (binding as contract;
   exact escaping fixed at implementation):

   ```
   # cube-idp:begin <cube-name>
   rewrite stop {
       name regex (.*)\.<regex-escaped domain>\.$ traefik.gateway-system.svc.cluster.local.
       answer auto
   }
   # cube-idp:end <cube-name>
   ```

   Three properties are load-bearing: the target is the gateway
   Service's **stable** cluster DNS name (the release name is pinned in
   the thin-helm pack so the Service name never floats); **`answer
   auto` rewrites the response name back**, without which clients
   reject answers issued for the Service name; and the domain is
   regex-escaped, never interpolated raw. The block is spliced
   **inside the default server block** (`.:53 { … }`) directly after
   its opening line — CoreDNS plugin execution order is compiled in,
   so the textual position only needs to be deterministic, and this
   one is.
2. The splice is **idempotent and preserving**: replace the block
   between the cube's markers if present, insert it if absent, touch
   nothing unmarked — everything outside the markers is preserved
   byte-for-byte.
3. The write runs under **optimistic concurrency** — an update with
   the read `resourceVersion`, retried on conflict; never a
   read-derived blind SSA write (which could clobber a concurrent
   Corefile change).
4. The ConfigMap is **never inventory-recorded** — the bootstrap
   inventory is a deletion seed, and a system object must never be
   seeded for deletion.
5. Teardown is **restore, not delete** — strip the marker block, leave
   the object — published as a requirement on the M13 `down` gate.

CoreDNS reloads on config change; verification of the rewrite is a
best-effort resolution probe, not a readiness gate.

## What the domain owns (pure) vs the edge (I/O)

| `internal/gateway` (pure) | CLI/orchestrator edge (I/O) |
|---|---|
| embedded prerequisite packs (`gateway-api-crds`, `traefik-gateway`) + their provenance checks | SSA-applying every prerequisite via bootstrap machinery |
| the rendered `HelmRelease` + OCI source pair (chart values derived from `spec.gateway` and the injected leaf-Secret name) | reading the live Corefile, splicing, optimistic-concurrency write-back |
| the CoreDNS marker block for a domain | assembling the ordered prerequisite list and injecting it into bootstrap |
| readiness predicates for its declared CRs (`HelmRelease`/OCI source: `Ready` **and** `status.observedGeneration == metadata.generation` — the no-stale-success doctrine in this domain's vocabulary) | the CA-Secret read and the `ca` handoff (see `docs/domains/ca.md`) |
| the gateway namespace fact (`gateway-system` — exported invariant, mirroring the substrate-namespace fact; engine-neutral by name so a gateway content swap never moves the namespace) | |

The predicates deliberately mirror the flux driver's freshness rule
without importing `internal/engine` — the doctrine ("no stale success
counts as reconciled") is cross-cutting; each domain states it over its
own CRs, and function values cross the edge as neutral vocabulary.

## Error codes (`CUBE-GWY-*`, exit 1)

Declared in the domain's `errors.go`; the initial catalog (numbers
final, meanings binding; extensions follow the normal per-domain rule):

| Code | Meaning |
|---|---|
| `CUBE-GWY-001` | embedded prerequisite payload fails its sha256 provenance (the `CUBE-ENG-003` analogue) |
| `CUBE-GWY-002` | embedded prerequisite payload fails to parse into objects |
| `CUBE-GWY-003` | object handed to the readiness predicate outside the domain's declared coverage (the `CUBE-ENG-002` analogue) |
| `CUBE-GWY-004` | live Corefile does not contain the structure the splice requires (unparseable / markers corrupted) — raised by the pure splice function, surfaced by the edge |

Document-layer `spec.gateway` errors are `CUBE-CFG-*` (exit 2); codes
are never re-tagged across domains.

## Testing

Hermetic, no Docker: provenance and parse checks over the embedded
packs; the edge-level pack-contract dogfood test per embedded pack
(load+render ≡ domain emission, deep equality of ordered lists — the
substrate's discipline); table-driven splice tests over real
kubeadm-shaped Corefile fixtures (absent block, present block,
corrupted markers, unmarked content preserved byte-for-byte, the
regex-escaping of exotic-but-valid domains); predicate tables over
ready / not-ready / stale / unknown fixtures. The real round-trip (prerequisites applied →
HelmRelease Ready → hostname resolves through the gateway on a kind
cluster) extends `make test-e2e`; never the green gate.

## Frozen — not designed in M11 (D10)

The bus write path and `RenderPlan.Prerequisites` semantics (M12 —
which inherits this list as prior art); the air-gap embedded-raw
fallback decision (M12); steady-state ownership migration of the
prerequisites (M12+, one answer with substrate self-management);
route-host discovery and any host-resolution emission (M12); CoreDNS
restore and teardown semantics (M13); OS resolver or hosts-file
mutation (a future explicit verb, operator policy); a gateway driver
seam; multi-gateway and non-kind port topologies; certificate rotation
and ACME (see `docs/domains/ca.md`).
