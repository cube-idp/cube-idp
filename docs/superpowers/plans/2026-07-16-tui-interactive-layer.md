# TUI Interactive Layer (Approach B) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **This file is the persistent ledger** — see "Agent Execution Protocol" below. The dispatch prompt lives in [2026-07-16-tui-agent-prompt.md](2026-07-16-tui-agent-prompt.md).

**Goal:** Deliver the full interactive terminal-UI layer specified in
[docs/superpowers/specs/2026-07-16-tui-interactive-layer-design.md](../specs/2026-07-16-tui-interactive-layer-design.md)
— adaptive theme, widened events, live step tree, one prompt seam, consent
flows, menus, watch mode, styled help — with the spec's §2 **Target
Experience frames (TE-1…TE-4) as the mandatory acceptance contract**.

**Architecture:** Everything plugs into existing seams: the event stream
(`internal/ui/event` + `Console` + `RunPipeline`), the renderer picker
(`internal/ui/pipeline.go`), and the huh-v2 precedent (`cmd/init.go`).
Producers never render; all richness routes through `ModeStyled`/`ModeLive`
on a real TTY; plain/JSON projections change only via the three ratified
deviations R1/R2/R3 (spec §5).

**Tech Stack:** Go, cobra, `charm.land/bubbletea/v2 v2.0.8`,
`bubbles/v2 v2.1.1`, `lipgloss/v2 v2.0.5`, `huh/v2 v2.0.3` (all already in
`go.mod`, **never bump mid-project**); new pinned deps only in W2.T13
(`charm.land/fang/v2`).

---

## Agent Execution Protocol (normative — read fully before any work)

Every task is executed by one dispatched agent in an **isolated git
worktree** on a **task-specific feature branch**, with this plan file as the
**shared ledger** committed on `main`. Any agent must be able to pick up the
next task cold, with no context beyond this file, the spec, and git history.

**Repo root** (contains `go.mod` and `cube.yaml`):
`/Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/neocube`
— referred to as `$ROOT` below.

### Branch & worktree naming (mandatory)

- Branch: `tui/w<wave>-t<NN>-<slug>` — e.g. `tui/w1-t01-theme`. The wave and
  zero-padded task number MUST appear in the branch name.
- Worktree: `$ROOT/.claude/worktrees/<branch-slug>` — e.g.
  `$ROOT/.claude/worktrees/w1-t01-theme`. (`.claude/worktrees/` is already
  gitignored.)

### Task lifecycle (mandatory, in this order)

1. **Identify your task.** The first `### W…T…` section whose Outcome block
   says `STATUS: UNCLAIMED` (or `BLOCKED` if you were dispatched to unblock
   it). Tasks execute **strictly in plan order** — a task may start only
   when every earlier task's STATUS is `DONE` or `DONE_WITH_CONCERNS`.
   Cross-check `git log --oneline -20`: if the task's work already exists in
   git, do NOT redo it — fill the Outcome block from the evidence, tick the
   boxes, commit the ledger, report DONE.
2. **Claim it (before any code).** In `$ROOT` on `main` with a clean tree:
   edit ONLY this task's `STATUS:` line to
   `IN_PROGRESS(<agent-or-session-id>, <UTC timestamp>)`, then
   `git add docs/superpowers/plans/2026-07-16-tui-interactive-layer.md && git commit -m "docs: tui plan — claim W<w>.T<NN>"`.
   If the STATUS is already `IN_PROGRESS` with a timestamp under 24h old,
   STOP and report — another agent owns it.
3. **Create the worktree** from current `main`:
   `git -C $ROOT worktree add $ROOT/.claude/worktrees/<slug> -b tui/w<w>-t<NN>-<slug> main`
   All code work happens inside the worktree. Never edit code in `$ROOT`'s
   checkout; never edit this plan file from the worktree.
4. **Execute the task's steps in order** — TDD as written (failing test →
   verify fail → implement → verify pass → commit). Every commit goes on the
   task branch with the exact conventional message the step specifies, ending
   with the project's standard trailer if one is configured. Run each
   verification command and compare against its "Expected" line. If a result
   does not match: stop, record it under FINDINGS/BLOCKERS, and follow the
   Outcome rules below. No workarounds, no force-pushes.
5. **Task-level verification** — every task ends with:
   `go build ./... && go vet ./... && go test ./...` inside the worktree.
   Expected: all pass. TE-tagged tests (`go test ./internal/ui/... ./cmd/... -run TE`)
   must pass for every task that touches a Target Experience frame — this is
   the spec's mandatory conformance gate (spec §6.1).
6. **Merge back** (only when everything above is green):
   ```
   cd $ROOT
   git status --porcelain        # must be empty
   git branch --show-current     # must be main
   git merge --no-ff tui/w<w>-t<NN>-<slug> -m "merge: tui W<w>.T<NN> <slug> (tui/w<w>-t<NN>-<slug>)"
   go test ./...                 # post-merge sanity in $ROOT
   git worktree remove $ROOT/.claude/worktrees/<slug>
   ```
   Do NOT `git push` — merges stay local unless the dispatcher says
   otherwise. Do NOT delete the branch (it documents the task).
7. **Close the ledger.** In `$ROOT` on `main`: tick every checkbox of YOUR
   task, fill the complete Outcome block (see template), then
   `git add docs/superpowers/plans/2026-07-16-tui-interactive-layer.md && git commit -m "docs: tui plan — W<w>.T<NN> complete"`.
   Also append one line to `.superpowers/sdd/progress.md` in the same commit
   if that ledger is in use.
8. **Report** to the dispatcher: STATUS / Task / Branch / Commits (hashes +
   messages) / Evidence (key verification commands + actual output lines) /
   Handoff.

### Outcome block rules

Each task carries an `#### Outcome` block. It is the ONLY part of this file
an executing agent may edit (plus its own task's checkboxes). Statuses:

- `UNCLAIMED` → nobody has started.
- `IN_PROGRESS(<who>, <UTC ts>)` → claimed; stale after 24h.
- `DONE` → merged to main, all boxes ticked, verification green.
- `DONE_WITH_CONCERNS` → merged, but FINDINGS contains items the owner
  should read.
- `BLOCKED` → work stopped; worktree and branch LEFT IN PLACE; BLOCKERS
  says exactly what command failed with what output and what is needed.

FINDINGS must record every deviation from the plan's literal steps (API
drift, different line numbers, renamed symbols), every decision made, and
anything the next agent must know. Never leave a field as `—` on a
non-UNCLAIMED status: write `none` explicitly.

### If you are BLOCKED

Set STATUS, fill BLOCKERS (command, actual output, diagnosis), commit the
ledger on main, leave the worktree+branch intact, report, stop. Never merge
red work to main.

---

## Global Constraints (bind every task — copied from spec §4 + Decisions)

- **Frozen:** byte-frozen plain projection (except R1/R2), JSONL additive
  only, `-o json` documents untouched, resolve-ladder precedence, CUBE-XXXX
  codes append-only, diagnosis-last ordering, `ExitCodeFor` plugin
  passthrough, nil-input rule for live on pipes, CUBE-7104 refusal
  semantics.
- **Inline forever:** `AltScreen` never set on any Bubble Tea view.
- **Prompt doctrine:** no prompt without dual-TTY + rich mode + no
  suppressing flag; every prompt has a flag twin; prompts run BEFORE
  `RunPipeline`, never inside a producer.
- **Charm versions pinned:** bubbletea v2.0.8 / bubbles v2.1.1 / lipgloss
  v2.0.5 / huh v2.0.3 — do not bump.
- **CI must never hang:** every prompt-capable path gets a
  buffer-stdin-never-blocks test with a timeout.
- **Semantic colors:** basic ANSI 16 only, glyph + word always paired.
- **TE conformance:** spec §2 frames are merge gates via the §6.1 test
  matrix. Golden fixtures live in `internal/ui/render/testdata/te*.golden`.
- Historical docs under `docs/superpowers/` and `.superpowers/` are
  edit-forbidden except this plan's checkboxes/Outcome blocks.
- Local e2e note: a squatting kind cluster owns 8443 on this machine — use
  `CUBE_IDP_E2E_GATEWAY_PORT=18443` if you run e2e locally (unit tests are
  the default gate; e2e only where a step says so).

## Task Index

| Task | Branch | Delivers | Spec |
|------|--------|----------|------|
| W1.T01 | `tui/w1-t01-theme` | `internal/ui/theme` + rewire 6 palette sites | WP1 |
| W1.T02 | `tui/w1-t02-events` | StepFailed{Msg,Dur}, Index/Total, StepLog, console/producer plumbing | WP2 |
| W1.T03 | `tui/w1-t03-epilogue` | `event.Epilogue` + **R2** (glyph out of content) | WP2, TE-4 |
| W1.T04 | `tui/w1-t04-startlines` | **R1** plain/styled StepStarted start lines | WP2, P12 |
| W1.T05 | `tui/w1-t05-livetree` | Live step tree: TE-1/TE-2/TE-4 renderer + goldens | WP3 |
| W1.T06 | `tui/w1-t06-prompts` | `internal/ui/prompt.go` seam + trust/plugin migrations | WP4 |
| W1.T07 | `tui/w1-t07-consent` | down preview + typed consent + **R3**; upgrade apply-confirm | WP5, TE-3 |
| W1.T08 | `tui/w1-t08-exitpaths` | exit sentinel, CUBE-0009, `RenderErrorTo` | WP9 |
| W1.T09 | `tui/w1-t09-spinner-sweep` | delete hand-rolled spinner; wave-1 sweep | WP9 |
| W2.T10 | `tui/w2-t10-explain` | `cube-idp explain CUBE-XXXX` + TE-2.3 box footer | WP8 |
| W2.T11 | `tui/w2-t11-packmenu` | `pack install` MultiSelect menu | WP6 |
| W2.T12 | `tui/w2-t12-watch` | `status --watch` (--interval/--exit-status/--compact) | WP7 |
| W2.T13 | `tui/w2-t13-fang-color` | fang help/errors; NO_COLOR/CLICOLOR/--color compliance | WP8 |
| W2.T14 | `tui/w2-t14-fence-docs` | mode-matrix fence, README, VHS tapes | WP10 |

---

### W1.T01: `internal/ui/theme` — one adaptive palette, glyphs, stage metadata

**Branch:** `tui/w1-t01-theme` · **Depends:** none

**Files:**
- Create: `internal/ui/theme/theme.go`, `internal/ui/theme/theme_test.go`
- Modify: `internal/ui/ui.go` (style vars at :193–196, :238–243, :285),
  `internal/ui/rendererr.go` (:65–70), `internal/ui/render/styled.go`
  (:18–22, :66), `internal/ui/render/live.go` (:66–74),
  `cmd/status.go` (:135–138), `internal/doctor/doctor.go` (:119–123)

**Interfaces (later tasks rely on these exact names):**
- Produces: `theme.Theme{Badge, Msg, OK, Err, Warn, Dim, Section, ErrPanel, ErrLabel lipgloss.Style}`;
  `theme.New(isDark bool) Theme`; `theme.Detect(in, out *os.File) Theme`;
  glyph consts `theme.GlyphStep/GlyphOK/GlyphErr/GlyphWarn`;
  `theme.Stages map[string]theme.StageMeta` and `theme.BadgeWidth() int`.

- [x] **Step 1: Write the failing test** — `internal/ui/theme/theme_test.go`:

```go
package theme

import (
	"strings"
	"testing"
)

// Light and dark palettes must actually differ (adaptivity is the point),
// and every semantic color must stay in the basic ANSI-16 range (spec §2
// color-roles rule: user terminal themes keep control).
func TestThemeLightDarkDiffer(t *testing.T) {
	light, dark := New(false), New(true)
	if light.Badge.Render("[x]") == dark.Badge.Render("[x]") {
		t.Fatal("light and dark Badge render identically — LightDark pairs not applied")
	}
}

func TestBadgeWidthCoversAllStages(t *testing.T) {
	w := BadgeWidth()
	for name := range Stages {
		if len(name)+2 > w { // "[" + name + "]"
			t.Fatalf("stage %q wider than BadgeWidth %d", name, w)
		}
	}
	if !strings.Contains(GlyphOK, "✔") {
		t.Fatal("GlyphOK changed — glyph set is normative (spec §2)")
	}
}
```

- [x] **Step 2: Run it to verify it fails**
Run: `go test ./internal/ui/theme/ -v`
Expected: FAIL — `package internal/ui/theme` does not exist yet.

- [x] **Step 3: Implement `internal/ui/theme/theme.go`** (complete file; a
LEAF package — imports only lipgloss v2, x/term, stdlib):

```go
// Package theme is the single source of cube-idp's terminal look: one
// background-adaptive palette, one glyph set, and per-stage presentation
// metadata. It is a LEAF package (lipgloss + x/term + stdlib only) so both
// internal/ui and internal/ui/render can import it — dissolving the cycle
// that forced render/styled.go to duplicate ui.go's styles.
package theme

import (
	"os"

	lipgloss "charm.land/lipgloss/v2"
	"golang.org/x/term"
)

// Glyphs — the single normative set (spec §2). Renderers own glyphs; event
// content never carries them (spec R2).
const (
	GlyphStep = "▸"
	GlyphOK   = "✔"
	GlyphErr  = "✗"
	GlyphWarn = "⚠"
)

// Theme is the semantic style set. Foregrounds stay inside the basic
// ANSI-16 range so user terminal palettes keep control; meaning never rides
// on color alone (glyph + word are always paired by callers).
type Theme struct {
	Badge    lipgloss.Style // [stage] badges
	Msg      lipgloss.Style // step message text
	OK       lipgloss.Style // success
	Err      lipgloss.Style // failure
	Warn     lipgloss.Style // advisories, spinner
	Dim      lipgloss.Style // durations, hints, log tails
	Section  lipgloss.Style // headings
	ErrPanel lipgloss.Style // CUBE-xxxx box border
	ErrLabel lipgloss.Style // "cause:"/"fix:" labels
}

// New builds the palette for a dark or light background. Pure — tests and
// the live program (via tea.BackgroundColorMsg) call it directly.
func New(isDark bool) Theme {
	ld := lipgloss.LightDark(isDark)
	return Theme{
		Badge:    lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("4"), lipgloss.Color("12"))),
		Msg:      lipgloss.NewStyle().Foreground(ld(lipgloss.Color("0"), lipgloss.Color("7"))),
		OK:       lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("2"), lipgloss.Color("10"))),
		Err:      lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("1"), lipgloss.Color("9"))),
		Warn:     lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("3"), lipgloss.Color("11"))),
		Dim:      lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		Section:  lipgloss.NewStyle().Bold(true),
		ErrPanel: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ld(lipgloss.Color("1"), lipgloss.Color("9"))).Padding(0, 1),
		ErrLabel: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
}

// Detect resolves the background ONCE for static (non-Bubble-Tea) output.
// Guarded to real TTYs; any doubt = dark, so the worst case equals today's
// dark-assuming palette (OSC queries can hang under tmux — never query a
// non-terminal). Inside a live program use tea.RequestBackgroundColor
// instead; never both.
func Detect(in, out *os.File) Theme {
	if in == nil || out == nil ||
		!term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return New(true)
	}
	return New(lipgloss.HasDarkBackground(in, out))
}

// StageMeta attaches presentation to the open stage-name strings documented
// in internal/ui/event/event.go. Producers never change for presentation.
type StageMeta struct {
	Group string // phase grouping for the live tree
}

// Stages covers every stage name event.go documents plus down's stages.
// An unknown stage renders fine — metadata is additive decoration.
var Stages = map[string]StageMeta{
	"config": {Group: "prepare"}, "ca": {Group: "prepare"},
	"cluster": {Group: "cluster"}, "registry": {Group: "cluster"},
	"packs-crd": {Group: "engine"}, "engine": {Group: "engine"},
	"tls": {Group: "gateway"}, "pack": {Group: "packs"},
	"packs": {Group: "packs"}, "lock": {Group: "packs"},
	"dns": {Group: "gateway"}, "health": {Group: "verify"},
	"cnoe": {Group: "packs"}, "cascade": {Group: "teardown"},
	"trust": {Group: "teardown"},
}

// BadgeWidth is the fixed "[stage]" column width (TE-1.2 alignment).
func BadgeWidth() int {
	w := 0
	for name := range Stages {
		if n := len(name) + 2; n > w {
			w = n
		}
	}
	return w
}
```

- [x] **Step 4: Run the theme tests**
Run: `go test ./internal/ui/theme/ -v`
Expected: PASS (2 tests). If `lipgloss.LightDark` or `HasDarkBackground`
does not exist under these names in the pinned lipgloss v2.0.5, STOP:
run `go doc charm.land/lipgloss/v2 | grep -i 'lightdark\|darkbackground'`,
use the actual v2 symbol, and record the exact name in FINDINGS.

- [x] **Step 5: Commit**
`git add internal/ui/theme/ && git commit -m "feat(ui): add theme leaf package — adaptive palette, glyphs, stage metadata"`

- [x] **Step 6: Rewire the six duplication sites.** In each file below,
delete the local `lipgloss.NewStyle()` var blocks and replace uses with a
package-level `var th = theme.Detect(os.Stdin, os.Stdout)` (in `cmd/` and
`internal/doctor`) or a `th theme.Theme` passed/held where a struct already
exists. Mapping (old → new): `stepBadgeStyle/styledBadgeStyle/liveBadgeStyle
→ th.Badge`; `stepMsgStyle/styledMsgStyle/liveMsgStyle → th.Msg`;
`okStyle/liveOKStyle → th.OK`; `errStyle/liveErrStyle/errPanelGlyphStyle/
errPanelCodeStyle → th.Err`; `warnStyle/liveWarnStyle/styledWarnStyle/
progressStyle → th.Warn`; `liveDimStyle/errPanelLabelStyle → th.Dim` /
`th.ErrLabel`; `sectionStyle/styledBadgeSectionStyle/liveHeaderStyle →
th.Section`; `errPanelStyle → th.ErrPanel`. In `render/live.go` store the
Theme ON the model (`liveModel.th`, initialized `theme.New(true)` — T05
makes it background-adaptive). Delete render/styled.go's "deliberate
duplication" comment block (:12–17) — the cycle it documents is dissolved.
ModePlain paths never touch theme, so plain bytes cannot change.

- [x] **Step 7: Verify no stray palettes, all tests green**
Run: `grep -rn '"39"\|"42"\|"196"\|"214"\|"245"\|"240"' internal/ cmd/ --include='*.go' | grep -v _test.go`
Expected: zero hits (theme.go itself uses only ANSI-16 values).
Run: `go build ./... && go test ./...`
Expected: all pass — styled tests compare ANSI-stripped content
(`stripANSI` in `internal/ui/render/styled_test.go`), so color-value churn
is invisible to them. If any test pins exact ANSI bytes, update it in the
same commit and record it in FINDINGS.

- [x] **Step 8: Commit**
`git add -A && git commit -m "refactor(ui): route all styling through internal/ui/theme — six palettes become one"`

- [x] **Step 9: Task-level verify + merge + ledger** (protocol steps 5–7).

#### Outcome — W1.T01 (ledger; the executing agent MUST fill this)
- STATUS: `DONE`
- BRANCH: `tui/w1-t01-theme` (merged: yes — cec3e8b)
- COMMITS: `ef9493d` feat(ui): add theme leaf package — adaptive palette, glyphs,
  stage metadata · `48b5b1d` refactor(ui): route all styling through
  internal/ui/theme — six palettes become one · `cec3e8b` merge: tui W1.T01
  theme (tui/w1-t01-theme)
- FINDINGS:
  - lipgloss v2.0.5 API names match the plan exactly: `lipgloss.LightDark(isDark)
    LightDarkFunc` and `lipgloss.HasDarkBackground(in, out term.File) bool`
    (verified via `go doc`). No API drift, no dependency changes
    (`golang.org/x/term v0.45.0` was already in go.mod).
  - Deviation (scope of the `th` var): the plan prescribed package-level
    `var th = theme.Detect(os.Stdin, os.Stdout)` only for `cmd/` and
    `internal/doctor`, struct-held elsewhere. `internal/ui` (used by
    package-level `renderErrorForMode`) and `internal/ui/render` (used by
    package-level `scrollbackLine`, declared in styled.go) also got the
    package-level var, since no struct exists on those paths. `liveModel`
    holds its own `th theme.Theme` field initialized `theme.New(true)` per
    the plan (T05 makes it adaptive via tea.BackgroundColorMsg).
  - Deviation (doctor): `doctorPanelStyle` (colorless rounded border) was
    KEPT as a local style — it borders Warning/Note groups too, so
    `th.ErrPanel`'s red border would be wrong, and it duplicates no palette
    value. `doctorFixStyle` ("fix:" label, 245) mapped to `th.ErrLabel`
    (matching rendererr's label mapping), not `th.Msg`.
  - Mapping notes: `statusHeaderStyle → th.Section`, `statusDimStyle →
    th.Msg`, `progressStyle → th.Warn` — all as the plan's table implies.
  - Zero test updates were needed: no test pins exact ANSI bytes; the full
    suite passed untouched (plain-mode paths never touch theme).
  - Behavior note: on a real TTY, `theme.Detect` runs
    `lipgloss.HasDarkBackground` once at package init (OSC query, guarded to
    real TTYs, dark default on any doubt). Non-TTY/CI paths are byte-identical
    to before. Styled/live color values moved from 256-palette (39/42/196/
    214/245/240) to basic ANSI-16 LightDark pairs — the sanctioned WP1 change.
- REVIEW: `go test ./internal/ui/theme/ -v` red before implementation
  (undefined symbols), green after (2 tests). Acceptance grep
  `grep -rn '"39"\|"42"\|"196"\|"214"\|"245"\|"240"' internal/ cmd/
  --include='*.go' | grep -v _test.go` → zero hits. `go build ./... && go vet
  ./... && go test ./...` green in the worktree pre-merge and `go test ./...`
  green in $ROOT post-merge. No TE frame touched by this task (TE gate n/a —
  TE tests arrive in T05).
- BLOCKERS: none
- HANDOFF: `theme.Theme{Badge,Msg,OK,Err,Warn,Dim,Section,ErrPanel,ErrLabel}`,
  `theme.New(isDark)`, `theme.Detect(in,out)`, glyphs
  `GlyphStep/GlyphOK/GlyphErr/GlyphWarn`, `theme.Stages`, `theme.BadgeWidth()`
  all exist under the exact planned names. Package `render` has a package-level
  `th` (declared in styled.go) that `scrollbackLine` already uses; `liveModel.th`
  is fixed dark until T05. `ui.GlyphOK/GlyphErr/GlyphWarn` constants still
  exist separately in package ui (unifying them onto theme was not in scope).
  Branch `tui/w1-t01-theme` left in place per protocol.

---

### W1.T02: Event widening — failures carry Msg/Dur, pack N/M, StepLog

**Branch:** `tui/w1-t02-events` · **Depends:** W1.T01

**Files:**
- Modify: `internal/ui/event/event.go`, `internal/ui/console.go`,
  `internal/ui/pipeline.go` (unwind guard ~:113–118),
  `internal/ui/render/json.go` (+`json_test.go`),
  `internal/ui/render/plain.go`, `internal/ui/render/styled.go`,
  `internal/ui/render/live.go` (compile-only arms),
  `internal/up/up.go` (pack loop ~:250–307)

**Interfaces:**
- Produces: `event.StepFailed{Stage, Msg string; Dur time.Duration; Err *diag.Error}`;
  `event.StepStarted{Stage, Msg string; Index, Total int}`;
  `event.StepDone{Stage, Msg string; Dur time.Duration; Index, Total int}`;
  `event.StepLog{Stage, Line string}`;
  `Console.ProgressN(stage, msg string, index, total int) *ConsoleProgress`;
  `Console.Log(stage, format string, args ...any)`.
- Consumed by: T03 (Epilogue rides the same JSON window), T04 (start lines
  read Index/Total), T05 (live renderer renders everything here).

- [x] **Step 1: Failing tests.** Add to `internal/ui/render/json_test.go`
(match the file's existing golden/clock style — read it first):

```go
// StepFailed must carry msg/dur_ms; steps carry idx/of — all additive
// under the v1-EXPERIMENTAL window (spec WP2).
func TestJSONStepFailedCarriesMsgAndDur(t *testing.T) {
	var buf bytes.Buffer
	project := JSONWithClock(&buf, fixedClock(t))
	project(event.StepFailed{Stage: "packs", Msg: "gitea@0.1.0 pull failed", Dur: 4 * time.Second})
	line := buf.String()
	for _, want := range []string{`"type":"step_failed"`, `"msg":"gitea@0.1.0 pull failed"`, `"dur_ms":4000`} {
		if !strings.Contains(line, want) {
			t.Fatalf("step_failed line missing %s: %s", want, line)
		}
	}
}

func TestJSONStepCarriesIndexTotal(t *testing.T) {
	var buf bytes.Buffer
	project := JSONWithClock(&buf, fixedClock(t))
	project(event.StepStarted{Stage: "pack", Msg: "delivering gitea", Index: 3, Total: 7})
	if !strings.Contains(buf.String(), `"idx":3`) || !strings.Contains(buf.String(), `"of":7`) {
		t.Fatalf("step_started missing idx/of: %s", buf.String())
	}
}
```

And to `internal/ui/pipeline_test.go` (or `ui_test.go`, matching where
Console is tested):

```go
// ConsoleProgress.Stop must forward the message and elapsed it already
// holds — the bare information-free StepFailed{Stage} is audit P4.
func TestConsoleStopCarriesMsg(t *testing.T) {
	ch := make(chan event.Event, 4)
	con := &Console{ch: ch}
	pr := con.Progress("packs", "delivering gitea@0.1.0")
	pr.Stop()
	<-ch // StepStarted
	ev := (<-ch).(event.StepFailed)
	if ev.Msg != "delivering gitea@0.1.0" || ev.Dur <= 0 {
		t.Fatalf("Stop dropped msg/dur: %+v", ev)
	}
}
```

(`Console{ch: ch}` construction requires same-package test — these files are
already `package ui` / `package render` internal tests; keep that.)

- [x] **Step 2: Verify they fail**
Run: `go test ./internal/ui/... -run 'TestJSONStepFailed|TestJSONStepCarries|TestConsoleStopCarries' -v`
Expected: compile FAIL (`unknown field Msg in event.StepFailed`, etc.).

- [x] **Step 3: Widen `event.go`.** `StepFailed` gains `Msg string` and
`Dur time.Duration` (keep the existing `Err *diag.Error` field and its
comment); `StepStarted` and `StepDone` gain `Index, Total int` with the
comment `// Index/Total: 1-based n-of-m for repeated stages (pack loop); 0
means not enumerated.`; add:

```go
// StepLog is one line of an in-flight step's captured output — the live
// renderer's bounded tail (TE-1.4) and failure dump (TE-2.2). Plain,
// styled, and JSON projections emit ZERO bytes for it (live-only richness;
// a JSON projection may be added later under its own ratification).
type StepLog struct{ Stage, Line string }

func (StepLog) event() {}
```

- [x] **Step 4: Console plumbing** (`internal/ui/console.go`):
`ConsoleProgress` gains `msg string` and `idx, total int` fields. `Progress`
delegates: `func (c *Console) Progress(stage, message string) *ConsoleProgress { return c.ProgressN(stage, message, 0, 0) }`.
New:

```go
// ProgressN is Progress for enumerated repeats (pack 3/7): Index/Total ride
// StepStarted and the eventual StepDone so renderers can show n-of-m.
func (c *Console) ProgressN(stage, message string, index, total int) *ConsoleProgress {
	c.mu.Lock()
	c.openStage, c.openMsg = stage, message
	c.mu.Unlock()
	c.ch <- event.StepStarted{Stage: stage, Msg: message, Index: index, Total: total}
	return &ConsoleProgress{con: c, stage: stage, msg: message, idx: index, total: total, start: time.Now()}
}

// Log forwards one line of the open step's output (live-only tail).
func (c *Console) Log(stage, format string, args ...any) {
	c.ch <- event.StepLog{Stage: stage, Line: fmt.Sprintf(format, args...)}
}
```

Track `openMsg string` beside `openStage` (set in ProgressN, cleared in
`resolve`), and change `open()` to `func (c *Console) open() (stage, msg string)`.
`Done` emits `StepDone{..., Index: cp.idx, Total: cp.total}`. `Stop` becomes:

```go
func (cp *ConsoleProgress) Stop() {
	if cp.resolved {
		return
	}
	cp.resolved = true
	cp.con.resolve(cp.stage)
	cp.con.ch <- event.StepFailed{Stage: cp.stage, Msg: cp.msg, Dur: time.Since(cp.start)}
}
```

In `pipeline.go`'s unwind guard, use both returns:
`if st, msg := con.open(); st != "" { ch <- event.StepFailed{Stage: st, Msg: msg} }`.

- [x] **Step 5: Renderer arms.** `json.go`: `jsonStep` gains
`Idx int \`json:"idx,omitempty"\`` and `Of int \`json:"of,omitempty"\``
(emit from StepStarted/StepDone); `jsonStepFailed` gains
`Msg string \`json:"msg,omitempty"\`` and `DurMS int64 \`json:"dur_ms,omitempty"\``;
add `case event.StepLog:` → emit nothing (explicit arm + comment).
`plain.go` and `styled.go`: add `event.StepLog` to the explicit zero-bytes
case list. `live.go`: add `case event.StepLog:` returning `""` in
`scrollbackLine` and a no-op arm in `applyEvent` (T05 gives it behavior).

- [x] **Step 6: Producer — pack loop N/M** (`internal/up/up.go` ~:250).
Wrap each iteration so every pack delivery is an enumerated open step whose
Done message is byte-identical to today's `con.Step` line (plain never
prints Dur → zero plain drift):

```go
for i, pref := range refs {
	if err := func() error {
		pr := con.ProgressN("pack", "delivering "+pref.Ref, i+1, len(refs))
		defer pr.Stop() // no-op after Done; resolves the step on any error return
		stepFetchSource(con, pref.Ref)
		// ... existing body unchanged up to the final line, which becomes:
		pr.Done("%s@%s delivered", rendered.Name, rendered.Version)
		return nil
	}(); err != nil {
		return err
	}
}
```

The existing `con.Step("pack", "%s@%s delivered", ...)` at ~:307 is replaced
by `pr.Done(...)` — same stage, same words.

- [x] **Step 7: All green**
Run: `go test ./internal/ui/... ./internal/up/... -v 2>&1 | tail -20`
Expected: PASS including the three new tests; `render/plain_test.go`
untouched and green (zero plain drift is the gate).

- [x] **Step 8: Commit**
`git add -A && git commit -m "feat(ui): widen event vocabulary — StepFailed msg/dur, pack n/m, StepLog tail feed"`

- [x] **Step 9: Task-level verify + merge + ledger** (protocol 5–7). Note in
FINDINGS: JSONL additions are inside the documented v1-EXPERIMENTAL window;
changelog entry required before the D5 freeze (T14 owns the README note).

#### Outcome — W1.T02
- STATUS: `DONE`
- BRANCH: `tui/w1-t02-events` (merged: yes — 3a12f42)
- COMMITS: 3d3fa57 `docs: tui plan — claim W1.T02` · 267d360 `feat(ui): widen
  event vocabulary — StepFailed msg/dur, pack n/m, StepLog tail feed` ·
  3a12f42 `merge: tui W1.T02 events (tui/w1-t02-events)`
- FINDINGS: (1) Plan's test snippets called `fixedClock(t)`; the existing
  json_test.go helper takes no argument — used `fixedClock()`, tests otherwise
  verbatim. (2) `jsonStep` gained `Idx`/`Of` (`omitempty`) and
  `jsonStepFailed` gained `Msg`/`DurMS` (`omitempty`) exactly as planned; the
  JSON golden (`TestJSONGoldenUpRun`) passed unchanged because its fixture
  events carry no Index/Total and omitempty drops the zero values. (3) Real
  `up` runs now additionally emit `step_started`/`step_done` with `idx`/`of`
  for the pack stage, a `step_started` "delivering <ref>" line per pack, and
  `dur_ms` on the pack `step_done` — all additive inside the documented
  v1-EXPERIMENTAL JSONL window; changelog entry required before the D5 freeze
  (T14 owns the README note). (4) `stepFetchSource` verified to be a bare
  `con.Step` (StepDone only) — it cannot clobber the enumerated open step's
  openStage/openMsg. (5) up.go's loop body moved into the per-iteration
  closure with existing comments preserved; `con.Step("pack", "%s@%s
  delivered", ...)` became `pr.Done(...)` — same stage, same words, so plain
  bytes are unchanged (plain never prints Dur; StepStarted is zero plain
  bytes). (6) `gofmt -l` flags three pre-existing files this task never
  touched (internal/bundle/bundle.go, internal/config/types.go,
  internal/syncer/synconce_test.go) — left alone. (7) Zero existing test
  assertions modified; only the three planned tests were added.
- REVIEW: Step 2 red run failed compile exactly as Expected (`unknown field
  Msg in struct literal of type event.StepFailed`, etc.). Step 7 green:
  `go test ./internal/ui/... ./internal/up/...` all ok, three new tests PASS,
  plain_test.go untouched and green (zero plain drift). Task gate
  `go build ./... && go vet ./... && go test ./...` in the worktree: 29
  packages ok, zero FAIL. Post-merge `go test ./...` in $ROOT clean. TE gate
  n/a (no TE frame touched; TE goldens arrive in T05).
- BLOCKERS: none
- HANDOFF: `event.StepLog{Stage, Line}` exists with explicit zero-byte arms
  in plain/styled/json and placed-but-inert arms in live.go (`scrollbackLine`
  returns "", `applyEvent` no-op) — T05 gives it the bounded tail (TE-1.4).
  `Console.ProgressN(stage, msg, index, total)` and `Console.Log(stage,
  format, ...)` exist; Log has no producer callers yet. `Console.open()` now
  returns `(stage, msg string)` — the pipeline unwind guard forwards both.
  T04's start lines can read Index/Total off StepStarted. `.superpowers/` is
  gitignored, so the progress.md append is on-disk only (same note as T01).

---

### W1.T03: `event.Epilogue` + ratified R2 (glyph leaves event content)

**Branch:** `tui/w1-t03-epilogue` · **Depends:** W1.T02

**Files:**
- Modify: `internal/ui/event/event.go`, `internal/ui/console.go`,
  `internal/up/up.go` (:385 Note), `internal/ui/render/plain.go` (+test),
  `internal/ui/render/styled.go`, `internal/ui/render/json.go` (+test),
  `internal/ui/render/live.go` (arm only; full TE-4 render in T05)
- Possibly: any test/e2e pinning the `✔ cube` substring (step 1 finds them)

**Interfaces:**
- Produces: `event.Epilogue{Cube, GatewayURL, Context, Registry, Hint string}`;
  `Console.Epilogue(e event.Epilogue)`. T05 renders the TE-4 block from it.

- [ ] **Step 1: Pin the blast radius FIRST.**
Run: `grep -rn '✔ cube' --include='*.go' . | grep -v '.claude/'`
Expected: `internal/up/up.go:385` plus zero-or-more test assertions.
List every hit in FINDINGS. Any test asserting the `✔ cube %q is up` bytes
must be updated in step 5's commit (same-commit rule, spec §5).

- [ ] **Step 2: Failing golden test** — in `internal/ui/render/plain_test.go`
add (R2: plain bytes differ from today by EXACTLY the leading `✔ `):

```go
// R2 (spec §5): the epilogue is data; plain projects it WITHOUT the glyph.
// These bytes are the new frozen contract for event.Epilogue. (Name is
// normative — spec §6.1 matrix.)
func TestTE4_PlainBytesR2Only(t *testing.T) {
	var buf bytes.Buffer
	Plain(&buf)(event.Epilogue{
		Cube: "voodoo", GatewayURL: "https://voodoo.local:8443",
		Hint: "credentials: cube-idp get secrets",
	})
	want := "\ncube \"voodoo\" is up — https://voodoo.local:8443\n  credentials: cube-idp get secrets\n"
	if buf.String() != want {
		t.Fatalf("epilogue plain bytes:\n got %q\nwant %q", buf.String(), want)
	}
}
```

Run: `go test ./internal/ui/render/ -run TestTE4_PlainBytesR2Only -v` → FAIL (no Epilogue type).

- [ ] **Step 3: Implement.** `event.go`:

```go
// Epilogue is the post-success "what you actually need" block (TE-4,
// helm-NOTES pattern). Context/Registry may be "" when the producer does
// not know them; renderers omit empty rows. R2: the ✔ glyph is
// presentation — renderers add it; this event never carries it.
type Epilogue struct{ Cube, GatewayURL, Context, Registry, Hint string }

func (Epilogue) event() {}
```

`console.go`: `func (c *Console) Epilogue(e event.Epilogue) { c.ch <- e }`.
`plain.go` arm (exact bytes from step 2's golden):

```go
case event.Epilogue:
	fmt.Fprintf(w, "\ncube %q is up — %s\n  %s\n", e.Cube, e.GatewayURL, e.Hint)
```

`styled.go` arm: same content through `th.OK.Render(theme.GlyphOK)` headline
prefix + `th.Dim` hint (content-identical rule). `json.go`: new
`jsonEpilogue` struct + `"type":"epilogue"` record with all five fields
(omitempty for Context/Registry) + a golden-style test. `live.go`
`scrollbackLine`: temporary minimal arm returning the styled headline (T05
replaces with the full TE-4 block).

- [ ] **Step 4: Producer swap** (`internal/up/up.go` :385) — the Note dies:

```go
con.Epilogue(event.Epilogue{
	Cube:       cube.Metadata.Name,
	GatewayURL: fmt.Sprintf("https://%s:%d", cube.Spec.Gateway.Host, cube.Spec.Gateway.Port),
	Hint:       "credentials: cube-idp get secrets",
})
```

(`internal/up` imports `internal/ui/event` — check it doesn't already; add
the import. Context/Registry stay "" here; if the surrounding code has the
kubeconfig context or zot address in scope, fill them and note it in
FINDINGS — do not plumb new parameters for them in this task.)

- [ ] **Step 5: Update the pinned substrings found in step 1** (drop the
`✔ ` prefix in expectations), run everything:
Run: `go test ./... 2>&1 | tail -5`
Expected: PASS. Then verify the one-glyph rule:
Run: `go test ./internal/ui/render/ -run TestTE4_PlainBytesR2Only -v`
Expected: PASS.

- [ ] **Step 6: Commit**
`git add -A && git commit -m "feat(ui): structured Epilogue event; R2 — glyph out of event content (ratified, spec §5)"`

- [ ] **Step 7: Task-level verify + merge + ledger.**

#### Outcome — W1.T03
- STATUS: `IN_PROGRESS(a68e5830-aa68-47e2-903a-e18b60390fc5, 2026-07-17T05:24:43Z)`
- BRANCH: `tui/w1-t03-epilogue` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W1.T04: Ratified R1 — StepStarted start lines in plain & styled

**Branch:** `tui/w1-t04-startlines` · **Depends:** W1.T02

**Files:**
- Modify: `internal/ui/render/plain.go` (+`plain_test.go`),
  `internal/ui/render/styled.go` (+`styled_test.go`)
- Check: `tests/e2e/*.go` substring assertions (additive lines — should be
  unaffected; verify, don't assume)

- [ ] **Step 1: Failing golden test** (`plain_test.go`):

```go
// R1 (spec §5): a started step is visible in CI logs — hung and slow must
// be distinguishable (audit P12). Exact bytes: "▸ [stage] msg...\n".
func TestPlainStepStartedLine(t *testing.T) {
	var buf bytes.Buffer
	Plain(&buf)(event.StepStarted{Stage: "cluster", Msg: "creating kind cluster"})
	if got, want := buf.String(), "▸ [cluster] creating kind cluster...\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
```

Run: `go test ./internal/ui/render/ -run TestPlainStepStartedLine -v` → FAIL
(zero bytes today).

- [ ] **Step 2: Implement.** `plain.go`: move `event.StepStarted` out of the
zero-bytes case into `fmt.Fprintf(w, "▸ [%s] %s...\n", e.Stage, e.Msg)`.
`styled.go`: same content, badge via `th.Badge`, msg + `...` via `th.Dim`.
Do NOT touch the live renderer (its region already shows in-flight state)
or JSON (`step_started` records already exist).

- [ ] **Step 3: Full suite — expect deliberate golden churn**
Run: `go test ./... 2>&1 | tail -20`
Any failing pipeline/e2e-adjacent test that asserted the ABSENCE of start
lines or pinned full plain transcripts gets updated in this commit; list
every updated assertion in FINDINGS (this is the ratified R1 churn).
Expected after updates: PASS.

- [ ] **Step 4: Commit**
`git add -A && git commit -m "feat(ui): R1 — plain/styled start lines for StepStarted (ratified, spec §5; fixes silent CI waits)"`

- [ ] **Step 5: Task-level verify + merge + ledger.**

#### Outcome — W1.T04
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w1-t04-startlines` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W1.T05: Live step tree — TE-1, TE-2 (renderer half), TE-4 block

**Branch:** `tui/w1-t05-livetree` · **Depends:** W1.T01–T04

**Files:**
- Modify: `internal/ui/render/live.go` (largest task — evolve `liveModel`,
  never rewrite the lifecycle), `internal/ui/render/live_test.go`
- Create: `internal/ui/render/testdata/te1_scrollback.golden`,
  `internal/ui/render/testdata/te2_failure.golden`,
  `internal/ui/render/testdata/te4_epilogue.golden`

**Hard invariants (from `Live()`'s doc comment — violating any is a
BLOCKED-level failure):** inline mode only (AltScreen never set), scrollback
only via `p.Println`, quit only on `eofMsg`, ctrl-c/`tea.InterruptMsg` →
`cancel()` (never `os.Exit`), guaranteed channel drain, nil input on pipes.

**Interfaces:**
- Consumes: T02's widened events, T03's Epilogue, T01's theme.
- Produces: the TE golden fixtures + a stateful `scrollback` projector
  (replacing the pure `scrollbackLine`) that T10 extends.

- [ ] **Step 1: Restructure scrollback to be stateful.** The forwarding
goroutine in `Live()` is single-threaded — give it state for log tails:

```go
// scrollback projects events into permanent scrollback lines. Stateful:
// it buffers StepLog lines per open stage so a failure can dump the full
// captured tail (TE-2.2) ahead of the diagnosis box.
type scrollback struct {
	th    theme.Theme
	tails map[string][]string
}

func newScrollback(th theme.Theme) *scrollback {
	return &scrollback{th: th, tails: map[string][]string{}}
}

// lines returns zero or more finished scrollback lines for ev.
func (s *scrollback) lines(ev event.Event) []string {
	switch e := ev.(type) {
	case event.StepLog:
		s.tails[e.Stage] = append(s.tails[e.Stage], e.Line)
		return nil
	case event.StepDone:
		delete(s.tails, e.Stage) // BuildKit collapse: success discards the tail
		return []string{s.stepDoneLine(e)}
	case event.StepFailed:
		out := []string{s.stepFailedLine(e)}
		for _, l := range s.tails[e.Stage] { // full dump, most important info last
			out = append(out, "  "+s.th.Dim.Render("│ "+l))
		}
		delete(s.tails, e.Stage)
		return out
	// Note, Warn, Access, Epilogue, RunDone arms below …
	default:
		return nil
	}
}
```

In `Live()` replace `if line := scrollbackLine(ev); line != "" { p.Println(line) }`
with `for _, line := range sb.lines(ev) { p.Println(line) }` where
`sb := newScrollback(theme.Detect(os.Stdin, outFile(out)))` (helper:
`outFile` returns `out.(*os.File)` or nil). Keep `p.Send(evMsg{ev})` after.

- [ ] **Step 2: Line layout (TE-1.1/1.2, TE-2.1).** Layout constants + the
two line builders:

```go
// TE-1 layout: fixed badge column, message field, right-aligned dim
// duration at durCol (golden-pinned at 80 cols; wider terminals keep the
// same columns — scrollback lines are permanent and must not depend on
// resize).
const durCol = 62

func (s *scrollback) stepDoneLine(e event.StepDone) string {
	return s.stepLine(s.th.OK.Render(theme.GlyphOK), e.Stage, e.Msg, e.Dur)
}

func (s *scrollback) stepFailedLine(e event.StepFailed) string {
	msg := e.Msg
	if msg == "" {
		msg = "failed" // never a naked ✗ again (audit P4)
	}
	return s.stepLine(s.th.Err.Render(theme.GlyphErr), e.Stage, msg, e.Dur)
}

func (s *scrollback) stepLine(glyph, stage, msg string, dur time.Duration) string {
	badge := s.th.Badge.Render(fmt.Sprintf("%-*s", theme.BadgeWidth(), "["+stage+"]"))
	line := fmt.Sprintf("%s %s %s", glyph, badge, msg)
	if dur > 0 {
		d := s.th.Dim.Render(fmt.Sprintf("(%s)", dur.Round(time.Second)))
		if pad := durCol - lipgloss.Width(line); pad > 1 {
			return line + strings.Repeat(" ", pad) + d
		}
		return line + " " + d
	}
	return line
}
```

Epilogue arm (TE-4.1–4.3): headline
`th.OK.Render(GlyphOK) + " cube %q is up " + th.Dim.Render("("+run dur+")")`
— run duration comes from RunDone, which arrives after Epilogue, so render
the headline without duration here and let the RunDone arm print the final
`✔ up finished in …` summary line (TE frame's `(2m13s)` lives there; record
this composition decision in FINDINGS); then one `  key value` row per
non-empty field with keys in `th.Dim` (`gateway`, `context`, `registry`,
each key padded to 11 chars), GatewayURL through `th.Badge`; last row
`  th.Dim.Render("next: cube-idp status · " + e.Hint)`.
RunDone arm: `✔ up finished in Xs` via th.OK / `✗ up failed after Xs` via
th.Err (only when the run had a RunStarted — track `started bool` on
scrollback from RunStarted).

- [ ] **Step 3: Live region (TE-1.3/1.4/1.5).** `liveModel` gains:

```go
th        theme.Theme
width     int                  // from tea.WindowSizeMsg; 0 = unknown, clamp only when known
prog      progress.Model       // bubbles/v2 progress bar
packIdx   int                  // Index/Total of the open enumerated step
packTotal int
tails     map[string][]string  // last ≤5 StepLog lines per open stage
```

`Init`: `return tea.Batch(m.spin.Tick, tea.RequestBackgroundColor)`.
`Update` gains: `tea.BackgroundColorMsg` → `m.th = theme.New(msg.IsDark())`
(check the exact v2 accessor with `go doc charm.land/bubbletea/v2 BackgroundColorMsg`;
record in FINDINGS); `tea.WindowSizeMsg` → `m.width = msg.Width`.
`applyEvent` gains: `StepStarted` also records Index/Total when `>0`;
`StepLog` appends to `m.tails[e.Stage]` keeping only the last 5
(`if len(t) > 5 { t = t[len(t)-5:] }`); `StepDone/StepFailed` delete the
stage's tail and clear pack counters when it was the enumerated stage;
keep `HealthTick` but REMOVE the two `m.components = nil` resets in the
StepDone/StepFailed "health" arms — the health snapshot persists until
RunDone (spec WP3).
`View()` per open step: spinner + fixed-width badge + msg + right-aligned
`n/m` counter when enumerated; progress bar line
(`m.prog.ViewAs(float64(m.packIdx) / float64(m.packTotal))`, bar width 30 —
construct with `progress.New(progress.WithWidth(30))`; verify option names
via `go doc charm.land/bubbles/v2/progress New`) under the enumerated step;
tail lines under the active step as `"             " + th.Dim.Render("│ "+l)`;
health rows through the theme (drop the `hasStage` gate per above); every
region line clamped: when `m.width > 0`, truncate to width with
`lipgloss.Width`-aware cutting and a trailing `…` (write helper
`clamp(s string, w int) string`) — the region must never wrap (TE-1.5).

- [ ] **Step 4: TE golden tests.** In `live_test.go`, following its
existing model-driven pattern (drive `applyEvent`/`View` directly, injectable
`now`), with a fixed theme (`theme.New(true)`) and ANSI stripped
(reuse/move the `stripANSI` helper from `styled_test.go`):

```go
// TE-1: golden of the scrollback lines + live region for the canonical up
// sequence (spec §2 frame TE-1; test names are normative, spec §6.1).
func TestTE1_UpLiveFrame(t *testing.T)      { /* feed: RunStarted, 4×StepDone with durs, ProgressN packs 3/7 StepStarted, 2×StepLog; assert stripANSI(join(scrollback lines)+View()) == testdata/te1_scrollback.golden */ }
func TestTE1_TailBounded(t *testing.T)      { /* 7 StepLogs → region shows exactly the last 5 */ }
func TestTE1_DurationColumn(t *testing.T)   { /* two StepDones, different msg lengths → "(28s)"/"(6s)" start at the same column */ }
func TestTE2_StepFailedCarriesMsgDur(t *testing.T) { /* StepFailed{Msg,Dur} → line contains msg and (4s); empty Msg → "failed" */ }
func TestTE2_FailureFlushesTail(t *testing.T)      { /* 2×StepLog then StepFailed → lines() returns ✗ line followed by both │-prefixed lines */ }
func TestTE4_EpilogueGolden(t *testing.T)          { /* Epilogue+RunDone → testdata/te4_epilogue.golden */ }
```

Write the golden files by running the test with a `-update` flag pattern if
the suite has one, else paste the expected output verbatim; then eyeball
each golden against the spec §2 frames — the golden IS the frame. Every
`«dynamic»` span from the spec appears as the fixed test value.

- [ ] **Step 5: Green + invariants intact**
Run: `go test ./internal/ui/... -v 2>&1 | tail -30` → PASS.
Run: `grep -n "AltScreen" internal/ui/render/live.go` → zero hits.
Run: `go test ./internal/ui/render/ -run 'TE' -v` → all TE tests PASS
(this exact command is the spec §6.1 merge gate).

- [ ] **Step 6: Commit**
`git add -A && git commit -m "feat(ui): live step tree — BuildKit collapse, n/m progress, bounded tails, TE-1/2/4 goldens"`

- [ ] **Step 7: Manual smoke (best-effort, non-gating).** If a TTY is
available: `go build -o /tmp/cube-idp . && CUBE_IDP_PROGRESS=live /tmp/cube-idp status` —
confirm no terminal corruption on exit. Record result (or "no TTY in this
environment") in FINDINGS.

- [ ] **Step 8: Task-level verify + merge + ledger.**

#### Outcome — W1.T05
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w1-t05-livetree` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W1.T06: The prompt seam — `internal/ui/prompt.go` + two migrations

**Branch:** `tui/w1-t06-prompts` · **Depends:** W1.T01

**Files:**
- Create: `internal/ui/prompt.go`, `internal/ui/prompt_test.go`
- Modify: `internal/ui/pipeline.go` (pipelineActive flag),
  `cmd/trust.go` (:39–59), `internal/plugin/trust.go` (:143–174),
  `internal/plugin/exec.go` (thread streams if needed)

**Interfaces:**
- Produces: `ui.PromptsAllowed(in io.Reader, out io.Writer) bool`;
  `ui.Confirm(in io.Reader, out io.Writer, o ui.ConfirmOpts) (bool, error)`;
  `ui.InputExact(in io.Reader, out io.Writer, title, want string) (bool, error)`.
  T07 (down/upgrade) and T11 (pack menu) build on these.

- [ ] **Step 1: Failing gate tests** (`internal/ui/prompt_test.go`):

```go
// The single prompt gate (spec Decision 4 + §6.3): buffers, pipes, and
// non-rich modes can NEVER prompt — and a disallowed Confirm must return
// the default without reading or writing a byte, within milliseconds.
func TestPromptsAllowedFalseForBuffers(t *testing.T) {
	SetMode(ModeStyled)
	defer SetMode(ModeStyled)
	if PromptsAllowed(&bytes.Buffer{}, &bytes.Buffer{}) {
		t.Fatal("buffers must never be promptable")
	}
}

func TestConfirmNonTTYReturnsDefaultWithoutBlocking(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		in, out := &bytes.Buffer{}, &bytes.Buffer{}
		ok, err := Confirm(in, out, ConfirmOpts{Title: "?", Default: false})
		if err != nil || ok != false || out.Len() != 0 {
			t.Errorf("disallowed Confirm leaked: ok=%v err=%v wrote=%q", ok, err, out.String())
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Confirm blocked on a non-TTY — the exact failure mode that hangs CI")
	}
}

func TestPromptsRefusedWhilePipelineActive(t *testing.T) {
	pipelineActive.Store(true)
	defer pipelineActive.Store(false)
	if PromptsAllowed(os.Stdin, os.Stdout) {
		t.Fatal("prompts must never share the terminal with a running pipeline (spec Decision 5)")
	}
}
```

Run: `go test ./internal/ui/ -run 'TestPrompts|TestConfirmNonTTY' -v` → compile FAIL.

- [ ] **Step 2: Implement `internal/ui/prompt.go`** (mirror `cmd/init.go`'s
proven huh v2 construction — read init.go:243–320 first and reuse its exact
option style):

```go
package ui

import (
	"io"
	"os"

	huh "charm.land/huh/v2"
)

// PromptsAllowed is the single gate every interactive surface routes
// through (spec Decisions 4/5): both streams are real TTYs, the resolved
// mode is rich, and no event pipeline currently owns the terminal.
func PromptsAllowed(in io.Reader, out io.Writer) bool {
	if pipelineActive.Load() {
		return false
	}
	if !IsTerminal(in) || !IsTerminal(out) {
		return false
	}
	m := CurrentMode()
	return m == ModeStyled || m == ModeLive
}

// ConfirmOpts configures one yes/no consent prompt.
type ConfirmOpts struct {
	Title       string
	Description string
	Default     bool // returned verbatim when prompting is not allowed
}

// Confirm asks a yes/no question through huh v2. When prompting is not
// allowed it returns o.Default immediately — it MUST NOT read or write.
// $ACCESSIBLE (non-empty) swaps the TUI for sequential prompts (gh's
// documented retrofit; spec Decision 8).
func Confirm(in io.Reader, out io.Writer, o ConfirmOpts) (bool, error) {
	if !PromptsAllowed(in, out) {
		return o.Default, nil
	}
	ok := o.Default
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(o.Title).Description(o.Description).Value(&ok),
	)).WithInput(in).WithOutput(out).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		return false, err
	}
	return ok, nil
}

// InputExact is the severe-tier consent (TE-3.2, terraform/gh-repo-delete
// model): returns true only when the user types want exactly.
func InputExact(in io.Reader, out io.Writer, title, want string) (bool, error) {
	if !PromptsAllowed(in, out) {
		return false, nil
	}
	var got string
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title(title).Value(&got),
	)).WithInput(in).WithOutput(out).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		return false, err
	}
	return got == want, nil
}
```

`pipeline.go`: add `var pipelineActive atomic.Bool` (package ui);
`runPipeline` sets it true at entry, `defer pipelineActive.Store(false)`.
If huh v2.0.3's `Form` lacks `WithInput/WithOutput` under those exact names,
STOP, check `go doc charm.land/huh/v2 Form` and cmd/init.go's usage, adapt,
and record the real API in FINDINGS.

- [ ] **Step 3: Migrate `cmd/trust.go`** (:39–59). Keep `--yes` and every
byte of the fallback path; the huh prompt engages only when allowed:

```go
if !yes {
	subject := "your cube-idp gateway's HTTPS"
	if cube, cerr := config.Load(file); cerr == nil {
		subject = "https://*." + cube.Spec.Gateway.Host
	}
	desc := "This adds the cube-idp local CA to your OS trust stores so browsers accept\n" +
		subject + " without warnings (mkcert mechanism).\n" +
		"It is fully removed by `cube-idp trust --uninstall` or `cube-idp down`."
	if ui.PromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
		ok, err := ui.Confirm(c.InOrStdin(), c.OutOrStdout(),
			ui.ConfirmOpts{Title: "Trust the cube-idp local CA?", Description: desc})
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
			return nil
		}
		fmt.Fprintln(c.OutOrStdout(), "  hint: cube-idp trust --yes") // flag-twin hint (spec Decision 4)
	} else {
		// non-TTY fallback: byte-identical to the pre-migration prompt
		fmt.Fprint(c.OutOrStdout(), desc+"\nProceed? [y/N] ")
		line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
			return nil
		}
	}
}
```

(The fallback wording must reproduce today's exact bytes — diff the printed
string against the original before committing; `cmd/trust_test.go` pins it.)

- [ ] **Step 4: Migrate `internal/plugin/trust.go`** (:143–174).
`EnsureTrusted` keeps its signature. Replace the raw stderr bufio block:
when `ui.PromptsAllowed(os.Stdin, os.Stderr)` → `ui.Confirm(os.Stdin,
os.Stderr, ui.ConfirmOpts{Title: fmt.Sprintf("plugin %q is not trusted — run it and remember this hash?", name),
Description: fmt.Sprintf("path: %s\nsha256: %s\nplugins run with your full user permissions", path, shortSum(sum))})`;
on accept, store the hash exactly as today. When NOT allowed →
the existing CUBE-7104 refusal (`diag.New(diag.CodePluginUntrusted, …)`)
**byte-for-byte** — this tightens the old "TTY but plain-mode still
prompted on stderr" hole into a clean refusal; record that behavior change
prominently in FINDINGS (it is spec-Decision-4-correct). `internal/plugin`
now imports `internal/ui` — verify no import cycle:
`go build ./internal/plugin/` must pass (ui does not import plugin).

- [ ] **Step 5: Green**
Run: `go test ./internal/ui/ ./internal/plugin/ ./cmd/ 2>&1 | tail -10`
Expected: PASS — trust_test.go still green because buffer streams take the
byte-identical fallback path.

- [ ] **Step 6: Commit**
`git add -A && git commit -m "feat(ui): huh-v2 prompt seam with hard TTY/mode/pipeline gating; migrate trust + plugin-trust prompts"`

- [ ] **Step 7: Task-level verify + merge + ledger.**

#### Outcome — W1.T06
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w1-t06-prompts` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W1.T07: Consent flows — down preview + typed consent + R3; upgrade apply-confirm

**Branch:** `tui/w1-t07-consent` · **Depends:** W1.T06

**Files:**
- Modify: `cmd/down.go`, `cmd/down_test.go`, `cmd/upgrade.go`,
  `cmd/up.go` (extract `runUp` helper), `internal/diag/codes.go`
  (register `CUBE-0010`), `tests/e2e/e2e_test.go` (:88, :167),
  `tests/e2e/phase3_test.go` (:141, :301, :356, :367, :439, :496)

- [ ] **Step 1: Register the refusal code** (`internal/diag/codes.go`, 0xxx
command-contract range — after CUBE-0008):

```go
CodeConfirmRequired Code = "CUBE-0010" // a destructive command refused to run without confirmation (--yes / --confirm)
```

- [ ] **Step 2: Failing tests** (`cmd/down_test.go`):

```go
// R3 (spec §5 + TE-3.4): non-TTY down without --yes REFUSES — it must
// never destroy silently in CI, and must never hang waiting for input.
func TestTE3_NonTTYRefusesWithoutYes(t *testing.T) {
	// arrange a cube.yaml in a temp dir the way this file's other tests do
	root := NewRootCmd()
	root.SetArgs([]string{"down", "-f", cubeYAMLFixture(t)})
	root.SetIn(&bytes.Buffer{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.ExecuteContext(context.Background())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeConfirmRequired {
		t.Fatalf("want CUBE-0010 refusal, got %v", err)
	}
}

func TestTE3_YesSkipsPrompt(t *testing.T) { /* same setup + "--yes": err is NOT CodeConfirmRequired (it may fail later for cluster reasons — assert only the code differs) */ }

// Decline path (TE-3.3) — prompting needs a TTY, so down.go exposes seams
// (the trust.go trustInstall pattern): `var downPromptsAllowed = ui.PromptsAllowed`
// and `var downConfirmName = ui.InputExact`. Override both here: allowed=true,
// InputExact returns (false, nil) → exact wording, nil error, no pipeline run.
func TestTE3_DeclineAbortsCleanly(t *testing.T) {
	downPromptsAllowed = func(io.Reader, io.Writer) bool { return true }
	downConfirmName = func(_ io.Reader, _ io.Writer, _, _ string) (bool, error) { return false, nil }
	defer func() { downPromptsAllowed = ui.PromptsAllowed; downConfirmName = ui.InputExact }()
	// run "down" with fixture cube.yaml; assert err == nil and output
	// contains "aborted — nothing was changed" and NOT "cluster deleted"
}
```

Run: `go test ./cmd/ -run TestTE3 -v` → FAIL (down has no --yes and
proceeds today).

- [ ] **Step 3: Implement the down gate.** In `newDownCmd`, add flags
`--yes` and `--confirm string`. In RunE, BEFORE `ui.RunPipeline` (spec
Decision 5 — a prompt and the pipeline must never share the terminal):

```go
RunE: func(c *cobra.Command, _ []string) error {
	if !yes {
		cube, err := config.Load(file)
		if err != nil {
			return err
		}
		if confirmName != "" {
			if confirmName != cube.Metadata.Name {
				return diag.New(diag.CodeConfirmRequired,
					fmt.Sprintf("--confirm=%q does not match cube %q", confirmName, cube.Metadata.Name),
					fmt.Sprintf("pass --confirm=%s (or --yes)", cube.Metadata.Name))
			}
		} else if downPromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
			printDownPreview(c.OutOrStdout(), cube, keepCluster) // TE-3.1
			ok, err := downConfirmName(c.InOrStdin(), c.OutOrStdout(),
				"Type the cube name to confirm:", cube.Metadata.Name)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed") // TE-3.3, trust.go's exact wording
				return nil
			}
			fmt.Fprintln(c.OutOrStdout(), "  hint: cube-idp down --yes")
		} else {
			return diag.New(diag.CodeConfirmRequired, // TE-3.4 / R3
				fmt.Sprintf("destroying cube %q requires confirmation", cube.Metadata.Name),
				"re-run with --yes (or --confirm=<cube-name>) — non-interactive runs never destroy silently")
		}
	}
	return ui.RunPipeline(c.Context(), "down", c.OutOrStdout(),
		func(ctx context.Context, con *ui.Console) error {
			return runDown(ctx, con, file, keepCluster)
		})
},
```

Declare the test seams at file scope (trust.go's pattern):
`var downPromptsAllowed = ui.PromptsAllowed` and
`var downConfirmName = ui.InputExact`.
`printDownPreview` (same file) enumerates the REAL deletion set per TE-3.1,
mirroring runDown's actual branches (`cube.Spec.Cluster.Provider`,
`keepCluster`, pack count `len(cube.Spec.Packs)`, and a trust-store line
only if `trust.LoadState` on `trustDir()` reports Installed — reuse the
seams trust.go already defines). Bullet rows use `theme.Detect` styles with
plain-text content; golden-test it as `TestTE3_DownPreviewGolden` with a
fixture cube (ANSI-stripped compare against
`cmd/testdata/te3_preview.golden` — create the testdata dir).

- [ ] **Step 4: The eight e2e call sites gain `--yes`** (same commit — R3's
same-commit rule): `tests/e2e/e2e_test.go:88` (`exec.Command(bin, "down")` →
`exec.Command(bin, "down", "--yes")`), `:167` (`run(t, dir, bin, "down")` →
`run(t, dir, bin, "down", "--yes")`), and the six `phase3_test.go` sites
(:141, :301, :356, :367, :439, :496) identically. Then verify none remain:
Run: `grep -rn '"down"' tests/e2e/ | grep -v -- --yes`
Expected: zero hits.

- [ ] **Step 5: upgrade apply-confirm.** Extract up's pipeline body into a
shared helper in `cmd/up.go`:
`func runUpPipeline(c *cobra.Command, file string) error` (move the existing
`ui.RunPipeline(...)` call; `newUpCmd`'s RunE becomes a one-liner calling
it). In `cmd/upgrade.go`, at the point where `--plan` has reported drift on
a TTY (read the file first — wire AFTER all plan output is flushed and only
when NOT in --plan-only-and-exit paths):

```go
if ui.PromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
	ok, err := ui.Confirm(c.InOrStdin(), c.OutOrStdout(), ui.ConfirmOpts{
		Title: "apply now (runs cube-idp up)?", Default: false})
	if err != nil {
		return err
	}
	if ok {
		fmt.Fprintln(c.OutOrStdout(), "  hint: cube-idp up")
		return runUpPipeline(c, file)
	}
}
```

Non-TTY behavior unchanged (exit semantics are T08's business).

- [ ] **Step 6: Green + never-blocks fence**
Run: `go test ./cmd/ -run 'TestTE3|TestDown|TestUpgrade' -v -timeout 60s` → PASS.
Run: `go test ./... 2>&1 | tail -5` → PASS.

- [ ] **Step 7: Commit**
`git add -A && git commit -m "feat(cmd): down destroy preview + typed consent; R3 non-TTY refusal (CUBE-0010, ratified); upgrade apply-confirm"`

- [ ] **Step 8: Task-level verify + merge + ledger.** FINDINGS must note:
R3 is a behavior change for piped `down` (release-note item, T14 README).

#### Outcome — W1.T07
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w1-t07-consent` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W1.T08: Exit-path & error hygiene — sentinel, CUBE-0009, `RenderErrorTo`

**Branch:** `tui/w1-t08-exitpaths` · **Depends:** none (order still binds)

**Files:**
- Modify: `cmd/exit.go`, `cmd/exit_test.go`, `cmd/doctor.go` (:114, :119),
  `cmd/upgrade.go` (:20–21, :27), `cmd/diff.go` (:22),
  `internal/diag/codes.go` (`CUBE-0009`),
  `internal/ui/rendererr.go`, `internal/ui/rendererr_test.go`, `main.go`

- [ ] **Step 1: PIN the exit codes FIRST** (`cmd/exit_test.go`) — before any
refactor, add table rows asserting today's semantics: plugin
`*exec.ExitError` → (its code, render=false); generic error → (1, true).
Run: `go test ./cmd/ -run TestExitCodeFor -v` → PASS (they pin current
behavior; if a test with this name exists, extend it).

- [ ] **Step 2: Failing sentinel test** (same file):

```go
// os.Exit inside RunE skips main.go's cleanup and would leave a future
// live program's terminal raw (audit P8). The sentinel keeps "exit 1,
// print nothing" semantics through the normal return path.
func TestExitCodeForSentinel(t *testing.T) {
	code, render := ExitCodeFor(errExitCode(1))
	if code != 1 || render {
		t.Fatalf("sentinel: got (%d,%v) want (1,false)", code, render)
	}
}
```

Run → FAIL (`errExitCode` undefined).

- [ ] **Step 3: Implement.** `cmd/exit.go`:

```go
// exitStatus carries a bare exit code through the normal error return path
// so deferred cleanup (main.go's signal stop, renderer teardown) always
// runs — the os.Exit-in-RunE replacement (spec WP9).
type exitStatus struct{ code int }

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func errExitCode(code int) error { return exitStatus{code: code} }
```

Extend `ExitCodeFor`: check `exitStatus` (via `errors.As(err, &es)`) FIRST
→ `(es.code, false)`; keep the `*exec.ExitError` arm and the `(1, true)`
fallback verbatim. Replace `os.Exit(1)` with `return errExitCode(1)` at
`cmd/doctor.go:114,119`, `cmd/upgrade.go:27`, `cmd/diff.go:22` — read each
site: if it is not inside RunE's direct return path (e.g. inside a helper),
thread the error up; the RunE must return it.

- [ ] **Step 4: CUBE-0009 for upgrade's raw error.**
Run: `sed -n '15,30p' cmd/upgrade.go` — locate the raw
`fmt.Errorf` with the embedded newline (audit P7, ~:20). Register in
`internal/diag/codes.go`:
`CodeUpgradeGuard Code = "CUBE-0009" // upgrade refused: see summary (was the last un-coded user-facing error)`
and convert: summary = the current first line verbatim, remediation = the
current second line verbatim (words preserved; only the envelope changes).
Update any test pinning the old raw error text (list in FINDINGS).

- [ ] **Step 5: `RenderErrorTo`** (`internal/ui/rendererr.go`):

```go
// RenderErrorTo renders err for a SPECIFIC writer, applying the same
// per-writer downgrade NewFor gives stdout: the styled panel only ever
// reaches a real terminal; a redirected stderr gets diag.Render verbatim
// (audit P11 — no more ANSI borders inside `2>file`).
func RenderErrorTo(w io.Writer, err error) string {
	if !IsTerminal(w) {
		return diag.Render(err)
	}
	return renderErrorForMode(CurrentMode(), err)
}
```

`main.go`: `fmt.Fprintln(os.Stderr, ui.RenderError(err))` →
`fmt.Fprintln(os.Stderr, ui.RenderErrorTo(os.Stderr, err))`. Keep
`RenderError` and the Printer variant untouched (syncer.Watch). Add to
`rendererr_test.go`: a bytes.Buffer writer under `SetMode(ModeStyled)`
yields `diag.Render` bytes exactly (no ANSI).

- [ ] **Step 6: Green**
Run: `go test ./cmd/ ./internal/ui/ ./internal/diag/... 2>&1 | tail -5` → PASS.
Run: `grep -rn "os.Exit" cmd/*.go | grep -v _test.go`
Expected: zero hits (main.go is the only os.Exit in the binary).

- [ ] **Step 7: Commit**
`git add -A && git commit -m "fix(cmd/ui): exit sentinel replaces os.Exit-in-RunE; CUBE-0009 for upgrade guard; writer-aware RenderErrorTo"`

- [ ] **Step 8: Task-level verify + merge + ledger.**

#### Outcome — W1.T08
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w1-t08-exitpaths` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W1.T09: Delete the hand-rolled spinner + Wave-1 sweep

**Branch:** `tui/w1-t09-spinner-sweep` · **Depends:** W1.T01–T08 all DONE

**Files:**
- Modify: `internal/ui/ui.go` (:280–373 — `Progress` type, `spinnerFrames`,
  `progressTick`, `eraseLine`, `Progress()/loop()/render()/Stop()/Done()`),
  `internal/ui/ui_test.go`, `cmd/progress_test.go`

- [ ] **Step 1: Prove zero production call sites**
Run: `grep -rn '\.Progress(' --include='*.go' internal/ cmd/ | grep -v _test.go | grep -v 'con\.' | grep -v 'Console'`
Expected: zero hits outside `internal/ui/ui.go` itself (Console.Progress /
ConsoleProgress are the event-stream seam and STAY). If a production call
site exists, STOP → BLOCKED with the site listed (the audit said none; a
new one appearing means an intervening change — do not delete under it).

- [ ] **Step 2: Delete** `Printer.Progress` and its machinery from `ui.go`
(the raw `\r\x1b[2K` writes are audit P2 — the corruption hazard). Port or
delete its tests in `ui_test.go` and `cmd/progress_test.go` (tests of
ConsoleProgress/pipeline behavior stay; tests of the deleted goroutine
spinner go). Update ui.go's package comment: Printer is the STATIC surface
(Step/Section/Glyph/Warn/AccessSummary); every animated or multi-step
surface goes through RunPipeline/RunPipelineStatic.

- [ ] **Step 3: Wave-1 sweep** — the full gate battery:
Run: `go build ./... && go vet ./... && go test ./... 2>&1 | tail -5` → PASS.
Run: `go test ./internal/ui/... ./cmd/... -run TE -v 2>&1 | tail -15`
Expected: every TE test from T03/T04/T05/T07 passes — Wave 1's spec §6.1
conformance gate.
Run: `go test -race ./internal/ui/... 2>&1 | tail -3` → PASS (the deleted
spinner was the main data-race surface).

- [ ] **Step 4: Commit + merge + ledger**
`git add -A && git commit -m "refactor(ui): delete hand-rolled spinner — bubbles/spinner in the live renderer is the only animation"`
FINDINGS: record the full sweep output summary (test counts). This closes
Wave 1 — note it in HANDOFF for the dispatcher.

#### Outcome — W1.T09
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w1-t09-spinner-sweep` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W2.T10: `cube-idp explain CUBE-XXXX` + TE-2.3 box footer

**Branch:** `tui/w2-t10-explain` · **Depends:** Wave 1 complete

**Files:**
- Create: `cmd/explain.go`, `cmd/explain_test.go`,
  `internal/diag/registry.go`, `internal/diag/registry_test.go`
- Modify: `cmd/root.go` (AddCommand), `internal/ui/rendererr.go` (footer),
  `internal/ui/rendererr_test.go`, TE-2 golden

- [ ] **Step 1: Failing tests**

```go
// internal/diag/registry_test.go — every registered code must be
// explainable; explain is the lookup half of the stable-code contract
// (rustc --explain pattern, spec WP8).
func TestEveryCodeHasDescription(t *testing.T) {
	for _, c := range AllCodes() {
		d, ok := Describe(c)
		if !ok || d.Summary == "" {
			t.Fatalf("code %s has no description", c)
		}
	}
}
```

```go
// cmd/explain_test.go
func TestExplainKnownCode(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"explain", "CUBE-0007"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CUBE-0007") {
		t.Fatalf("no code in output: %s", out.String())
	}
}

func TestExplainUnknownCodeFails(t *testing.T) { /* "CUBE-9999" → diag error naming the docs, not a panic */ }
```

Run → compile FAIL.

- [ ] **Step 2: Implement `internal/diag/registry.go`.** A `Desc{Summary,
Detail, Remediation string}` map keyed by `Code`, one entry per constant in
`codes.go` — lift each entry's Summary from the constant's `//` comment
verbatim; `AllCodes() []Code` returns them sorted; `Describe(Code) (Desc,
bool)` looks up. (Mechanical but long — every existing code gets an entry;
the test from step 1 enforces completeness forever.)

- [ ] **Step 3: Implement `cmd/explain.go`.** `Use: "explain CUBE-XXXX"`,
Args: exactly 1; normalize case; unknown → `diag.New(diag.CodeBadFlagValue,
…, "see internal/diag/codes.go ranges; run cube-idp explain --list")`;
`--list` prints all codes + summaries via tabwriter. Output: code, summary,
detail, `fix:` remediation — plain text, `-o json` NOT supported in this
task (record as future work). Register in root.go.

- [ ] **Step 4: The TE-2.3 footer.** In `renderErrorForMode`'s styled
branch, append after the fix line:
`fmt.Fprintf(&b, "%s\n", errPanelLabelStyle.Render("more:  cube-idp explain "+string(de.Code)))`
— a Dim line INSIDE the panel (the spec's border-embedded footer is
approximated inside the box; the plan records this as the approved
rendering — note it in FINDINGS). Update the TE-2 diag-box golden
(`TestTE2_DiagBoxGolden` — if T05 did not create it, create it now in
`rendererr_test.go`: fixed diag.Error → ANSI-stripped panel equals
`internal/ui/testdata/te2_box.golden`).

- [ ] **Step 5: Green + commit**
Run: `go test ./cmd/ ./internal/diag/ ./internal/ui/ -run 'Explain|Code|TE2' -v` → PASS; then full `go test ./...` → PASS.
`git add -A && git commit -m "feat(cmd): cube-idp explain — the lookup half of the CUBE-code contract; diag box footer"`

- [ ] **Step 6: Task-level verify + merge + ledger.**

#### Outcome — W2.T10
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w2-t10-explain` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W2.T11: `pack install` — the MultiSelect menu

**Branch:** `tui/w2-t11-packmenu` · **Depends:** Wave 1 complete

**Scope decision (recorded here, echo in FINDINGS):** no `pack install`
command exists today (only `pack push`). This task CREATES it as a
**config-mutating** command: selected packs are appended to `cube.yaml`'s
`spec.packs`; delivery happens on the next `cube-idp up` (the hint says so).
No cluster mutation in this task.

**Files:**
- Modify: `cmd/pack.go` (+`cmd/pack_test.go`)
- Read first: `cmd/init.go`'s optional-pack MultiSelect (its option catalog
  and huh construction are the pattern AND the catalog source — reuse, do
  not duplicate: extract init's pack-option list into a shared unexported
  helper `packCatalogOptions()` in cmd)

- [ ] **Step 1: Failing tests** (`cmd/pack_test.go`):

```go
// gh doctrine: args → never prompt; non-TTY bare invocation → refuse with
// the flag twin named, never hang (spec WP6 + Decision 4).
func TestPackInstallWithArgsNeverPrompts(t *testing.T) {
	// cube.yaml fixture in t.TempDir(); run: pack install <known-ref>
	// assert: ref appended to spec.packs, no prompt bytes on out, exit nil
}

func TestPackInstallBareNonTTYRefuses(t *testing.T) {
	// bytes.Buffer stdin; run: pack install
	// assert: diag error naming "pass pack refs as arguments", within 2s timeout
}
```

Run → FAIL (unknown command "install").

- [ ] **Step 2: Implement** `newPackInstallCmd()` in `cmd/pack.go`,
registered on `packCmd`. With args: validate each ref (reuse whatever ref
validation `up`/config already applies — grep `PackRef`), load cube.yaml,
append non-duplicate refs to `spec.packs`, write the file back (preserve
the file's existing marshaling style — check how `init` writes cube.yaml
and reuse that writer), print
`▸ [pack] added <ref>` per ref via `ui.NewFor(c.OutOrStdout()).Step(...)`
and end with `next: cube-idp up` as a Note-style line. Bare + allowed
(`ui.PromptsAllowed`): huh MultiSelect over `packCatalogOptions()`
(filterable — huh's MultiSelect filters natively), then ONE
`ui.Confirm` summarizing the selection (TE-3.2's summary-then-confirm
shape), then the same append path, then the dim scriptable-twin hint:
`hint: cube-idp pack install <ref...>` with the actual refs. Bare + NOT
allowed: `diag.New(diag.CodeConfirmRequired, "pack install needs pack refs
in non-interactive mode", "pass refs: cube-idp pack install
oci://ghcr.io/cube-idp/packs/<name>:<version>")`.

- [ ] **Step 3: Green + commit**
Run: `go test ./cmd/ -run TestPackInstall -v -timeout 60s` → PASS; full suite → PASS.
`git add -A && git commit -m "feat(cmd): pack install — filterable MultiSelect on TTY, config-mutating, flags-first"`

- [ ] **Step 4: Task-level verify + merge + ledger.**

#### Outcome — W2.T11
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w2-t11-packmenu` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W2.T12: `status --watch` — the gh-run-watch clone

**Branch:** `tui/w2-t12-watch` · **Depends:** Wave 1 complete

**Files:**
- Modify: `cmd/status.go` (+`cmd/status_test.go`)

**Semantics (normative):** `--watch` re-renders the one-shot status view
every `--interval` (default `3s`) and **exits 0 when every component is
Ready**. `--exit-status`: also exit 1 immediately when interrupted (ctrl-c)
while unhealthy — the CI gate
`cube-idp status --watch --exit-status && run-e2e`. `--compact` hides
Ready rows. The watch is the SAME view on a timer — no new TUI (spec WP7).

- [ ] **Step 1: Failing test** — refactor seam first: extract the existing
one-shot render into `func renderStatusOnce(...) (allReady bool, err error)`
(pure over collected state; read status.go's three render paths and thread
through whichever the mode picks). Test:

```go
func TestWatchExitsWhenAllReady(t *testing.T) {
	// inject a fake collector (add a package-level seam var like trust.go's
	// trustInstall pattern) that reports unready once, then ready;
	// run: status --watch --interval=10ms with buffer streams
	// assert: returns nil within 2s, output contains both renders
}
```

Run → FAIL (no --watch flag).

- [ ] **Step 2: Implement.** Non-TTY (buffers, CI): a plain `for` loop —
render, check allReady, `select { case <-ctx.Done(): ...; case <-time.After(interval): }`
— each render appended (no ANSI clearing on pipes; flux-style repeated
blocks). TTY + rich mode: an inline Bubble Tea tick program (reuse the
liveModel pattern minimally: a model holding the last rendered view string,
`tea.Tick(interval, ...)` recollect + re-render, quit when allReady,
ctrl-c → quit with interrupted flag; AltScreen never set). `--exit-status`
+ interrupted-while-unhealthy → `return errExitCode(1)` (T08's sentinel).
`--compact` filters Ready rows in `renderStatusOnce`.

- [ ] **Step 3: Green + commit**
Run: `go test ./cmd/ -run 'TestWatch|TestStatus' -v -timeout 60s` → PASS; full suite → PASS.
`git add -A && git commit -m "feat(cmd): status --watch/--interval/--exit-status/--compact — one-shot view on a timer"`

- [ ] **Step 4: Task-level verify + merge + ledger.**

#### Outcome — W2.T12
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w2-t12-watch` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W2.T13: fang styled help + NO_COLOR/CLICOLOR/--color compliance

**Branch:** `tui/w2-t13-fang-color` · **Depends:** Wave 1 complete

**Files:**
- Modify: `go.mod` (add `charm.land/fang/v2`, pinned), `cmd/root.go`,
  `main.go` (if Execute's wrapper moves), `internal/ui/ui.go`
  (Resolve/Request), `internal/ui/ui_test.go`
- This is the fuzziest task: two sub-deliverables, two commits.

- [ ] **Step 1 (commit A): fang.**
`go get charm.land/fang/v2@latest && go mod tidy` — record the pinned
version in FINDINGS. In `cmd/root.go`'s `Execute`, replace
`root.ExecuteContext(ctx)` with
`fang.Execute(ctx, root, fang.WithErrorHandler(<adapter calling ui.RenderErrorTo(os.Stderr, err)>), fang.WithColorSchemeFunc(<scheme built from theme.New>))`
— check `go doc charm.land/fang/v2 Execute` for the real option names and
adapt (record actual API in FINDINGS). **The exec-plugin fallthrough block
above it stays byte-identical and runs BEFORE fang.** Add a test: root with
`--output json`-style piped stdout produces ZERO ANSI on help
(`root.SetArgs([]string{"--help"})` into a buffer → `stripANSI(out) == out`).
If fang cannot satisfy the plugin fallthrough or double-prints errors
(SilenceErrors interplay), fall back to option (b) from the spec: skip fang,
implement `root.SetUsageTemplate/SetHelpFunc` with theme styles gated on
`ui.IsTerminal(os.Stdout)`, set STATUS to DONE_WITH_CONCERNS, and record
exactly why in FINDINGS. Commit A:
`git commit -m "feat(cmd): styled help/usage/version via fang v2 (pinned; CUBE boxes stay ours)"`

- [ ] **Step 2 (commit B): color-spec compliance** (spec WP8; no-color.org
+ bixense ladder). Changes to `internal/ui/ui.go`:
- `Request.NoColor bool` semantics: set only when `NO_COLOR` is present AND
  non-empty (`v, ok := os.LookupEnv("NO_COLOR"); noColor := ok && v != ""`)
  — fix the call site in `cmd/root.go` (:41).
- `NoColor` no longer forces `ModePlain` in `Resolve` (delete it from rung
  8; `TERM` dumb/unset keeps forcing plain). Instead colors are stripped at
  the writer: add `Request.ColorFlag string` (new persistent root flag
  `--color=auto|always|never`, default auto) and expose
  `ui.ColorEnabled(w io.Writer) bool` implementing the ladder —
  `--color=never` or non-empty NO_COLOR → false; `--color=always` or
  `CLICOLOR_FORCE` non-empty → true; else `IsTerminal(w)`.
- Apply it where styles engage: `NewFor` and `pickRenderer`/
  `pickRendererStatic` pass a no-color signal down — concretely, when
  `!ColorEnabled(w)` but the mode is styled/live-on-tty, wrap `w` in
  `colorprofile.NewWriter(w, os.Environ())` forced to the Ascii profile
  (import `github.com/charmbracelet/colorprofile`, already an indirect
  dep) so ANSI is stripped while layout/glyphs survive — NO_COLOR's actual
  spec. When `ColorEnabled` is true on a non-TTY (CLICOLOR_FORCE in CI):
  keep ModePlain layout but do NOT strip — i.e. the styled-static
  projection is selected for RunPipelineStatic surfaces; record the exact
  reach you implement in FINDINGS (full three-axis separation is future
  work; this task must at minimum make NO_COLOR strip-only and
  CLICOLOR_FORCE/--color=always force color through pipes without
  animations).
- Extend `ui_test.go`'s Resolve/behavior table: empty NO_COLOR = unset;
  non-empty NO_COLOR keeps ModeStyled on a TTY but renders zero ANSI;
  `--color=never` on a TTY → zero ANSI; `CLICOLOR_FORCE=1` + CI → colored
  styled-static bytes on a pipe.
Commit B: `git commit -m "feat(ui): NO_COLOR per spec (strip color only), CLICOLOR_FORCE, --color=auto|always|never"`

- [ ] **Step 3: Green**
Run: `go test ./... 2>&1 | tail -5` → PASS. Run the TE gate:
`go test ./internal/ui/... ./cmd/... -run TE` → PASS (goldens are
ANSI-stripped; color plumbing must not move them).

- [ ] **Step 4: Task-level verify + merge + ledger.**

#### Outcome — W2.T13
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w2-t13-fang-color` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

### W2.T14: Regression fence, README, VHS — wave close-out

**Branch:** `tui/w2-t14-fence-docs` · **Depends:** W2.T10–T13

**Files:**
- Modify: `internal/ui/pipeline_test.go`, `README.md`
- Create: `docs/vhs/up.tape`, `docs/vhs/down-consent.tape` (+ recorded GIFs
  if the `vhs` binary is available — otherwise commit the tapes and record
  "vhs binary unavailable" in FINDINGS; tapes are the deliverable, GIFs are
  best-effort)

- [ ] **Step 1: Mode-matrix fence** (`pipeline_test.go`): a table test — for
each mode in {ModePlain, ModeStyled, ModeJSON, ModeLive} × {RunPipeline,
RunPipelineStatic}, run a canonical fake producer (RunStarted, ProgressN
start/done, StepLog, a failing step with Stop, Epilogue, error return) into
a `bytes.Buffer` and assert: (a) styled-on-buffer output is byte-identical
to plain output (the per-writer downgrade); (b) JSON mode emits one valid
`{"v":1,...}` object per line, ending `run_done` then `diagnosis`; (c) no
goroutine survives (the existing leak-check pattern in this file, if any —
else `runtime.NumGoroutine` before/after with tolerance); (d) the buffer
never contains ESC bytes in plain/JSON modes.

- [ ] **Step 2: Prompt-fence completeness.** One table test driving every
prompt-capable command (`down`, `trust`, `upgrade`, `pack install`) with
empty-buffer stdin + 5s `-timeout` headroom asserting completion (the §6.3
gate — most already exist from T06/T07/T11; this step makes the TABLE so a
future prompt can't dodge it).

- [ ] **Step 3: README.** New "Terminal output & interactivity" section:
the mode ladder + `--color`; prompt doctrine (TTY-only, `--yes`/`--confirm`
twins, `--no-input`-equivalent behavior, `ACCESSIBLE`); R1/R2/R3 changelog
notes (R3 prominently: piped `down` now requires `--yes`); `explain`,
`status --watch`, `pack install`; JSONL additive fields (T02/T03) with the
pre-freeze EXPERIMENTAL caveat.

- [ ] **Step 4: VHS tapes.** `docs/vhs/up.tape` scripting: `cube-idp up`
against a kind-less demo (or `--help` + `status` if a cluster is
unavailable — tapes must run without a live cluster; use `explain CUBE-0007`
and the down-consent decline path as the second tape). Record GIFs if `vhs`
exists on PATH.

- [ ] **Step 5: The full battery, one last time**
Run: `go build ./... && go vet ./... && go test ./... && go test ./internal/ui/... ./cmd/... -run TE`
Expected: all PASS. This is the Approach-B exit gate: spec §6.1 matrix
green + §6.2 frozen-contract fence green + §6.3 prompt fence green.

- [ ] **Step 6: Commit + merge + ledger**
`git add -A && git commit -m "test(ui)+docs: mode-matrix and prompt fences; README UX contract; VHS tapes"`
HANDOFF: state that Approach B is complete and list anything deferred
(pack-contributed epilogue lines, StepLog JSON projection, full three-axis
color model, status/doctor event-stream migration — all recorded as future
work in the spec).

#### Outcome — W2.T14
- STATUS: `UNCLAIMED`
- BRANCH: `tui/w2-t14-fence-docs` (merged: no)
- COMMITS: —
- FINDINGS: —
- REVIEW: —
- BLOCKERS: —
- HANDOFF: —

---

## Plan self-review record (2026-07-16, plan author)

- **Spec coverage:** WP1→T01, WP2→T02/T03/T04, WP3→T05, WP4→T06, WP5→T07,
  WP6→T11, WP7→T12, WP8→T10+T13, WP9→T08/T09, WP10→T14. TE-1/2/4→T05(+T10
  footer), TE-3→T07. R1→T04, R2→T03, R3→T07. All spec MUSTs mapped.
- **Known deliberate scope cuts** (recorded, not silent): StepLog has no
  JSON projection; deep subprocess-log capture (kind/flux stdout → StepLog)
  is NOT wired by any task — TE-1.4/TE-2.2 are proven against synthetic
  StepLog events, and producer-side capture is future work; TE-2.3's footer
  renders inside the panel, not in the border; Epilogue Context/Registry
  fields are populated only where already in scope.
- **Type consistency check:** `theme.Theme` fields, `event.*` field names,
  `ProgressN/Log/Epilogue`, `PromptsAllowed/Confirm/InputExact`,
  `errExitCode`, `RenderErrorTo` — names match across all tasks that
  consume them.
