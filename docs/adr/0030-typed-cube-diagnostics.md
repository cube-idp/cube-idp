---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 30. Typed CUBE Diagnostics as the Only Failure Surface, Including Bounded Waits

## Context and Problem Statement

`cube-idp` orchestrates cluster provisioning, pack delivery and engine reconciliation — long-running work with many ways to fail, much of it against remote systems that may never converge. Two failure modes make such a tool unusable in practice: an opaque error string that tells the operator nothing actionable, and a spinner that never stops because a component never became ready.

The tool also has two consumers, not one. A human reads a terminal; CI and other machine consumers read a JSON event stream. Both need to know, unambiguously, that the run failed, why, and what to do about it — and the machine consumer additionally needs to know when the stream has ended.

A further hazard is partial success. Several commands perform a side effect (creating a repo, applying manifests) before a later step fails. An operator who cannot tell what already landed cannot safely retry.

This decision fixes a single failure surface that covers all of these.

## Decision

Every user-facing failure is a typed `CUBE-xxxx` `diag.Error` constructed via `diag.New` or `diag.Wrap`, carrying a one-line cause and a copy-pasteable remediation. No code path may silently fall back or degrade: failures are rendered as a diagnosis rather than a bare error string, and surfaces that are not yet built are typed refusals rather than invented output.

Remediation text is always emitted as unstyled, copy-paste-safe plain text, even inside a styled diagnosis panel. `Diagnosis.Raw` always holds `err.Error()`; `Diagnosis.Err` holds the typed error only when `errors.As` finds one.

A caller that fails after a partial side effect wraps the failure in a single dedicated code stating what already succeeded and that the command is idempotent and safe to re-run.

Every wait has a hard deadline and ends in a rendered diagnosis rather than an infinite spinner. Component health is polled every 5 seconds until all components are ready or the deadline expires, at which point the timeout diagnostic lists each unready component with its message.

`diagnosis` is a first-class JSON event type carrying `type`, `code`, `summary`, `cause` and `remediation` fields, mapping 1:1 to a `CUBE-xxxx` diagnosis. On failure the `Diagnosis` event is always the terminal event, emitted after `RunDone{OK:false}`; on success nothing follows `RunDone{OK:true}`. Machine consumers may therefore treat `Diagnosis` as the terminal record.

## Consequences

* Good, because every failure is greppable, searchable and stable: a `CUBE-xxxx` code is a durable identifier that survives message rewording.
* Good, because remediation is part of the error's type, not an afterthought — the compiler-level shape of `diag.New`/`diag.Wrap` makes it impossible to construct a diagnostic without one.
* Good, because machine consumers get a well-defined terminal record and never have to guess whether more output is coming.
* Good, because bounded waits turn an unbounded hang into a diagnosis that names exactly which components are unready and why.
* Good, because partial-side-effect failures state what already landed and assert idempotency, so retry is always safe.
* Bad, because every new failure path costs a code declaration plus a registry description, enforced by tests — a real tax on adding error handling.
* Bad, because codes are effectively append-only: retiring one risks breaking consumers that match on it.
* Bad, because the "no silent fallback" clause is a discipline, not a mechanically enforced property; it can regress without a test failing.
* Bad, because hard deadlines can fire on slow-but-healthy environments, converting a would-be success into a failure the operator must re-run.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Every user-facing failure is a typed `CUBE-xxxx` `diag.Error` carrying a copy-pasteable remediation, every wait has a hard deadline ending in a rendered diagnosis rather than an infinite spinner, and no code path silently falls back. | `internal/diag/codes.go:5-19` |
| Every declared diagnostic code must also have a `Desc` entry with a non-empty summary in `internal/diag/registry.go`, enforced by `TestRegistryCoversEveryDeclaredCode`. | `internal/diag/registry_test.go:41` |
| Component health is polled every 5 seconds until all components are ready or the deadline expires, at which point `CUBE-3004` lists each unready component with its message. | `internal/up/up.go:893` |
| `deployRepo` wraps every deploy-registration failure occurring after the repo was created in a single `CUBE-7303` wrapper stating the repo was created but the deploy source could not be registered, with a remediation noting repo creation is idempotent. | `cmd/repo.go:152` |
| `diag.Error{Code, Summary, Cause, Remediation}` and its `Render` emit the three-line diagnosis block `✗ CUBE-xxxx summary / cause: / fix:`. | `internal/diag/diag.go:14-53` |
| `diagnosis` is a first-class JSON event type carrying type, code, summary, cause and remediation, mapping 1:1 to `CUBE-xxxx` diagnoses. | `internal/ui/event/event.go:123-148` |
| On failure `Diagnosis` is always the terminal event, emitted after `RunDone{OK:false}`; on success nothing follows `RunDone{OK:true}`. | `internal/ui/pipeline.go:43` |
| `Diagnosis.Raw` is always set to `err.Error()`, while `Diagnosis.Err` holds the typed error only when `errors.As` finds one. | `internal/ui/pipeline.go:191-197` |
| Remediation text (`fix:` lines) is always rendered in copy-paste-safe unstyled plain text, even inside the styled diagnosis panel. | `internal/ui/rendererr.go:76-79` |

### Verification

- [ ] `internal/diag/codes.go` declares typed `CUBE-xxxx` `Code` constants only (109 `CUBE-` literals as of this record).
- [ ] `internal/diag/diag.go` exposes exactly `New(code, summary, remediation)` and `Wrap(cause, code, summary, remediation)` — no constructor omits remediation.
- [ ] `go test ./internal/diag/` passes `TestRegistryCoversEveryDeclaredCode` (`registry_test.go:41`) and `TestEveryCodeHasDescription` (`registry_test.go:11`).
- [ ] `internal/up/up.go:47` sets `healthPoll = 5 * time.Second` and `up.go:57` sets `healthTimeout = 5 * time.Minute`.
- [ ] `internal/up/up.go:893` returns `diag.New(diag.CodeEngineHealthTimeout, …unreadySummary(health)…)` on deadline expiry (`CodeEngineHealthTimeout` = `CUBE-3004`, `codes.go:79`).
- [ ] `cmd/repo.go:152` defines a single `wrap` closure producing `diag.CodeRepoDeployFail` (`CUBE-7303`, `codes.go:166`) and every failure arm in `deployRepo` routes through it.
- [ ] `internal/ui/pipeline.go:191` sets `Raw: err.Error()` unconditionally and `d.Err` only inside the `errors.As` branch.
- [ ] `internal/ui/rendererr.go:78` styles only the `fix:` label, interpolating `de.Remediation` raw.
- [ ] `go test ./internal/ui/` passes `TestRunPipelineLiveDiagnosisAfterExit` (`rendererr_test.go:62`), the diagnosis-last structural fence.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the 2026-07-20 documentation audit; the underlying statements were validated against the code before this record was written.

Member origins:

- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:20` — typed diagnostics and bounded waits as a release requirement.
- `docs/archive/superpowers/specs/2026-07-15-cube-idp-phase4-first-release-design.md:35` — the `diag.Error` shape and three-line render block.
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:491` — `Diagnosis.Raw` / `Diagnosis.Err` split.
- `docs/archive/superpowers/specs/2026-07-16-tui-interactive-layer-design.md:138` — diagnosis-last event ordering.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:793` — 5-second health poll and the `CUBE-3004` timeout listing.

Rationale for merging bounded waits into this record: a bounded wait is a specific instance of the rule that every failure terminates in a rendered typed diagnosis rather than a bare error or a hang.
