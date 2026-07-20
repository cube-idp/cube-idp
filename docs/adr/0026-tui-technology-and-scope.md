---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 26. Terminal UI Technology: Charm v2, Inline-Only, No Alt Screen

## Context and Problem Statement

cube-idp is a CLI that runs long, multi-step cluster operations (`up`, `down`, `sync`,
`status`) whose progress a user wants to watch. Rendering that progress well needs a real
terminal-UI toolkit, but a CLI is not a dashboard: its output must remain pipeable, must
survive CI and non-TTY environments, and must leave usable scrollback behind when the
command exits.

Two failure modes had to be foreclosed. The first is dependency drift: the Charm ecosystem
ships v1 and v2 lines under different import paths, and letting both into the module graph
produces two incompatible renderers, two color pipelines, and duplicated style definitions.
The second is scope creep toward a resident full-screen application — an alt-screen HUD or
daemon that owns the terminal, erases scrollback on exit, and cannot be composed with other
tools. This record fixes both the library set and the shape of terminal interaction that
set is allowed to produce.

## Decision

The terminal UI is built exclusively on the Charm v2 line at the versions pinned in
`go.mod` — `charm.land/bubbletea/v2` in inline mode, `charm.land/bubbles/v2` for
spinner/progress/table, `charm.land/lipgloss/v2`, and `charm.land/huh/v2` for forms and
prompts. v1 and v2 import paths never coexist, and these majors are not bumped during the
project.

`internal/ui/theme` is the single source of the terminal look. It stays a leaf package
importing only lipgloss v2, `x/term` and the standard library, so that both `internal/ui`
and `internal/ui/render` can import it without an import cycle.

Bubble Tea programs are transient and inline only. They are permitted for `up`, `down` and
watch-style commands; persistent, daemon, and alt-screen TUIs are forbidden, and the rich
view exits cleanly with the command. The live renderer is evolved rather than rewritten,
preserving verbatim its inline mode, `p.Println` scrollback, `eofMsg` quit, ctrl-c context
cancel, guaranteed drain, and nil-input-on-pipes lifecycle guarantees.

## Consequences

* Good, because one pinned major line means one renderer, one color pipeline, and one
  style vocabulary — no v1/v2 skew to debug.
* Good, because inline mode leaves completed work in native scrollback, so `up` output
  remains readable after exit and composes with pagers, pipes and CI logs.
* Good, because the leaf-package rule for `internal/ui/theme` dissolves the cycle that
  previously forced the renderer to duplicate the CLI's styles.
* Good, because a guaranteed-drain, cancel-on-ctrl-c renderer means the producer can never
  block on a dead UI and no goroutine outlives the command.
* Bad, because pinning majors for the project's duration forecloses upstream fixes and
  features that land only in a newer major.
* Bad, because the alt-screen prohibition rules out multi-pane dashboards outright, so
  richer views must be expressed within a single managed bottom region.
* Bad, because every watch-style command needs two renderers — an inline Bubble Tea path
  for a rich TTY and a plain appending loop for pipes, CI and JSON — doubling the surface
  to test.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The TUI is built exclusively on Charm v2 libraries at the versions pinned in `go.mod` (bubbletea/v2 v2.0.8 inline, bubbles/v2 v2.1.1 spinner/progress/table, lipgloss/v2 v2.0.5, huh/v2 v2.0.3), and these majors are not bumped during the project. | `go.mod:45-49` |
| A transient inline Bubble Tea program is permitted for `up`/`down` (and later `sync --watch`), but persistent, daemon or alt-screen TUIs are forbidden and the rich view exits cleanly with the command. | `internal/ui/render/live.go:20-36` |
| The live renderer evolves the existing `liveModel` rather than being rewritten, preserving verbatim its inline mode, `p.Println` scrollback, `eofMsg` quit, ctrl-c context cancel, guaranteed drain and nil-input-on-pipes lifecycle guarantees. | `internal/ui/render/live.go:22-58,210-282,337-339` |
| The CLI is built on cobra v1.10 with huh for the init wizard and missing-value prompts and lipgloss for status lines. (Superseded: the kernel does hold Bubble Tea programs.) | `internal/ui/render/live.go:36` |
| cube-idp rejects fang, pterm, tview and tuist as UI dependencies. (Superseded: fang v2 is now the cobra dispatcher; pterm/tview/tuist remain absent.) | `cmd/root.go:134-141` |
| `status` renders a rich static lipgloss snapshot that exits immediately; a `--watch` mode for status is out of scope. (Superseded: `--watch` shipped.) | `cmd/status.go:81-83` |
| `diff` and `upgrade --plan` reuse the existing Section/Glyph styling only — no live view and no JSON document. (Superseded for `upgrade --plan`; still true for `diff`.) | `cmd/upgrade.go:33-45` |
| Out of scope: resident `status --watch`, multi-pane dashboards or any alt-screen/persistent view, interactive doctor "apply fix" actions, fang-style themed help, and `sync --watch`. (Superseded except the alt-screen prohibition.) | `cmd/status.go:81-83`; `cmd/sync.go:39-77` |

### Verification

- [ ] `go.mod` pins `charm.land/bubbles/v2 v2.1.1`, `charm.land/bubbletea/v2 v2.0.8`,
      `charm.land/huh/v2 v2.0.3`, `charm.land/lipgloss/v2 v2.0.5` (`go.mod:45-49`).
- [ ] No v1 Charm TUI import exists: `grep -rn "github.com/charmbracelet/\(bubbletea\|lipgloss\|bubbles\|huh\)" --include='*.go' .` returns nothing.
- [ ] `internal/ui/theme/theme.go` imports only `charm.land/lipgloss/v2`, `golang.org/x/term` and stdlib, and imports neither `internal/ui` nor `internal/ui/render`.
- [ ] `internal/ui/render/live.go:36` constructs `tea.NewProgram` with only `WithOutput`/`WithInput` — no `WithAltScreen`, no mouse capture anywhere in the file.
- [ ] `internal/ui/render/live.go` still centers on `liveModel` (line 215) with `p.Println` scrollback (line 49), `eofMsg` quit (lines 53, 254), ctrl+c mapped to the cancel func (lines 270-273) and the drain loop (lines 45, 58).
- [ ] `cmd/status.go:81-83` registers `--watch`, `--interval` and `--exit-status`, and `cmd/status.go:215` runs an inline Bubble Tea program with AltScreen never set.
- [ ] `cmd/sync.go:39-77` documents `--watch` as the sanctioned long-running foreground mode and calls `syncer.Watch`.
- [ ] `cmd/root.go:134-141` dispatches the root command through `fang.Execute` with `WithColorSchemeFunc(cubeColorScheme)`.
- [ ] `cmd/diff.go` remains styling-only: `diff.Run` into `c.OutOrStdout()`, `errExitCode(1)` on drift, no output-format flag.

## History

The stack was first described as cobra plus huh plus lipgloss status lines with no Bubble
Tea at all. That was superseded once a transient inline Bubble Tea program was permitted
for `up`, `down` and watch-style commands; the kernel now runs real `tea.NewProgram`
instances in `internal/ui/render/live.go` and `cmd/status.go`.

`fang` was originally rejected as a UI dependency alongside pterm, tview and tuist. It was
later adopted as the cobra dispatcher for styled help/usage, version and completions, with
`cubeColorScheme` mapping fang's help roles onto `internal/ui/theme`. pterm, tview and
tuist remain absent from the module graph and the source tree.

Three scope exclusions were later built. `status --watch` was declared out of scope twice
and then delivered as a gh-run-watch clone. `sync --watch` was likewise excluded and then
sanctioned as the long-running foreground mode under a ratified deferral, kept outside the
event pipeline. `upgrade --plan` was to be styling-only and gained an interactive
"apply now (runs cube-idp up)?" confirm on a real TTY. The alt-screen and
no-persistent-view prohibitions survive all three revisions unchanged.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:184` — original cobra/huh/lipgloss stack, no Bubble Tea.
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:6` — transient inline Bubble Tea permitted, alt screen forbidden.
- `docs/archive/superpowers/specs/2026-07-16-tui-interactive-layer-design.md:47` — Charm v2 exclusivity and pinned versions.
- `docs/archive/superpowers/specs/2026-07-14-cube-idp-ux-design.md:659` — rejection of fang, pterm, tview, tuist.
- `docs/archive/superpowers/specs/2026-07-16-tui-interactive-layer-design.md:423` — live renderer evolved, not rewritten.
