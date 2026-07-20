# cube-idp — Terminal UX design: one event stream, three renderers

**Date:** 2026-07-14
**Status:** APPROVED DESIGN — implements Owner Decisions #10/#15 (2026-07-14, spec D13): rich-by-default terminal UX, machine-guarded. The owner selected research Proposal B ("One console"), executed in two stages A→B.
**Derived from:** `docs/superpowers/research/2026-07-14-cube-idp-ux-research.md` (§3 architecture, §4 Proposal B). This document is self-contained: Tasks 14b and 14c are briefed from THIS file alone.
**Amends:** spec §4.1 ("No full Bubble Tea app in the kernel — a spinner is not a TUI") — a *transient inline* Bubble Tea program is now sanctioned for `up`/`down` (and later `sync --watch`); persistent/alt-screen TUIs remain forbidden.
**Implemented by:** Task 14b (stage A), Task 14c (stage B) — checklists in §11.

---

## 0. Summary

cube-idp gets BuildKit's architecture at cube-idp's scale: every long-running
command emits **one canonical stream of typed events**, and three
interchangeable renderers *project* (never invent) that stream:

- **PlainRenderer** — byte-for-byte today's output. The plain projection of
  each event is *defined as* the bytes `internal/ui` emits today, so every
  pinned test and e2e assertion survives unchanged (one deliberate exception,
  §9).
- **LiveRenderer** — Bubble Tea v2 **inline mode** (no alt screen): completed
  steps stream into native scrollback via `tea.Println`; a managed bottom
  region holds only in-flight state (spinner, elapsed, health table); the
  program exits leaving clean scrollback. Persistent full-screen dashboards
  are explicitly rejected (research Proposal C; Tilt retired exactly that
  shape to `--legacy` after users defected to plain logs).
- **JSONRenderer** — JSON lines, exactly one event per object per line
  (BuildKit's rawjson consumers demanded this — moby/buildkit #4769), with a
  `"v":1` version field. Schema labeled **experimental** until the D5 v1
  config freeze.

Commands that are request/response rather than long-running (`status`,
`doctor`, `get`, `diff`, `upgrade --plan`, `init`) do **not** use the event
stream. They keep `ui.Printer` as their seam; stage B enriches their styled
mode and adds gh-style `--output json` *documents* where meaningful.

Renderer selection happens **once**, in `cmd/root.go`'s `PersistentPreRunE`
(where `ui.PlainFlag` is set today), via an extended `ui.Resolve` ladder (§6).

---

## 1. Ground truth — the real seams this design builds on

Every seam below exists today; implementers should verify against these files.

| Seam | Where | What it is today |
|---|---|---|
| `ui.Printer` | `internal/ui/ui.go` | The single print seam: `Step`, `Section`, `Glyph`, `Warn`, `Progress`, `AccessSummary`. Two modes: `ModeStyled` (0), `ModePlain` (1). |
| `ui.PlainFlag` | `internal/ui/ui.go` | Package-level bool mirroring `--plain`, set once by `cmd/root.go`. Read by `ui.New(w, ui.PlainFlag)` at every call site (`internal/up/up.go`, `internal/doctor/doctor.go`, `internal/diff/diff.go`, `internal/upgrade/plan.go`, `cmd/status.go`, `cmd/get.go`, `cmd/cnoe.go`). |
| `ui.Resolve` | `internal/ui/ui.go` | Pure precedence function `Resolve(plainFlag, isTTY bool, ciEnv string) Mode`. Checks flag → non-TTY → `$CI`. Does NOT yet honor `NO_COLOR`/`TERM=dumb`. |
| `ui.IsTerminal` | `internal/ui/ui.go` | `*os.File` + `term.IsTerminal` check; any non-`*os.File` writer (every test buffer) is never a terminal. This is what keeps all tests plain with zero plumbing. |
| The pinned plain format | `internal/ui/ui.go` `Step` | `"▸ [%s] %s\n"` — pinned byte-for-byte by `internal/ui/ui_test.go` (`TestPlainMatchesPhase1Format`, which also asserts zero ANSI escapes) and grepped by `internal/up/tls_test.go` (`"▸ [ca]"` present, `"▸ [cluster]"` absent). |
| `ui.Progress` | `internal/ui/ui.go` | Hand-rolled single-line spinner goroutine (`\r\x1b[2K` erase, 100ms tick). **Hard invariant** (pinned by `TestProgressPlainEmitsNothingBeforeDone` / `TestProgressPlainStopEmitsNothing`): in `ModePlain` it emits zero bytes until `Done`; `Done` is byte-identical to `Step`; `Stop` (error path) emits nothing. |
| `ui.AccessSummary` | `internal/ui/ui.go` | Styled-mode-only "Access" block after `up`; **complete no-op in ModePlain** (pinned by `TestAccessSummaryPlainNoOp`). This is the one pinned contract this design deliberately changes (§9). |
| `up` call sites | `internal/up/up.go` | `Run(ctx, cfgPath string, out io.Writer)` drives up through `step(out, stage, ...)` (stages: `config`, `ca`, `cluster`, `registry`, `packs-crd`, `engine`, `tls`, `pack`, `lock`, `dns`, `health`, `packs`) plus three `Progress` wraps around the long waits (cluster create, engine install, `waitHealthy`'s poll loop) and one raw success `fmt.Fprintf` (`"\n✔ cube %q is up — https://%s:%d\n  credentials: cube-idp get secrets\n"`) followed by `AccessSummary`. |
| `waitHealthy` | `internal/up/up.go` | Polls `eng.Health(ctx, a)` (`engine.ComponentHealth{Name, Ready, Message}`, `internal/engine/engine.go`) every 5s under one `Progress("health", ...)`. |
| Selection point | `cmd/root.go` | `PersistentPreRunE` sets `ui.PlainFlag = plain` before any `RunE`. The `--plain` persistent flag lives here. |
| `down` | `cmd/down.go` | Prints almost nothing today: `revertTrust`'s warning lines and `"reverted: cube-idp CA removed from OS trust stores"` via raw `fmt.Fprintf/Fprintln`. No step lines, no Progress. `cmd/down_test.go` asserts substrings only (`"warning"`, `"cube-idp trust --uninstall"`). |
| `init` wizard | `cmd/init.go` | huh **v1** (`github.com/charmbracelet/huh` v1.0.0) 3-field form (name, engine, include-gitea), gated by `wizardApplicable(c)`: stdin AND stdout must both be real terminals, and neither `--name` nor `--engine` explicitly passed. |
| `status` | `cmd/status.go` | Glyph-prefixed component lines (`"%s %s Ready\n"` via `p.Glyph(ui.GlyphOK)`), inventory count, and `formatInventory`'s tabwriter table under `--details`. |
| `doctor` | `cmd/doctor.go` + `internal/doctor/doctor.go` | Collects `diag.Finding`s; `doctor.Render(out, findings)` prints `"%s %s  %s\n    fix: %s\n"` per finding (glyph via `Printer.Glyph`), `"✔ no problems found"` when empty, returns hasErrors (drives `os.Exit(1)`). |
| Error model | `internal/diag/diag.go` | `*diag.Error{Code, Summary, Cause, Remediation}` (CUBE-xxxx); `diag.Render(err)` produces the `✗ CUBE-… / cause: / fix:` block. `main.go` prints `diag.Render(err)` to **stderr** on any command failure — the single final-error print point. |
| Charm deps | `go.mod` | `github.com/charmbracelet/huh v1.0.0`, `lipgloss v1.1.0` (direct); `bubbletea v1.3.6`, `bubbles v0.21.x` (indirect, via huh). All on v1 import paths. |
| e2e | `tests/e2e/e2e_test.go` | Substring assertions only (component names, pack URLs, `gitea_admin`) — no full-byte pins. |

---

## 2. Architecture

```
internal/up (stage A), cmd/down (stage A), internal/sync --watch (later)
        │  produce, via the ui.Console facade (§4.3)
        ▼
   ┌──────────────────────────────┐
   │ internal/ui/event            │  typed, renderer-agnostic events (§3)
   └────────────┬─────────────────┘
                │ chan event.Event (buffered; producer effectively never
                │ blocks on UI — §4.2)
    ┌───────────┼──────────────────────────────┐
    ▼           ▼                              ▼
PlainRenderer  LiveRenderer                 JSONRenderer
(§5.1 — IS     (§5.2 — bubbletea v2         (§5.3 — one JSON object
today's plain   inline; tea.Println for      per line, {"v":1,...};
Printer path,   done steps; managed bottom   diagnosis as a first-class
byte-stable)    region; clean exit)          event type)
```

Static commands (`status`, `doctor`, `get`, `diff`, `upgrade`, `init`,
`trust`, `config`, `version`) stay on `ui.Printer` directly — no channel, no
program. The resolved `ui.Mode` (§6) is the single knob both surfaces obey:

| Resolved mode | Event-stream commands (`up`, `down`) | Static commands |
|---|---|---|
| `ModeStyled` | LiveRenderer | Styled `Printer` output (stage B: richer) |
| `ModePlain` | PlainRenderer | Plain `Printer` output (bytes unchanged) |
| `ModeJSON` | JSONRenderer | Stage A: plain projection. Stage B: `--output json`-style documents on `status`/`doctor`/`get secrets`; commands with no meaningful document (e.g. `version`, `trust`) keep the plain projection permanently — plain IS their machine contract. Never styled. |

---

## 3. Event vocabulary — `internal/ui/event`

New package `internal/ui/event`. It imports only `time` and
`internal/diag` (no lipgloss, no bubbletea, no ui) so producers and all
three renderers can depend on it without cycles.

```go
// Package event defines the renderer-agnostic vocabulary of everything a
// cube-idp run can tell a user. Renderers project these events; they never
// invent content (spec D13, BuildKit's SolveStatus precedent).
package event

import (
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
)

// Event is the closed set of run events. The marker method keeps the set
// closed at compile time (a renderer switch over these types is exhaustive
// by convention; new events require touching every renderer).
type Event interface{ event() }

// RunStarted opens a run. Cube is the cube name from cube.yaml; emitted by
// the producer immediately after config.Load succeeds (it is not emitted at
// all when config loading fails — consumers must tolerate a stream that is
// only RunDone+Diagnosis).
type RunStarted struct{ Cmd, Cube string }

// StepStarted marks a stage as in-flight. Today's ui.Progress start.
// Plain projection: zero bytes (the pinned Progress invariant).
type StepStarted struct{ Stage, Msg string }

// StepDone completes a stage. Today's ui.Printer.Step / Progress.Done.
// Dur is 0 for instantaneous steps (plain projection never includes it).
type StepDone struct {
	Stage, Msg string
	Dur        time.Duration
}

// StepFailed marks the in-flight stage as failed. Today's Progress.Stop on
// an error path. Err is nil when the authoritative error arrives later as
// Diagnosis (the common case: the producer's error unwinds to the run
// lifecycle, which emits Diagnosis).
type StepFailed struct {
	Stage string
	Err   *diag.Error
}

// ComponentState mirrors engine.ComponentHealth (internal/engine/engine.go)
// without importing it (event stays dependency-light).
type ComponentState struct {
	Name    string
	Ready   bool
	Message string
}

// HealthTick carries one waitHealthy poll result. Emitted on the FIRST poll
// and thereafter only when any component's Ready/Message changed — keeps the
// JSON stream from repeating identical lines every 5s.
type HealthTick struct{ Components []ComponentState }

// Note is a neutral passthrough line (e.g. up's final success block, down's
// trust-revert messages). Msg carries any embedded newlines; renderers add
// exactly one trailing newline.
type Note struct{ Msg string }

// Warn is an advisory (e.g. get secrets' legacy-label deprecation note).
type Warn struct{ Msg string }

// PackAccess is one delivered pack's access info (today's ui.PackAccess —
// internal/ui keeps `type PackAccess = event.PackAccess` as an alias so
// internal/up's construction sites don't churn).
type PackAccess struct {
	Name string
	URLs []string
}

// Access is the post-up "here's what you just got" summary. See §9: as of
// stage A this HAS a plain projection (the one deliberate plain change).
type Access struct {
	Packs []PackAccess
	Hint  string
}

// Diagnosis is ALWAYS the last event on a failed run. Err is the typed
// CUBE-xxxx error when errors.As finds one; Raw is err.Error() and is
// always set (the fallback for untyped errors).
type Diagnosis struct {
	Err *diag.Error
	Raw string
}

// RunDone closes a run. On failure it is emitted immediately BEFORE
// Diagnosis (so Diagnosis stays terminal).
type RunDone struct {
	OK  bool
	Dur time.Duration
}

func (RunStarted) event()  {}
func (StepStarted) event() {}
func (StepDone) event()    {}
func (StepFailed) event()  {}
func (HealthTick) event()  {}
func (Note) event()        {}
func (Warn) event()        {}
func (Access) event()      {}
func (Diagnosis) event()   {}
func (RunDone) event()     {}
```

**Stage names are today's badge names** — the exact strings already passed to
`step()`/`Progress` in `internal/up/up.go`: `"config"`, `"ca"`, `"cluster"`,
`"registry"`, `"packs-crd"`, `"engine"`, `"tls"`, `"pack"`, `"lock"`,
`"dns"`, `"health"`, `"packs"` (plus `"cnoe"` from `cmd/cnoe.go`). `down`
introduces new stage names in stage A: `"engine"` (uninstall), `"dns"`
(CoreDNS rewrite revert), `"cascade"` (inventory DeleteAll), `"cluster"`
(kind delete), `"trust"` (trust-store revert). Stage is an open `string`,
not an enum — packs and future commands add stages without touching event.

**Ordering rules** (normative):

1. `RunStarted` first, when emitted at all (skipped if config.Load fails).
2. Every `StepStarted` is resolved by the next `StepDone` or `StepFailed`
   for the same stage, or implicitly by `RunDone` (renderers MUST treat
   `RunDone`/`Diagnosis` as resolving any still-open step).
3. Success termination: `... → Access? → RunDone{OK:true, Dur}`. Nothing
   follows `RunDone` on success.
4. Failure termination: `... → StepFailed? → RunDone{OK:false, Dur} →
   Diagnosis`. **Diagnosis is always the final event on failure** — machine
   consumers may treat it as the terminal record (Terraform's `diagnostic`
   precedent).

## 4. Delivery and lifecycle

### 4.1 Channel

Events travel on a `chan event.Event` with a fixed buffer (**cap 256**; a
full `up` emits well under 100 events even with health ticks). The producer
sends; exactly one renderer receives. The producer effectively never blocks
on UI: renderers consume promptly (the live renderer forwards each event to
`tea.Program.Send`, which is non-blocking after program start; plain/JSON
renderers do one `Fprintf` per event), and the 256 buffer absorbs any
transient stall. Renderers MUST NOT perform blocking work that depends on
the producer (no deadlock cycle exists: producer → channel → renderer →
stdout only).

The run lifecycle (below) closes the channel after the terminal event;
renderers exit their receive loop on channel close.

### 4.2 The run lifecycle — strictly inside RunE

New helper in `internal/ui` (name normative):

```go
// RunPipeline owns one command's event pipeline: it builds the renderer for
// the resolved Mode, hands the producer a Console, and guarantees that by
// the time it returns (a) the terminal is fully released (the bubbletea
// program, if any, has exited) and (b) no goroutine it started survives.
// It returns exactly the producer's error, so cobra/main.go error handling
// is unchanged.
func RunPipeline(ctx context.Context, cmdName string, out io.Writer,
	fn func(ctx context.Context, con *Console) error) error
```

Contract:

1. `RunPipeline` resolves the renderer from `CurrentMode()` (§6):
   `ModeJSON` → JSONRenderer; `ModeLive` → LiveRenderer (explicit force,
   even on a non-TTY); `ModeStyled` → LiveRenderer when `out` is also a real
   TTY (`ui.IsTerminal`), else PlainRenderer; `ModePlain` → PlainRenderer.
2. It creates the channel and a cancellable child context, starts the
   producer `fn` on a goroutine, and runs the renderer on the calling
   goroutine (bubbletea input handling wants the foreground; plain/JSON run
   the same receive loop shape).
3. When `fn` returns: on success it emits `RunDone{OK:true, Dur}`; on error
   it emits `StepFailed{Stage}` for the still-open stage if any (the Console
   tracks it), then `RunDone{OK:false, Dur}`, then
   `Diagnosis{Err: asDiag(err), Raw: err.Error()}`, then closes the channel.
4. It waits for the renderer to drain and exit (for live: `tea.Program`
   returns after `tea.Quit`, triggered by seeing the channel close /
   terminal event), joins the producer goroutine, and returns `fn`'s error.
   **No goroutine survives RunPipeline** — the bubbletea program's lifetime
   is strictly inside the command's `RunE`.
5. Ctrl-C: `main.go`'s `signal.NotifyContext` already cancels the command
   context. Additionally the live program maps its `ctrl+c` key event to the
   same cancel func (passed in by RunPipeline) — the producer then unwinds
   through its normal error paths (e.g. `waitHealthy`'s existing
   `ctx.Done()` → CUBE-3004 wrap), terminal events flow, and the program
   quits through the single ordinary path. The live program NEVER calls
   `os.Exit` and never swallows the interrupt.

`cmd/up.go`'s RunE becomes (shape, not final code):

```go
RunE: func(c *cobra.Command, _ []string) error {
	return ui.RunPipeline(c.Context(), "up", c.OutOrStdout(),
		func(ctx context.Context, con *ui.Console) error {
			return up.Run(ctx, file, con)
		})
},
```

`cmd/down.go` wraps its existing RunE body the same way.

### 4.3 The Console facade — how `internal/up` migrates without churn

`internal/ui` gains `Console`, the producer-side facade that **constructs
events** and mirrors today's `Printer` method set, so `internal/up/up.go`
call sites barely change:

```go
// Console is the producer's handle on the event stream. Its method set
// deliberately mirrors ui.Printer so orchestrator call sites migrate
// mechanically: step(out, ...) -> con.Step(...), p.Progress(...) ->
// con.Progress(...), raw fmt.Fprintf -> con.Note(...).
type Console struct { /* ch chan<- event.Event; open stage tracking */ }

func (c *Console) Start(cmd, cube string)                    // emits RunStarted
func (c *Console) Step(stage, format string, args ...any)    // emits StepDone{Dur:0}
func (c *Console) Progress(stage, message string) *ConsoleProgress // emits StepStarted
func (c *Console) Note(format string, args ...any)           // emits Note
func (c *Console) Warn(format string, args ...any)           // emits Warn
func (c *Console) Health(components []event.ComponentState)  // emits HealthTick (change-filtered)
func (c *Console) Access(packs []event.PackAccess, hint string) // emits Access

// ConsoleProgress mirrors ui.Progress's resolution contract exactly: every
// Progress is resolved by exactly one Done (success) or Stop (abandoned on
// error). Done emits StepDone{Dur: since start}; Stop emits
// StepFailed{Stage, Err: nil}.
func (cp *ConsoleProgress) Done(format string, args ...any)
func (cp *ConsoleProgress) Stop()
```

`up.Run`'s signature changes from `(ctx, cfgPath string, out io.Writer)` to
`(ctx, cfgPath string, con *ui.Console)`. The migration map for
`internal/up/up.go` (every current output call site, in order):

| Today (`internal/up/up.go`) | Stage A |
|---|---|
| `step(out, "config", "cube %q loaded and validated", ...)` | `con.Start("up", cube.Metadata.Name)` then `con.Step("config", ...)` (same message) |
| `p := ui.New(out, ui.PlainFlag)` | delete (Console passed in) |
| `step(out, "ca"/"registry"/"packs-crd"/"tls"/"pack"/"lock"/"dns"/"packs", ...)` | `con.Step(same stage, same message)` |
| `p.Progress("cluster", ...)` / `.Done` / `.Stop` | `con.Progress("cluster", ...)` / `.Done` / `.Stop` — same message strings |
| `p.Progress("engine", ...)` | `con.Progress("engine", ...)` |
| `waitHealthy(..., out, ...)`'s `Progress("health", ...)` | `waitHealthy(..., con, ...)`; inside the poll loop, after each `eng.Health` call: `con.Health(mapComponents(health))` |
| final `fmt.Fprintf(out, "\n✔ cube %q is up — https://%s:%d\n  credentials: cube-idp get secrets\n", ...)` | `con.Note("\n✔ cube %q is up — https://%s:%d\n  credentials: cube-idp get secrets", ...)` — note: NO trailing `\n` in the format; Note's projection adds exactly one (§5.1), so bytes are identical |
| `p.AccessSummary(access, hint)` | `con.Access(access, hint)` |
| package-level `step(w, stage, format, ...)` helper | deleted |

The `Printer` type itself, and every static-command call site
(`doctor.Render`, `internal/diff`, `internal/upgrade`, `cmd/status.go`,
`cmd/get.go`, `cmd/cnoe.go`), is untouched by stage A except for the mode
plumbing in §6.4.

### 4.4 `sync --watch` (forward-compatibility note only — no code in 14b/14c)

When Phase 3 builds `sync --watch`, it is a LiveRenderer model variant that
additionally consumes fsnotify-driven events on the same channel until
Ctrl-C: a single-pane rolling view (last-push status line + rolling event
list), per Owner Decision #15 item 7. The vocabulary above is open to new
event types for it; nothing in stage A/B may assume the event set is frozen.

---

## 5. Renderer contracts

### 5.1 PlainRenderer — the byte-preservation contract

**PlainRenderer IS today's `ui.Printer` plain path.** The plain projection
of each event is *defined as* the bytes the current code emits, quoting the
exact format strings from `internal/ui/ui.go` and `internal/up/up.go`. This
table is normative; golden tests pin it (§12):

| Event | Plain projection (exact) | Defined as today's… |
|---|---|---|
| `RunStarted` | *zero bytes* | nothing is printed at run start today |
| `StepStarted{stage,msg}` | *zero bytes* | `Progress` in ModePlain: "no goroutine, no bytes" (pinned by `TestProgressPlainEmitsNothingBeforeDone`) |
| `StepDone{stage,msg}` | `fmt.Fprintf(out, "▸ [%s] %s\n", stage, msg)` — `Dur` NEVER printed | `Printer.Step`'s ModePlain branch, the phase-1 checkpoint-0.13 format (pinned by `TestPlainMatchesPhase1Format`) |
| `StepFailed` | *zero bytes* | `Progress.Stop` in ModePlain (pinned by `TestProgressPlainStopEmitsNothing`); the error itself is rendered by `main.go`'s `diag.Render`, not by the renderer |
| `HealthTick` | *zero bytes* | the health poll prints nothing today until `Done` |
| `Note{msg}` | `fmt.Fprintln(out, msg)` | up's raw success `Fprintf` and down's `revertTrust` `Fprintf/Fprintln` lines, byte-identical when producers pass the message without a trailing newline (§4.3 table) |
| `Warn{msg}` | `fmt.Fprintln(out, msg)` | `Printer.Warn`'s ModePlain branch (pinned by `TestWarnPlainExactLiteral`) |
| `Access{packs,hint}` | **NEW bytes — the one deliberate plain change, §9**: `"\nAccess\n"`, then per pack URL `fmt.Fprintf(out, "  %-12s %s\n", pack.Name, url)`, then `fmt.Fprintf(out, "  %s\n", hint)` | previously a complete no-op (`TestAccessSummaryPlainNoOp`) — that test is replaced |
| `Diagnosis` | *zero bytes* on stdout | the final error block remains `main.go`'s job: `fmt.Fprintln(os.Stderr, diag.Render(err))`, unchanged — `diag.Render` (`internal/diag/diag.go`) keeps producing the `✗ CUBE-… / cause: / fix:` block |
| `RunDone` | *zero bytes* | no run-summary line exists today |

Proof obligation for 14b: with the §4.3 migration map applied, a plain-mode
`up` (and `down`) run produces **byte-identical stdout to today except for
the Access block** (§9). Concretely, these existing pins must pass
unmodified: all of `internal/ui/ui_test.go` except `TestAccessSummaryPlainNoOp`,
`internal/up/tls_test.go`'s `"▸ [ca]"` / no-`"▸ [cluster]"` assertions,
`cmd/down_test.go`'s substrings, and every `tests/e2e/e2e_test.go` assertion.

PlainRenderer is a pure `func(w io.Writer) func(event.Event)` receive loop —
no goroutines of its own beyond the RunPipeline structure, no ANSI ever
(the existing zero-escapes test generalizes to a golden-stream test, §12).

### 5.2 LiveRenderer — Bubble Tea v2 inline

Runtime: `charm.land/bubbletea/v2` in **inline mode** — never
`tea.WithAltScreen`. The program manages only the bottom N lines; everything
completed is permanent scrollback. (This is the Dagger/gh pattern: "run a
command, it shows live progress, and then leaves all the output in your
terminal scrollback." Tilt's retired full-screen HUD is the anti-pattern.)

Projection contract:

- `RunStarted` → optional one-line header in the live region (e.g.
  `cube-idp up — cube "dev"`); NOT printed to scrollback.
- `StepStarted` → the live region shows a `bubbles/spinner` line:
  `⠋ [stage] msg… (elapsed)` — the same visual as today's hand-rolled
  `Progress` (which this replaces in styled mode).
- `StepDone` → `tea.Println` of the styled completed line — same content as
  plain, styled presentation, plus duration when `Dur > 0`:
  `✔ [stage] msg (12s)`. Once printed it is scrollback; the live region
  drops the spinner.
- `StepFailed` → `tea.Println` of `✗ [stage] msg` (content = the
  StepStarted msg); live region clears that spinner.
- `HealthTick` → the live region renders a compact component table
  (`bubbles/table` or hand-rolled lipgloss rows): glyph, name, message —
  one row per `ComponentState`. The table exists only while the `health`
  stage is open; it collapses when `StepDone{"health"}` arrives.
- `Note` → `tea.Println(msg)` verbatim (content-identical rule: styled
  presentation may add color, never different words).
- `Warn` → `tea.Println` prefixed with the amber `⚠` (same as
  `Printer.Warn` styled mode today).
- `Access` → `tea.Println` of the existing styled AccessSummary block
  (`internal/ui/ui.go AccessSummary`'s layout: `Access` section header,
  `  %-12s %s` per URL, dimmed hint) — printed into scrollback so it
  survives exit.
- `RunDone` → the live region collapses to zero lines (or one dim summary
  line `done in 3m12s`); the model returns `tea.Quit`.
- `Diagnosis` → see the diagnosis-last rule below. The model quits if still
  running; the renderer itself prints nothing for this event.

**Diagnosis-last rule (normative).** On failure the ordering guarantees of
§3 plus RunPipeline's structure produce: (a) the live region stops and any
in-flight state is emitted as final `tea.Println` lines (the `StepFailed`
projection), (b) the program EXITS (quit on `RunDone`/channel close), (c)
only after `tea.Program.Run` has returned — i.e. after the TUI has fully
released the terminal — does the diagnosis render. The render itself stays
in `main.go`, which changes from unconditional `diag.Render` to:

```go
if err := cmd.Execute(ctx); err != nil {
	fmt.Fprintln(os.Stderr, ui.RenderError(err)) // internal/ui, new
	os.Exit(1)
}
```

where `ui.RenderError(err)` returns `diag.Render(err)` verbatim in
ModePlain/ModeJSON (byte-identical to today) and, in ModeStyled, a lipgloss
**panel**: bordered block with the CUBE-xxxx code badge, summary, cause, and
the `fix:` remediation in copy-paste-safe plain text (no styling inside the
remediation string itself). The diagnosis can therefore never be overwritten
by the live region or trapped in a dead screen — it is the last thing on the
terminal, printed by the process's single final-error print point.

Charm components: `bubbletea/v2` (runtime), `bubbles/v2` spinner + table,
`lipgloss/v2` styles. No alt screen, no mouse capture, no `fang`.

### 5.3 JSONRenderer — JSON lines, versioned, experimental

- Stream target: **stdout** (stderr stays free for `diag.Render`, which
  `main.go` still prints in JSON mode — human-readable belt for a machine
  pipe).
- Exactly **one JSON object per line, one event per object** — never
  batched, never pretty-printed (the buildkit #4769 lesson).
- Every line carries `"v":1` (schema version) and `"ts"` (RFC3339Nano).
- Schema status: **experimental** until the D5 v1 config freeze — documented
  with that label wherever the flag/env is documented. After the freeze it
  gets a Terraform-style compatibility promise; until then fields may change
  without notice.

Event encodings (field names normative):

```json
{"v":1,"ts":"…","type":"run_started","cmd":"up","cube":"dev"}
{"v":1,"ts":"…","type":"step_started","stage":"cluster","msg":"creating kind cluster"}
{"v":1,"ts":"…","type":"step_done","stage":"cluster","msg":"kind cluster ready (context kind-dev)","dur_ms":72340}
{"v":1,"ts":"…","type":"step_failed","stage":"engine"}
{"v":1,"ts":"…","type":"health_tick","components":[{"name":"cube-idp-traefik","ready":false,"message":"reconciling"}]}
{"v":1,"ts":"…","type":"note","msg":"…"}
{"v":1,"ts":"…","type":"warn","msg":"…"}
{"v":1,"ts":"…","type":"access","packs":[{"name":"gitea","urls":["https://gitea.cube.local:8443"]}],"hint":"credentials: cube-idp get secrets"}
{"v":1,"ts":"…","type":"run_done","ok":false,"dur_ms":123456}
{"v":1,"ts":"…","type":"diagnosis","code":"CUBE-3004","summary":"…","cause":"…","remediation":"…","raw":"…"}
```

`step_done.dur_ms` omitted when 0. `diagnosis.code/summary/remediation` come
from the `*diag.Error` when present; `diagnosis.cause` is
`diag.Error.Cause.Error()` when `Cause` is non-nil (`Cause` is an `error`,
not a string) and the field is **omitted** when `Cause` is nil; `raw`
(err.Error()) is always set.
`diagnosis` is a first-class event type CI systems can annotate PRs from
(Terraform's `diagnostic` precedent) and is always the last line on failure.

---

## 6. Mode resolution — the resolve ladder

### 6.1 Modes

`internal/ui`'s `Mode` gains two values; existing constants keep their order
and numeric values (no test churn):

```go
const (
	ModeStyled Mode = iota // rich (auto-resolved): styled static output; LiveRenderer on event-stream commands; per-writer downgradeable (§6.4)
	ModePlain              // the byte-stable phase-1 projection
	ModeJSON               // machine: JSON-lines event stream (stage A: up/down); JSON documents (stage B: status/doctor/get secrets); plain projection elsewhere
	ModeLive               // explicitly forced live (--progress=live / CUBE_IDP_PROGRESS=live only): LiveRenderer even on a non-TTY — the ONLY mode that bypasses the per-writer downgrade
)
```

`ModeLive` and `ModeStyled` render identically on a real terminal; they
differ only in downgrade behavior. Auto-detection can only ever produce
`ModeStyled` (downgradeable), and an explicit `live` request is the sole
producer of `ModeLive` — so `NewFor`/`RunPipeline` can distinguish "the user
demanded live" from "live because a TTY was detected", which a single
`ModeStyled` value could not represent.

### 6.2 The ladder

Single resolve, highest rung wins (codifies gh/buildx/terraform practice +
clig.dev + no-color.org):

1. `--progress=json` → `ModeJSON`
2. `--progress=plain` → `ModePlain`
3. `--progress=live` → `ModeLive` (explicit force — works even on a
   non-TTY, the `GH_FORCE_TTY` analog; `ModeLive` is the only value that
   bypasses the per-writer downgrade in §6.4)
4. `--plain` → `ModePlain` (**permanent alias** for `--progress=plain`;
   never deprecated — existing docs, tests, and muscle memory keep working)
5. `CUBE_IDP_PROGRESS` ∈ {`plain`,`live`,`json`} → `ModePlain` / `ModeLive` /
   `ModeJSON` respectively (CI images set policy once — the
   `BUILDKIT_PROGRESS` precedent). Value `auto`, empty, or unrecognized →
   fall through.
6. stdout not a TTY → `ModePlain`
7. `$CI` set (non-empty) → `ModePlain`
8. `$NO_COLOR` set (present at all, even empty — no-color.org semantics) or
   `TERM` = `dumb`/unset → `ModePlain` (the strictest reading: plain, not
   merely uncolored — this also fixes today's gap where styled mode colors
   output on a TTY despite `NO_COLOR`)
9. → `ModeStyled` (the rich-by-default decision)

`--progress` and `--plain` conflicts: `--progress` wins (more specific);
document, don't error.

### 6.3 Resolve signature

`Resolve` stays a pure, side-effect-free function (the existing
`TestResolve` table extends; no real terminal or env lookups inside):

```go
// Request carries every input the resolve ladder consults. cmd/root.go
// fills it exactly once, in PersistentPreRunE.
type Request struct {
	ProgressFlag string // --progress value; "" or "auto" = not forced (flag ships in stage B, field exists from stage A)
	PlainFlag    bool   // --plain, the permanent alias
	EnvProgress  string // $CUBE_IDP_PROGRESS
	IsTTY        bool   // ui.IsTerminal(os.Stdout)
	CIEnv        string // $CI
	NoColor      bool   // $NO_COLOR present (os.LookupEnv ok-bool)
	Term         string // $TERM
}

func Resolve(r Request) Mode
```

The existing 3-arg `Resolve(plainFlag, isTTY, ciEnv)` is replaced; its five
`TestResolve` cases map 1:1 onto `Request` values and must keep passing with
identical outcomes.

### 6.4 Selection point and plumbing

Selection happens **once**, in `cmd/root.go`'s `PersistentPreRunE` — the
exact place `ui.PlainFlag = plain` is set today:

```go
PersistentPreRunE: func(*cobra.Command, []string) error {
	_, noColor := os.LookupEnv("NO_COLOR")
	ui.SetMode(ui.Resolve(ui.Request{
		ProgressFlag: progress, // stage B; "" in stage A
		PlainFlag:    plain,
		EnvProgress:  os.Getenv("CUBE_IDP_PROGRESS"),
		IsTTY:        ui.IsTerminal(os.Stdout),
		CIEnv:        os.Getenv("CI"),
		NoColor:      noColor,
		Term:         os.Getenv("TERM"),
	}))
	return nil
},
```

- `ui.SetMode(Mode)` / `ui.CurrentMode() Mode` replace the package var
  `ui.PlainFlag` (which is deleted in stage A).
- Every `ui.New(w, ui.PlainFlag)` call site (`internal/doctor/doctor.go`,
  `internal/diff/diff.go`, `internal/upgrade/plan.go`, `cmd/status.go`,
  `cmd/get.go`, `cmd/cnoe.go`, `internal/up` until its Console migration)
  becomes `ui.NewFor(w)`:

```go
// NewFor builds a Printer for out from the process-wide resolved mode,
// downgraded per-writer: auto-resolved styled output only ever reaches a
// real terminal; only an explicit ModeLive skips that check.
func NewFor(out io.Writer) *Printer {
	m := CurrentMode()
	switch {
	case m == ModeJSON:
		m = ModePlain // a Printer has no JSON form (§2 table)
	case m == ModeLive:
		m = ModeStyled // explicit force: the ONLY path that skips the TTY downgrade
	case m == ModeStyled && !IsTerminal(out):
		m = ModePlain // per-writer downgrade: auto-styled never reaches a non-terminal
	}
	return &Printer{out: out, mode: m}
}
```

- **Per-writer downgrade rule (load-bearing):** even when the resolved mode
  is `ModeStyled`, a writer that is not a real terminal renders plain. This
  is exactly today's `New(out, plain)`+`IsTerminal` behavior and is what
  keeps every unit test (bytes.Buffer), every e2e pipe, and every CI log
  byte-stable with zero plumbing. The sole exception is `ModeLive`
  (producible only by `--progress=live` / `CUBE_IDP_PROGRESS=live`, ladder
  rungs 3/5), which forces the live/styled path regardless of writer — an
  explicit, documented escape hatch that auto-detection can never trigger.
- `New(out io.Writer, plain bool)` is kept for tests (its `plain=true` form
  is used throughout `ui_test.go`) but production call sites move to
  `NewFor`.
- `--progress` flag registration (stage B): a persistent string flag on the
  root command, value set `auto|plain|live|json`, default `auto`, validated
  in PersistentPreRunE (unknown value → a `diag` preflight error). Help text
  labels `json` **experimental**.

---

## 7. Charm v2 migration boundary

Policy (Owner Decision #15): `github.com/charmbracelet/*` v1 →
`charm.land/*` v2 lands **in the same PR as the first live view** — i.e.
inside Task 14b. There is no separate mechanical-migration PR, and v1/v2
import paths never coexist.

Current state (`go.mod`): direct deps `github.com/charmbracelet/huh v1.0.0`,
`github.com/charmbracelet/lipgloss v1.1.0`; indirect
`github.com/charmbracelet/bubbletea v1.3.6`, `bubbles v0.21.x`.

14b flips, in one PR:

| Dep | From | To | Files touched |
|---|---|---|---|
| lipgloss | `github.com/charmbracelet/lipgloss` v1.1.0 | `charm.land/lipgloss/v2` | `internal/ui/ui.go` (style vars; API is source-compatible for the `NewStyle().Bold().Foreground()` subset used) |
| huh | `github.com/charmbracelet/huh` v1.0.0 | `charm.land/huh/v2` | `cmd/init.go` only — the 3-field form migrates **mechanically, unchanged in shape**; the multi-group wizard expansion is stage B |
| bubbletea | indirect v1.3.6 | `charm.land/bubbletea/v2` (becomes **direct**) | new LiveRenderer code only |
| bubbles | indirect | `charm.land/bubbles/v2` (direct: spinner, table) | new LiveRenderer code only |

Rejected (research §1, for the record): `fang` (experimental dep),
`pterm` (duplicates ui+lipgloss), `tview` (alt-screen residency),
`tuist` (too young; revisit only if live line volume ever strains the
managed region — `up` emits ~10–20 steps, far inside Bubble Tea's sweet
spot).

---

## 8. What stays byte-identical — the preservation proof

> **OWNER RATIFIED 2026-07-14:** the sanctioned plain-output changes are
> exactly two — the Access summary (§9) and this section's additive `down`
> step lines. Everything else in plain mode is byte-frozen.

The design rule from `internal/ui/ui.go`'s package doc is unchanged and now
covers renderers: *the phase-1 plain format IS the CI/e2e contract.*

Enumerated guarantees:

1. `Step`/`StepDone` plain bytes: `"▸ [%s] %s\n"` — same format string,
   same call-site messages (§4.3 map). Pinned by
   `TestPlainMatchesPhase1Format` + `internal/up/tls_test.go`.
2. Progress silence: `StepStarted`/`StepFailed`/`HealthTick` project to zero
   plain bytes — the `TestProgressPlainEmitsNothingBeforeDone` /
   `TestProgressPlainStopEmitsNothing` invariants restated as renderer
   contract.
3. `Warn`/`Note`: `fmt.Fprintln(out, msg)` — `TestWarnPlainExactLiteral`
   and the raw call sites in `internal/up/up.go` / `cmd/down.go`.
4. `Section`/`Glyph` (static surface, untouched by the stream):
   `TestSectionPlainExactLiteral`, `TestGlyphPlainPassesThrough`,
   `doctor.Render`'s `"%s %s  %s\n    fix: %s\n"`, status's
   `"%s %s Ready\n"`, the tabwriter tables in `cmd/status.go`/`cmd/get.go`
   — all plain paths keep their exact bytes through stage B.
5. `diag.Render` output and `main.go`'s stderr print: byte-identical in
   ModePlain and ModeJSON (§5.2).
6. Non-TTY/CI behavior: unchanged and strengthened (ladder rungs 6–8).

Additive-only changes (new bytes on paths nothing pins):

- `down` gains step lines in plain mode for the first time (`▸ [engine] …`,
  `▸ [cascade] …`, …) — additive: no test pins down's full plain output
  (`cmd/down_test.go` and e2e assert substrings that remain present). This
  is a new-output addition, not a mutation of a pinned contract.

Exactly **one** pinned plain contract changes: §9.

---

## 9. The one deliberate plain-output change: the Access summary

Owner Decision #15 (research §5 item 6, recommended default accepted):
`AccessSummary` stops being styled-only. The Access information becomes
**data** — a first-class event — with a **stable plain projection**, because
"what URLs did I just get" is exactly what scripts and CI want to scrape.

Lands in **stage A** (14b), because the JSON stream ships there and `access`
must be a data event from its first release; doing the plain lines in the
same change keeps it one contract, one test update.

New plain projection (bytes normative; mirrors the styled layout minus ANSI):

```
\nAccess\n
  %-12s %s\n      … one line per pack URL (pack name %-12s-padded, then URL)
  %s\n            … the hint line
```

Example (pack `gitea`, hint as emitted by `up.Run` today):

```
(blank line)
Access
  gitea        https://gitea.cube.local:8443
  credentials: cube-idp get secrets
```

Test updates this entails (complete list — nothing else changes):

1. `internal/ui/ui_test.go` — **`TestAccessSummaryPlainNoOp` is deleted and
   replaced** by `TestAccessSummaryPlainStableLines`, pinning the bytes
   above (and still asserting zero ANSI escapes in plain mode).
2. `internal/up/up.go` — the comment block above the final success
   `fmt.Fprintf` and `AccessSummary` call ("the access summary below is
   additive and styled-only" / "adds zero bytes to plain/CI output") is
   updated to describe the new contract.
3. `tests/e2e/e2e_test.go` — no change required (nothing asserts the absence
   of Access lines); 14b MAY add a positive substring assertion for
   `"Access"` in the plain up output.

JSON projection: the `access` event of §5.3. Styled/live projection: the
existing styled block, unchanged.

---

## 10. Stage B static-surface contracts (the "one console" wave)

All stage B work obeys: plain bytes unchanged (§8 item 4), styled mode
enriched, `ModeJSON` becomes a *document* on request/response commands
(gh-style final object — a stream is wrong for commands that answer once).

- **`status`** (`cmd/status.go`): styled mode renders a lipgloss/bubbles
  static table (glyph, component name, message), the inventory count, and
  the gateway/access URLs — a snapshot that exits immediately (`--watch` is
  explicitly out of scope). ModeJSON: one document, e.g.
  `{"v":1,"cube":"dev","components":[{"name":…,"ready":…,"message":…}],"inventory":{"count":N,"objects":[…] when --details},"ready":bool}`.
  Exit-code behavior (diag error when unhealthy) unchanged in all modes.
- **`doctor`** (`cmd/doctor.go` + `internal/doctor/doctor.go Render`):
  styled mode groups findings by severity in bordered sections, remediation
  styled as copy-paste blocks (remediation text itself stays unstyled), and
  a final one-line verdict. Plain: `doctor.Render`'s exact current bytes.
  ModeJSON: `{"v":1,"findings":[{"code":…,"severity":…,"message":…,"remediation":…}],"errors":bool}` —
  the findings array with codes/severities is CI-annotation gold. Optional
  interactivity (huh "re-run with --verbose?"-style follow-up) only under a
  `wizardApplicable`-equivalent guard (both stdin+stdout TTY): skippable,
  never blocking CI. `os.Exit(1)` semantics unchanged.
- **`init`** (`cmd/init.go`): the 3-field huh form (already on v2 imports
  after 14b) grows into a multi-group huh v2 wizard: cube name (validated by
  the existing `cubeNameRe`, which mirrors `internal/config/schema.cue` —
  the wizard must never accept what `Load()` rejects), provider (kind |
  existing, with a kubeconfig context picker for `existing`), engine
  (flux | argocd), gateway host/port with a port-conflict pre-check via
  `doctor.CheckPortFree(port, clusterExists)`, and a pack multi-select.
  The `wizardApplicable` gate (TTY stdin+stdout, no explicit flags) is
  unchanged; huh v2's in-tree `accessible` mode is enabled form-level (the
  gh screen-reader-prompter precedent).
- **`get secrets`**: ModeJSON document of the secret rows.
  `diff`/`upgrade --plan`: Section/Glyph styling only — no live view, no
  document (their plain text is already diff-shaped).
- **`--progress` flag**: registered (§6.4) — the ladder rungs 1–3 activate.

---

## 11. Staging — the implementable split

### Task 14b implements (stage A — "prove the event stream"):

- [ ] `internal/ui/event`: the §3 vocabulary, ordering rules, marker method.
- [ ] `internal/ui`: `Console`/`ConsoleProgress` facade (§4.3),
      `RunPipeline` (§4.2), `SetMode`/`CurrentMode`/`NewFor`, `Request`-based
      `Resolve` with rungs 4–9 (flag rungs 1–3 dormant: `ProgressFlag` field
      exists, no cobra flag yet), `RenderError` (§5.2), delete `PlainFlag`.
- [ ] `cmd/root.go`: PersistentPreRunE computes `ui.SetMode(ui.Resolve(…))`
      (§6.4); `--plain` flag unchanged.
- [ ] PlainRenderer per §5.1 (golden-stream tested).
- [ ] LiveRenderer per §5.2 for `up` and `down` (inline, spinner + health
      table, diagnosis-last, clean exit).
- [ ] JSONRenderer per §5.3 for `up` and `down` (selected via
      `CUBE_IDP_PROGRESS=json` until 14c ships the flag), documented as
      experimental.
- [ ] `internal/up/up.go`: Console migration per the §4.3 table (signature
      `Run(ctx, cfgPath, con *ui.Console)`); `waitHealthy` emits
      `HealthTick`s; `cmd/up.go` wraps with `RunPipeline`.
- [ ] `cmd/down.go`: RunPipeline wrap; emit `StepStarted`/`StepDone` pairs
      for stages `engine`, `dns`, `cascade`, `cluster`; `revertTrust` lines
      become `Note`s (byte-identical) plus a `trust` step on success.
- [ ] `main.go`: `diag.Render` → `ui.RenderError` (§5.2).
- [ ] The Access plain change + its named test updates (§9).
- [ ] Charm v2 migration, same PR (§7), including the mechanical
      `cmd/init.go` huh v1→v2 flip (form unchanged).
- [ ] All other `ui.New(w, ui.PlainFlag)` call sites → `ui.NewFor(w)`
      (mechanical; plain bytes proven unchanged).
- [ ] Tests: §12 stage-A rows.

### Task 14c implements (stage B — "one console everywhere"):

- [ ] `--progress=auto|plain|live|json` persistent flag + validation +
      ladder rungs 1–3 live; help text marks `json` experimental (§6).
- [ ] `status` rich static render + JSON document (§10).
- [ ] `doctor` severity-grouped render + JSON document + optional TTY-gated
      follow-up (§10).
- [ ] `init` multi-group huh v2 wizard incl. `CheckPortFree` pre-check and
      accessible mode (§10).
- [ ] `get secrets` JSON document; `diff`/`upgrade --plan` Section/Glyph
      polish only (§10).
- [ ] Docs page for the JSON event schema + document schemas (experimental
      label, Terraform-style layout).
- [ ] Tests: §12 stage-B rows.

Out of scope for both: `sync --watch` (design note §4.4 only), any
alt-screen/persistent view, `fang`-style help theming, "apply fix" doctor
actions.

---

## 12. Testing contracts

Renderers are independently testable: golden tests feed a **recorded
`[]event.Event` slice** into each renderer and assert output.

Stage A:

- Golden plain stream: a canonical up-run slice (all event types) →
  PlainRenderer → exact-bytes golden (must reproduce §5.1's table; asserts
  zero ANSI escapes).
- Golden JSON stream: same slice → JSONRenderer → one-object-per-line
  golden; failure slice asserts `diagnosis` is the final line.
- LiveRenderer: model-level unit tests (event → expected
  `tea.Println` content / region state); `teatest` frame assertions
  optional, not required.
- `TestResolve` extended: every ladder rung 4–9 — including
  `CUBE_IDP_PROGRESS=live` → `ModeLive` (rung 5) and, dormant-field, the
  rung-3 `--progress=live` → `ModeLive` case — plus the auto rung never
  producing `ModeLive`. `NewFor` downgrade matrix: bytes.Buffer under
  `SetMode(ModeStyled)` → plain; under `SetMode(ModeLive)` → styled (the
  bypass); `ModeJSON` → `ModePlain` Printer downgrade.
- RunPipeline lifecycle: goroutine-leak check (e.g. `goleak`-style or
  before/after `runtime.NumGoroutine` discipline) proving nothing survives;
  failure-path test proving event order `StepFailed → RunDone{false} →
  Diagnosis` and that the producer's error is returned verbatim.
- The §9 test swap: `TestAccessSummaryPlainNoOp` →
  `TestAccessSummaryPlainStableLines`.
- Must pass unchanged: every other test in `internal/ui/ui_test.go`,
  `internal/up/tls_test.go`, `cmd/down_test.go`, `tests/e2e/e2e_test.go`.

Stage B:

- Document-mode goldens for `status`/`doctor`/`get secrets` JSON.
- Plain-bytes regression: `status`/`doctor` plain output before/after the
  rich render (byte-identical).
- `--progress` flag validation (unknown value → diag error) and precedence
  over `--plain` and `CUBE_IDP_PROGRESS`.
- Wizard: `wizardApplicable` gating unchanged (piped init never prompts —
  the e2e suite pipes it); name validation parity with `schema.cue`.

---

## 13. Exemplar record (why these choices)

- **BuildKit / buildx** — the architecture itself (one canonical event
  stream, renderers as interchangeable consumers with a mode enum), the
  `--progress` knob + env-var policy override, and the one-event-per-line
  JSON guarantee (their rawjson batching bug, moby/buildkit #4769, is why
  §5.3 forbids batching from day one).
- **Terraform machine-readable UI** — versioned, documented JSON lines with
  `diagnostic` as a first-class typed event; the model for treating the JSON
  stream as a product surface with a compatibility promise (ours deferred to
  the D5 freeze).
- **gh** — independent stdin/stdout TTY checks (`wizardApplicable` already
  does this), `NO_COLOR` honor, accessible prompter built on huh, and
  spinner→static-text as a degradation tier (our plain projection of
  `StepStarted` = zero bytes, resolved by the `StepDone` line).
- **Dagger** — the UX north star sentence ("shows live progress, then leaves
  all the output in your terminal scrollback") and the cautionary tale: keep
  live line volume small (we're ~20 steps), never capture the mouse, never
  fight scrollback.
- **Tilt** — the direct evidence against persistent/alt-screen dashboards
  (retired to `--legacy`); why Proposal C was rejected and why `RunDone`
  collapses the live region instead of lingering.
