---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 34. Plugin Trust Consent Flow and External Provenance Verification

## Context and Problem Statement

A cube-idp plugin is an executable the CLI runs on the operator's machine, so
installing one is a trust decision, not a download. Two pressures act on that
decision.

First, the install surface grew. The original path fetched a plugin from a
sha256-pinned git index; later an official OCI index
(`oci://ghcr.io/cube-idp/plugins/index:latest`) was added. Each new install path
is an opportunity to quietly weaken the consent gate — by auto-trusting what the
registry served, by prompting where prompting is unsafe, or by keying the trust
store on something an attacker or a stray `cd` can influence.

Second, packs and plugins are published with GitHub-native artifact attestations
(keyless OIDC). That raises the question of whether the binary should verify
those attestations itself at pull time, pulling a signature-verification stack
(sigstore/rekor/cosign) into the CLI.

This record fixes both answers so that future install paths inherit them instead
of relitigating them.

## Decision

Plugin integrity rests on **digest pinning taken from the index** plus an
**explicit sha256 trust-store consent flow**. The consent flow, the CUBE-7104
non-TTY refusal, and `plugin trust` semantics are invariant: adding
official-index resolution or any new install path must not move that doctrine.
The prompt-gating mechanics of that refusal (when `ui.PromptsAllowed` permits a
prompt at all) are an instance of the prompt doctrine — see ADR-0027 for the
authoritative statement of the interactive prompt gate.

`plugin install` resolves the official index by digest and then hands off to the
unchanged consent flow; `--yes` records trust as explicit flag consent. The
pre-existing sha256-pinned git-index path (`--index`) keeps working unchanged,
including its deliberate exception: it auto-trusts the installed binary on the
strength of its sha256-pinned manifest rather than routing through the consent
prompt.

Plugin trust-store entries are keyed by the absolute, symlink-resolved canonical
path (`filepath.Abs` then `filepath.EvalSymlinks`) on both record and lookup,
falling back to the raw path when canonicalization fails, so trust decisions are
never cwd-dependent.

Pack provenance verification is a **documented external command**
(`gh attestation verify oci://ghcr.io/cube-idp/packs/<name>:<ver> --owner cube-idp`).
The binary never verifies attestations at pull time and relies on digest pinning
over TLS instead.

## Consequences

* Good, because the official-index path funnels through a single consent seam,
  so a new index-backed install path cannot accidentally introduce a silent
  trust. The `--index` git path auto-trusts on the strength of its
  sha256-pinned manifest (`internal/plugin/index.go:106`) — a deliberate
  pre-existing exception, not a seam a new path should copy.
* Good, because canonical-path keying makes trust decisions stable across
  symlinked, relative, and differently-rooted invocations — a trusted binary
  stays trusted and a swapped one is still caught by its sha256.
* Good, because refusing to prompt in a non-TTY (CUBE-7104) means CI can never
  be tricked into "answering yes" by default.
* Good, because keeping signature verification out of the binary avoids
  embedding a sigstore/rekor client and its transitive supply chain.
* Bad, because provenance verification is opt-in and manual: an operator who
  never runs `gh attestation verify` gets digest+TLS integrity only, not
  publisher identity.
* Bad, because the CUBE-7104 refusal makes fully unattended official-index
  installs require an explicit `--yes` for any binary whose sha256 is not
  already in the trust store, which is one more thing to get right in
  automation.
* Bad, because canonicalization can fail (broken symlink, permissions) and the
  fallback then keys on a less canonical path, so two spellings of the same
  binary could theoretically hold separate entries.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Plugin trust-store entries are keyed by the absolute, symlink-resolved canonical path (`filepath.Abs` + `EvalSymlinks`) on both record and lookup, falling back to the raw path when canonicalization fails, so trust decisions are not cwd-dependent. | `internal/plugin/trust.go:96-106` |
| Plugin integrity rests on digest pinning from the index plus the existing sha256 trust-store consent flow; adding official-index resolution leaves the consent flow, the CUBE-7104 non-TTY refusal, `plugin trust` semantics, and the git-index install path unchanged. | `internal/plugin/officialindex.go:185-231` |
| Pack provenance verification is a documented external `gh attestation verify oci://...` command; the binary never verifies attestations at pull time and relies on digest pinning over TLS. | `docs/pack-contract-v1.md:207-211`, `docs/pack-contract-v1.md:232-242` |
| The `--index` git path auto-trusts the installed binary rather than routing through the consent prompt — a deliberate pre-existing exception. | `internal/plugin/index.go:106` |
| `plugin install` resolves the plugin index by digest and then hands off to the existing, unchanged sha256 trust-consent flow. | `cmd/plugin.go:180-231` |

### Verification

- [ ] `internal/plugin/trust.go:96-106` defines `canonicalPath` as `filepath.Abs`
      then `filepath.EvalSymlinks`, returning the raw path if `Abs` fails and the
      absolute path if `EvalSymlinks` fails.
- [ ] `canonicalPath` is applied on record (`internal/plugin/trust.go:119`, in
      `Trust`), on lookup (`:130`, in `isTrusted`), and in `EnsureTrusted`
      (`:152`).
- [ ] `TestTrustKeyCanonicalization` (`internal/plugin/plugin_test.go:111`)
      asserts symlinked/relative and resolved-absolute lookups agree.
- [ ] `internal/plugin/officialindex.go:199-213` rebuilds the pull ref as
      `repo@digest` (`byDigest` at `:211`, pulled at `:213`) and returns an error
      (`:206-209`) when a platform entry carries no digest.
- [ ] `internal/plugin/officialindex.go:222-231` hands off to `EnsureTrusted`,
      with `autoTrust` routing to `Trust` for `--yes`; the same comment records
      that the git index auto-trusts a sha-proven archive instead.
- [ ] `internal/plugin/index.go:106` ends the `--index` git-index install with an
      unconditional `Trust(name, installedPath)` — no prompt, no CUBE-7104.
- [ ] `internal/diag/codes.go:151` still defines `CodePluginUntrusted` as
      `CUBE-7104`, and `internal/plugin/trust.go:142` documents the refusal as
      byte-for-byte frozen.
- [ ] `cmd/plugin.go:228` still exposes `--index` for the sha256-pinned git-index
      path, and `cmd/plugin.go:200-210` dispatches to `plugin.Install` for it.
- [ ] `docs/pack-contract-v1.md:207-211` documents verification as exactly
      `gh attestation verify oci://ghcr.io/cube-idp/packs/<name>:<ver> --owner cube-idp`,
      and `docs/pack-contract-v1.md:241-242` (in the `Verifying pack provenance`
      section, `:232-242`) states cube-idp does not re-verify attestations at
      pull time.
- [ ] `grep -rEi 'cosign|sigstore|rekor' internal/ cmd/ --include='*.go'` returns
      no matches; the only `attestation` hit under `internal/` and `cmd/` Go
      sources is a comment at `cmd/pack_publish.go:35`; `go.mod` declares no
      sigstore/cosign dependency.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`)
during the 2026-07-20 documentation audit; the underlying statements were
validated against the code before this record was written.

Member origins:

- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:944` — canonical-path trust keys.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:3784` — trust doctrine invariant across official-index resolution.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:193` — external provenance verification.
- `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:56` — `plugin install` digest resolution into the consent flow.

Related: `README.md:521-524` documents the git-index install path that this
decision preserves.
