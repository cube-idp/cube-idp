# Domain: gateway

Living contract of the gateway domain (`internal/gateway`).
Cross-cutting rules: `docs/ARCHITECTURE.md`. Originating design gate:
`docs/DECISIONS.md` 2026-08-27 (M11, epic #177), amended 2026-08-28
(M11-A0: the prerequisite list becomes spec data; the thin-helm pack
is fully static and cube-derived wiring is a domain-emitted `Gateway`
object) — **gated ahead of
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
the `ca` domain (`docs/domains/ca.md`), gated in the same M11 decision.
Three flows stay distinct: this domain **exports the CA and leaf
Secret names** (platform facts the edge injects into `ca`), `ca` mints
the material into those Secrets, and the gateway implementation reads
the leaf at serving time through the emitted `Gateway`'s
`certificateRefs`.

The domain is **pure** in the substrate's mold: it owns embedded
prerequisite content and the prerequisite-list surface
(`spec.prerequisites`), and emits objects, blocks, and predicates
derived from `spec.gateway`. All I/O — applying anything, reading or writing the
live CoreDNS ConfigMap — stays at the CLI/orchestrator edge, on
bootstrap's machinery or the edge's own client.

## The prerequisite model (the ordered list)

Prerequisites are an **ordered list of prerequisite units** that
bootstrap installs **ahead of the engine** — after the substrate's
kind-set readiness, before the engine's sync wiring and bundle. The
list is **spec-level data**: `spec.prerequisites`, this domain's
second config surface (below), **defaulted to exactly the four units
described here, in this order** (operator decision, 2026-08-28 —
`docs/DECISIONS.md` M11-A0). Nothing about what the list is *for*
changes with that; what changes is that the four are the **compiled
default list** — a starting point a user may replace — and no longer
the only list that can run.
The mechanics are day-0 bootstrap SSA, with the thin-helm member's
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

**The embedded copies are default resolution, not the model.** Making
the list config demotes the cube-shipped content, in two shapes. The
two cube-shipped **packs** — the CRDs pack bytes, the traefik pack —
demote to *the default resolution of a well-known pack name*: what a
compiled default entry resolves to when the user writes nothing, and
an override entry needs no cube-shipped pack at all — it names a unit
and points a ref at content this repo has never seen. The **built-in
units'** content — the platform objects, the CA handoff — is
name-selected domain behavior, never resolvable content; its demotion
is from *always-present* to *present-when-listed* (the compiled
defaults list both). The embedded assets stay exactly as
specified below — pinned, sha256-checked, never fetched at runtime —
they simply stop being the only reachable content.

The compiled default list, in order:

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
   corrected on the record. **The binary-size impact was measured and
   is trivial** (2026-08-28): +1.17 MB raw on a 65.2 MB binary, under
   2% — **raw embedding is retained**. Gzip-compressing the asset was
   **considered and rejected**: it would complicate the pack-contract
   dogfood test's deep equality over parsed objects (the bytes would
   have to be inflated before either side could be compared) for no
   meaningful size win. Bootstrap SSA-applies it and waits the
   kind-set (CRD `Established`).
3. **The CA material** (`ca-secrets`) — an **inert unit**, not a
   pack: the embedded
   prerequisite packs carry no cluster-specific secret material, so
   the `ca` domain's minted-or-reused CA and wildcard leaf cross the
   edge as plain Secret objects for the gateway namespace, SSA-applied
   and inventory-recorded like any bootstrap-owned object. This unit
   **explicitly depends on unit 1** (its Secrets land in
   `gateway-system`; SSA into a nonexistent namespace is NotFound),
   which the list order guarantees. Its readiness rule is the inert
   flavor defined in the bootstrap amendment: Secrets carry no status,
   so **apply success is the unit's readiness** — a later reader must
   not "fix" the absent wait. Ordered before the
   `traefik-gateway` unit because the `Gateway` object applied there
   references the leaf Secret by name.
4. **`traefik-gateway`** — the gateway itself, a **thin-helm**
   prerequisite pack in the M9 delegation shape (this is deliberate
   M9 dogfooding): an embedded `type: helm` pack (`category:
   "gateway"`) whose rendered output is the `HelmRelease` + OCI source
   CR pair, reconciled in-cluster by the **substrate's own
   helm-controller**. The chart reference is **digest-pinned** (the
   chart is OCI-published — verified at the gate:
   `ghcr.io/traefik/helm/traefik`). Bootstrap SSA-applies the pair and
   runs the reconciliation wait with this domain's predicates.

   **The pack's values are entirely static, by contract** (the R1
   resolution, 2026-08-28): every `#Values` default is compile-time
   constant — the Gateway API provider enable, the chart's own
   Gateway/listener creation **disabled**, hostPorts 80/443, the
   `ingress-ready` nodeSelector, `fullnameOverride`. **No cube-derived
   chart value exists to bake**, so the pack sits fully inside the pack
   contract's instance boundary — "a pack that cannot be handed to a
   different operator, on a different cluster, unchanged, is carrying
   instance state" (`docs/domains/pack.md`) — and nothing about *where*
   it installs is in the artifact. (Disabling the chart's own Gateway
   only shrinks the render measured under D2 below, which enabled it;
   the footprint comparison holds a fortiori.)

   **Everything cube-derived crosses as a cube-authored Gateway API
   `Gateway` object**, domain-emitted — pure emission, this domain's
   native job, the same class as the platform Service and the CoreDNS
   block. It carries the wildcard listener for `*.<domain>` and the TLS
   `certificateRefs` entry naming the leaf Secret, and the edge applies
   it **within this unit, beside the CR pair** (unit 2 precedes, so the
   `Gateway` kind is Established). Being **domain-authored**, it
   carries its own `metadata.namespace`, exactly as the platform
   Namespace and Service do; only *pack-rendered* output is
   deliberately namespace-less and stamped at the edge (below). The
   canonical shape, binding as contract:

   ```yaml
   apiVersion: gateway.networking.k8s.io/v1
   kind: Gateway
   metadata:
     name: gateway
     namespace: gateway-system
   spec:
     gatewayClassName: traefik
     listeners:
       - name: websecure
         protocol: HTTPS
         port: 443
         hostname: "*.<spec.gateway.domain>"
         tls:
           mode: Terminate
           certificateRefs:
             - kind: Secret
               name: <the exported leaf-Secret fact>
   ```

   Six properties are load-bearing:
   - **One HTTPS listener in M11**, not a 443/80 pair. No cube-owned
     application endpoints exist yet (D5b), so a plaintext listener
     would serve nothing; adding one is a content change inside this
     object when M12 route wiring needs it. That wiring's route
     `parentRefs` attach to this object **by name**, which is why the
     name (`gateway`) is a fixed platform fact rather than derived —
     the one platform identity, shared with the stable Service
     (different kinds, no collision): DNS resolves the Service,
     routes attach to the `Gateway`, both under the same name.
   - **The Secret and the `Gateway` share `gateway-system`**, so the
     `certificateRefs` entry is same-namespace and **no
     `ReferenceGrant` is required** — a cross-namespace certificate
     reference would need one, and the unit order already puts the
     Secrets there (unit 3).
   - **Route attachment (`allowedRoutes`) is deliberately unstated**,
     and absent from the shape above rather than frozen to a value: no
     routes exist until M12, which owns route wiring (D10) and is the
     gate that should choose the policy.
   - **Readiness is deliberately not gated in M11**: the unit's
     reconciliation wait covers the `HelmRelease` + source pair. A
     `Programmed`-condition predicate for the `Gateway` is a **named
     breakdown option**, not gate surface.
   - **`gatewayClassName` references the chart-created class** — a
     compiled default in the stable Service backend pointer's posture
     ("Two pointers stay compiled", below); like the chart digest it
     is gate-time evidence, re-pinned at the breakdown.
   - **The cube's specifics are expressed in the routing contract D2
     chose** (Gateway API), not in one implementation's chart knobs —
     which is precisely the shape an implementation swap wants.

   Naming and namespaces, fixed here because the stable Service's
   `spec.externalName` references the release identity (and for chart
   tidiness — the CoreDNS block itself targets only the stable name,
   D6):
   - **Release identity is the M9 mechanism, not a new knob**:
     `releaseName` is the effective instance id, which for this
     embedded singleton is the pack name, `traefik-gateway`
     (`spec.packs` is uninvolved and stays so — the override path for
     a prerequisite pack is a `spec.prerequisites` entry, and an
     override pack's own name is then its effective id). The pack's
     baked values also pin
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
same as `"engine"`. This ordered list is **not**
`RenderPlan.Prerequisites`, and being spec data does not make it so:
`spec.prerequisites` is bootstrap-phase configuration — which units
install ahead of the engine — while `RenderPlan.Prerequisites` stays
per-pack `lifecycle: pre` data, frozen to the M12 bus; the list feeds
it as prior art only. The two share a word, never a mechanism.

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

## Config surface (`spec.prerequisites`)

The second surface this domain owns: an optional **top-level** typed
sub-struct on `ConfigSpec` — a list — with its defaults and validation
beside it in `api/config/v1alpha1`, like every other component surface
(M11-A0, 2026-08-28).

**Why top-level, owned here** (author's call at the follow-up gate):
the list spans gateway packs **and** CA material, and it runs before
the engine — so neither neighbouring home works. Under `spec.engine`
it would re-couple prerequisites to the tier-2 engine vocabulary the
M11 reviews explicitly fenced off ("prerequisites are not engine
content"); under `spec.gateway` it would misplace the CA unit and
every future non-gateway member. The gateway domain owns the
prerequisite *model*, so its contract carries the surface — the same
**owner-is-not-executor** split already shipped for `spec.engine`
(the engine domain owns that surface; bootstrap executes it).

```go
// PrerequisiteSpec is one ordered entry of the bootstrap prerequisite list.
type PrerequisiteSpec struct {
    // Name identifies the unit. The compiled defaults' names are
    // well-known: "gateway-platform", "gateway-api-crds", "ca-secrets",
    // "traefik-gateway".
    Name string `json:"name"`
    // Ref locates a pack for a pack-shaped unit, in the internal/ref
    // grammar (local tree/file and https today; oci/git at M12).
    // Empty on a well-known cube-shipped pack name selects the
    // embedded copy; forbidden on built-in units; required otherwise.
    Ref string `json:"ref,omitempty"`
}
```

**Two entry classes**, distinguished by name, not by a discriminator
field:

- **Built-in units** — `gateway-platform` and `ca-secrets`: cube-owned
  content and behavior (domain-emitted objects, the CA handoff). There
  is nothing to point a ref at, so `ref` is **forbidden**.
- **Pack units** — `gateway-api-crds` and `traefik-gateway` with an
  empty `ref` resolve to the **embedded cube-shipped copy**; **any
  other name requires a `ref`**, resolved through `internal/ref` at the
  edge. Default-to-embedded resolution happens **by name at the edge,
  before `internal/ref` is consulted** — no new ref scheme and no
  grammar extension; no `embedded://`-style scheme is introduced.

**Merge semantics: replace-whole-list.** Absent or empty
`spec.prerequisites` means the compiled defaults (the four, in order).
A present, non-empty list **replaces them entirely** — what is written
is exactly what runs, in list order. Rationale (author's call):
per-entry override would need merge keys, positional insertion rules,
and delete markers — three new concepts with no v0 consumer. One rule
beats three, and the compiled defaults are documented above, so
copy-and-edit is trivial. The consequence is stated plainly and is
not a gap: **order and inter-unit dependency are the list author's**
— the defaults' one hard dependency (`ca-secrets` needs
`gateway-platform` before it, and both CRD consumers need their CRDs)
is satisfied by the default order, and a list that reorders or drops
a unit owns what follows. No dependency graph is introduced here;
that is the same merge machinery rejected above. The consequence
extends to the built-ins: a custom list that omits `gateway-platform`
or `ca-secrets` genuinely omits them — a cube without the gateway
fabric is **constructible by explicit choice**, and that is intended
configurability, not a hole (absent `spec.prerequisites` still means
the full defaults; D8's absent-means-installed posture is about
absence, not about an explicit list). The emitted `Gateway` object
follows the same rule, because it is **name-selected domain behavior**
like the built-in units' content: a `traefik-gateway` entry pointing at
someone else's pack still gets the object, and a list that renames the
unit gets no listener — which the list author owns, exactly as with a
dropped built-in.

**Two pointers stay compiled: the stable Service's backend and the
emitted `Gateway`'s class.** Two
different acts share the word "swap", and only the first is
configurable today: swapping list **content** (which units install,
from where) is what `spec.prerequisites` exists for; swapping the
**implementation the stable Service fronts** is not a list edit. The
`gateway-platform` built-in emits the ExternalName with its backend
reference at the **compiled default implementation's** effective id
(`traefik-gateway.gateway-system.svc.cluster.local`). The list
becoming config does not make that pointer config: an override list
can replace or extend prerequisite *content*, but **swapping the
gateway implementation** additionally requires re-pointing the stable
Service's backend **and** the emitted `Gateway`'s
`gatewayClassName` — two one-field changes in two cube-owned objects,
deliberately not yet configurable: no v0 consumer exists, the config
analogue of the second-implementation doctrine. Both activate at the
gate that brings a real alternative implementation. The stability
contract is
unaffected either way: consumers target the stable name, which is
exactly what makes the backend re-pointing a one-object change when
that gate comes.

**Validation** — document-layer, `CUBE-CFG-*` document errors (exit 2),
no I/O ever:

- `name` is required and **unique across the list**;
- `ref` is **forbidden** on a built-in name;
- `ref` is **required** on a name that is not well-known;
- `ref`, when present, must be a **well-formed reference token**
  (non-empty, no whitespace or control characters) — the same bound
  `spec.packs.packRef` already lives under.

The well-known names are string constants in `api/config/v1alpha1`
(the `spec.engine.provider: flux` precedent) — `api/` states the
vocabulary, never imports `internal/gateway`. **The boundary that
split implies, stated for C2:** `Default()` in `api/config/v1alpha1`
**materializes** the compiled default entries — they are data, and
defaulting is where data lands; `internal/gateway` owns the model's
**meaning** — which names are built-in, what each unit installs, what
the list is for. That is documentation ownership, not code location,
and the import direction is unaffected: `api/` never imports
`internal/gateway`. And the document layer
stops at *presence and shape*; past it, error ownership follows the
existing catalogs: ref **resolution** failures are `internal/ref`'s
(`CUBE-REF-*`); an override pack's **load/validate/render** failures
are the pack contract's (`CUBE-PKG-*`); `CUBE-GWY-001/002` cover only
the **embedded** cube-shipped assets' provenance and parse — the
layering `docs/domains/pack.md` fixed for `packRef`, applied
unchanged, and codes are never re-tagged across domains.

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

**Recorded as a breakdown item, deliberately not gate surface:** the
splice's **failure taxonomy** — the marker-corruption cases, a foreign
cube's block preserved byte-for-byte, multiple `.:53` server blocks,
regex and Caddyfile escaping — is enumerated and reviewed **with its
kubeadm-shaped fixtures in the C4 PR**, where a fixture makes each case
concrete. This gate fixes the safety envelope; the case list is an
implementation-review item under it.

## What the domain owns (pure) vs the edge (I/O)

| `internal/gateway` (pure) | CLI/orchestrator edge (I/O) |
|---|---|
| the **`spec.prerequisites` surface** — entry shape, the compiled default list, the well-known-name vocabulary, the replace-whole-list rule: the *model*, never its assembly | |
| embedded prerequisite packs (`gateway-api-crds`, `traefik-gateway`) + their provenance checks, and the `gateway-platform` unit's Namespace + stable `gateway` Service objects | SSA-applying every prerequisite via bootstrap machinery |
| the rendered `HelmRelease` + OCI source pair (chart values **fully static** — nothing cube-derived) and, beside it, the cube-authored Gateway API `Gateway` carrying the wildcard listener and the leaf-Secret `certificateRefs` | reading the live Corefile, splicing, optimistic-concurrency write-back |
| the CoreDNS marker block for a domain | assembling the ordered prerequisite list **from `spec.prerequisites`** (the compiled defaults when absent or empty) — resolving well-known names to the embedded copies by name, every other entry's pack through `internal/ref` — and injecting it into bootstrap |
| readiness predicates for its declared CRs (`HelmRelease`/OCI source: `Ready` **and** `status.observedGeneration == metadata.generation` — the no-stale-success doctrine in this domain's vocabulary) | the CA-Secret read and the `ca` handoff (see `docs/domains/ca.md`) |
| the platform facts — namespace `gateway-system`, stable Service `gateway`, the `Gateway` object `gateway` (one platform identity, two kinds), and the **CA and leaf Secret names** the edge injects into the `ca` domain (exported invariants; of the Secret names only the leaf has an emitted counterpart to tie it to — the `Gateway`'s `certificateRefs`, checked by the fact-ties test; the mirroring of the substrate fact is of the construct — an exported fact injected as a string — and the names are deliberately implementation-neutral, unlike `flux-system`, so a gateway content swap never moves the namespace or the DNS anchor) | |

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

Document-layer `spec.gateway` and `spec.prerequisites` errors are
`CUBE-CFG-*` (exit 2); an override ref that fails to resolve surfaces
`internal/ref`'s own `CUBE-REF-*`, and an override pack that fails to
load, validate, or render surfaces the pack contract's `CUBE-PKG-*` —
`CUBE-GWY-001/002` cover only the embedded assets; codes are never
re-tagged across domains.

## Testing

Hermetic, no Docker: provenance and parse checks over the embedded
packs; the edge-level pack-contract dogfood test per embedded pack
(load+render ≡ domain emission, deep equality of ordered lists — the
substrate's discipline; for the thin-helm pack this deliberately locks
the domain's hand-built CR pair, including the effective-id-derived
`releaseName`, to every M9 field-level rule — loud by design).

**The equality's scope is exact.** It covers the **pack-derived**
objects — the CR pair — at `#Values` defaults, which is the pack's
whole content now that no value is cube-derived; the `Gateway` object,
like the platform Service, is cube-authored **beside** the pack and is
never part of a pack render. Being fully static is what makes the
equality trivially well-defined: a prerequisite pack has no
`spec.packs` instance, so `pack.Load`+`Render` yields the `#Values`
defaults and nothing else, leaving nothing for the hand-built pair to
disagree about. **The cost, recorded honestly:** those static values
live twice — CUE `#Values` defaults in `pack.cue`, Go literals in the
hand-built pair — and this dogfood equality is the single deliberate
guard on the duplication. It is loud by design, and §8's cuelang
confinement (`cuelang.org/go` is `internal/pack`'s alone) is why the
domain cannot simply parse its own `pack.cue` instead.

Also hermetic, the
**fact-ties-to-content test**: the exported namespace and
service-name facts equal the `metadata.name` of the Namespace and
Service objects the `gateway-platform` unit
emits, the Service's `spec.externalName` equals the effective
id's FQDN, and the exported **leaf-Secret-name** fact equals the
`certificateRefs` entry on the emitted `Gateway` (the substrate's
namespace-tie test, mirrored and
extended). A **`spec.prerequisites`-overridden pack takes the same
conformance path** — loaded and rendered through the pack contract
exactly as an embedded one, and it must satisfy the same pack rules to
install at all; what it cannot have is the deep-equality half, which
compares against *domain emission* and so is inherently about the
embedded packs (an override has no domain-emitted counterpart to equal).
Also table-driven
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
