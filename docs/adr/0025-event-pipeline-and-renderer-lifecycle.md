---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 25. Typed Event Pipeline and Renderer Lifecycle

## Context and Problem Statement

cube-idp's long-running commands (`up`, `down`, `vendor`, `pack`, `plugin`, `repo`,
`sync`'s one-shot path — its `--watch` setup path bypasses the pipeline) all need to
report progress, and they need to report it in three very different
shapes: a plain line-oriented log, a live in-terminal step tree, and a machine-readable
JSON stream. Letting each command print for itself would fan out formatting decisions
across the codebase, make the JSON contract impossible to guarantee, and make the
terminal-ownership question (who holds the cursor, who releases it, who prints last)
unanswerable.

Two failure modes in particular motivated the design. First, a live terminal renderer
that outlives its command leaks goroutines and leaves the terminal in an unknown state.
Second, when a run fails, the error diagnosis is the single most important thing on the
screen — and a live region that is still redrawing, or an alternate screen buffer that
gets torn down after the error is printed, can overwrite it or swallow it entirely.

## Decision

Every long-running command emits one canonical typed event stream defined in
`internal/ui/event`, consumed by interchangeable renderers that project events without
inventing content.

Events travel on a buffered `chan event.Event` of fixed capacity 256, with exactly one
producer sending and exactly one renderer receiving, so producers never block on the UI.

`ui.RunPipeline` and `ui.RunPipelineStatic` (a shared `runPipeline` body differing only
in the renderer picker) own a single command's pipeline — producer on a goroutine,
renderer on the calling goroutine — and guarantee on return that the terminal is fully released,
that no goroutine it started survives, and that exactly the producer's error is returned.
The Bubble Tea program's lifetime is strictly bounded by the command's `RunE`.

The final error block is printed to stderr from `main.go`'s single final-error print
point via `ui.RenderErrorTo(os.Stderr, err)` for every error except a plugin's own
non-zero exit, which propagates verbatim and unrendered (`cmd.ExitCodeFor`), followed by
a non-zero exit, never by a renderer. stderr
stays reserved for diagnostics even in JSON mode, while the event stream goes to stdout.

On failure the live renderer stops the live region, flushes in-flight step state as
permanent scrollback lines, and exits before the diagnosis block renders, so a diagnosis
can never be overwritten or trapped in an alternate screen.

## Consequences

* Good, because the JSON stream is a byproduct of the same events the humans see — the
  three projections cannot drift in content, only in presentation.
* Good, because terminal ownership has exactly one answer: `RunPipeline` holds it, and
  the diagnosis prints only after `RunPipeline` has returned.
* Good, because a 256-slot buffer plus a single-producer/single-consumer topology means
  a slow or dead renderer can never stall business logic.
* Good, because the closed `Event` interface makes a renderer's switch exhaustive by
  convention — adding an event forces a decision in every renderer.
* Bad, because adding an event type is a cross-cutting change touching every renderer.
* Bad, because commands that stay on `ui.Printer` (`status`, `doctor`, `get`, `diff`,
  `upgrade --plan`, `init`) get none of these guarantees, leaving three output seams in
  the codebase: the event pipeline, `ui.Printer`, and `status --watch`'s own inline
  Bubble Tea program.
* Bad, because the mode-to-renderer mapping has grown command-class-dependent
  (`RunPipeline` vs `RunPipelineStatic`), so "which renderer runs" is no longer a single
  table.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Every long-running command emits one canonical typed event stream in `internal/ui/event`, consumed by interchangeable renderers that project events without inventing content. | `internal/ui/event/event.go:33-36` |
| Events travel on a buffered `chan event.Event` with fixed capacity 256, one producer sending and exactly one renderer receiving, so producers never block on the UI. | `internal/ui/pipeline.go:25`, `internal/ui/pipeline.go:135` |
| On failure the live renderer stops the live region, flushes in-flight state as permanent lines and exits before the diagnosis block renders, so a diagnosis can never be overwritten or trapped in an alt screen. | `internal/ui/render/live.go:34-63` (Println flush, Quit, drain to done); `internal/ui/render/live.go:108-114` (StepFailed tail dump); `internal/ui/render/live.go:140-144` (diagnosis-last comment) |
| The Bubble Tea program's lifetime is strictly bounded by the command's `RunE` — it quits after the terminal event and no goroutine it started survives. | `internal/ui/render/live.go:34,60-62` (Bubble Tea program exit and drain); `internal/ui/pipeline.go:160-165` (producer goroutine join) |
| `ui.RunPipeline` and `ui.RunPipelineStatic` own one command's pipeline through a shared `runPipeline` body, producer on a goroutine and renderer on the calling goroutine, returning only after the terminal is released and exactly the producer's error is available. | `internal/ui/pipeline.go:50-53`, `internal/ui/pipeline.go:62-65`, `internal/ui/pipeline.go:124-166` |
| The final error block is printed to stderr by `main.go`'s single final-error print point, not by any renderer; stderr stays reserved for diagnostics even in JSON mode. | `main.go:20-33` |
| *(superseded)* The live renderer covers `up` and `down` only; all other commands keep static output and request/response commands keep `ui.Printer` as their seam. | `internal/ui/pipeline.go:56-65` |
| *(superseded)* `RunPipeline` maps modes to renderers four ways: JSON → JSON, Live → Live, Styled → Live on a TTY else Plain, Plain → Plain. | `internal/ui/pipeline.go:78-89` |

### Verification

- [ ] `internal/ui/event/event.go` declares `type Event interface{ event() }` — a closed
      set kept closed by an unexported marker method.
- [ ] `internal/ui/pipeline.go` declares `const eventBuffer = 256` and the pipeline
      creates the channel as `make(chan event.Event, eventBuffer)`.
- [ ] `runPipeline` in `internal/ui/pipeline.go` launches the producer with `go func()`,
      calls the renderer picker on the calling goroutine, and returns `<-errCh` — the
      producer's error verbatim, joined after the channel close.
- [ ] The producer in `internal/ui/pipeline.go` emits, on failure, `event.StepFailed`
      for any still-open step, then `event.RunDone{OK: false}`, then `event.Diagnosis`
      as the final event before `close(ch)`.
- [ ] `internal/ui/render/live.go` returns no scrollback lines for `event.Diagnosis` and
      documents that the diagnosis renders after the program exits; it never calls
      `os.Exit`.
- [ ] `main.go` is the only place calling `ui.RenderErrorTo(os.Stderr, err)`, and it
      does so after `cmd.Execute` has returned, followed by `os.Exit(code)`.
- [ ] `grep -rn "RenderErrorTo\|RenderError(" internal/ui/render/` returns nothing — no
      renderer prints the final error block.
- [ ] `go test ./internal/ui/...` passes, including `internal/ui/pipeline_test.go`,
      which exercises the pipeline lifecycle.

## History

Two parts of the original design have been superseded.

The event stream was originally scoped to `up` and `down` only, with request/response
commands keeping `ui.Printer` as their seam. `RunPipelineStatic`
(`internal/ui/pipeline.go:62-65`) extended the stream to `pack`, `plugin`, `repo`, `sync`
and `vendor`, and `status` gained its own inline Bubble Tea watch program
(`cmd/status.go:202-215`). The residual truth — that `status`, `doctor`, `get`, `diff`,
`upgrade --plan` and `init` still use `ui.Printer` rather than the stream — survives, but
the stated scope does not.

The four-way mode-to-renderer table in `RunPipeline` was superseded by the color policy
(`forceColorUpgrade`, `internal/ui/pipeline.go:113-121`) plus `RunPipelineStatic`'s
separate picker. `pickRenderer` now has a fifth arm that renders the animation-free
`render.Styled` projection when color is forced through a pipe, and `pickRendererStatic`
(`internal/ui/pipeline.go:97-110`) diverges further: `ModeStyled` on a real TTY gets
`render.Styled(stripFor(out))` rather than the live renderer. The renderer count also
grew from three to four with `internal/ui/render/styled.go`, but the
one-stream / interchangeable-projection architecture is unchanged.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member origins:

- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:13` — one canonical typed
  event stream, three interchangeable renderers.
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:238` — buffered channel of
  capacity 256, single producer and single consumer.
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:272` — `ui.RunPipeline`
  ownership and return-time guarantees.
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:464` — single final-error
  print point in `main.go`.
- `docs/archive/superpowers/research/2026-07-14-cube-idp-ux-research.md:277` — diagnosis-last
  rule for the live renderer.
