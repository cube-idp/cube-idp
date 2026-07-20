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

The same restraint applies to how progress itself is rendered. A one-shot installer that
seizes the alt-screen destroys the operator's scrollback — including the record of what was
just delivered — and a rendering layer with a closed set of progress stages forces every
new pack or subcommand to modify shared code. Both undermine the goal of a run whose
output is a durable, reviewable artifact.

## Decision

Credentials are never printed implicitly during normal operation; they are surfaced on
demand.

`cube-idp get secrets` resolves credentials by pivoting Pack → `expose.authSecretRef` →
Secret, merging the pack's `impliedFields` underneath the Secret's own keys (the Secret
wins on conflict). Output is filterable by pack via `-p` and printed as an aligned
PACK / NAMESPACE / NAME / KEY=VALUE table. The legacy `cube-idp.dev/cli-secret=true` label
lookup is deprecated: it is honored for exactly one more release behind an explicit
deprecation notice, and only for packs not already resolved via `authSecretRef`.

A successful `up` ends with a styled access summary listing every delivered pack's URLs
plus a `cube-idp get secrets` hint — not the credentials themselves — and exits 0; plain
mode keeps its single final line plus the one deliberately-added `Access` block. The `Pack`
CRD declares `additionalPrinterColumns` so `kubectl get packs` renders
NAME / VERSION / URL / AUTH-SECRET / READY, making the credential's location visible
without revealing its value.

One-shot commands run the Bubble Tea program inline and never set alt-screen: completed
steps are emitted to native scrollback via `tea.Println`/`p.Println`, and only in-flight
state lives in the managed bottom region. Stage is an open string rather than an enum, so
packs and future commands add stages without modifying the event package; no code may
assume the event set is frozen.

## Consequences

* Good, because a routine `up` transcript is safe to share: it names what was installed and
  where to get credentials, but contains none.
* Good, because `expose.authSecretRef` makes the credential location part of the pack's
  declared contract, discoverable through `kubectl get packs` without any cube-idp tooling.
* Good, because inline rendering leaves the run's history in native scrollback, so the
  access summary survives the program exiting.
* Good, because an open stage vocabulary lets new packs and future commands (`sync --watch`)
  add progress stages without touching shared event code.
* Bad, because operators must run a second command to get what a naive installer would have
  handed them, which is friction on first use.
* Bad, because supporting the deprecated label lookup means carrying two resolution paths
  for a release.
* Bad, because `impliedFields` merging encodes knowledge about a pack's credential shape
  (e.g. Argo CD's implicit `admin` username) that the Secret itself does not carry.
* Bad, because an open stage string cannot be validated — a typo'd stage name is a silent
  rendering oddity rather than a compile error.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `cube-idp get secrets` pivots Pack → `expose.authSecretRef` → Secret and merges `impliedFields` under the Secret's own keys; the legacy `cube-idp.dev/cli-secret=true` label lookup is deprecated behind a notice and honored one more release; output is filterable by pack via `-p` and printed as an aligned PACK/NAMESPACE/NAME/KEY=VALUE table. | `cmd/get.go:69-125` |
| The `Pack` CRD defines `additionalPrinterColumns` so `kubectl get packs` renders NAME / VERSION / URL / AUTH-SECRET / READY. | `internal/pack/manifests/pack-crd.yaml:55-62` |
| A successful `up` ends with a styled access summary listing delivered pack URLs plus a `cube-idp get secrets` hint, and exits 0; plain mode keeps its single final line. | `internal/up/up.go:567-590`; `internal/ui/render/plain.go:45-46` |
| Spoke credentials are minted via the Kubernetes TokenRequest API with `expirationSeconds` 315360000 (10 years; the server may clamp), and every `up` re-issues the token and rewrites the hub Secret so a clamped token self-heals. | `internal/spoke/bootstrap.go:26` |
| One-shot commands run the Bubble Tea program inline and never set alt-screen; completed steps go to native scrollback via `p.Println` and only in-flight state lives in the managed bottom region. | `internal/ui/render/live.go:20-36` |
| Stage is an open string rather than an enum, so packs and future commands add stages without modifying the event package, and no code assumes the event set is frozen. | `internal/ui/event/event.go:18-24` |

### Verification

- [ ] `cmd/get.go:69-118` reads `spec.authSecretRef.namespace`/`name` off each Pack, follows
      it to the Secret, and merges `spec.impliedFields` *underneath* the Secret's keys.
- [ ] `cmd/get.go:122-126` emits a deprecation note naming the legacy `cube-idp.dev/cli-secret`
      and `cube-idp.dev/pack-name` labels and stating that label support ends next release.
- [ ] `internal/pack/manifests/pack-crd.yaml:55-58` lists VERSION, URL, AUTH-SECRET, READY in
      that order; `internal/pack/discovery_test.go:24` reads the column slice back.
- [ ] `internal/up/up.go:567-590` emits `event.Epilogue` with hint
      `"credentials: cube-idp get secrets"` and then `con.Access(...)` — and no credential
      value appears in either.
- [ ] `internal/spoke/bootstrap.go:26` sets `tokenTTL int64 = 315360000`, and `up` calls
      `spoke.Bootstrap` unconditionally per spoke on every run.
- [ ] `grep -r WithAltScreen internal/ cmd/` returns nothing; `internal/ui/render/live_test.go:180`
      asserts `m.View().AltScreen` is false.
- [ ] `internal/ui/event/event.go` declares `Stage` as a plain `string` on Step/StepDone/
      StepFailed/StepLog — no enum type, no constant block, no validation.

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
- `docs/archive/superpowers/specs/2026-07-16-tui-interactive-layer-design.md:48` — inline, never
  alt-screen.
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:361` — Stage as an open string.

The rationale for the ten-year spoke token TTL beyond "the server may clamp, so re-issue
every run" is not recorded in the source material.
