# cube-idp — Terminal UX research: rich-by-default, plain-for-machines

**Date:** 2026-07-14
**Status:** DECIDED 2026-07-14 — owner chose **Proposal B, staged A→B**; flag surface = `--progress=auto|plain|live|json` + `CUBE_IDP_PROGRESS` env, `--plain` kept as permanent alias; Charm v2 migration lands with the first live-view PR. Recommended defaults accepted for §5 items 3/5/6/7 (Resolve hardening, experimental JSON schema until D5 freeze, Access-as-data + stable plain lines, B's single-pane watch view). Full record: the plan's Owner Decisions #15.
**Amends:** spec §4.1 ("No full Bubble Tea app in the kernel — a spinner is not a TUI")
**Decision context (2026-07-14):** the DEFAULT human-facing experience becomes a full
rich interactive terminal UX; CI/automation is guarded by env-var / non-TTY detection
and gets byte-stable plain output plus a JSON/structured-event option.

---

## 0. What already exists (ground truth in this repo)

- `internal/ui` is the single print seam: `Printer.Step/Section/Glyph/Warn/Progress/AccessSummary`,
  a package-level `PlainFlag` set once by `cmd/root.go`, and a pure `Resolve(plainFlag, isTTY, ciEnv)`
  precedence function. **The plain format `"▸ [%s] %s\n"` is pinned byte-for-byte by tests**
  (`ui_test.go` asserts zero ANSI escapes, exact bytes, non-TTY⇒plain). Progress is a
  hand-rolled single-line spinner goroutine (`\r\x1b[2K` erase) that emits *zero bytes*
  in plain mode until `Done`.
- `internal/up/up.go` (~370 lines) drives `up` through one `Printer` with three
  `Progress` wraps around the long waits (cluster create, engine install, health poll).
- `cmd/init.go` already runs a 3-field **huh v1** form, gated by `wizardApplicable()`
  (both stdin+stdout TTY, no explicit flags).
- `cmd/status.go` / `cmd/doctor.go` print glyph-prefixed lines and tabwriter tables;
  `doctor.Render` walks `diag.Finding`s.
- `internal/diag` is the typed error model: `CUBE-xxxx` code + summary + cause +
  copy-pasteable remediation; `diag.Render(err)` produces the `✗ CUBE-… / cause: / fix:` block.
- `go.mod`: huh **v1.0.0**, lipgloss **v1.1.0**, bubbletea **v1.3.6 (indirect, via huh)**,
  bubbles v0.21.x (indirect) — all on `github.com/charmbracelet/*` import paths.

Everything below has to slot into that reality, not replace it.

---

## 1. Framework landscape (as of mid-2026)

### 1.1 The Charm v2 event

On **2026-02-23** Charm shipped **Bubble Tea v2, Lip Gloss v2, and Bubbles v2** out of
beta simultaneously ([charm.land/blog/v2](https://charm.land/blog/v2/)), followed by
**huh v2** (`charm.land/huh/v2`, v2.0.3 on 2026-03-09). Key facts:

- **New import paths**: vanity domain `charm.land/bubbletea/v2` etc. (breaking for all
  ~25k dependents; v1 stays on `github.com/charmbracelet/*` and receives bugfixes).
- **"Cursed Renderer"**: ncurses-style cell-diff rendering, claimed orders-of-magnitude
  output reduction — directly relevant to flicker-free live views over SSH/tmux.
- **Declarative `tea.View` struct** instead of `View() string`: alt-screen, mouse mode,
  window title, cursor position/shape are now *view fields*, not imperative commands.
  This removes the classic v1 footgun class where component libraries fought over
  terminal modes.
- **Synchronized output (DEC mode 2026)**: atomic frame updates, no tearing.
- Battle-tested claim: the v2 branches powered **Crush** (Charm's coding agent) in
  production for months before release.
- **huh v2** requires Bubble Tea v2/Lip Gloss v2; its `accessible` mode moved in-tree
  and is a form-level switch (this is what `gh` builds its screen-reader prompter on).

Consequence for cube-idp: **any new TUI work should target the v2 line**; staying on v1
means adopting a framework already in maintenance mode, and huh v1 → v2 migration is
small (3 fields in `cmd/init.go`).

### 1.2 Landscape table

| Framework | What it is | Maintenance (mid-2026) | Fit for "transient live view → clean plain exit" | Verdict for cube-idp |
|---|---|---|---|---|
| **Bubble Tea v2** (`charm.land/bubbletea/v2`) | Elm-architecture TUI runtime | Very healthy; v2 stable Feb 2026; huge ecosystem | **Good**: inline (non-alt-screen) mode manages only the live region; `tea.Println` streams completed lines into native scrollback; program exit leaves the terminal clean. This is exactly the pattern needed | **Adopt** — the live-region runtime |
| **Bubbles v2** | Component lib (spinner, progress, table, viewport, help) | Same release train | Components drop into inline programs | **Adopt selectively** (spinner, progress, table) |
| **Lip Gloss v2** | Styling/layout (already used) | Same release train | N/A (pure rendering) | **Adopt** (upgrade from v1.1.0) |
| **huh v2** | Forms/wizards on BT v2 | v2.0.3 Mar 2026 | Forms are inherently transient; built-in accessible mode | **Adopt** (upgrade `cmd/init.go` from v1) |
| **fang** (`charmbracelet/fang`) | "CLI starter kit": styled cobra help/errors, manpages, completions | Explicitly **experimental**; community already questioning it as a dependency (e.g. CipherSwarmAgent #125 removing it) | Orthogonal (help pages, not live views) | **Rule out for now** — steal the *idea* of styled help/error pages via lipgloss we already have; don't take an experimental dep into the kernel |
| **pterm** | Imperative pretty-printers (spinners, live areas, trees, tables) | Maintained but visibly less momentum than Charm; no v2-class investment | Live "areas" work but redraw model is cruder; mixing pterm+lipgloss = two styling systems | **Rule out** — overlaps 100 % with what `internal/ui`+lipgloss already do better |
| **rivo/tview** (+tcell) | Full-screen widget toolkit (what k9s uses) | Mature, actively maintained | **Poor** — designed for persistent alt-screen apps, not transient progress that exits to scrollback | **Rule out** — cube-idp is a pusher that exits; k9s-style residency is a non-goal |
| **vito/tuist** (Dagger's new engine) | Component TUI framework "designed for infinite scrollback"; per-component render caches, line-level diffs, native scrollback, mouse never captured | Brand-new (Dagger v0.20.2, 2026-03-19); one flagship consumer | **Excellent on paper** — it exists *because* Bubble Tea's managed-viewport model fought terminal scrollback at Dagger's scale | **Watch, don't adopt** — too young for a kernel dep; revisit if our live tree ever needs thousands of lines of streamed spans |
| Hand-rolled ANSI (extend current `ui.Progress`) | What we have: `\r\x1b[2K` + goroutine | Ours | Works for 1 line; multi-line regions, resize, and input handling get hairy fast | **Keep as the plain/CI fallback layer only** |

**Recommendation:** Charm v2 stack (bubbletea v2 in **inline mode** + bubbles v2
spinner/progress/table + lipgloss v2 + huh v2), no fang, no pterm/tview. One ecosystem,
one styling system, already partly in `go.mod`, static-binary friendly (pure Go),
nothing persists after exit.

### 1.3 The one pattern that matters: transient inline program

The requirement "rich live view during long operations, terminal left with plain,
scrollback-friendly lines afterwards" maps to a specific Bubble Tea usage pattern:

- run the program **without alt-screen** (inline mode) — BT manages only the N lines of
  the live region at the bottom;
- every *completed* step is emitted via **`tea.Println`** — it is printed *above* the
  managed region and becomes permanent scrollback, exactly like a plain line;
- the live region holds only *in-flight* state (spinners, health table, elapsed);
- on quit, the final model view collapses to zero lines (or a one-line summary) and the
  scrollback reads like a sane log.

Both Dagger (pre-rewrite) and gh's release-download progress use this shape. Dagger's
2026 move off Bubble Tea is the cautionary tail-risk: at *very* high line volume the
managed-region model strains (their fix: tuist). cube-idp's `up` emits ~10–20 steps,
not thousands of build spans — comfortably inside Bubble Tea's sweet spot.

---

## 2. Exemplar survey — how the best infra CLIs do it

### 2.1 BuildKit / docker buildx — **the gold standard for dual-mode**

- `--progress=auto|tty|plain|quiet|rawjson` (+ `BUILDKIT_PROGRESS` env var).
  `auto` = tty if a terminal, else plain.
- **tty**: multi-line live region, one line per vertex, elapsed timers, log tails under
  active vertexes, collapses finished vertexes; **plain**: timestamped append-only
  lines (`#7 [stage 2/4] RUN …`, `#7 DONE 1.2s`); **rawjson**: JSON-lines marshaling of
  the internal `SolveStatus` events for external programs.
- Architecture is the steal: the solver emits **one canonical event stream**
  (`chan *client.SolveStatus` — `{Vertexes, Statuses, Logs, Warnings}` with digests,
  start/complete timestamps, current/total progress) and
  `util/progress/progressui.Display` is just **one interchangeable consumer** with a
  mode enum (`AutoMode/TTYMode/PlainMode/QuietMode/RawJSONMode`). Renderers never
  invent content; they *project* the same events.
- **Steal:** the event-stream-plus-pluggable-display architecture, the `auto` mode
  ladder, env-var override of progress mode. **Avoid:** rawjson batching quirks
  (moby/buildkit #4769 — consumers wanted strictly one JSON record per line; if we do
  JSON lines, guarantee one event per line from day one).

### 2.2 Dagger — event stream taken to its logical extreme

- Every engine operation emits **OpenTelemetry spans**; the TUI is "a live-streaming
  OTel trace visualizer". Frontends: `--progress auto|plain|tty|dots|logs`.
- v0.20.2 (Mar 2026) replaced Bubble Tea with **tuist**: native scrollback, mouse never
  captured, per-span render caches, inline `/` search. Their stated ideal: *"a TUI that
  feels like normal terminal output, but with the rich structure of a trace viewer"* —
  "run a command, it shows live progress, and then leaves all the output in your
  terminal scrollback". That sentence is cube-idp's UX north star verbatim.
- **Steal:** the design goal (feels like normal output; scrollback survives; exits
  clean); `--progress plain` respected in CI automatically. **Avoid:** OTel-as-UI-bus is
  over-engineering for ~20 steps; their multi-year TUI churn (progrock → bubbletea →
  tuist) shows the cost of maximalism.

### 2.3 Terraform — the structured-output contract

- Human output: plain streamed lines + styled diff; no live TUI at all.
- `terraform plan -json` / `apply -json` emit the **documented machine-readable UI**:
  JSON lines, each with `@level`, `@message`, `@timestamp`, and a `type` discriminator
  (`planned_change`, `apply_progress`, `apply_complete`, `diagnostic`, …), versioned
  and covered by compatibility promises.
- **Steal:** *versioned, documented* JSON-lines schema where `diagnostic` is a
  first-class event type — maps 1:1 to `CUBE-xxxx` diagnoses. **Avoid:** nothing; but
  note Terraform proves rich TTY UX is not required for beloved infra tooling — the
  contract quality is.

### 2.4 GitHub CLI (gh) — TTY discipline + accessibility

- Every command checks TTY-ness of stdin/stdout independently; piped output drops
  color, spinners, and prompts automatically; `GH_FORCE_TTY` overrides; honors
  `NO_COLOR`; `GH_PROMPT_DISABLED` kills interactivity outright.
- Machine mode is per-command `--json <fields>` + `--jq` + `--template` — pull, don't
  stream (fits request/response commands; less fit for our long `up`).
- 2025–26 accessibility push (v2.72+, `gh a11y`): spinners replaced by **static text
  progress lines** ("Working…"), and an **accessible prompter built on charmbracelet/huh**
  (`GH_ACCESSIBLE_PROMPTER`) — numbered-list prompts instead of redraw-heavy menus.
- **Steal:** independent stdin/stdout TTY checks (cmd/init.go already does this);
  huh's accessible mode for the init wizard (free in huh v2); spinner⇒static-text as a
  degradation tier, which our `Progress.Done ⇒ Step` already implements. **Avoid:**
  `--json fields` as the *only* machine mode — `up` needs an event stream, not a final
  document (we should offer both: stream for `up/down`, document for `status/doctor`).

### 2.5 Tilt — the cautionary tale

- Shipped a full-screen terminal HUD; users found scrolling/streaming logs in it
  cumbersome and it had memory issues; Tilt demoted it to `--legacy`, made plain
  `--stream` logs + a **web UI** the real interface.
- **Avoid:** persistent full-screen dashboards for dev-loop tools; when the rich view
  fights the terminal's native strengths (scrollback, copy/paste, grep), users defect.
  Direct evidence for keeping cube-idp's live view *transient and inline*.

### 2.6 k9s — great, and out of scope

- tview/tcell alt-screen application; superb as a *resident cluster browser*. It is a
  different product category: users launch it *as* the activity, not around one.
  cube-idp's non-goal ("gets out of the way") rules this shape out for everything
  except possibly `sync --watch` — and even there Tilt's lesson applies.

### 2.7 flyctl — mid-tier reference

- Charm-ecosystem user; spinners + styled lines during `fly deploy` (BuildKit progress
  embedded for builds, machine-update lines after); `--json` on most commands; honors
  non-TTY. Notable wart: mixed output systems (BuildKit's UI inside flyctl's UI) can
  interleave badly. **Steal:** nothing unique. **Avoid:** two renderers sharing one
  terminal without one owning it — if we embed kind's own progress output, silence it
  and re-emit through our stream (we already own kind as a library).

### 2.8 helm / kind / k3d — the incumbent baseline

- **helm**: near-silent success, table output, no live progress; machine mode via
  `-o json`. Proof that "quiet + fast" is a valid aesthetic, but its silence during
  long installs is exactly the "went silent for minutes" problem our `Progress` fixed.
- **kind**: `✓ Ensuring node image 🖼` check-mark step lines with a TTY-only spinner —
  effectively our current `ui.Printer` design. cube-idp already wraps kind as a
  library and re-emits through its own printer; keep that ownership.
- **k3d**: logrus `INFO[0000]` lines, occasionally emoji; no dual-mode discipline.
  **Avoid:** logger-as-UI — timestamps and levels are noise for humans.

### 2.9 gum / glow — charm's own apps

- **gum**: exposes huh/bubbles primitives (`gum spin`, `gum choose`, `gum input`) to
  shell scripts; every subcommand degrades when non-TTY. Good evidence the components
  survive transient single-shot use. **glow**: alt-screen pager, not relevant.
- **Steal:** gum's per-primitive degradation discipline; also gum-friendliness as a
  target: our plain output should be trivially consumable *by* scripts wrapping us.

### 2.10 skaffold — the structured-event sleeper

- Colored per-artifact log prefixes in the terminal, no full TUI; but its **Event API
  v2** (gRPC + HTTP stream of typed build/deploy/status events) is what powers IDE
  integrations (Cloud Code). A CLI that emits typed events gets IDE/agent integration
  for free. **Steal:** treat the JSON event stream as a *product surface for tooling*
  (editors, AI agents, CI annotators), not just a CI convenience.

### 2.11 Cross-cutting conventions (community consensus)

Detection ladder used by gh/buildx/terraform and codified by clig.dev + no-color.org:

1. Explicit flag always wins (`--plain`, `--progress=…`, `--output=…`).
2. Tool-specific env var (`BUILDKIT_PROGRESS`, `GH_FORCE_TTY`, ours: `CUBE_IDP_PLAIN` /
   `CUBE_IDP_PROGRESS`) — lets CI images set policy once.
3. `stdout` not a TTY ⇒ plain. (Check stderr/stdin separately for prompts.)
4. `CI` env set ⇒ plain (already implemented).
5. `NO_COLOR` set ⇒ no color (may still be line-rewriting; strictest reading: plain).
6. `TERM=dumb` or unset ⇒ plain.

Gaps in our current `Resolve`: it checks (1)(3)(4) but not `NO_COLOR`/`TERM=dumb`, and
the styled mode currently colors output even when `NO_COLOR` is set on a TTY. Cheap fix
regardless of which proposal wins.

---

## 3. Architecture sketch — one event stream, three renderers

The BuildKit lesson, adapted to this codebase with minimum churn:

```
internal/up, internal/apply, internal/engine, internal/pack, internal/doctor
        │  emit (already funneled through one seam today: ui.Printer)
        ▼
   ┌──────────────────────────────┐
   │ internal/ui/event            │   typed, renderer-agnostic events
   │  RunStarted{cmd, cube}       │
   │  StepStarted{stage, msg}     │   stage == today's badge names
   │  StepDone{stage, msg, dur}   │   ("cluster","engine","pack","health","tls",…)
   │  StepFailed{stage, *diag.E}  │
   │  HealthTick{[]ComponentState}│   the health-wait table, updated per poll
   │  Note / Warn{msg}            │
   │  Access{[]PackAccess, hint}  │
   │  Diagnosis{*diag.Error}      │   ALWAYS the last event on failure
   │  RunDone{ok, dur}            │
   └────────────┬─────────────────┘
                │ chan Event (buffered; producer never blocks on UI)
    ┌───────────┼──────────────────────────────┐
    ▼           ▼                              ▼
PlainRenderer  LiveRenderer                 JSONRenderer
(today's       (bubbletea v2 inline:        (one JSON object per line,
 Printer —     tea.Println for done steps,   versioned {"v":1,"type":…},
 byte-stable,  managed bottom region for     diagnosis as first-class
 tests keep    spinners + health table;      event type; stdout)
 passing       exits leaving scrollback)
 unchanged)
```

Load-bearing properties:

- **PlainRenderer IS the current `ui.Printer` plain path.** `Step` becomes
  `emit(StepDone…)` consumed by a renderer that prints `"▸ [%s] %s\n"` — the pinned
  bytes and every e2e assertion survive because the plain projection of each event is
  *defined as* today's output (including "Progress emits zero bytes until Done" ⇒
  plain renderer simply ignores `StepStarted`/`HealthTick`). Migration can even keep
  the `Printer` API as a facade that constructs events, so `internal/up` call sites
  barely change.
- **Renderer selection = existing `Resolve`, extended**: `--output=json` >
  `--plain`/`CUBE_IDP_PLAIN` > non-TTY > `CI` > `NO_COLOR`/`TERM=dumb` > live.
  Selection still happens once, in `cmd/root.go` PersistentPreRunE.
- **Diagnoses become MORE readable, never buried**: `StepFailed`/`Diagnosis` events
  cause the LiveRenderer to (a) stop the live region, (b) `tea.Println` all in-flight
  state as final lines, (c) exit the program, and only then (d) render the diag block
  as a lipgloss panel (code badge, cause, `fix:` in copy-paste-safe plain text) —
  *after* the TUI has released the terminal, so it can never be overwritten or trapped
  in an alt screen. Plain mode: `diag.Render` unchanged. JSON mode: the diagnosis is a
  structured event (`{"type":"diagnosis","code":"CUBE-3002","summary":…,"remediation":…}`)
  — CI systems can annotate PRs off it (Terraform's `diagnostic` precedent).
- **Nothing after exit**: the bubbletea program's lifetime is strictly inside the
  command's `RunE`; `tea.Quit` on `RunDone`/`Diagnosis`; no goroutine survives.
  `sync --watch` (sanctioned foreground mode) is just a LiveRenderer whose model also
  handles fsnotify-driven events until Ctrl-C.
- **JSON stream schema**: JSON lines, one event per line (learn from buildkit #4769),
  `"v":1` version field, documented in docs/ like Terraform's machine-readable UI.
  `status`/`doctor`/`get secrets` additionally get *document*-style `--output json`
  (gh-style final object) since they are request/response commands.

Testing: renderers become independently testable — golden-file tests feed a recorded
event slice into each renderer (`teatest` exists for bubbletea if we want frame
assertions; plain/JSON renderers are pure `io.Writer` funcs). The existing
`ui_test.go`/e2e plain assertions keep guarding the plain projection.

---

## 4. Proposals

All three share: Charm v2 stack, the §3 event stream, `Resolve` hardening
(`NO_COLOR`, `TERM=dumb`, `CUBE_IDP_PLAIN`), JSON-lines event output for `up`/`down`,
plain-output invariant untouched, diagnosis-last rendering rule.

### Proposal A — "Clean exit" (transient live step-tree, minimal surface)

*The Dagger design goal implemented at 1/20th the machinery.*

- **Covered commands:** `up`, `down` get the LiveRenderer (inline bubbletea: completed
  steps stream to scrollback via `tea.Println`; bottom region = current step spinner +
  elapsed + a compact health-wait table during the final wait). Everything else keeps
  today's styled-line output. `init` wizard stays huh (upgraded v1→v2). No `sync --watch`
  UI work (Phase 3 ships it with plain line output).
- **Frameworks:** bubbletea v2 + bubbles (spinner/progress) + lipgloss v2 + huh v2.
- **CI/JSON:** `--plain` as today; `--output=json` on `up`/`down` emits the event
  stream; `status --output=json` emits a final document.
- **Effort:** ~1–1.5 weeks (event types + plain facade ½wk, live model ½wk, JSON ¼wk,
  Charm v2 migration ¼wk).
- **Risks:** low. Charm v2 migration touches huh/lipgloss call sites (small: 2 files).
  Biggest risk is scope creep — A is deliberately the "prove the event stream" step.
- **What it looks like:** `cube-idp up` on a TTY shows check-marked lines scrolling by
  (`✔ [cluster] kind cluster ready 12s`), one animated line + a 4-row component table
  at the bottom during the health wait, then an Access panel; on failure the live area
  collapses and a bordered CUBE-xxxx panel is the last thing on screen.

### Proposal B — "One console" (recommended) — live up/down + rich static status/doctor + wizard

*Everything a human touches gets the new visual language; nothing becomes resident.*

- **Covered commands:** all of A, plus:
  - `status`: rich static render — lipgloss table of components (glyph, name, age,
    message), inventory summary, gateway/access URLs; `--watch` **not** included.
  - `doctor`: findings grouped by severity in bordered sections, remediation styled as
    copy-paste blocks; a final one-line verdict; interactive *only* in that huh offers
    "re-run with --verbose?" style follow-ups when TTY (skippable, never blocking CI —
    same `wizardApplicable` guard as init).
  - `init`: full huh v2 multi-group wizard (name, provider incl. `existing` context
    picker from kubeconfig, engine, gateway host/port with port-conflict pre-check via
    the doctor's `CheckPortFree`, pack multi-select) — replacing the current 3-field form.
  - `diff`/`upgrade --plan`: reuse Section/Glyph styling, no live view.
  - `sync --watch` (when Phase 3 builds it): LiveRenderer persistent pane — last-push
    status line + rolling event list, Ctrl-C exits clean. Scoped design only; no code
    until Phase 3.
- **Frameworks:** as A; bubbles `table` for status; no alt-screen anywhere.
- **CI/JSON:** as A, plus `status/doctor/get secrets --output=json` documents
  (doctor's findings array with codes/severities is CI-annotation gold), and
  `CUBE_IDP_PROGRESS=plain|live|json` env policy knob (BuildKit precedent).
- **Effort:** ~2.5–3 weeks (A + status/doctor rendering 1wk, wizard ¾wk, JSON docs ¼wk).
- **Risks:** moderate-low. Wizard growth must not outpace config schema (validate
  against the same CUE rules — `cubeNameRe` precedent already set). Doctor
  interactivity must stay optional-by-TTY. Test surface grows: golden event-slice
  tests per renderer keep it tractable.
- **What it looks like:** one visual language everywhere — the badge/glyph system you
  already have, elevated: `up` as in A; `status` reads like a mini k9s snapshot that
  exits immediately; `doctor` reads like a lab report; `init` feels like `gh repo create`.

### Proposal C — "Cockpit" (maximal) — persistent live views + full interactivity

- **Covered commands:** all of B, plus `up --watch`-style multi-pane live dashboard
  (step tree + per-component log tails, BuildKit-tty-style collapsing), `status --watch`
  resident view, `sync --watch` with a split pane (file events / reconcile status /
  log tail), interactive doctor with expandable findings and "apply fix" actions,
  fang-style themed help output.
- **Frameworks:** B's stack + bubbles viewport/split layouts; possibly tuist if the
  log-tail volume breaks the managed-region model (unproven dependency).
- **CI/JSON:** as B (the machine story doesn't grow — that's telling).
- **Effort:** 4–6 weeks, plus permanent maintenance drag on every new command.
- **Risks:** **high, and history is against it.** Tilt shipped this and retired it to
  `--legacy`; Dagger needed a bespoke framework and three rewrites to make dense live
  trees feel native; log tails in a managed region fight scrollback/copy-paste (the #1
  user complaint pattern); "apply fix" actions from doctor blur the pusher-not-operator
  line; alt-screen-ish residency contradicts "gets out of the way". Also inflates the
  contract-test story: every live pane needs a plain projection to keep CI honest.
- **Honest assessment:** C's extra surface serves the demo more than the daily user.
  Its one genuinely valuable piece — a good `sync --watch` pane — is already in B's
  scope for Phase 3.

### Recommendation

**B**, executed as **A first** (the event stream + `up`/`down` live view is a shippable
increment that proves the architecture), then B's status/doctor/wizard wave. Adopt the
Charm v2 line in the same change. Defer C-class ideas until `sync --watch` usage data
exists; the §3 architecture leaves the door open (a dashboard is just another renderer).

---

## 5. Decisions needed from the owner

1. **Proposal**: A, B (recommended, staged A→B), or C.
2. **Flag surface**: keep `--plain` + add `--output=json`? Or adopt BuildKit-style
   `--progress=auto|plain|live|json` as the single knob (with `--plain` kept as an
   alias for compatibility with existing tests/docs)?
3. **Env vars**: bless `CUBE_IDP_PROGRESS` (policy) and honor `NO_COLOR`/`TERM=dumb`
   in `Resolve` — OK to tighten even before the TUI lands?
4. **Charm v2 migration timing**: move to `charm.land/*` v2 now (huh v1→v2 touches
   only `cmd/init.go`), or pin v1 and migrate with the first live view? (Recommend:
   with the first live view, one PR, since bubbletea is currently only an indirect dep.)
5. **JSON schema ownership**: commit to a versioned, documented event schema
   (Terraform-style compatibility promise) at v1alpha, or label it experimental until
   the v1 config freeze (D5)?
6. **Plain-mode Access summary**: `AccessSummary` is currently styled-mode-only; should
   the JSON/plain modes gain an equivalent (as data / as stable lines), or stay silent?
7. **`sync --watch` UX ambition** (Phase 3): B's single-pane rolling view vs C's split
   pane — decide when Phase 3 is planned, but the event stream should carry watch
   events from day one.

---

## Appendix: sources

- Charm v2 announcement: https://charm.land/blog/v2/ (BT/Lip Gloss/Bubbles v2, 2026-02-23)
- Bubble Tea v2 details: https://github.com/charmbracelet/bubbletea/discussions/1374
- huh v2 releases: https://github.com/charmbracelet/huh/releases (v2.0.3, 2026-03-09)
- buildx progress modes: https://docs.docker.com/reference/cli/docker/buildx/build/
- BuildKit progressui: https://pkg.go.dev/github.com/moby/buildkit/util/progress/progressui ;
  rawjson line-format issue: https://github.com/moby/buildkit/issues/4769
- Dagger TUI rewrite (tuist, v0.20.2 2026-03-19): https://dagger.io/changelog/ ;
  TUI docs: https://docs.dagger.io/features/visualization
- Terraform machine-readable UI: https://developer.hashicorp.com/terraform/internals/machine-readable-ui
- gh accessibility (accessible prompter on huh, spinner→text): https://github.blog/engineering/user-experience/building-a-more-accessible-github-cli/ ;
  env vars: https://cli.github.com/manual/gh_help_environment
- Tilt HUD retirement: https://blog.tilt.dev/2020/06/19/the-right-display-for-now.html ;
  https://docs.tilt.dev/cli/tilt_up.html (`--legacy`, `--stream`)
- fang (experimental): https://github.com/charmbracelet/fang
- Conventions: https://clig.dev/ ; https://no-color.org/
