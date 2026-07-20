---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 24. Plain Output Is Byte-Frozen and Additive-Only

## Context and Problem Statement

`cube-idp` writes progress to stdout from many commands, and that stdout is consumed by two
very different audiences. Humans on a terminal want a rich, animated view. CI logs, e2e test
assertions, and shell scripts want text they can diff and scrape, and they break the moment a
glyph, a spacer, or a duration suffix moves. Without an explicit rule, every cosmetic
improvement to the CLI silently rewrites the contract that the test suite and downstream
automation depend on.

The CLI therefore needs one output surface that is treated as an interface, not as
presentation: stable across releases, pinned by tests, and clearly separated from the
terminal-only richness layered on top of it.

## Decision

Plain-mode output — what is emitted on a non-TTY writer, under `$CI`, or with
`--plain`/`--progress=plain` — is byte-frozen, unless `--color=always`/`CLICOLOR_FORCE`
explicitly opts a pipe into the animation-free styled-static projection instead. It is
pinned by golden and e2e tests and is the CI/e2e contract, covering the renderers as well
as `Printer`.

Every live view must have an equivalent plain projection, so CI output stays complete rather
than degrading to silence. New output must be additive only and must be routed through
`internal/ui` (Console, Printer, event stream); producers construct events and never render.
Richness appears only under `ModeStyled` or `ModeLive` on a real TTY.

Any deliberate change to plain output must update its affected test in the same commit. The
only sanctioned deviations from the original freeze are the ratified R1 (step start lines)
and R2 (epilogue glyph treated as presentation) changes. R3 (down confirmation refusal) is
ratified alongside them but is a consent gate that refuses to run, not a plain-byte delta.

## Consequences

* Good, because e2e assertions and CI log scraping keep working across releases: piped output
  is byte-identical regardless of terminal capabilities, absent an explicit color-force
  override.
* Good, because "producers never render" keeps exactly one renderer per run, so adding a new
  presentation mode does not require touching orchestrator code.
* Good, because a plain projection exists for every live view, so a CI log is never less
  informative than the interactive view.
* Good, because the same-commit test rule makes every intentional output change visible in
  review as a golden diff.
* Bad, because improving plain output requires a ratification step and a golden update, which
  is friction on what looks like a cosmetic fix.
* Bad, because the freeze is now defined relative to a growing list of sanctioned deltas
  rather than a single recorded byte set, so the true contract lives in the renderer's
  comments plus the goldens.
* Bad, because information the styled renderer can afford to show (durations, context,
  registry) is deliberately withheld from plain, so CI logs are less detailed by design.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Plain-mode output is byte-frozen, golden/e2e-pinned, additive only, routed through `internal/ui`, and any deliberate change updates its test in the same commit. | `internal/ui/ui.go:1-17` |
| The sanctioned deviations from byte-stable plain output are the ratified R1 (StepStarted start line) and R2 (epilogue glyph as presentation) changes. | `internal/ui/render/plain.go:19-23` |
| R3 is a consent gate, not a plain-byte delta: a non-TTY `down` without `--yes` refuses to run rather than changing plain output. | `cmd/down.go:67-70` |
| Richness routes through `ModeStyled`/`ModeLive` on a real TTY (or the color-forced styled-static arm on a pipe), and producers never render. | `internal/ui/pipeline.go:78-89`; `internal/ui/console.go:12-17` |
| Plain-mode CLI output is byte-frozen and golden/e2e-pinned; new output is additive and routed through `internal/ui`. | `internal/ui/render/plain.go:24-59` |
| The CLI surface itself (commands, flags, defaults, Short strings) is separately frozen against a golden. | `cmd/clitree_test.go` `TestCommandTreeGolden` (line 40) |

### Verification

- [ ] `internal/ui/ui.go:1-17` states the phase-1 plain format `"▸ [%s] %s\n"` IS the CI/e2e
      contract and that piped output is byte-identical (the later W2.T13 color policy adds
      the force-color carve-out below).
- [ ] `internal/ui/ui_test.go` `TestPlainMatchesPhase1Format` (line 15) and
      `TestNonTTYWriterForcesPlain` (line 28) pin the format and the non-TTY downgrade.
- [ ] `internal/ui/render/plain_test.go` pins the projection bytes against recorded goldens
      `internal/ui/render/testdata/plain_up_pretask.golden` and `plain_fail_pretask.golden`.
- [ ] `internal/ui/pipeline_test.go:234` `TestModeMatrixFence` is the cross-mode fence.
- [ ] `internal/ui/render/plain.go:53-56` emits zero bytes for `RunStarted`, `StepFailed`,
      `StepLog`, `HealthTick`, `Diagnosis` and `RunDone`.
- [ ] `internal/ui/render/plain.go:19-23` names R1 (StepStarted start line) and R2 (epilogue
      glyph as presentation) as sanctioned deviations; `cmd/down.go:67-70` implements R3, the
      non-TTY confirmation refusal (`diag.CodeConfirmRequired`).
- [ ] `internal/ui/console.go:12-17` states the Console constructs events and never renders —
      exactly one renderer, chosen by `RunPipeline`.
- [ ] `internal/ui/pipeline.go:78-110` selects Styled/Live only on `ModeStyled`/`ModeLive`
      plus `IsTerminal`, defaulting to `render.Plain`; styled richness lives only in
      `internal/ui/render/styled.go` and `internal/ui/render/live.go`.
- [ ] `internal/ui/pipeline.go:112-118` `forceColorUpgrade` is the one carve-out: with
      `--color=always`/`CLICOLOR_FORCE` and no explicit plain/json ask, a pipe renders the
      styled-static arm instead of `render.Plain`
      (`internal/ui/ui_test.go` `TestColorForcePipesStyledStatic`, line 328).
- [ ] JSON additivity is enforced by `omitempty`-shaped records in
      `internal/ui/render/json.go`.
- [ ] All command output routes through `ui.RunPipeline`/`RunPipelineStatic`: `cmd/up.go:32`,
      `cmd/down.go:72`, `cmd/sync.go:86`, `cmd/vendor.go:28`, `cmd/pack.go:36`,
      `cmd/plugin.go:65`, `cmd/repo.go:74`.
- [ ] CLI-surface goldens exist: `cmd/testdata/clitree.golden` and
      `cmd/testdata/te3_preview.golden`; the first is fenced by `cmd/clitree_test.go`
      `TestCommandTreeGolden` (line 40), which pins command/flag structure — not plain-mode
      bytes.

## History

The freeze was first defined as exactly the bytes `ui.Printer` emitted, with the plain
renderer ignoring `StepStarted` and `HealthTick` and emitting zero bytes for a step until it
completed — the Access block being the sole exception.

Two ratifications changed that. R1 (TUI design doc §5) made plain emit a start line per
started step, so CI logs can distinguish a hung step from a slow one; `plain.go:27-30` now
prints `▸ [stage] msg...` for `event.StepStarted`. R1 removed only `StepStarted` from the
zero-byte set; the set still holds all six of `RunStarted`, `StepFailed`, `StepLog`,
`HealthTick`, `Diagnosis` and `RunDone` (`plain.go:53-56`). R2 made the epilogue glyph
presentation, so plain projects the epilogue without the `✔` glyph (`plain.go:37-42`).

The freeze itself survives both; what changed is its baseline. Plain output is now frozen
relative to three sanctioned byte deltas — the Access block, the epilogue glyph, and the
`StepStarted` start line — rather than to the original pre-Task-14b byte set. R3
(`cmd/down.go:67-70`) was ratified in the same period but changes no plain bytes; it refuses
to run a non-TTY `down` without `--yes`.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:207`
- `docs/archive/superpowers/plans/2026-07-16-tui-interactive-layer.md:14`
- `docs/archive/superpowers/specs/2026-07-16-tui-interactive-layer-design.md:49`
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:17`
- `docs/archive/superpowers/research/2026-07-14-cube-idp-ux-research.md:270`
