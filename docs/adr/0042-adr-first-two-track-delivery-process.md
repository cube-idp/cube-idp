# 0042 — ADR-First Two-Track Delivery Process on GitHub

Status: accepted
Date: 2026-07-20

## Context

The 2026-07-20 documentation audit validated the planning corpus (31 documents,
~236k words) against the code: 113 fully-specified decisions were never built,
69 were unverifiable, and 39 ADRs had to be reconstructed after the fact
(#16–#20). Design text lived on branches and in dated files main never saw;
nothing linked decisions to delivery. With AI-assisted coding the divergence
rate compounds: agents generate specs faster than humans notice they were
abandoned. The org stays on GitHub only — process, not new software.

## Decision

**Track A — decision-first** (features, architecture, anything hard to reverse):
1. Open an **epic issue** via the Epic form, titled `[ADR-NNNN] <name>`,
   labeled `type:adr`.
2. Open a **small PR adding `docs/adr/NNNN-<slug>.md`** (status `proposed`)
   with an Implementation Plan section (affected paths, patterns, tests,
   verification checkboxes). **PR review is the decision gate; merge =
   accepted.** The spec reaches main at acceptance time — before
   implementation, never on a long-lived side branch.
3. Create **sub-issues** under the epic, one per deliverable from the
   Implementation Plan. Each closes via a PR whose body carries
   `Closes #N` and `Implements ADR-NNNN`.
4. The epic closes when all sub-issues close and the ADR's verification
   checkboxes pass.

**Track B — routine** (bug/chore/docs): plain issue → PR with `Closes #N`.
Escalation guard: hitting a real architectural choice mid-implementation
stops the work and proposes an ADR (Track B → Track A).

**Spikes** are timeboxed and terminate in exactly one of: an ADR PR, or
closed-as-not-planned *with the reason in the closing comment*. Silent
abandonment is the failure mode this process exists to kill; a reasoned
"not doing X because Y" close is a first-class outcome.

**Taxonomy:** namespaced labels — `type:` (bug/feature/chore/docs/adr/spike/
question), `area:` (mirrors ADR domains), `status:blocked` (the only status
label — an orthogonal impediment flag, readable without GraphQL). Pipeline
position lives exclusively on the delivery board's Status field (§Board).
Normative label list: `.github/labels.yml`. Milestones are per-repo release
buckets; unassigned = backlog.

## Delivery board (Projects v2)

One org-level Projects v2 board ("cube-idp delivery") is the **single owner
of workflow state**. Issues only — PRs are never board items (they are
transient, and the one PR-driven transition below keys off the *linked*
issue, which built-in workflows cannot do anyway).

**Division of labor:** labels are the machine-readable API (`type:`,
`area:`, `status:blocked` — cheap for agents/CI to read via REST); the
board Status is the pipeline view — machine-written, human-read. Agents
NEVER write board state; automation owns it.

**Fields (deliberately minimal — no duplication of what labels carry):**
- `Status` (single-select): `Backlog → Proposed → Accepted → In progress →
  In review → Done`.
- `Iteration`, `Estimate` — the only typed fields labels cannot express.
- NO `Area`/`Track`/`ADR` fields: area is a label, track is derivable
  (`type:adr`/`type:spike`), the ADR number is in the epic title
  `[ADR-NNNN] …` (that prefix is machine-parseable and load-bearing —
  it is the join key between an ADR PR and its epic).

**Status transitions (all automated; manual Status edits are a process
violation):**

| Transition | Trigger | Mechanism |
| --- | --- | --- |
| → Backlog | issue opened / added | built-in (auto-add + item-added workflow) |
| → Proposed | ADR PR opened carrying `ADR-NNNN` | `board-sync` workflow |
| → Accepted | that PR merged (adds `docs/adr/NNNN-*.md`) | `board-sync` workflow |
| → In progress | draft PR opened with `Closes #N` | `board-sync` workflow |
| → In review | PR ready for review with `Closes #N` | `board-sync` workflow |
| → Done | issue closed (incl. auto-close on merge) | built-in (item-closed workflow) |

**Credential:** org-installed GitHub App (org Projects: read/write) minted
per-run via `actions/create-github-app-token`; `GITHUB_TOKEN` cannot write
org projects. Org config: variable `BOARD_APP_ID`, secret
`BOARD_APP_PRIVATE_KEY`, variable `BOARD_PROJECT_NUMBER`.

**Instantiation:** the board cannot be created by committing a file. This
section is the board's source of truth; the board is its instantiation
(plan task T14 — scripted `gh project` commands plus a documented one-time
UI checklist for built-in workflows, which have no write API).

**What the board does NOT do:** milestones stay (release grouping); the
SDD status heartbeat stays the intra-run truth; decisions stay in ADRs —
the board tracks delivery, never decisions.

## Documentation layout

`docs/` top level is a CLOSED set. Adding a top-level directory or loose
file is an architectural act: it requires updating this ADR (Track A);
CI (`process-gate` doc-consistency) rejects unknown entries.

| Directory | Contents | Nature |
| --- | --- | --- |
| `docs/adr/` | numbered decision records | WHY — append-only |
| `docs/architecture/` | living system map, one file per `area:*` label | HOW it works NOW — updated in the same PR as the behavior change |
| `docs/reference/` | user-facing contracts: cube.yaml, kind config, machine-readable output, pack contract | WHAT users rely on |
| `docs/process/` | delivery machinery: SDD templates, `plans/`, model map | HOW we work |
| `docs/archive/` | frozen history | read-only — never added to |
| `docs/vhs/` | demo tapes | assets |

There is deliberately NO `docs/features/` and no per-feature design docs:
feature *decisions* are ADRs, feature *delivery* is issues + the board,
feature *current behavior* is `architecture/` + `reference/`. A features
folder is exactly the artifact class whose divergence the 2026-07-20 audit
measured (113 fully-specified never-built decisions).

**Area markers.** Every `docs/architecture/<area>.md` begins with a
machine-readable header comment; subsections may carry section markers:

    <!-- cube:doc area=packs code=internal/pack,internal/catalog adrs=0002,0003,0004,0005,0008 -->
    <!-- cube:section area=packs topic=fetching code=internal/pack/fetch adrs=0003 -->

Grammar: HTML comment · `cube:doc` | `cube:section` · space-separated
`key=value` pairs · comma-separated lists · `area` values must exist in
`.github/labels.yml`. Agents locate work by
`grep -rn 'cube:\(doc\|section\).*area=<area>' docs/architecture/`, then
follow `code=` to entry points and `adrs=` to the governing decisions.
CI validates header presence and area values; deep content stays human-owned.

**WIP rule:** before opening a new Track-A epic, list open epics in the
current milestone; an unfinished one must be justified as non-blocking in
the new epic's Scope field.

**Enforcement:** `CLAUDE.md` binds agent sessions (consult `docs/adr/`
before implementing in a governed area; propose an ADR on triggers; every
PR references an issue or ADR; no new design docs outside `docs/adr/`).
CI job `process-gate` rejects PRs whose body references neither `#N` nor
`ADR-NNNN` — the same guarantee makes `board-sync` deterministic (every PR
carries a join key). Subagent-driven execution follows `docs/process/`
templates.

## Non-Goals

- Board auto-add across all five org repos from day one — main repo only;
  extend when other repos' issue volume warrants (auto-add workflow count
  is plan-tier-limited; the `board-sync` script path has no such limit).
- No retroactive re-issueing of shipped work; ADRs 0001–0041 already record it.
- Issue forms gate the web UI only; agent-side enforcement is CLAUDE.md's job.

## Consequences

- Every feature has a falsifiable paper trail: ADR → epic → sub-issues → PRs.
- Ceremony is bounded: Track B stays one-issue-one-PR light.
- `docs/superpowers/` is frozen as an archive; new plans attach to ADRs/epics.
- Follow-ups: #17–#20 must each get a Track-A revive or a reasoned close.

## Implementation Plan

- **Affected paths:** `.github/ISSUE_TEMPLATE/`, `.github/workflows/process-gate.yaml`,
  `.github/workflows/board-sync.yaml`, `.github/labels.yml`, `CLAUDE.md`,
  `AGENTS.md`, `docs/process/`, `docs/adr/README.md`.
- **Installed by:** `docs/process/plans/2026-07-20-github-process-and-sdd.md`
  (tasks T2–T13; board instantiation T14), the first SDD run using the new
  templates.
- **Pattern for future Track-A work:** pilot ADR-0043 (issue #7).

## Verification

- [ ] `gh label list` shows only the namespaced taxonomy (+ community labels);
      the only `status:*` label is `status:blocked`
- [ ] Every open issue carries a `type:` label
- [ ] `process-gate` fails a PR with no `#N`/`ADR-NNNN` reference, passes one with
- [ ] Issue #7 retitled `[ADR-0043] …` with an accepted ADR and sub-issues
- [ ] `CLAUDE.md` present at repo root; agent session confirms it loads
- [ ] Board: a test issue lands in Backlog on open and moves to Done on close
      with zero manual Status edits; an ADR PR open/merge moves its epic to
      Proposed/Accepted (T14 verification)
