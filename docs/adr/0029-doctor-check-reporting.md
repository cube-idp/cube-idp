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
green passed, yellow warning, red error — with the glyph and the word always
paired, and with passing checks shown rather than silent. The command exits 1 if
and only if at least one error row exists. `-o json` carries an additive `checks`
array alongside the existing `findings` and `errors` fields.

Checks that cannot be probed for a given cube or host are not registered at all.
A checklist row therefore always means "this was probed now", and JSON consumers
must treat an absent check row as *not applicable* rather than *passed*.

## Consequences

* Good, because the operator sees the full probe surface, not just its failures —
  a green row is positive evidence that a check ran.
* Good, because the word (`ok` / `warn` / `fail`) always accompanies the glyph, so
  the render stays meaningful on plain writers and for readers who cannot rely on
  color.
* Good, because the `checks` array is purely additive: pre-existing consumers of
  `findings` and `errors` keep working unchanged.
* Good, because the exit code has a single, testable driver — an error row — so
  warnings never fail a pipeline.
* Bad, because conditional registration puts the burden on JSON consumers: they
  must handle three states (`ok`, `warn`, absent) rather than two, and an absent
  row is easy to misread as success.
* Bad, because the checklist makes `doctor` output longer and more verbose on
  healthy systems than a failures-only report would be.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `doctor` renders exactly one row per registered check as a tri-state passed / warning / error with glyph and word always paired, exits 1 if and only if any error row exists, and adds an additive `checks` array to `-o json`. | `cmd/doctor.go:158-176` |
| `doctor` renders every check as a tri-state row (green ✔ / yellow ⚠ / red ✗) with passes shown rather than silent, exits 1 if and only if any check is red, and emits an additive `checks` JSON array. | `cmd/doctor.go:153-197` |
| Doctor JSON consumers must treat an absent check row as "not applicable" rather than "passed", because checks that cannot be probed for a given cube or host are not registered at all. | `cmd/doctor.go:96-100` |

### Verification

- [ ] `cmd/doctor.go:153-176` (`renderDoctorChecklist`) prints one line per
      `doctor.CheckResult`, including results whose status is `ok`.
- [ ] `cmd/doctor.go:172-175` emits the status word unconditionally and prepends
      the glyph only when the printer is styled.
- [ ] `cmd/doctor.go:181-192` (`doctorRowGlyph`) maps `ok` → `ui.GlyphOK`,
      `fail` → `ui.GlyphErr`, and everything else → `ui.GlyphWarn`.
- [ ] `internal/doctor/doctor.go:381-391` (`CheckResult.Status`) returns exactly
      one of `ok`, `warn`, `fail`, returning `fail` for any error-severity finding.
- [ ] `cmd/doctor.go:214-238` declares `doctorDoc.Checks` as a `checks` JSON array
      that sits beside the pre-existing `findings` and `errors` fields.
- [ ] `cmd/doctor.go:242-265` (`writeDoctorJSON`) appends one `doctorCheck` per
      result and returns whether any finding is an error.
- [ ] `cmd/doctor.go:64-71` returns `errExitCode(1)` only when the error verdict is
      true, in both the JSON and the rendered path.
- [ ] `cmd/doctor.go:91-100` documents and implements conditional registration of
      the spoke-reachability row — registered only when spokes are declared and the
      hub answered.
- [ ] `internal/doctor/doctor.go:436-493` appends the `container-runtime`,
      `http-port`, `disk-space` and `inotify` checks only under their provider,
      config or platform conditions, so those rows are absent when not probed.

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
