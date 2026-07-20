---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 29. Doctor Reports One Tri-State Row per Registered Check

## Context and Problem Statement

`cube-idp doctor` is the preflight and health command: it probes the host, the
cluster provider and the running engine, and tells the operator whether the
environment is fit to run a cube. A diagnostic command that only prints problems
leaves the operator unable to distinguish "checked and fine" from "never checked" —
a silent pass and a skipped probe look identical, both in the terminal render and
in `-o json`. That ambiguity is worse in automation than in a terminal: a CI job
that reads the JSON document cannot tell whether a missing check means the host is
healthy or that the check was inapplicable on this platform.

Doctor also needs a stable, additive machine contract. It already emitted a
`findings` array and an `errors` verdict; consumers of those fields must not break
when the checklist is introduced. And the render must stay legible without color —
a colored glyph alone carries no meaning on a plain (piped, non-TTY) writer.

## Decision

`doctor` renders exactly one row per registered check as a tri-state result —
green passed, yellow warning, red error — with the status word always present and
the themed glyph prepended only on styled output, so a plain writer never loses
meaning, and with passing checks shown rather than silent. The command exits 1
whenever any error-severity finding is present; because every red row is backed by
an error finding, any error row forces exit 1 — but error findings not attached to
a check (config load, provider diagnose, engine health) also drive the exit code.
`-o json` carries an additive `checks` array alongside the existing `findings` and
`errors` fields.

Checks that cannot be probed for a given cube or host are not registered at all.
A checklist row therefore always means "this was probed now", and JSON consumers
must treat an absent check row as *not applicable* rather than *passed*. The
exception is a check that genuinely runs but has nothing to act on — `git-cli`
stays registered and reports an honest vacuous detail rather than disappearing.

## Consequences

* Good, because the operator sees the full probe surface, not just its failures —
  a green row is positive evidence that a check ran.
* Good, because the word (`ok` / `warn` / `fail`) is emitted unconditionally and
  the glyph only decorates it on styled output, so the render stays meaningful on
  plain writers and for readers who cannot rely on color.
* Good, because the `checks` array is purely additive: pre-existing consumers of
  `findings` and `errors` keep working unchanged.
* Good, because the exit code has one testable driver — an error-severity finding —
  so warnings never fail a pipeline.
* Bad, because conditional registration puts the burden on JSON consumers: they
  must handle three states (`ok`, `warn`, absent) rather than two, and an absent
  row is easy to misread as success.
* Bad, because the checklist makes `doctor` output longer and more verbose on
  healthy systems than a failures-only report would be.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `doctor` renders exactly one row per registered check as a tri-state passed / warning / error, with the status word unconditional and the glyph prepended only on styled output. | `cmd/doctor.go:158-179` |
| The command exits 1 whenever any error-severity finding is present, in both the JSON and the rendered path. | `cmd/doctor.go:64-74` |
| `-o json` carries an additive `checks` array beside the pre-existing `findings` and `errors` fields. | `cmd/doctor.go:214-219`, `cmd/doctor.go:254-259` |
| Doctor JSON consumers must treat an absent check row as "not applicable" rather than "passed", because checks that cannot be probed for a given cube or host are not registered at all (documented at `cmd/doctor.go:91-99`). | `cmd/doctor.go:138-149`, `internal/doctor/doctor.go:437-480` |

### Verification

- [ ] `cmd/doctor.go:158-179` (`renderDoctorChecklist`) prints one line per
      `doctor.CheckResult`, including results whose status is `ok`.
- [ ] `cmd/doctor.go:172-175` emits the status word unconditionally and prepends
      the glyph only when the printer is styled.
- [ ] `cmd/doctor.go:181-192` (`doctorRowGlyph`) maps `ok` → `ui.GlyphOK`,
      `fail` → `ui.GlyphErr`, and everything else → `ui.GlyphWarn`.
- [ ] `internal/doctor/doctor.go:382-391` (`CheckResult.Status`) returns exactly
      one of `ok`, `warn`, `fail`, returning `fail` for any error-severity finding;
      any non-error finding yields `warn`, so the three-state guarantee depends on
      no check emitting `SeverityInfo`.
- [ ] `cmd/doctor.go:214-219` declares `doctorDoc.Checks` as a `checks` JSON array
      that sits beside the pre-existing `findings` and `errors` fields.
- [ ] `cmd/doctor.go:242-265` (`writeDoctorJSON`) appends one `doctorCheck` per
      result and returns whether any finding is an error.
- [ ] `cmd/doctor.go:64-74` returns `errExitCode(1)` only when the error verdict is
      true, in both the JSON and the rendered path.
- [ ] `cmd/doctor.go:91-99` documents and `cmd/doctor.go:138-149` implements conditional registration of
      the spoke-reachability row — registered only when spokes are declared and the
      hub answered.
- [ ] `internal/doctor/doctor.go:436-493` appends the `container-runtime`,
      `http-port`, `disk-space` and `inotify` checks only under their provider,
      config or platform conditions, so those rows are absent when not probed.
- [ ] `internal/doctor/doctor.go:481-492` registers `git-cli` unconditionally and
      reports the vacuous detail "no git-sourced pack refs — git not needed" when
      there is nothing to check.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`)
during the 2026-07-20 documentation audit; the underlying statements were
validated against the code before this record was written.

Member origins:

- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:254` — tri-state
  checklist row, exit contract and additive `checks` array.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2229` — absent row
  means "not applicable", not "passed".
- `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:55`
  — passes shown rather than silent; glyph/word pairing.

Rationale beyond what is captured above was not recorded in the source material.
