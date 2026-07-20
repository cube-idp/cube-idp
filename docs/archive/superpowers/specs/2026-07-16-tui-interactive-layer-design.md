# cube-idp — Terminal UI, Approach B: the interactive layer with graceful fallback

**Date:** 2026-07-16
**Status:** Approved design, pending implementation plan
**Context:** A 7-agent coordinated research run (1 codebase auditor, 3 research
lenses, 3 plan authors; 27 primary sources) produced the report at
<https://claude.ai/code/artifact/d9db2a5d-be93-42d6-87ea-ffc4d6ba512a>.
The owner selected **Approach B — full interactive layer**. This spec turns
that decision into buildable contracts. It **extends** the parent UX spec
[2026-07-14-cube-idp-ux-design.md](2026-07-14-cube-idp-ux-design.md) (event
stream, renderer projections, mode ladder, byte-preservation) — nothing here
overrides that spec's frozen contracts except the three ratified changes in §5.

**Normative language:** MUST / MUST NOT / SHOULD are used in the RFC-2119
sense. Every MUST in §2 (Target Experience) and §6 (Conformance) is a merge
gate: a task is not DONE while any of its MUSTs fail.

---

## 0. Summary

The audit's verdict: the architecture (event stream → four renderer
projections, CUBE-XXXX diagnostics, resolve ladder) is correct; the
presentation layer is unfinished. Approach B keeps every existing seam and
delivers, in order:

1. one adaptive **theme** package (kills the six duplicated palettes),
2. an **additively widened event vocabulary** (failures carry message +
   duration; pack N/M; structured Epilogue),
3. the **live step tree** — BuildKit-style collapse with bounded log tails,
4. one **prompt seam** on huh v2 (replaces the three divergent prompt stacks),
5. **consent flows** (Terraform-style destroy preview for `down`),
6. **menus** (pack MultiSelect), **`status --watch`**, styled help via
   **fang v2**, and `cube-idp explain CUBE-XXXX`,
7. **exit-path and error hygiene** (no `os.Exit` in RunE, writer-aware
   diagnostics).

The **mandatory acceptance bar** is §2: the delivered code MUST reproduce the
Target Experience frames, enforced by golden tests (§6) — not by eyeball.

---

## 1. Decisions

| # | Decision | Choice |
|---|----------|--------|
| 1 | UI stack | **Charm v2 only** — the versions already pinned in `go.mod` (`charm.land/bubbletea/v2 v2.0.8`, `bubbles/v2 v2.1.1`, `lipgloss/v2 v2.0.5`, `huh/v2 v2.0.3`). No tview, no pterm (both evaluated and rejected — alt-screen/second-stack and repaint-conflict respectively). **Do not bump these majors mid-project.** |
| 2 | Screen model | **Inline forever** for one-shot commands. `tea.View.AltScreen` is never set. Completed lines go to native scrollback via `p.Println`; only in-flight state lives in the managed bottom region. (Pattern: Bubble Tea `examples/package-manager`; rationale: scrollback is the bug-report transcript and the CI capture.) |
| 3 | Where richness lives | Only under `ModeStyled`/`ModeLive` on a real TTY. `ModePlain` and `ModeJSON` change **only** via the ratified items in §5. The resolve ladder (parent spec §6) is untouched. |
| 4 | Prompt doctrine | gh's rule, hard: **no prompt ever fires unless stdin AND stdout are TTYs** and the mode is Styled/Live and no `--yes`/`--no-input`-class flag suppressed it. Every prompt has a flag twin; after an accepted prompt the CLI prints the scriptable twin as a dim hint. Non-TTY behavior is per-command (see WP5). |
| 5 | Prompt/pipeline ownership | A huh form and the live Bubble Tea program MUST NOT share the terminal: prompts run **before** `ui.RunPipeline`, never inside a producer. Enforced by a debug assertion in the prompt package (§3 WP4). |
| 6 | Destructive consent | `cube-idp down` adopts the Terraform model: resource preview → typed-name confirm on a TTY; **refuses** (exit 1, CUBE code) on non-TTY without `--yes`. This is ratified change R3 (§5) — the eight e2e `down` call sites gain `--yes` in the same commit. |
| 7 | New dependencies | Exactly two, both pinned: `charm.land/fang/v2` (experimental — thin, replaceable layer; our `ErrorHandler` keeps CUBE boxes ours) and `github.com/charmbracelet/x/exp/teatest/v2` (test-only). |
| 8 | Accessibility | `$ACCESSIBLE` (non-empty) → `Form.WithAccessible(true)` on every huh form + static `working: <step>…` lines instead of spinners. This mirrors gh's documented retrofit. |
| 9 | Effort & staging | Wave 1 (WP1–WP5, WP9): ~12–15 person-days. Wave 2 (WP6–WP8, WP10): ~4–6 person-days. Waves are independently shippable. |
| 10 | Execution mode | One dispatched agent per plan task, ticks in the plan file, ledger in `.superpowers/sdd/progress.md` — same regime as the org migration. |

---

## 2. THE TARGET EXPERIENCE — normative, mandatory

The four frames below are copied from §3 of the research artifact and are the
**acceptance contract**. Delivered code MUST reproduce them structurally.
"Structurally" is defined per frame; `«angle-quoted»` spans are dynamic
(names, durations, digests, counts, URLs) and everything else — glyphs,
wording, prefixes, ordering, alignment rules, color roles — is literal.

Color roles (theme tokens, §3 WP1): `OK`=green ✔, `Err`=red ✗, `Warn`=amber,
`Badge`=blue `[stage]`, `Dim`=faint. Semantic colors MUST come from the 8/16
basic ANSI range (Primer rule: the user's terminal theme keeps control) and
meaning MUST survive with color stripped (glyph + word always present).

### TE-1 · `cube up`, live mode: collapse + N/M progress

```
✔ [config]   cube "«voodoo»" loaded and validated
✔ [cluster]  kind cluster ready (context kind-«voodoo»)      («28s»)
✔ [registry] zot ready at «zot.cube-idp-system:5000»          («6s»)
✔ [engine]   flux installed                                   («2s»)
⠋ [packs]    delivering «gitea@0.1.0»                         «3/7»
             ██████████░░░░░░░░░░░░░░ «43»%
             │ «fetching oci://ghcr.io/cube-idp/packs/gitea:0.1.0»
             │ «verifying digest sha256:9f2c…»
```

Requirements:

- **TE-1.1** Completed steps live in scrollback as
  `✔ [stage] msg` + right-aligned `(dur)` in `Dim`. Durations round to
  seconds (`time.Duration.Round(time.Second)`), rendered in a right-aligned
  column computed from terminal width (`tea.WindowSizeMsg`), ANSI-aware
  padding via `lipgloss.Width`. Steps that complete in <1s MAY omit `(0s)`
  only if today's plain output omits it (it does not — keep parity).
- **TE-1.2** Stage badges are left-aligned in a fixed column sized to the
  longest known stage name (stage metadata map, WP1) so messages start at one
  x-position. No more ragged `[config]`/`[packs-crd]` starts.
- **TE-1.3** The active step renders in the managed region: `bubbles/v2
  spinner` + `[stage]` + present-tense message; when the step carries
  `Index/Total`, a right-aligned `«n»/«m»` counter on the same line and a
  `bubbles/v2 progress` bar (width ≤ 30 cols) on the next line.
- **TE-1.4** Bounded log tail: while a step is active, up to the **last 5
  lines** of its captured subprocess/reconcile output render beneath it in
  `Dim` with the `│ ` prefix. On success the tail vanishes (BuildKit
  collapse); it never reaches scrollback.
- **TE-1.5** The managed region MUST clamp to terminal width and never wrap
  (truncate with `…`); resize (`tea.WindowSizeMsg`) reflows the region.
- **TE-1.6** Nothing in TE-1 changes `ModePlain`/`ModeJSON` output except
  ratified item R1 (§5).

### TE-2 · failure: dump the log, then the box — most important info last

```
✗ [packs]    «gitea@0.1.0 pull failed»                        («4s»)
  │ «GET https://ghcr.io/v2/…/gitea/manifests/0.1.0 → 401»

╭─ ✗ CUBE-«4012» ─────────────────────────────────────────╮
│ «cannot pull pack "ghcr.io/cube-idp/packs/gitea:0.1.0"» │
│ cause: «registry returned 401: authentication required» │
│ fix:   «cube-idp repo login ghcr.io»                     │
╰────────────────── cube-idp explain CUBE-«4012» ─────────╯
```

Requirements:

- **TE-2.1** A failed step's scrollback line is
  `✗ [stage] msg (dur)` — message and duration are MANDATORY. This kills
  today's information-free bare `✗ [stage]` ([live.go:91–94]) and requires
  `event.StepFailed{Msg, Dur}` (WP2).
- **TE-2.2** The failed step's captured tail (up to its full buffer, not the
  5-line window) is flushed to scrollback in `Dim` beneath the ✗ line,
  **before** the diagnosis box. Logs are never lost behind progress UI
  (clig.dev rule).
- **TE-2.3** The CUBE box keeps its current anatomy (red rounded border, code
  badge, `cause:`, unstyled copy-paste-safe `fix:` — [rendererr.go]) and gains
  a footer rendering `cube-idp explain CUBE-XXXX`. The footer appears only
  once WP8's `explain` command exists (same wave); the box MUST NOT advertise
  a command that doesn't run.
- **TE-2.4** Ordering guarantee unchanged: step lines → RunDone → diagnosis
  last, after the pipeline released the terminal (parent spec §4.2).
- **TE-2.5** `fix:` line content remains unstyled (copy-paste-safe) in every
  mode.

### TE-3 · `cube down`: Terraform-style consent (TTY only)

```
Destroying cube "«voodoo»" will delete:
  • kind cluster + kubeconfig context «kind-voodoo»
  • zot registry volume, generated TLS certs
  • «7» installed packs

? Type the cube name to confirm: ▊
  hint: cube-idp down --yes
```

Requirements:

- **TE-3.1** The preview enumerates the **real** deletion set for the active
  config branch: kind/k3d → whole cluster + context; `--keep-cluster` /
  provider `existing` → engine uninstall + CoreDNS revert + inventory cascade
  (mirror `runDown`'s actual paths in [cmd/down.go]); pack count from
  `cube.Spec.Packs`; OS trust-store revert line only if `trust.LoadState`
  says installed.
- **TE-3.2** Confirmation is a huh v2 `Input` validated against the exact
  cube name (clig.dev "severe" tier). `--confirm=<name>` and `--yes` are the
  scriptable twins; the dim hint line is mandatory after the prompt renders.
- **TE-3.3** Decline or mismatched name → print `aborted — nothing was
  changed` (the exact wording [cmd/trust.go] already uses), exit 0.
- **TE-3.4** Non-TTY (or `ModePlain`/`ModeJSON`) without `--yes` → **refuse**:
  exit 1 with new code `CUBE-0010` (`confirmation required; pass --yes`),
  never proceed, never hang. This is ratified change R3 (§5).
- **TE-3.5** The prompt runs before `ui.RunPipeline` (Decision 5). `$ACCESSIBLE`
  swaps the huh input for a plain sequential prompt (Decision 8).

### TE-4 · success epilogue: what users actually need

```
✔ cube "«voodoo»" is up («2m13s»)
  gateway     «https://voodoo.local:8443»
  context     «kind-voodoo»
  registry    «zot.cube-idp-system:5000»
  next: cube-idp status · credentials: cube-idp get secrets
```

Requirements:

- **TE-4.1** Rendered from a structured `event.Epilogue` (WP2) — never from a
  glyph-laden `Note` string. The ✔ is presentation (renderer-supplied), not
  content; this removes the leaked glyph at [up.go:385] (ratified R2, §5).
- **TE-4.2** Key–value rows are left-aligned (keys in `Dim`, one column), the
  gateway URL in `Badge` blue and — where the terminal supports it — a
  lipgloss v2 hyperlink. Total run duration in `Dim` on the headline.
- **TE-4.3** The `next:` hint line is `Dim` and lists at least `cube-idp
  status` and the credentials command. (helm-NOTES pattern; packs contributing
  epilogue lines is out of scope for B — recorded as future work.)
- **TE-4.4** TE-4 applies to `ModeStyled`/`ModeLive` only. `ModePlain` keeps
  today's exact epilogue bytes except the R2 one-glyph change; `ModeJSON`
  gains an additive `epilogue` record.

### Conformance is testable or it didn't happen

Every TE requirement above maps to a named golden/unit test in §6's matrix.
A reviewer MUST be able to run `go test ./internal/ui/... -run 'TE'` and see
the frames enforced.

---

## 3. Work packages

### WP1 — `internal/ui/theme`: one adaptive palette, glyphs, stage metadata

New leaf package (imports only `charm.land/lipgloss/v2` + stdlib), dissolving
the ui↔render import cycle documented at [internal/ui/render/styled.go:12–17].

- `Theme` struct: `Badge, Msg, OK, Err, Warn, Dim, Section, ErrPanel,
  ErrLabel lipgloss.Style`, built from `lipgloss.LightDark(isDark)` pairs.
  Lip Gloss v2 removed `AdaptiveColor` — LightDark is the blessed pattern
  (<https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.0>). Semantic
  colors from basic ANSI only (Decision on color roles, §2). Replaces the
  hardcoded 256-palette values `39/42/196/214/245/240` duplicated in
  [internal/ui/ui.go:193–196,238–243,285], [internal/ui/rendererr.go:65–70],
  [internal/ui/render/styled.go:18–22], [internal/ui/render/live.go:66–74],
  [cmd/status.go:135–138], [internal/doctor/doctor.go:119–123].
- `New(isDark bool) Theme` (pure, for tests/live program) and
  `Detect(in, out *os.File) Theme` via `lipgloss.HasDarkBackground` — guarded
  by a real-TTY check, **defaulting to dark on any doubt** (OSC background
  queries can hang under tmux/screen; worst case must equal today).
  Inside the live program, adaptivity instead comes from
  `tea.RequestBackgroundColor` → `tea.BackgroundColorMsg` in `Init`/`Update`.
- Glyph constants: `▸ ✔ ✗ ⚠ !` — single source, ending the ▸-vs-✔ split
  between plain and live.
- `Stages map[string]StageMeta{Label, Group}` for the stage names in
  [internal/ui/event/event.go]'s doc (config, ca, cluster, registry,
  packs-crd, engine, tls, pack, lock, dns, health, packs, cnoe + down's
  stages) — presentation metadata without touching producers; also yields the
  TE-1.2 badge column width.

Acceptance: `grep -rn '"39"\|"245"\|"196"' internal/ cmd/ --include='*.go'`
hits only `theme.go`; theme unit test proves light ≠ dark rendering and zero
ANSI at the ascii color profile.

### WP2 — event vocabulary widening (additive; pre-freeze window)

In [internal/ui/event/event.go]:

- `StepFailed` gains `Msg string; Dur time.Duration`.
  [internal/ui/console.go:126–135] (`ConsoleProgress.Stop`) currently
  discards the message it already holds — it MUST now emit both; the
  pipeline's unwind-guard `StepFailed` ([internal/ui/pipeline.go]) carries
  `Msg` too.
- `StepStarted`/`StepDone` gain optional `Index, Total int` (pack N/M);
  emitted from `internal/up`'s pack-delivery loop.
- New `StepLog{Stage, Line string}` — the bounded-tail feed (TE-1.4/TE-2.2).
  Producers forward subprocess/reconcile lines; renderers own windowing.
  Plain/styled/JSON projections: zero bytes (tail is live-only richness);
  JSON MAY carry it later — out of scope now.
- New `Epilogue{Cube, GatewayURL, Context, Registry, Hint string}` (TE-4).
- JSONL: additive `msg`/`dur_ms` on `step_failed`, `idx`/`of` (omitempty) on
  steps, new `epilogue` record — legal under the documented
  v1-EXPERIMENTAL window (parent spec §5.3); changelog note required before
  the D5 v1 freeze.

The closed event switch in all four renderers forces compile-time coverage of
every new type; `render/plain.go` adds explicit zero-byte arms so frozen
bytes cannot drift silently.

### WP3 — live renderer: the step tree (TE-1, TE-2, TE-4)

Evolve `liveModel` in [internal/ui/render/live.go] — never rewrite; its
lifecycle guarantees (inline mode, `p.Println` scrollback, eofMsg quit,
ctrl-c → context cancel, guaranteed drain, nil input on pipes
[internal/ui/pipeline.go:141–147]) are preserved verbatim.

- `Init` returns `tea.Batch(m.spin.Tick, tea.RequestBackgroundColor)`;
  `tea.BackgroundColorMsg` selects `theme.New(isDark)`.
- Scrollback lines per TE-1.1/TE-2.1; duration column per TE-1.1;
  badge column per TE-1.2 (from `theme.Stages`).
- Managed region per TE-1.3–TE-1.5: spinner line per open step, progress bar
  (`progress.New(progress.WithWidth(30))`, `ViewAs(float64(n)/float64(m))`)
  while `Index/Total` present, ≤5-line `StepLog` ring buffer per open stage.
- On `StepFailed`: flush the failed stage's full log buffer to scrollback
  (TE-2.2) before the ✗ line collapses the region entry.
- Health table: keep the `HealthTick` component rows visible until `RunDone`
  (today they vanish when the health stage closes — data dropped invisibly);
  render via `lipgloss/v2` table sized to width.
- `RunDone`: final scrollback line `✔ up finished in «dur»` /
  `✗ up failed after «dur»`, then the TE-4 epilogue block when the run
  succeeded.
- Pattern references: Bubble Tea package-manager example
  (<https://github.com/charmbracelet/bubbletea/tree/main/examples/package-manager>),
  practitioner rules (never block in Update/View, `tea.Sequence` for ordered
  finals) — <https://leg100.github.io/en/posts/building-bubbletea-programs/>.

### WP4 — `internal/ui/prompt`: the one prompt seam (huh v2)

New package wrapping huh v2 exactly the way [cmd/init.go] already proves
(dual-TTY gate, `WithInput/WithOutput`, `$ACCESSIBLE`):

- `Allowed(in io.Reader, out io.Writer) bool` — true iff both are real TTYs
  (`ui.IsTerminal`) AND `ui.CurrentMode() ∈ {ModeStyled, ModeLive}`. The
  single gate every prompt routes through.
- `Confirm(in, out, ConfirmOpts) (bool, error)`, `Input` (typed-name consent),
  `Select[T]`, `MultiSelect[T]` — all `WithAccessible(os.Getenv("ACCESSIBLE")
  != "")`, themed from WP1 (`huh.ThemeCharm(isDark)`-style value themes).
- When `!Allowed`, each helper returns the caller-supplied non-interactive
  outcome (default value or a `diag` error naming the flag twin) — it MUST
  NOT read or write anything.
- Debug-build assertion (Decision 5): panics if a pipeline is active when a
  prompt engages.
- After any accepted prompt: dim hint with the scriptable twin (§2 TE-3.2).
- Migrations: [cmd/trust.go:50–58] (raw stdout bufio y/N, no TTY guard —
  piped stdin silently reads EOF as "no" today) and
  [internal/plugin/trust.go:163–171] (raw os.Stderr/os.Stdin, bypasses cobra
  streams). The plugin path's non-interactive `CUBE-7104` refusal
  ([internal/diag/codes.go:131]) is preserved byte-for-byte — it is a
  security gate.

### WP5 — consent flows (TE-3)

- `cube-idp down`: preview + typed-name confirm per TE-3, `--yes` and
  `--confirm=<name>` flags, refuse-on-non-TTY per TE-3.4/R3. The eight e2e
  call sites gain `--yes` in the same commit:
  [tests/e2e/e2e_test.go:88,167],
  [tests/e2e/phase3_test.go:141,301,356,367,439,496]; `cmd/down_test.go`
  gains the never-blocks-on-buffer fence.
- `cube-idp upgrade` (after `--plan` reports drift, TTY only): one optional
  `Confirm` — `apply now (runs cube-idp up)?`, default No; non-TTY behavior
  unchanged.
- `cube-idp trust`: keep `--yes` and the exact consent copy; huh Confirm on a
  TTY; current text fallback otherwise.

### WP6 — menus (Wave 2)

- Bare `cube-idp pack install` on a TTY → huh `MultiSelect` (filtering on)
  over the pack catalog (`ghcr.io/cube-idp/packs`), options as
  `name@version — description`; one summary `Confirm`; then the dim
  scriptable-twin hint. With positional args or non-TTY: never prompts —
  today's path byte-identical.
- Where a single choice is needed (future multi-cube configs), huh `Select`
  — no bespoke pickers.

### WP7 — `status --watch` (Wave 2)

gh-run-watch model (<https://cli.github.com/manual/gh_run_watch>): the watch
is the one-shot status view redrawn on a timer, not a separate TUI.

- `--interval` (default 3s), `--exit-status` (non-zero if any component
  unhealthy — CI gate: `cube-idp status --watch --exit-status && run-e2e`),
  `--compact` (hide healthy rows).
- Implementation: a small inline Bubble Tea tick loop invoking the existing
  status renderer; non-TTY + `--watch` → plain re-render per interval, no
  ANSI clearing.

### WP8 — styled help + `explain` + color-spec compliance (Wave 2)

- `fang.Execute(ctx, root, fang.WithColorSchemeFunc(<from theme>),
  fang.WithErrorHandler(<ui.RenderErrorTo>))`
  (<https://github.com/charmbracelet/fang>): styled help/usage, silent-usage
  on error, `--version` from build info, manpages, completions. CUBE-XXXX
  rendering stays in [internal/ui/rendererr.go] — fang never formats domain
  diagnostics. Preserve the `cube-idp-<name>` exec-plugin fallthrough in
  [cmd/root.go]. Pin fang; keep a zero-dep cobra-template fallback decision
  open at PR time.
- `cube-idp explain CUBE-XXXX`: prints the code's summary, documented range
  meaning, and remediation from [internal/diag/codes.go] (rustc `--explain`
  pattern, <https://doc.rust-lang.org/error_codes/error-index.html>). Enables
  the TE-2.3 box footer.
- NO_COLOR per spec (<https://no-color.org>): non-empty value required
  (empty = unset — today's [internal/ui/ui.go:76,92] deviates); strips color
  only, not layout/glyphs. Honor `CLICOLOR_FORCE=1` to re-enable color in CI
  (GitHub Actions renders ANSI; <https://bixense.com/clicolors/>); add
  `--color=auto|always|never` overriding all env vars.

### WP9 — exit-path and error hygiene

- Typed sentinel in [cmd/exit.go] (`exitStatus{code int}` implementing
  `error`); `ExitCodeFor` maps it beside the `*exec.ExitError` plugin
  passthrough. Replace `os.Exit(1)` inside RunE at [cmd/doctor.go:114,119]
  and [cmd/upgrade.go:27] — mandatory before any TUI wraps these commands
  (a killed program must never leave the terminal raw). Pin exit codes in
  `cmd/exit_test.go` **before** refactoring.
- The last unstructured user-facing error, [cmd/upgrade.go:20–21] (raw
  `fmt.Errorf` with embedded newline), becomes `diag.New` with new code
  `CUBE-0009` in the 0xxx command-contract range.
- `ui.RenderErrorTo(w io.Writer, err error)` in [internal/ui/rendererr.go]:
  styled panel only when `w` is a real terminal, else `diag.Render` verbatim
  — main.go calls it with `os.Stderr`, fixing ANSI borders landing in
  `2>file` redirects. `Printer.RenderError` stays for
  [internal/syncer/watch.go]'s mid-stream use.
- Delete the hand-rolled goroutine spinner ([internal/ui/ui.go:280–373],
  raw `\r\x1b[2K` writes) — bubbles/spinner in the live renderer becomes the
  only animation system. Document in ui.go: Printer = static lines only;
  anything animated goes through the pipeline.

### WP10 — regression fence, docs, demo (Wave 2 close-out)

- Mode-matrix table test in [internal/ui/pipeline_test.go]: for each mode ×
  {RunPipeline, RunPipelineStatic}, byte-golden against pre-change output
  (styled-on-pipe projects Plain via the per-writer downgrade), JSONL
  validity + diagnosis-last on failure.
- Prompt-gating fence (§6.3).
- README: prompt gating rules, `--yes`/`--confirm` twins, `ACCESSIBLE`,
  `--color`, `explain`, watch flags.
- VHS tape (<https://github.com/charmbracelet/vhs>) of `cube-idp up` +
  failure + `down` consent, checked into `docs/vhs/`, regenerated per release
  — the human-eye complement to the golden tests.

---

## 4. Contracts that MUST NOT change

Unchanged and re-asserted by tests (parent spec sections in parentheses):

1. **Byte-frozen plain projection** ([internal/ui/render/plain.go], §5.1) —
   except R1/R2 below.
2. **JSONL event stream** shape — additive fields only (§5.3).
3. **`-o json` documents** ([cmd/output.go]) — untouched.
4. **Resolve ladder + per-writer downgrade**
   ([internal/ui/ui.go:97–124,168–179], §6) — WP8's NO_COLOR/CLICOLOR work
   refines inputs to the ladder, not its precedence.
5. **CUBE-XXXX codes and diagnosis-last ordering** ([internal/diag],
   [main.go]) — codes are append-only.
6. **`ExitCodeFor` plugin passthrough** ([cmd/exit.go]).
7. **Nil-input rule for live on pipes** ([internal/ui/pipeline.go:141–147]).
8. **CUBE-7104 plugin-trust refusal semantics** — byte-for-byte.
9. **CI must never hang**: no prompt without the WP4 gate; per-test timeouts
   make a violation a fast failure, not a stuck pipeline.

---

## 5. Ratified deviations (owner approval of this spec ratifies these)

Same governance path as the Access-summary block (parent spec §9). All three
land before the D5 v1 schema freeze, each with goldens updated in the same
commit and a changelog entry.

| # | Change | Rationale |
|---|--------|-----------|
| **R1** | `ModePlain`/`ModeStyled` project `StepStarted` as a `▸ [stage] msg...` start line (today: zero bytes). | Fixes audit P12 — a minutes-long kind/engine wait is currently invisible in CI; hung and slow are indistinguishable. clig.dev: respond <100ms, discrete start/finish lines when not a TTY. |
| **R2** | The `✔` leaves the up-epilogue **content**: [up.go:385]'s Note becomes `event.Epilogue`; plain bytes change by exactly that one glyph (renderer re-adds it as presentation). JSON gains the structured `epilogue` record. | Fixes audit P3 — presentation baked into content leaks into JSON and CI logs, violating the content/presentation split the package documents. |
| **R3** | `cube-idp down` on non-TTY without `--yes` refuses (exit 1, `CUBE-0010`) instead of proceeding. Eight e2e call sites (§3 WP5) gain `--yes` in the same commit. | clig.dev severe tier + Terraform doctrine: never silently destroy in CI. Today a piped `down` destroys everything with zero consent surface. |

---

## 6. Conformance test contract (mandatory)

### 6.1 The TE golden matrix

Model-driven tests (the existing [internal/ui/render/live_test.go] style —
drive `Update`/`View`/`scrollbackLine` directly, no PTY) with pinned
determinism: `tea.WithWindowSize(80, 24)`-equivalent width injection and a
pinned color profile. Two assertion levels per frame:

- **content/layout**: ANSI-stripped output equals a golden fixture under
  `internal/ui/render/testdata/te*.golden` (dynamic `«»` spans injected as
  fixed test values);
- **semantics**: targeted assertions that spans use the correct theme role
  (e.g. the ✗ renders via `theme.Err`, the fix: line contains zero ANSI).

| Frame | Requirement | Test (name is normative) |
|-------|-------------|--------------------------|
| TE-1 | 1.1–1.5 layout, collapse, N/M, tail window | `TestTE1_UpLiveFrame`, `TestTE1_TailBounded`, `TestTE1_DurationColumn` |
| TE-2 | 2.1 failure line | `TestTE2_StepFailedCarriesMsgDur` |
| TE-2 | 2.2 log flush before box | `TestTE2_FailureFlushesTail` |
| TE-2 | 2.3 box anatomy + explain footer | `TestTE2_DiagBoxGolden` (rendererr_test.go) |
| TE-3 | 3.1–3.5 preview, typed consent, refuse | `TestTE3_DownPreviewGolden`, `TestTE3_NonTTYRefusesWithoutYes`, `TestTE3_DeclineAbortsCleanly` |
| TE-4 | 4.1–4.4 epilogue block | `TestTE4_EpilogueGolden` (live + styled), `TestTE4_PlainBytesR2Only` |

CI gate: `go test ./internal/ui/... ./cmd/... -run 'TE'` green is a merge
requirement for every WP that touches a frame. A WP task is DONE only when
its matrix rows pass.

### 6.2 Frozen-contract fence

`go test ./...` plus e2e (`CUBE_IDP_E2E=1`, local port squat:
`CUBE_IDP_E2E_GATEWAY_PORT=18443`). The plain/JSON goldens
([render/plain_test.go], [render/json_test.go]) are the merciless backstop —
they may change only in the three R-commits, each diff reviewable in
isolation.

### 6.3 Prompt-gating fence

Table test: every prompt-capable command path (`down`, `trust`, `upgrade`,
`pack install`, plugin trust) completes without blocking when stdin is an
empty `bytes.Buffer` — with per-test timeouts so a violation fails in
milliseconds, not as a hung e2e suite. `prompt.Allowed` MUST be false for
every non-`*os.File` stream combination.

### 6.4 Visual review

The WP10 VHS tapes are attached to the wave-closing PR. Golden tests prove
structure; the tape proves it also *looks* right — both are required for the
wave sign-off, reviewed against §2's frames.

---

## 7. Risks

1. **A missed TTY gate hangs CI** — the worst failure mode. Mitigation: the
   single `Allowed()` gate, the §6.3 fence, per-test timeouts.
2. **Terminal-background detection** (`HasDarkBackground` /
   `RequestBackgroundColor`) can hang or lie under tmux/screen/SSH.
   Mitigation: real-TTY guard + dark default; worst case equals today.
3. **Plain-byte governance**: R1–R3 are the only sanctioned drifts; goldens
   in the same commit; anything else that moves plain bytes is a bug by
   definition.
4. **fang is experimental**: pinned, thin, with the in-repo cobra-template
   fallback decision recorded at PR time (WP8).
5. **Inline-region behavior on narrow/resizing terminals**: clamp + truncate
   rules (TE-1.5), model-driven resize tests; never touch pipeline lifecycle
   code in visual commits.
6. **`down` muscle-memory change** (R3 + TTY consent): release-note
   prominently; `--yes` documented in README and the refusal message itself.
7. **Charm v2 majors are young**: versions stay pinned for the project's
   duration (Decision 1); upgrade decisions are their own future task.

---

## 8. References

Research report (the source of §2's frames — the artifact is the visual
authority if this file and it ever disagree):
<https://claude.ai/code/artifact/d9db2a5d-be93-42d6-87ea-ffc4d6ba512a>

Ecosystem: Charm v2 announcement <https://charm.land/blog/v2/> · Bubble Tea
v2 upgrade guide
<https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md> ·
package-manager example
<https://github.com/charmbracelet/bubbletea/tree/main/examples/package-manager>
· Bubbles v2 <https://github.com/charmbracelet/bubbles/releases/tag/v2.0.0> ·
Lip Gloss v2 <https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.0>
· huh (+ v2 upgrade guide) <https://github.com/charmbracelet/huh> · fang
<https://github.com/charmbracelet/fang> · colorprofile
<https://github.com/charmbracelet/colorprofile> · teatest/VHS
<https://github.com/charmbracelet/vhs> · leg100's Bubble Tea tips
<https://leg100.github.io/en/posts/building-bubbletea-programs/>

UX doctrine: clig.dev <https://clig.dev/> · Primer CLI foundations
<https://primer.style/design/native/cli/> · gh accessibility retrofit
<https://github.blog/engineering/user-experience/building-a-more-accessible-github-cli/>
· gh scripting doctrine
<https://github.blog/engineering/engineering-principles/scripting-with-github-cli/>
· NO_COLOR <https://no-color.org> · CLICOLOR <https://bixense.com/clicolors/>
· terraform apply consent
<https://developer.hashicorp.com/terraform/cli/commands/apply> · buildx
--progress <https://docs.docker.com/reference/cli/docker/buildx/build/> · gh
run watch <https://cli.github.com/manual/gh_run_watch> · gh repo delete
<https://cli.github.com/manual/gh_repo_delete> · Rust error index
<https://doc.rust-lang.org/error_codes/error-index.html> · Google error
messages <https://developers.google.com/tech-writing/error-messages>

Parent spec: [2026-07-14-cube-idp-ux-design.md](2026-07-14-cube-idp-ux-design.md)
(event vocabulary §3, lifecycle §4, renderer contracts §5, resolve ladder §6,
byte-preservation §8, Access-block ratification precedent §9).
