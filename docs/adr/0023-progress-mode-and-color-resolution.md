---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 23. Output Mode Resolution, Color Governance, and the Machine-Readable Contract

## Context and Problem Statement

`cube-idp` runs in three very different places: a developer's interactive terminal,
a CI job with no TTY, and a script that wants to parse what happened. A single
rendering path cannot serve all three. Left unmanaged, this multiplies into a
thicket of overlapping flags (`--quiet`, `--verbose`, `--no-tui`, `--json`,
`--no-color`), each with its own precedence rules, and into output that silently
changes shape depending on the terminal — which breaks golden-file tests and
downstream parsers alike.

Two concerns are frequently conflated and must not be. *Mode* is what the output
looks like structurally: a live-redrawing TUI, a byte-stable line-per-step log, or
a machine event stream. *Color* is only whether ANSI escapes are emitted. Treating
a color signal such as `NO_COLOR` as a mode input means a user who merely wants an
uncolored terminal loses layout and glyphs entirely.

Machine-readable output has its own split. A long-running command like `up` produces
progress over time and wants a stream; a request/response command like `status`
produces one answer and wants one document. Forcing both into the same shape makes
one of them awkward for its consumers.

## Decision

The CLI exposes exactly one progress knob: `--progress=auto|plain|live|json`, a
persistent root string flag defaulting to `auto`, backed by the `CUBE_IDP_PROGRESS`
environment variable, with `--plain` retained permanently as an alias for
`--progress=plain`. Mode resolution happens exactly once, in `PersistentPreRunE`
via `ui.Resolve`; an unrecognized value produces a preflight diagnostic error.
Resolution runs first and validation second, deliberately, so a bad-flag
diagnostic renders in the environment-appropriate mode.

Styled output is the default on a real TTY: the live step-tree renderer on
commands that run through `ui.RunPipeline`, styled-static elsewhere. Non-TTY and
CI runs are auto-detected and receive byte-stable plain output, with a structured
JSON event stream available on demand.

Color is governed separately from mode. `NO_COLOR` is honored only when non-empty
and strips color at the writer without altering layout, glyphs, or the resolved
mode. `CLICOLOR_FORCE=1` re-enables color, and `--color=auto|always|never` overrides
all environment variables.

Machine-readable output is split by command shape: commands that route through
`ui.RunPipeline` (`up`, `down`, `vendor`, `sync`, `repo create`, `plugin`,
`pack push`) emit a streaming event stream under `--progress=json`, while
request/response commands (`status`, `doctor`, `get secrets`) emit a single final
JSON document under `--output json`; `docs/machine-readable-output.md` carries the
authoritative command list. The event stream is written to stdout as JSON
Lines with exactly one event object per line, each carrying a `"v":1` version field
and an RFC3339Nano `"ts"` field, never batched and never pretty-printed, documented
in `docs/machine-readable-output.md`.

That schema is labeled v1-EXPERIMENTAL until the v1 config freeze. Within that
window changes must be additive, new fields are emitted with `omitempty`, and each
requires a changelog entry before the freeze.

## Consequences

* Good, because there is one place to look when output shape is surprising: a single
  ladder in `ui.Resolve`, evaluated once, with a fixed precedence.
* Good, because plain output is byte-stable, so CI logs and golden-file tests do not
  drift with terminal capabilities.
* Good, because separating color from mode means an uncolored terminal keeps the full
  styled layout instead of degrading to the phase-1 log format.
* Good, because `omitempty` on fields added to existing record types keeps
  pre-existing JSONL lines byte-identical, so additive schema growth does not
  break existing consumers.
* Bad, because the resolution ladder has eight conditions plus a default
  (nine documented rungs) plus a separate color policy
  ladder; the interaction of `--color`, `NO_COLOR`, and `CLICOLOR_FORCE` is only
  discoverable from tests and comments.
* Bad, because two machine-readable shapes (line stream and document) means two
  schemas to version, document, and freeze rather than one.
* Bad, because `--plain` is a permanent alias, so the flag surface can never be
  reduced to the single knob it conceptually is.
* Bad, because the JSON contract is explicitly experimental, so consumers built
  before the v1 config freeze carry migration risk.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The progress surface is a single BuildKit-style knob `--progress=auto\|plain\|live\|json` governed by a `CUBE_IDP_PROGRESS` env policy, with `--plain` kept permanently as an alias for `--progress=plain`. | `cmd/root.go:70-73` |
| `--progress` is a persistent root string flag defaulting to `auto`, backed by `CUBE_IDP_PROGRESS` (accepting plain/live/json; auto/empty/unrecognized fall through the ladder), validated in `PersistentPreRunE` where an unknown value produces a diag preflight error. | `cmd/root.go:72-73` |
| Styled output is the default on a TTY — the live step-tree renderer on `RunPipeline` commands, styled-static elsewhere — while CI, non-TTY and `--plain` runs get byte-stable plain output plus a JSON event-stream option. | `internal/ui/ui.go:137`; `internal/ui/pipeline.go:78-89,97-110` |
| Non-TTY stdout and a non-empty `$CI` auto-detect down to `ModePlain`; otherwise the resolved default is `ModeStyled`. | `internal/ui/ui.go:131-137` |
| Mode resolution is a single ladder evaluated exactly once via `ui.Resolve`, highest matching rung winning. | `internal/ui/ui.go:111-137` |
| `NO_COLOR` is honored only when non-empty and strips color without affecting layout or glyphs; `CLICOLOR_FORCE` (non-empty) re-enables color. | `internal/ui/ui.go:145-150,181-215` |
| `--color=auto\|always\|never` takes precedence over `NO_COLOR` and `CLICOLOR_FORCE` in the color ladder; the flag value itself is only validated in `cmd/output.go`. | override precedence: `internal/ui/ui.go:181-201`; validation: `cmd/output.go:34-42` |
| Machine-readable output is a streaming event stream on `RunPipeline` commands while request/response commands (`status`, `doctor`, `get secrets`) emit a single final JSON document under `--output json`. | stream: `internal/ui/pipeline.go:78-89`; document: `cmd/output.go:76-94` |
| The JSON event stream is emitted on stdout as JSON Lines, exactly one event object per line, each carrying `"v":1` and an RFC3339Nano `"ts"`, never batched and never pretty-printed. | `internal/ui/render/json.go:34-42` |
| The JSON event schema is labeled v1-EXPERIMENTAL until the v1 config freeze. | `internal/ui/render/json.go:12-15` |
| JSONL changes are additive only — `msg`/`dur_ms` on `step_failed`, `omitempty` `idx`/`of` on steps, and a new epilogue record — legal under the experimental window and requiring a changelog note before the freeze. | `internal/ui/render/json.go:99-113,131-141` (emitted at `json.go:59`) |

### Verification

- [ ] `cmd/root.go:70-73` registers `--plain` as a persistent bool documented as a permanent alias for `--progress=plain`, and `--progress` as a persistent string defaulting to `"auto"`.
- [ ] `cmd/output.go:19-27` (`validateProgressFlag`) returns `diag.CodeBadFlagValue` for any value outside `""|auto|plain|live|json`, and `cmd/root.go:64` calls it inside `PersistentPreRunE`.
- [ ] `internal/ui/ui.go:111-137` (`Resolve`) is the only mode ladder and never reads `r.NoColor`.
- [ ] `internal/ui/ui.go:145-150` (`EnvColorPolicy`) treats `NO_COLOR` as set only when present **and** non-empty, and `CLICOLOR_FORCE` as set when non-empty.
- [ ] `internal/ui/ui.go:181-215` (`colorOff`/`colorForce`/`ColorEnabled`) applies the `--color` flag ahead of `NO_COLOR`, and `NO_COLOR` ahead of `CLICOLOR_FORCE`.
- [ ] `cmd/output.go:34-42` (`validateColorFlag`) accepts only `""|auto|always|never`.
- [ ] `internal/ui/ui_test.go` `TestColorEnabledLadder` pins `--color=never` beating `CLICOLOR_FORCE`, `--color=always` beating `NO_COLOR`, and `NO_COLOR` beating `CLICOLOR_FORCE`.
- [ ] `internal/ui/render/json.go:34-42` emits one `json.Marshal`'d object per event followed by `'\n'`, with every envelope embedding `jsonHead{V:1, TS: RFC3339Nano, Type}` — no `MarshalIndent`, no batching.
- [ ] `internal/ui/render/json.go:103-105,111-112` tags `dur_ms`, `idx`, `of` and `step_failed`'s `msg`/`dur_ms` with `omitempty`.
- [ ] `cmd/output.go:44-56` pins `docSchemaVersion = 1` and registers `-o/--output` whose only recognized value is `json` on request/response commands.
- [ ] `internal/ui/pipeline.go:78-89` routes `ModeJSON` to `render.JSON`, `ModeLive`/TTY-`ModeStyled` to the live renderer, and everything else to `render.Plain`.
- [ ] `docs/machine-readable-output.md:3-9` labels both schemas EXPERIMENTAL until the v1 `cube.yaml` freeze.

## History

The mode ladder originally treated color as a mode input: `NO_COLOR` present at all
(even empty), or `TERM` being `dumb`/unset, forced `ModePlain` rather than merely
uncolored styled output. Under that rule a user who set `NO_COLOR` on a TTY lost the
styled layout entirely.

The color policy superseded both halves of that rule. Color is now stripped at the
writer; `NO_COLOR` counts only when non-empty (an empty value is unset, per
no-color.org); and the resolved `Mode` is left alone, so styled layout survives an
uncolored terminal. `internal/ui/ui.go:105-107` records the removed rung in place —
only `TERM` dumb/unset remains a mode rung — and suppression moved to the color
policy at `internal/ui/ui.go:141-150`.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during
the 2026-07-20 documentation audit; the underlying statements were validated against
the code before this record was written.

Member origins:

- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md:40` — the single `--progress` knob
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:527` — the resolution ladder
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:756` — the stream/document split
- `docs/archive/superpowers/specs/2026-07-15-cube-idp-phase4-first-release-design.md:69` — the JSON Lines contract
- `docs/archive/superpowers/specs/2026-07-16-tui-interactive-layer-design.md:367-371` — the color policy

See also `docs/machine-readable-output.md` for the schemas themselves.
