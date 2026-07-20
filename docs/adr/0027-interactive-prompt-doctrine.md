---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 27. Interactive Prompt Doctrine and the Prompt Gate

## Context and Problem Statement

cube-idp is a CLI that runs in two very different worlds: an operator's terminal, where
asking a question is helpful, and CI or a script, where asking a question is a hang. A
prompt written into a command without a guard blocks forever on a piped or buffered stdin
and takes the build down with it. cube-idp also drives a live event pipeline that owns the
terminal while it renders, so a prompt fired mid-pipeline corrupts the display.

Two of the CLI's surfaces are additionally dangerous rather than merely awkward.
`cube-idp down` destroys clusters and inventory-managed resources, and plugin execution
runs third-party binaries fetched from a network. Both need the operator to see and accept
what is about to happen before it happens — and both need a non-interactive path that is
explicit rather than silently permissive. Finally, any signal the CLI paints on the
terminal must survive being piped through a log aggregator that strips color.

The question this record settles: what single rule decides whether cube-idp may ask a
question, and what must every prompting surface guarantee.

## Decision

A single gate — `ui.PromptsAllowed(in, out)` — governs every interactive surface. It
returns true only when both streams are real TTYs, the resolved output mode is styled or
live, and no event pipeline currently owns the terminal. Every prompt has a scriptable
flag twin, and prompts run before `RunPipeline`, never inside a producer.

Destructive and environment-sensitive behaviour is made legible before it happens.
`cube-idp down` prints a bulleted preview of the real deletion set and requires the user to
type the exact cube name via a huh v2 Input, followed by a mandatory dim hint line;
`--confirm=<name>` and `--yes` are its scriptable twins. Declining prints
`aborted — nothing was changed` and exits 0 without running the pipeline. Resources carry a
per-resource opt-out annotation, so hand-edited or undeclared objects are never silently
deleted. Plugins are refused with the byte-for-byte CUBE-7104 diagnosis unless their sha256
is recorded in the trust store or the user confirms interactively; a changed binary hash
re-prompts.

The interactive `init` wizard runs only under the `wizardApplicable` guard — both stdin and
stdout are TTYs and the corresponding flag was not passed — with huh v2 accessible mode
enabled form-level. Explicit flags always win; non-TTY or `--plain` falls back to unchanged
flag-driven behaviour writing the same `cube.yaml`.

Terminal output draws semantic colors only from the basic ANSI 8/16 range and never conveys
meaning by color alone: a glyph and a word always accompany it, so meaning survives color
stripping.

Every prompt-capable code path is covered by a test proving buffered stdin never blocks
under a timeout, and every prompt-capable command appears in a shared prompt-fence table
test driven with empty-buffer stdin — so CI can never hang and a future prompt cannot dodge
the gate.

## Consequences

* Good, because a prompt can never hang CI: the gate is false for every non-`*os.File`
  stream, and the fence table test fails the build if a new prompt escapes it.
* Good, because every interactive path has a scriptable twin, so automation is a first-class
  path rather than an accident of the gate returning false.
* Good, because destruction is legible and consented: the operator sees the actual deletion
  set and must type the cube name, and non-interactive destruction requires an explicit flag.
* Good, because color-stripped output (logs, CI, pipes) loses no meaning.
* Bad, because every new prompt costs more than a prompt: a flag twin, a fence-table row, and
  placement outside the pipeline.
* Bad, because the gate is deliberately conservative — a terminal cube-idp cannot prove is a
  TTY gets no prompt, and the user must reach for the flag instead.
* Bad, because restricting to ANSI 8/16 gives up finer palettes and hands final appearance to
  the user's terminal theme.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `ui.PromptsAllowed` is the single gate for every interactive surface — false unless both streams are real TTYs, the mode is styled or live, and no pipeline owns the terminal; every prompt has a flag twin and runs before `RunPipeline`. | `internal/ui/prompt.go:13-22` |
| `prompt.Allowed(in, out)` semantics are realised as `ui.PromptsAllowed`, which must be false for every non-`*os.File` stream combination. | `internal/ui/prompt.go:13-22` |
| Every prompt-capable command (`down`, `trust`, `upgrade`, `pack install`, and others) appears in a shared prompt-fence table test driven with empty-buffer stdin under a timeout, so CI can never hang. | `cmd/promptfence_test.go:28-94` |
| On an interactive terminal `cube-idp down` prints a preview of the real deletion set and requires the typed cube name; declining prints `aborted — nothing was changed` and exits 0 without running the pipeline. | `cmd/down.go:54-64` |
| `cube-idp down` validates the typed name against the exact cube name via a huh v2 Input, offers `--confirm=<name>` and `--yes` as scriptable twins, and prints a mandatory dim hint line after the prompt renders. | `cmd/down.go:42-70` |
| Plugins are refused with the byte-for-byte CUBE-7104 diagnosis unless their sha256 is in the trust store or the user confirms interactively; a changed hash re-prompts. | `internal/plugin/trust.go:158-162` |
| `cube-idp init` runs its single interactive huh wizard only when stdin and stdout are TTYs and the corresponding flag was not passed; explicit flags always win and non-TTY falls back to the unchanged flag-driven write. | `cmd/init.go:280-284` |
| The init wizard is gated on `wizardApplicable`, and huh v2 accessible mode is enabled form-level. | `cmd/init.go:280-285` |
| Semantic colors come only from the basic ANSI 8/16 range, paired with glyph constants so meaning never rides on color alone. | `internal/ui/theme/theme.go:44-52` |
| Pruning honours a per-resource opt-out annotation so hand-edited or undeclared resources are never silently deleted. *(The always-show-a-preview-diff half of this decision was superseded — see History.)* | `internal/apply/inventory.go:152` |
| No Go version is hardcoded anywhere in the repo; every CI workflow pins the toolchain via `go-version-file: go.mod`. | `.github/workflows/ci.yaml:13` |

### Verification

- [ ] `internal/ui/prompt.go:13-22` returns false when `pipelineActive` is set, when either
      stream fails `IsTerminal`, or when the mode is neither `ModeStyled` nor `ModeLive`.
- [ ] `cmd/promptfence_test.go:28` (`TestPromptFenceNeverBlocksOnBufferStdin`) runs every
      prompt-capable command with an empty `bytes.Buffer` stdin and fails on blocking.
- [ ] `internal/ui/prompt_test.go:13` proves `PromptsAllowed` is false for buffer streams.
- [ ] `cmd/down.go:54-64` calls `printDownPreview`, then `downConfirmName`, and on decline
      prints exactly `aborted — nothing was changed` and returns nil before `ui.RunPipeline`.
- [ ] `cmd/down.go` prints the dim hint `hint: cube-idp down --yes` after a successful
      confirmation, and `--confirm=<name>` mismatch raises `diag.CodeConfirmRequired`.
- [ ] `cmd/down_test.go` pins the preview against `cmd/testdata/te3_preview.golden`.
- [ ] `internal/plugin/trust.go:158-162` returns `diag.CodePluginUntrusted` (CUBE-7104)
      whenever `!interactive || !ui.PromptsAllowed(os.Stdin, os.Stderr)`.
- [ ] `cmd/init.go:280-285` (`wizardApplicable`) returns false if `--name`, `--engine`, or
      `--gateway-pack` was changed, and otherwise requires `ui.IsTerminal` on both streams.
- [ ] `cmd/init.go:313` reads `$ACCESSIBLE` once and passes it to `.WithAccessible(...)` on
      each huh form.
- [ ] `internal/ui/theme/theme.go:44-52` uses only bare ANSI indices `"0"`–`"15"` (no hex, no
      256-color values), and glyph constants exist at `internal/ui/theme/theme.go:18-21`.
- [ ] `internal/apply/inventory.go:152` skips objects annotated with `PruneAnnotation`
      (`internal/apply/applier.go:23`) set to `disabled`.
- [ ] Every `setup-go` step in `.github/workflows/` uses `go-version-file: go.mod` and no
      workflow hardcodes a Go version.

## History

The original commitment was that pruning always shows a preview diff on every prune,
alongside the per-resource opt-out annotation. The preview-diff-always rule has been
superseded: prune is now confirmation-gated on `down` (`--yes` / `--confirm=<cube-name>`)
with a bulleted destruction summary rather than a rendered per-prune diff. The opt-out
annotation and the no-silent-deletion guarantee remain in force.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-16-tui-interactive-layer.md:1068` — the single prompt gate
- `docs/archive/superpowers/plans/2026-07-16-tui-interactive-layer.md:1284` — `down` typed-name confirmation
- `docs/archive/superpowers/specs/2026-07-16-tui-interactive-layer-design.md:69` — ANSI-16 plus glyph legibility
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:782` — init wizard guard and accessible mode
- `docs/archive/superpowers/research/2026-07-13-cube-idp-brainstorm/synthesis.md:50` — prune opt-out annotation
