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

Prerequisites are an **ordered list of prerequisite units** that
bootstrap installs **ahead of the engine** — after the substrate's
kind-set readiness, before the engine's sync wiring and bundle. The
mechanics are day-0 bootstrap SSA, with the thin-helm member's
*reconciliation* delegated to the substrate's helm-controller; nothing
enters the watched source (that path — the seam's "delivered through
tier 1" — is the M12 bus's). The **pack-shaped** members are the
**prerequisite packs** of the operator record (D1); the list admits
non-pack units where content cannot be pack-shaped. The list is plural
by design and its members are separable by design:

- **The Gateway API CRDs are their own prerequisite pack** — never
  folded into the gateway pack. Rationale (operator, D1): a future
  engine may ship its own Gateway API CRDs; keeping the CRDs a
  separate list member keeps that conflict from ever being locked
  inside one pack and lets the prerequisite list vary per setup.
  There can be more than one prerequisite pack, and later milestones
  may add members without reshaping the machinery.
- Each list member is applied and **waited ready before the next member
  is applied** — CRDs are Established before anything that instantiates
  them exists, and the namespace is Active before anything lands in it
  (the generalized bootstrap sequencing, D9;
  `docs/domains/bootstrap.md`).

M11's list, in order:

1. **`gateway-platform`** — the cube-owned platform vocabulary, two
   cube-authored objects, domain-emitted: the `Namespace`
   (`gateway-system`) and the **stable gateway Service** (`gateway`,
   the indirection layer in front of whatever implementation serves —
   operator direction, 2026-08-27; see "The stable gateway Service"
   below). Kind-set-waited: the Namespace `Active`; the Service is
   status-less and effectively inert within the raw unit (the
   kind-set wait ignores it by design). It is
   the **own leading unit**, deliberately: the vendored CRDs asset
   must stay byte-identical to upstream (injecting a cube object would
   pollute its recorded provenance), the thin-helm pack has no payload
   to carry it, and the platform vocabulary must exist independently
   of the CA
   provider choice (a future `cert-manager`/`kubernetes` provider may
   apply no Secrets unit at all, yet the gateway still needs its
   home). This mirrors the substrate's fact-ties-to-content
   discipline: the exported namespace and service-name facts and these
   emitted objects are
   tied by a green-gate test (Testing, below).
2. **`gateway-api-crds`** — an embedded **raw** pack (substrate
   discipline: `pack.cue` with `type: raw`, `category: "gateway"`, no
   `#Values`, no `namespace` — the M10 substrate shape; pinned
   version, recorded sha256, `make` regeneration, never fetched
   at runtime) vendoring the Gateway API **standard channel**
   `standard-install.yaml`. Verified at the gate (2026-08-27, the
   v1.6.1 release asset): 10 CRDs plus the upstream `safe-upgrades`
   admission policy, 1,170,953 bytes — the largest embedded asset in
   the binary (the Flux
   substrate asset is 232 KB); the scoping estimate of 100–300 KB is
   corrected on the record. Bootstrap SSA-applies it and waits the
   kind-set (CRD `Established`).
3. **The CA material** — an **inert unit**, not a pack: the embedded
   prerequisite packs carry no cluster-specific secret material, so
   the `ca` domain's minted-or-reused CA and wildcard leaf cross the
   edge as plain Secret objects for the gateway namespace, SSA-applied
   and inventory-recorded like any bootstrap-owned object. This unit
   **explicitly depends on unit 1** (its Secrets land in
   `gateway-system`; SSA into a nonexistent namespace is NotFound),
   which the list order guarantees. Its readiness rule is the inert
   flavor defined in the bootstrap amendment: Secrets carry no status,
   so **apply success is the unit's readiness** — a later reader must
   not "fix" the absent wait. Ordered before the gateway release
   because the release mounts the leaf.
4. **`traefik-gateway`** — the gateway itself, a **thin-helm**
   prerequisite pack in the M9 delegation shape (this is deliberate
   M9 dogfooding): an embedded `type: helm` pack (`category:
   "gateway"`) whose rendered output is the `HelmRelease` + OCI source
   CR pair, reconciled in-cluster by the **substrate's own
   helm-controller**. The chart reference is **digest-pinned** (the
   chart is OCI-published — verified at the gate:
   `ghcr.io/traefik/helm/traefik`). Bootstrap SSA-applies the pair and
   runs the reconciliation wait with this domain's predicates.
   Naming and namespaces, fixed here because the stable Service's
   `spec.externalName` references the release identity (and for chart
   tidiness — the CoreDNS block itself targets only the stable name,
   D6):
   - **Release identity is the M9 mechanism, not a new knob**:
     `releaseName` is the effective instance id, which for this
     embedded singleton is the pack name, `traefik-gateway` (no
     `spec.packs` override path exists for prerequisite packs in
     M11). The pack's baked values also pin
     `fullnameOverride: traefik-gateway` — tidiness, keeping the
     chart's resource names equal to the release identity. Since the
     stable-Service indirection (above), **DNS correctness no longer
     rides on either**: the effective id is an internal detail of the
     indirection's backend reference (`spec.externalName`), and the
     CoreDNS block targets only the stable name.
   - **`pack.cue` declares `namespace: gateway-system`**, which the M9
     mapping turns into `spec.targetNamespace` (+ its
     `createNamespace` coupling — a no-op by this point, since unit 1
     already created the namespace; harmless and unmodified).
   - **The edge stamps `metadata.namespace: gateway-system`** on the
     rendered, deliberately namespace-less `OCIRepository` +
     `HelmRelease` pair **after** rendering, before handing it to
     bootstrap — one decision for both CRs (the sourceRef
     same-namespace rule), and stamping post-render keeps the dogfood
     equality (Testing) against the contract's namespace-less output.

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
same as `"engine"`. This M11 ordered list is **not**
`RenderPlan.Prerequisites`: that field stays per-pack `lifecycle: pre`
data, frozen to the M12 bus; the list feeds it as prior art only.

## The stable gateway Service (operator-directed indirection, 2026-08-27)

The cube owns a **dedicated, predictable Service** in front of the
gateway implementation, so internal DNS and all future routing target
one stable name and never care what implementation is behind it:

- **Name: `gateway`** (canonical FQDN
  `gateway.gateway-system.svc.cluster.local.`) — the coordinator's
  naming ruling; the word *engine* stays reserved for the tier-2
  gitops-engine vocabulary and never names gateway objects.
- **Mechanism: `ExternalName`** — the Service carries
  `spec.externalName: <effective-id>.<gateway-namespace>.svc.cluster.local`
  (for M11: `traefik-gateway.gateway-system.svc.cluster.local`), i.e.
  a DNS-layer alias to the implementation's Service. Chosen over a
  selector-based Service because the indirection's whole job here is
  DNS (the internal redirect and future route targets are *names*,
  not endpoints), and the coupling surface is minimal: an
  ExternalName references only the implementation **Service name** —
  exactly the effective id this domain already derives — while a
  selector-based Service would couple to the chart's **pod labels**
  (which drift with implementation and chart versions) and duplicate
  the implementation Service's own job. Recorded limitation, honest
  and acceptable for v0: ExternalName is DNS-only — no ClusterIP, no
  endpoints, no kube-proxy programming stand behind the stable name;
  if something ever needs to *dial* the stable name by IP rather than
  resolve it, that gate revisits the mechanism (the name contract
  below survives either mechanism).
- **Verified empirically at the gate** (2026-08-27, kind node
  v1.35.0, CoreDNS v1.13.1): with the canonical marker block spliced
  into a live kubeadm Corefile and the `gateway` ExternalName in
  place, an in-cluster `dig app.<domain>` returns the CNAME to the
  implementation Service **plus the resolved A record of its
  ClusterIP in the same response** — modern CoreDNS's kubernetes
  plugin chases in-cluster CNAME targets itself (the historical
  CoreDNS/ExternalName in-cluster gap is closed), and the spliced
  block parsed and reloaded cleanly.
- **The stability contract, explicit:** the Service name is a
  **platform fact**, exported like the namespace facts.
  Implementation swaps, prerequisite-pack renames, and chart drift
  **never change it** — they change only the ExternalName's backend
  reference, inside this one cube-owned object. M12 route wiring and
  every future routing override **MUST target the stable name**,
  never an implementation Service.

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
  8443→443** plus the `ingress-ready` node label — above the
  conventional privileged-port range, so URLs carry ports
  (`https://app.<domain>:8443`).
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
    them needs a recreate; a host-port
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
   function of the domain and the platform facts (stable Service name
   + namespace); the edge performs
   the read-modify-write. The canonical shape (binding as contract;
   exact escaping fixed at implementation):

   ```
   # cube-idp:begin <cube-name>
   rewrite stop {
       name regex (.*)\.<regex-escaped domain>\.$ <gateway-service>.<gateway-namespace>.svc.cluster.local.
       answer auto
   }
   # cube-idp:end <cube-name>
   ```

   Both target components are **platform facts**, so the block is
   effectively constant: `gateway.gateway-system.svc.cluster.local.`.
   Three
   properties are load-bearing: the target is the **stable gateway
   Service** — never an implementation Service, never derived from a
   release identity — so implementation swaps and pack renames never
   touch a live Corefile (the effective id lives only inside the
   indirection's `spec.externalName`, and the resolution chain
   through it is verified — "The stable gateway Service", above);
   **`answer
   auto` rewrites the response name back**, without which clients
   reject answers issued for the Service name; and the domain is
   regex-escaped, never interpolated raw. The block is spliced
   **inside the default server block** (`.:53 { … }`) directly after
   its opening line — CoreDNS plugin execution order is compiled in,
   so the textual position only needs to be deterministic, and this
   one is (splice-position behavior confirmed in the same empirical
   run).
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

Sequencing and failure semantics, fixed here: the splice runs at the
edge **after the `traefik-gateway` unit is reconciled** (resolution
through the stable Service lands on that unit's implementation
Service) and before bootstrap success is reported;
a failed splice **fails the bootstrap** with its coded error
(`CUBE-GWY-004` from the pure function, or the wrapped conflict/write
cause from the edge — exit 1), never a silent degrade — a cube whose
in-cluster names do not resolve is not a bootstrapped cube. CoreDNS
reloads on config change; verification of the rewrite is a
best-effort resolution probe, not a readiness gate.

## What the domain owns (pure) vs the edge (I/O)

| `internal/gateway` (pure) | CLI/orchestrator edge (I/O) |
|---|---|
| embedded prerequisite packs (`gateway-api-crds`, `traefik-gateway`) + their provenance checks, and the `gateway-platform` unit's Namespace + stable `gateway` Service objects | SSA-applying every prerequisite via bootstrap machinery |
| the rendered `HelmRelease` + OCI source pair (chart values derived from `spec.gateway` and the injected leaf-Secret name) | reading the live Corefile, splicing, optimistic-concurrency write-back |
| the CoreDNS marker block for a domain | assembling the ordered prerequisite list and injecting it into bootstrap |
| readiness predicates for its declared CRs (`HelmRelease`/OCI source: `Ready` **and** `status.observedGeneration == metadata.generation` — the no-stale-success doctrine in this domain's vocabulary) | the CA-Secret read and the `ca` handoff (see `docs/domains/ca.md`) |
| the two platform facts (namespace `gateway-system`, stable Service `gateway` — exported invariants; the mirroring of the substrate fact is of the construct — an exported fact injected as a string — and both names are deliberately implementation-neutral, unlike `flux-system`, so a gateway content swap never moves the namespace or the DNS anchor) | |

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
substrate's discipline; for the thin-helm pack this deliberately locks
the domain's hand-built CR pair, including the effective-id-derived
`releaseName`, to every M9 field-level rule — loud by design); the
**fact-ties-to-content test**: the exported namespace and
service-name facts equal the `metadata.name` of the Namespace and
Service objects the `gateway-platform` unit
emits, and the Service's `spec.externalName` equals the effective
id's FQDN (the substrate's namespace-tie test, mirrored and
extended); table-driven
splice tests over real
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
