---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 36. Credentials Are Surfaced On Demand, Never Printed Implicitly

## Context and Problem Statement

Installing a platform produces credentials: an Argo CD admin password, a Grafana login, a
spoke-cluster service-account token. The obvious thing for an installer to do is print them
when it finishes — and that is exactly what makes the output of a routine `up` unsafe to
paste into a ticket, a CI log, or a screen share. Secrets that appear without being asked
for are secrets that leak.

At the same time, an operator who has just installed a platform genuinely does need those
credentials, and needs them without hunting through namespaces for the right Secret. Two
things therefore have to be true at once: the terminal must not spill credentials during
normal operation, and there must be a single, discoverable command that yields them when
the operator asks. That command needs a stable, machine-readable place to look — a pack
must be able to declare *where its credential lives* rather than relying on an out-of-band
labelling convention.

This decision assumes, but does not itself decide, that a run's output survives in native
scrollback: an installer that seizes the alt-screen would destroy the record of what was
just delivered, including the access summary. That rendering commitment is made in
ADR-0026 (inline, never alt-screen) and ADR-0025 (event pipeline); this ADR only depends
on it.

## Decision

Credentials are never printed implicitly during normal operation; they are surfaced on
demand.

`cube-idp get secrets` resolves credentials by pivoting Pack → `expose.authSecretRef` →
Secret, merging the pack's `impliedFields` underneath the Secret's own keys (the Secret
wins on conflict). Output is filterable by pack via `-p` and printed as an aligned
PACK / NAMESPACE / NAME / DATA table whose DATA cell is comma-joined `key=value` pairs.
The legacy `cube-idp.dev/cli-secret=true` label
lookup is deprecated: it is honored for exactly one more release behind an explicit
deprecation notice, and only for packs not already resolved via `authSecretRef`.

A successful `up` ends with a styled access summary listing every delivered pack's URLs
plus a `cube-idp get secrets` hint — not the credentials themselves; plain
mode keeps its single final line plus the one deliberately-added `Access` block. The `Pack`
CRD declares `additionalPrinterColumns` that include an AUTH-SECRET column, making the
credential's location visible in `kubectl get packs` without revealing its value.

See ADR-0026 for the authoritative statement of the inline, never-alt-screen rendering
model, and ADR-0025 for the authoritative statement of Stage being an open string rather
than an enum. This ADR relies on both but does not restate them as its own commitments.

## Consequences

* Good, because a routine `up` transcript is safe to share: it names what was installed and
  where to get credentials, but contains none.
* Good, because `expose.authSecretRef` makes the credential location part of the pack's
  declared contract, discoverable through `kubectl get packs` without any cube-idp tooling.
* Good, because the inline rendering model owned by ADR-0026 leaves the run's history in
  native scrollback, so the access summary survives the program exiting.
* Bad, because operators must run a second command to get what a naive installer would have
  handed them, which is friction on first use.
* Bad, because supporting the deprecated label lookup means carrying two resolution paths
  for a release.
* Bad, because `impliedFields` merging encodes knowledge about a pack's credential shape
  (e.g. Argo CD's implicit `admin` username) that the Secret itself does not carry.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `cube-idp get secrets` pivots Pack → `expose.authSecretRef` → Secret and merges `impliedFields` under the Secret's own keys. | `cmd/get.go:76-119` |
| The legacy `cube-idp.dev/cli-secret=true` label lookup is deprecated behind a notice and honored one more release, only for packs not already resolved via `authSecretRef`. | `cmd/get.go:124-154` |
| Output is filterable by pack via `-p` and printed as an aligned PACK/NAMESPACE/NAME/DATA table whose DATA cell is comma-joined `key=value` pairs. | `cmd/get.go:215`; `cmd/get.go:253-273` |
| The `Pack` CRD defines `additionalPrinterColumns` including an AUTH-SECRET column, so `kubectl get packs` shows where each pack's credential lives. The full column set is VERSION / URL / AUTH-SECRET / READY / CUSTOMIZED / DELIVERY / DEPENDS-ON (NAME is implicit). | `internal/pack/manifests/pack-crd.yaml:55-62` |
| A successful `up` ends with a styled access summary listing delivered pack URLs plus a `cube-idp get secrets` hint; plain mode keeps its single final line plus the `Access` block. | `internal/up/up.go:570-590`; `internal/ui/render/plain.go:42-50` |

### Verification

- [ ] `cmd/get.go:76-119` (`packSecretRows`) reads `spec.authSecretRef.namespace`/`name` off
      each Pack, follows it to the Secret, and merges `spec.impliedFields` *underneath* the
      Secret's keys.
- [ ] `cmd/get.go:124-127` (`legacyDeprecationNote`) builds a note naming the legacy
      `cube-idp.dev/cli-secret` and `cube-idp.dev/pack-name` labels and stating that label
      support ends next release; it is appended to `notes` at `cmd/get.go:149` and printed
      via `p.Warn` at `cmd/get.go:207-210`.
- [ ] `cmd/get.go:255` prints the header `PACK\tNAMESPACE\tNAME\tDATA`, and `:262-268` joins
      each row's sorted `key=value` pairs with commas into the DATA cell.
- [ ] `internal/pack/manifests/pack-crd.yaml:55-62` declares seven printer columns —
      VERSION, URL, AUTH-SECRET, READY, CUSTOMIZED, DELIVERY, DEPENDS-ON — in that order.
- [ ] `internal/pack/discovery_test.go:23-45` reads the column slice back but only asserts
      `len(cols) >= 5` plus explicit CUSTOMIZED and DELIVERY presence checks. **AUTH-SECRET
      is not pinned by any test** — the CRD manifest is the only guarantee.
- [ ] `internal/up/up.go:570-576` emits `event.Epilogue` with hint
      `"credentials: cube-idp get secrets"`, and `:590` calls `con.Access(...)` with the same
      hint — no credential value appears in either.
- [ ] `internal/ui/render/plain.go:42` prints the single final line and `:45-50` the `Access`
      block.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member provenance:

- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md:51` — the `get secrets` pivot
  and label deprecation.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:4393` — the Pack CRD printer
  columns.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:5743` — the `up` access summary.

The inline/never-alt-screen rendering model and the open-`Stage`-string rule were previously
restated here; they are owned by ADR-0026 and ADR-0025 respectively, along with their
provenance. The spoke TokenRequest TTL, likewise previously restated here, belongs with the
spoke-registration ADR — this record makes no decision about it.
