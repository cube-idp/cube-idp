# Domain: ca

Living contract of the certificate-authority domain (`internal/ca`).
Cross-cutting rules: `docs/ARCHITECTURE.md`. Originating design gate:
`docs/DECISIONS.md` 2026-08-27 (M11, epic #177) — **gated ahead of
code**: this contract is the operator-approved design; the package
lands via the M11 implementation breakdown, and no code exists before
that breakdown is aligned.

## Purpose

`internal/ca` owns the cube's certificate authority: minting the
per-cube CA and its wildcard leaf, the reuse contract that keeps trust
stable across re-bootstraps, the marker identity every minted CA
carries, and the operator-side trust surface (the ledger and the
`trust` verb group). It is a **component domain**, not part of the
gateway: the gateway *consumes* the leaf; who provides certificates is
its own axis of change — which is why this contract is written
**provider-seam-ready** (below).

The domain is pure in the repo's mold: minting is a pure function of
injected inputs (existing key material or injected entropy), Secret
and file *contents* are returned as bytes/objects, and every I/O —
reading the existing Secret, applying Secrets, writing ledger or
certificate files, invoking OS trust tooling — stays at the CLI edge.

## Provider-seam-READY, not provider-seamed (D3)

The **config surface** is designed now; the Go interface is not — the
second-implementation doctrine governs, and a seam activates at a later
gate when a real second provider lands. v0 has exactly one
implementation: the **cube-owned, stdlib-minted CA** (`crypto/x509` +
`crypto/ecdsa`; zero new Go dependencies). The named future providers,
recorded so the surface never has to be reshaped to admit them, each
behind its own design gate:

- **`user`** — a user-provided CA (bring-your-own key material);
- **`cert-manager`** — delegation to cert-manager, arriving as an
  ordinary pack plus a provider value;
- **`kubernetes`** — the native pod-certificate signing path
  (`PodCertificateRequest`, KEP-4317 — verified GA in Kubernetes
  v1.37).

## Config surface (`spec.ca`)

An optional typed sub-struct on `ConfigSpec`, defaults and validation
beside it in `api/config/v1alpha1`:

```go
// CASpec configures who provides the cube's certificate authority.
type CASpec struct {
    // Provider selects the CA provider. "cube" — the cube-owned,
    // stdlib-minted CA — is the default and the only admitted value
    // in M11; "user", "cert-manager", and "kubernetes"
    // (PodCertificateRequest, GA v1.37) are named future providers,
    // each admitted at its own design gate.
    Provider CAProvider `json:"provider,omitempty"`
}
```

- Absent `spec.ca` defaults to the `cube` provider (fabric, like the
  engine and the gateway — not opt-in).
- **The provider choice is immutable per cube** — the engine precedent
  (`docs/domains/engine.md`, Config surface), and sharper here: a
  provider change on a live cube implies trust rotation, which D10
  freezes. Recorded as contract now; mechanical enforcement lands with
  the second provider's gate, since today there is nothing to switch
  to.
- No opaque `forProvider` yet, for the engine domain's recorded
  reason: no provider-specific knob exists, and an empty opaque payload
  is ceremony; the gate that admits a second provider migrates the
  shape if and as needed.
- Validation rejects an unknown provider (`spec.ca.provider`,
  `CUBE-CFG-*`, exit 2).

## Custody (D4)

The CA private key lives **only in the in-cluster Secret** — never the
repo, never a vault (#142's remit, recorded adjacent), never on disk
beyond the mint call. Concretely:

- CA and wildcard-leaf key pairs are stored as Secrets in the gateway
  namespace, SSA-applied by bootstrap and inventory-recorded like any
  bootstrap-owned object. In the ordered prerequisite list the Secrets
  form an **inert unit** (apply success is readiness — Secrets carry no
  status; the rule is defined in the bootstrap amendment) that
  **explicitly depends on the `gateway-platform` unit** preceding it:
  its Secrets land in `gateway-system`, which must already be `Active`
  (`docs/domains/gateway.md`, the prerequisite model).
- **The Secret names are the gateway domain's exported platform
  facts**, injected at the edge into this domain's mint/ensure — never
  invented here, since domains never import each other
  (`docs/domains/gateway.md`, the platform facts). The gateway's
  emitted `Gateway` object references the leaf Secret under that same
  name, which is what ties the two halves together. The expected
  values are **`cube-idp-ca`** (the CA certificate and private key)
  and **`gateway-tls`** (the wildcard serving certificate) —
  deliberately implementation-neutral, per the gateway contract. Both
  Secrets carry type `kubernetes.io/tls` with `tls.crt`/`tls.key`,
  one shape and one decoder.
- **The leaf is minted on every bootstrap; only the CA is reused.**
  `spec.gateway.domain` is user-editable between bootstraps, so a reused
  leaf could silently carry SANs that no longer cover the current
  domain — minting every time is correct by construction. Trust is
  unaffected (clients trust the CA), the gateway reloads, and the edge's
  read stays what both contracts describe: the existing **CA** Secret,
  singular. A later reader must not "fix" this into leaf reuse without
  adding domain-match, expiry, and chain checks.
- The CA **certificate** (public) is the only exported artifact — it
  is what trust distribution handles.
- `down` destroying the cluster destroys the CA — correct for a local
  cube; durable custody backends are frozen (below).
- Long-lived v0 validity (CA ~10y, leaf ~2y); **no rotation
  machinery**.

**Mint-if-absent requires a read** — the named new edge behavior:
stable trust across re-bootstraps means the edge reads the existing CA
Secret (with the dynamic client it already constructs) *before*
minting, and hands any existing key material to the domain's pure
ensure function; only absence mints. The exported bootstrap machinery
deliberately has no read operation, and does not grow one — this is
edge composition, the edge's first cluster read outside a domain
operation (recorded at the gate; `docs/domains/bootstrap.md`). Without
it, every re-bootstrap would rotate the CA — the exact failure the
requirement exists to prevent.

## Marker identity

Every minted CA carries a **marker CN/OU** — CN
`cube-idp <cube-name> CA`, OU `cube-idp.dev` — so a cube-idp-minted CA
is identifiable wherever it is encountered. `trust remove` verifies the
marker before touching any store; the marker exists so identification
never needs orphan-scan machinery (operator directive: none is built).

## Trust distribution (D5b): verbs + ledger, deliberately minimal

The CLI grows a **`trust` verb group** — `trust install`, `trust
list`, `trust remove` — backed by a **minimal ledger**: one config
file recording, per entry, exactly **cube name + CA fingerprint
(SHA-256) + store + date**. Nothing more (operator directive: do not
over-complicate). The operator-artifact layout:

- `~/.cube-idp/<cube-name>/ca.crt` — the per-cube CA certificate,
  **idempotently synchronized from the effective CA on every
  bootstrap** (missing file recreated, divergent bytes corrected —
  emission is not conditioned on minting, else CA reuse would strand a
  deleted file). Bootstrap success output prints the path and a
  `curl --cacert` example.
- `~/.cube-idp/trust.yaml` — the ledger, one file for all cubes (it
  spans cubes and stores by nature).

Verb sizing, fixed at this gate:

- `install <cube>`: fingerprint the emitted CA, add it to the **user-
  level** trust store, append the ledger entry. v0 stores are user
  scope only, never system scope, so **sudo is never invoked**: the
  login keychain on macOS (via the OS's own `security` tool), the
  p11-kit user anchor store on Linux (via `trust anchor`).
- `list`: print the ledger.
- `remove <cube>`: locate the store certificate by the **ledger
  fingerprint** and verify **both** the fingerprint and the full
  marker (CN **and** OU)
  before touching the store — the marker alone never authorizes a
  removal (two cubes' CAs share the marker shape; the fingerprint is
  the identity). A ledger entry whose certificate is no longer in the
  store is stale: `remove` drops the entry and says so instead of
  failing.

The verbs shell out to the **operating system's own trust tooling** —
recorded as a platform-tool dependency, not a Go module (§8 stays
closed): there is no in-process way to write an OS trust store, which
distinguishes this from the rejected `kubectl kustomize` exec (that
outsourced work the binary can do itself). Capability is **probed, not
assumed**: a missing or unusable tool fails with a coded error naming
it and the per-OS manual instructions (`CUBE-CA-004`); Linux support
is explicitly conditional on p11-kit's user anchor store being
present — where it is not, the emit-only artifacts plus printed
instructions *are* the v0 behavior. Nothing is ever bundled.

**Sanctioned descope** (operator, pre-approved): if the verbs balloon
during implementation, M11 ships **emit-only CA + ledger** (the
`ca.crt` sync, the ledger file, per-OS trust instructions printed on
bootstrap success) and the verbs defer to the #142 gate. The descope
line runs between the verbs and the artifacts — the artifacts ship
either way.

Automatic trust installation during bootstrap stays rejected — silent
trust anchors are unforgivable; the verbs are explicit and auditable.

## Error codes (`CUBE-CA-*`, exit 1)

Declared in the domain's `errors.go`; the initial catalog (numbers
final, meanings binding; extensions follow the normal per-domain rule):

| Code | Meaning |
|---|---|
| `CUBE-CA-001` | CA/leaf minting failed (wrapped stdlib cause) |
| `CUBE-CA-002` | existing CA Secret is unusable (unparseable or incomplete key material — remediation names deleting the Secret to re-mint) |
| `CUBE-CA-003` | ledger file unreadable or malformed |
| `CUBE-CA-004` | trust-store operation failed (missing/unusable OS tool, tool exit failure, or fingerprint/marker mismatch on `remove`) |
| `CUBE-CA-005` | no implementation for the requested `spec.ca.provider` — the CLI edge's provider switch raises it (extension, M11) |

Document-layer `spec.ca` errors are `CUBE-CFG-*` (exit 2); codes are
never re-tagged across domains.

## Relationship to `TLS` / #142

The §5 `TLS` tag stays reserved, **re-scoped**: `internal/trust` is
#142's credential-bindings domain (`secretRef` on source CRs,
constrained `valuesFrom`, source verification, `lock`/`mirror`) — it no
longer covers "certificates / CA", which this domain owns from M11.
#142 adjacencies recorded here: key custody beyond the in-cluster
Secret (vaults, durable stores) and the trust-verb descope landing zone
are #142 gate material.

## Testing

Hermetic, no Docker, no OS trust store in the gate: mint/ensure tables
(absent → mint; present → reuse byte-for-byte; unusable → coded error)
with injected entropy for deterministic assertions where feasible;
marker CN/OU asserted on every minted CA; ledger round-trip and
malformed rows; trust-store operations behind an injected runner seam,
mocked function-field style — the real store is touched only by a
human running the verb. Filesystem through injected `fs.FS` where read,
explicit paths where written.

## Frozen — not designed in M11 (D10)

Rotation automation, ACME/public certificates; custody backends beyond
the in-cluster Secret; the provider seam interface (activates with a
second implementation at its gate); system-scope trust stores;
`secretRef`/source verification/`lock`/`mirror` (#142). The `user`,
`cert-manager`, and `kubernetes` providers are named, not designed.
