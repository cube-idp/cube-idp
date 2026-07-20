---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 38. Local CA, TLS at the Gateway, and the OS Trust Store Consent Boundary

## Context and Problem Statement

A cube-idp cluster serves every workload through one HTTPS gateway on a wildcard
hostname (`*.<gateway.host>`). To serve HTTPS at all, something has to issue a
certificate for that wildcard, and something has to decide whether browsers on the
developer's machine accept it. Both halves are dangerous in different ways.

The certificate half is a lifecycle problem: the gateway's TLS material must exist
*before* the cluster is created, because the cluster provider mounts the trust root
into the node at create time — a CA generated after `up` has already built the
cluster is too late. It must also be stable across runs; a CA regenerated on every
`up` invalidates whatever the operator trusted yesterday.

The trust half is a consent problem. Writing to the operating system trust store is
a machine-wide, privilege-escalating, hard-to-undo act. A tool that does it as a
silent side effect of "start my dev environment" has taken a decision that was never
the operator's to skip — and it makes the whole feature untestable, because CI and
`go test` must never mutate the runner's trust store. So the end-to-end proof that
the gateway's TLS actually works has to be constructible without any OS trust at all.

Many developers already have an mkcert root installed and trusted. Reusing it gives
green locks with zero prompts, but there is no portable API to *ask* the OS whether a
given root is trusted, so any reuse rule must be decidable from what is on disk.

## Decision

`up` generates a local certificate authority once per machine using the mkcert
mechanism as a library (`smallstep/truststore`) rather than the mkcert binary, stores
it under the user's per-OS application directory, and issues a wildcard certificate
for `*.<gateway.host>` before the cluster is created.

mkcert CA adoption is presence-based only: an existing mkcert root at `$CAROOT` or the
per-OS default is detected and reused by copying `rootCA.pem` and `rootCA-key.pem`,
without verifying that it is already trusted by the OS. Once a cube-idp CA exists on
disk, it always wins — no re-adoption, no regeneration.

The gateway must serve a certificate that chains to this CA and covers the wildcard
host, verified end-to-end without ever touching the OS trust store in CI.

cube-idp never modifies the operating system trust store implicitly. Trust-store
changes happen only inside the explicitly-consented `cube-idp trust` command, which
installs an untrusted adopted root exactly like a generated CA, and are fully reverted
by `cube-idp down`. `internal/trust` stays a leaf package with zero implicit side
effects.

## Consequences

* Good, because starting a cube is never a privileged operation: `up` can run
  unattended, in CI, and in `go test` without mutating the host's trust configuration.
* Good, because certificate correctness is falsifiable independently of OS trust — the
  e2e test builds its own pool from the CA file and dials the gateway.
* Good, because operators with an existing mkcert root get browser-trusted leaves with
  no prompt at all.
* Good, because the CA is idempotent per machine, so trust granted once survives every
  subsequent `up`, and `down` reverts exactly what `trust` installed.
* Bad, because presence-based adoption can adopt an mkcert root the OS does not
  actually trust, producing certificates that browsers still reject until the operator
  runs `cube-idp trust`; there is no portable way to detect this in advance.
* Bad, because the "once a cube-idp CA exists it wins" rule means an operator who later
  installs mkcert will not have it adopted — the existing CA directory must be removed
  by hand to change course.
* Bad, because HTTPS without warnings is a two-step experience (`up`, then `trust`)
  rather than one command.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| cube-idp never modifies the OS trust store implicitly; trust-store changes happen only inside the explicitly-consented `cube-idp trust` command and are fully reverted by `cube-idp down`, while `up` may generate a local CA but never touches the OS store. | `cmd/trust.go:17-40`; `internal/up/up.go:123-131` |
| `up` generates the local CA once per machine via the mkcert mechanism used as a library, stores it under the user's per-OS application directory, and issues a wildcard cert for `*.<gateway.host>` before cluster creation; an existing mkcert root at `$CAROOT` or the per-OS default is adopted by copying `rootCA.pem`/`rootCA-key.pem`, but an existing cube-idp CA always wins. | `internal/trust/ca.go:26-93`, `internal/trust/ca.go:99-140` |
| mkcert CA adoption is presence-based only: an existing mkcert root is reused without verifying OS-store trust, and an untrusted adopted root is installed by `cube-idp trust` exactly like a generated CA. | `internal/trust/ca.go:117-126` |
| The gateway must serve a TLS certificate chaining to the cube-idp local CA and covering the wildcard host, verified in e2e without ever touching the OS trust store in CI. | `internal/trust/ca.go:1-5`; `cmd/trust.go:24-80`; `tests/e2e/e2e_test.go:370-400` |

### Verification

- [ ] `internal/trust/ca.go:1-5` — the package doc states nothing in `internal/trust`
      touches the OS trust store implicitly.
- [ ] `internal/up/up.go:123-131` — `up` calls only `trust.Dir` + `trust.EnsureCA`; no
      call to `trust.InstallOS` exists anywhere under `internal/up`.
- [ ] `cmd/trust.go:17-19` — `trustInstall`/`trustUninstall` are seams bound to
      `trust.InstallOS`/`trust.UninstallOS`; `cmd/trust.go:41-71` gates install behind a
      consent prompt with a `--yes` escape, and `cmd/promptfence_test.go` fences a
      non-TTY run so it aborts rather than installing.
- [ ] `cmd/down.go:249-257` — `down` reads `trust.LoadState` and calls `trustUninstall`
      only when `st.Installed` is true.
- [ ] `internal/trust/ca.go:47-58` — `EnsureCA` returns the existing `ca.crt` if present,
      otherwise prefers `adoptMkcertCA` over generating a new key.
- [ ] `internal/trust/ca.go:99-118` — `mkcertCAROOT()` resolves `$CAROOT` first, then the
      per-OS mkcert default (darwin / windows / XDG).
- [ ] `internal/trust/ca.go:117-126` — the `adoptMkcertCA` doc states adoption is
      presence-based and that `cube-idp trust` installs an untrusted adopted root exactly
      like a generated CA; the body validates parseability and `IsCA` only, never trust.
- [ ] `internal/trust/store.go:4,16` — OS installation goes through
      `github.com/smallstep/truststore`, not a shelled-out `mkcert` binary.
- [ ] `internal/up/tls.go:28` — the issued cert covers both `gw.Host` and `"*."+gw.Host`.
- [ ] `tests/e2e/e2e_test.go:370-400` — `assertGatewayTLS` builds an `x509.CertPool` from
      the cube-idp CA file and dials the gateway with SNI
      `gitea.cube-idp.localtest.me`; the OS trust store is never consulted.

Known deviation from the statement as originally recorded: `trust.Dir()`
(`internal/trust/ca.go:26-35`) resolves `os.UserConfigDir()/cube-idp` — the per-OS
*config* directory — not the XDG *data* directory. The code is the authority; the
statement above is worded to match it.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during
the 2026-07-20 documentation audit; the underlying statements were validated against
the code before this record was written.

Member origins:

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:35` — implicit
  trust-store modification is forbidden; `trust` is the sole opt-in path.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:2581` — CA generated once
  per machine via the mkcert mechanism as a library, wildcard cert before cluster create.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:3007` — mkcert adoption
  narrowed to presence-based detection.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:5612` — gateway TLS chain
  verified in e2e without the OS trust store.
