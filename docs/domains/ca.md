# Domain: ca

Living contract of the certificate-authority domain (`internal/ca`).
Cross-cutting rules: `docs/ARCHITECTURE.md`. Originating design gate:
`docs/DECISIONS.md` 2026-08-27 (M11, epic #177); shipped by the M11
stack — minting and marker identity in M11-C1 (PR #192), the config
surface in M11-C2 (PR #193), the trust artifacts and `trust list` in
M11-C7a (PR #196), the store drivers and `trust install|remove` in
M11-C7b (PR #197), and the bootstrap-edge CA handoff in M11-C6
(PR #198). The **sanctioned descope** recorded below was not taken:
the verbs shipped.

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
certificate files, executing OS trust tooling — stays at the CLI edge.

The trust-store drivers are the case where that could easily have
slipped, and deliberately did not. They live in this domain, but they
reach the operating system only through an injected **`Runner`**
function value — `os/exec` itself lives at the CLI edge. The domain
imports `os/exec` for exactly one symbol, the `ErrNotFound` sentinel a
driver matches to tell "tool not installed" apart from "tool failed",
and executes nothing. That is what lets every store operation run in
the hermetic gate against a fake, with no OS trust store touched. The
edge also keeps the *choice* of store: which one this machine has is
decided there from `GOOS`, and the domain is handed a driver, never
asked to detect one.

It is also the one component domain that imports **no `api/config`**:
the provider arrives as a plain string, the Secret names arrive as
injected strings (the gateway domain's exported platform facts), and
the domain's own `ProviderCube` constant is what the edge compares
against. Its imports are `internal/cubeerr`, stdlib crypto,
apimachinery `unstructured`, and `sigs.k8s.io/yaml`.

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

The surface lives entirely in `api/config/v1alpha1` — the domain does
not read it (above). `Default()` fills `provider` only when the user
wrote `spec.ca:` at all, so the CLI edge derives `ProviderCube` for
the absent case, exactly as it derives the gateway's default domain
(`docs/ARCHITECTURE.md` §3).

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
  status; the flavor is defined in `docs/domains/bootstrap.md`) that
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

## Mint and ensure, as shipped

Two entry points, and the split between them is the reuse contract in
code. `Mint` returns **both** materials — a fresh CA and a leaf signed
by it — while `MintLeaf` signs a fresh leaf off CA material it is
given, which is the reuse path and the only one that ever runs against
an existing CA. `Ensure` chooses between them on exactly one
condition: absent existing CA material mints both; present material is
validated and then only the leaf is minted.

The pins are compile-time constants: a 10-year CA, a 2-year leaf, a
128-bit serial, and an hour of backdated `NotBefore` to absorb clock
skew. The CA is ECDSA P-256 signed `ECDSAWithSHA256`, with
`CertSign|CRLSign` usage and — load-bearing — `MaxPathLen: 0` with
`MaxPathLenZero` set, so it can issue end-entity certificates and
nothing else. The leaf covers **`*.<domain>` only**: there is
deliberately no apex SAN, because the apex does not resolve in-cluster.

Reuse validates before it trusts, and the criteria are fixed
(everything below is `CUBE-CA-002`): both PEM blocks must decode and
parse, the key must be ECDSA and must match the certificate's public
key, the certificate must be a CA, and a non-zero key usage must
include `CertSign`. Two things are deliberately **not** checked here —
expiry and marker presence. Expiry belongs to rotation, which is
frozen; the marker is an *identification* device for trust stores, and
refusing to reuse an unmarked-but-valid CA would strand a cube for a
reason that has nothing to do with whether the material works.

`Ensure` reports whether it minted, and that flag is **reporting
only** — the contract states outright that nothing may be conditioned
on it. The `ca.crt` artifact syncs on every bootstrap regardless,
which is what keeps a reused CA from stranding a file the user
deleted.

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

Three shipped details make that read safe. A `NotFound` is **absence,
not failure** — and on a first bootstrap the gateway namespace does
not exist yet, so that read is `NotFound` too, which is the same
answer. The read runs on the edge's own 30-second budget rather than a
slice of `--timeout`, so a nearly-spent readiness budget cannot fail
it for lack of time rather than for a fault. And the whole handoff is
**gated on the resolved list**: when `ca-secrets` is absent, nothing
is read, minted, or synced, and the zero result reaches no unit.

## Marker identity

Every minted CA carries a **marker CN/OU** — CN
`cube-idp <cube-name> CA`, OU `cube-idp.dev` — so a cube-idp-minted CA
is identifiable wherever it is encountered. `trust remove` verifies the
marker before touching any store; the marker exists so identification
never needs orphan-scan machinery (operator directive: none is built).

As shipped, the CN is composed by `CommonName(cubeName)` and the OU is
the `MarkerOrganizationalUnit` constant, and the check that decides
whether a certificate carries the marker requires **both**: the exact
CN and an OU list containing the marker. Requiring both is what keeps
"has the marker" from ever meaning "is safe to delete" on its own —
every cube-idp CA on a machine shares the OU, so identity is the
fingerprint and the marker is the family name.

Fingerprints are SHA-256 over the certificate's DER bytes, rendered as
lowercase colon-free hex. That helper's error is deliberately
**uncoded**: it is the caller who knows whether an unparseable
certificate came from the ledger, an artifact file, or a store
read-back, and therefore which code should carry it.

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
  deleted file). Bootstrap success output names the path, on a line
  reading `cube CA written to <path>`; it carries **no `curl --cacert`
  example**, the gate's sketch of that line notwithstanding.
- `~/.cube-idp/trust.yaml` — the ledger, one file for all cubes (it
  spans cubes and stores by nature).

The path layout and the modes shipped as: the `.cube-idp` directory
and its per-cube subdirectories at `0o700`, the ledger at `0o600`, and
`ca.crt` at `0o644` — the public half, written inside an otherwise
private tree. **The private key never leaves the in-cluster Secret**,
which is why no path above has a key counterpart.

The sync helper itself is a plain idempotent write: it reads what is
there, does nothing when the bytes already match, and creates or
overwrites otherwise, reporting whether it changed anything. It runs on
every bootstrap whose resolved list carries `ca-secrets`, and — the
point worth not losing — is **never conditioned on whether the CA was
minted**, because a reused CA must still restore a file the user
deleted. Whether the file changed is not what the success line reports.

The ledger's entry shape is fixed at four fields — cube, fingerprint,
store, date (`YYYY-MM-DD`, UTC, from an injected clock) — and parsing
is strict: an entry missing any field, or two entries naming the same
cube, is `CUBE-CA-003`. An empty or whitespace-only file is an **empty
ledger, not an error**, so a first `trust list` on a fresh machine
prints the plain no-certificates line rather than failing. Writes marshal entries sorted by
cube name, so the file is byte-deterministic, and the in-memory
operations (upsert, remove, find) are pure — they clone and return
rather than mutate, one entry per cube.

Verb sizing, fixed at this gate:

- `install <cube>`: fingerprint the emitted CA, add it to the **user-
  level** trust store, append the ledger entry. v0 stores are user
  scope only, never system scope, so **sudo is never invoked**: the
  login keychain on macOS (via the OS's own `security` tool), the
  p11-kit user anchor store on Linux (via `trust anchor`). As shipped,
  the certificate comes from the **local artifact only** — never a
  fallback read of the cluster Secret, because a second source of
  truth for the CA is precisely what the reuse contract exists to
  prevent. A missing artifact and an unparseable one are distinct
  coded failures.
- `list`: print the ledger — exit 0 on an absent or empty (valid)
  ledger, an aligned table when
  entries exist and a plain "no CA certificates installed by cube-idp"
  when the ledger is empty; an unreadable or malformed ledger is
  `CUBE-CA-003`, per the strict-parse rule above.
- `remove <cube>`: locate the store certificate by the **ledger
  fingerprint** and verify **both** the fingerprint and the full
  marker (CN **and** OU)
  before touching the store — the marker alone never authorizes a
  removal (two cubes' CAs share the marker shape; the fingerprint is
  the identity). Before any of that, the ledger's recorded `store`
  must match the store this machine actually has, or the verb refuses
  rather than operating on the wrong one. It is otherwise
  **idempotent**: an unknown cube and a certificate already gone from
  the store both print a message and exit 0, and only a
  present-but-wrong certificate is a hard failure.

**Store detection is the edge's**, a plain switch on `GOOS`: macOS
takes the keychain driver, Linux the p11-kit driver, and **any other
OS is a coded error naming the emitted certificate path** as the
manual remedy — never a silent no-op, because a trust verb that
quietly does nothing is worse than one that says it cannot help.

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

**Linux, precisely.** Mainstream distributions configure no
user-writable p11-kit anchor store, so a non-root `trust anchor`
ordinarily fails outright — and the anchors `curl`, OpenSSL and Go
binaries actually read (`/etc/ssl/certs`, `/etc/pki`) are regenerated
by root-only tools a user-scope p11-kit token would not feed anyway.
Emit-only artifacts plus printed per-distro instructions are therefore
the **ordinary** Linux experience, not the exception, and `CUBE-CA-004`
carries those instructions as its remediation. This narrows which
branch of the design above is common; it does not change the design.

**Marker verification is asymmetric between the stores, deliberately.**
macOS verifies the certificate it pulls *out of the keychain*, so the
check proves the store holds this cube's CA. p11-kit's removal takes a
**file** and offers no way to read an anchor back, so Linux verifies
the local `~/.cube-idp/<cube-name>/ca.crt` instead: that proves the
artifact is this cube's CA, not that the store held that exact
certificate. A removal with no local artifact to verify against is
refused (`CUBE-CA-004`) rather than run unverified, and `remove`
likewise refuses when the ledger records a store other than the running
machine's — the `store` field exists to make that decidable.

**Stale-ledger detection is therefore macOS-only, and the docs should
not imply parity.** The macOS driver searches the keychain by common
name and can report the certificate simply absent, which is the
ledger-vs-store mismatch case the `remove` UX above describes — and,
after a real delete, it re-runs that search rather than trusting the
exit status, because `security`(1) exits 0 whether it deleted the
certificate or merely printed that it could not find one. p11-kit
offers no read-back at all and `trust`(1) likewise exits 0 whether or
not the anchor was there, so the Linux driver **never reports the
not-found outcome**: on Linux, removing an already-absent anchor is
indistinguishable from removing a present one, and the "dropped a
stale entry and said so" message is in practice something only macOS
operators will ever see. This is a limitation of the stores, not a gap
in the design — but it is a real asymmetry in what an operator
experiences, so it is recorded rather than smoothed over.

**The sanctioned descope was not taken.** The operator pre-approved
falling back to **emit-only CA + ledger** — the `ca.crt` sync, the
ledger file, printed per-OS instructions — with the verbs deferring to
the #142 gate, had they ballooned during implementation. They did not:
`trust list`, `install`, and `remove` all shipped in M11, across
M11-C7a and M11-C7b. The escape hatch is recorded because it shaped
the sequencing (artifacts first, verbs second, so a descope would have
cut cleanly), not because anything is outstanding.

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
| `CUBE-CA-004` | trust-store operation failed: a missing or unusable OS tool, a non-zero tool exit, a missing operator artifact, an OS with no driver at all, or a fingerprint/marker mismatch refusing a removal |
| `CUBE-CA-005` | no implementation for the requested `spec.ca.provider` — the CLI edge's provider switch raises it (extension, M11) |

Document-layer `spec.ca` errors are `CUBE-CFG-*` (exit 2) — M11 added
no new code there, the validation being `field.ErrorList` machinery
aggregated under the existing `CUBE-CFG-003`; codes are
never re-tagged across domains.

**Most constructors are unexported, and the exceptions are principled
rather than convenient.** Four kinds of failure are raised by the CLI
edge on this domain's behalf, because the domain by contract never
touches files and never chooses the store: the provider switch
(`CUBE-CA-005`), the ledger read (`CUBE-CA-003`), and the trust verbs'
preconditions — a missing or unusable artifact, an unsupported store,
a store mismatch (all `CUBE-CA-004`). Those constructors are therefore
exported. The rule they preserve is the one that matters: the code
still belongs to the domain that owns the concept, even when the edge
is the only place that can detect the condition.

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
