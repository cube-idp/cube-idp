# cube-idp — Agent Rules (binding)

This file binds every AI agent session in this repository. Deviation
requires an explicit human instruction in the current session; note the
instruction in the work's FINDINGS/PR body. Process authority: ADR-0042
(`docs/adr/0042-adr-first-two-track-delivery-process.md`).

## 1. Decisions live in `docs/adr/`

- Before implementing in any governed area, read the relevant accepted
  ADRs — start at `docs/adr/README.md`; `area:*` labels mirror ADR domains.
- Never contradict an accepted ADR silently. Conflict → stop, flag, and
  propose a superseding ADR.
- Propose an ADR (stop and ask) when you are about to: add a dependency,
  create a new architectural pattern others must follow, choose between
  real alternatives with non-obvious tradeoffs, or contradict an ADR.
- Reference decisions in code as `ADR-NNNN` comments at the entry point;
  reference them in PR bodies as `Implements ADR-NNNN`.

## 2. Two-track intake (ADR-0042)

- **ADR track** (features, architecture, hard-to-reverse): epic issue
  `[ADR-NNNN] <name>` (`type:adr`) → PR adding the ADR (status `proposed`,
  with Implementation Plan) → merge = accepted → sub-issues per
  deliverable → PRs close sub-issues.
- **Direct track** (bug/chore/docs): plain issue → PR with `Closes #N`.
  Hitting an architectural choice mid-task escalates to the ADR track.
- **Spikes** are timeboxed and end in an ADR PR or close-with-reason.
  Closing "not doing X because Y" is a valid, valuable outcome.
- **WIP rule:** before opening a new Track-A epic, check open `type:adr`
  issues in the current milestone; justify non-blocking in the new Scope.

## 3. Issues & PRs

- Every PR body references an issue (`Closes #N`) or an ADR
  (`Implements ADR-NNNN`). CI (`process-gate`) enforces this.
- Issues created by agents carry the same required fields as the issue
  forms (`.github/ISSUE_TEMPLATE/`): type + area labels, repro/scope,
  version. `gh issue create` bypassing the forms does not bypass the fields.
- Labels are namespaced: exactly one `type:*`, `area:*` where known,
  `status:blocked` only when genuinely blocked. The normative label list is
  `.github/labels.yml`; no new labels without updating it AND ADR-0042.
- **Workflow status lives on the delivery board (ADR-0042 §Board), and the
  board is automation-owned. NEVER set board Status manually and NEVER
  script board mutations — `board-sync` and built-in workflows are the only
  writers.** `status:*` labels other than `status:blocked` do not exist.
- New design/planning documents go ONLY into `docs/adr/` (via the ADR track).
  `docs/archive/` is frozen — never add to it.
- **`docs/` top level is a closed set (ADR-0042 §Documentation layout):**
  `adr/ architecture/ reference/ process/ archive/ vhs/`. Never create a
  new top-level docs directory or loose file — that requires updating
  ADR-0042 first; CI rejects unknown entries.
- **Changing behavior in a governed area updates
  `docs/architecture/<area>.md` in the SAME PR.** Find the section via its
  `cube:doc` / `cube:section` markers; keep the markers' `code=`/`adrs=`
  lists current. When designing new functionality, read that area file
  FIRST — it is the map of what exists.

## 4. Branches, worktrees, commits

- Branch names: `adr-NNNN-<slug>` (ADR track), `issue-N-<slug>` (Direct track),
  `process/<slug>` (meta). Never work on `main`.
- **Never work in a main checkout.** All work — code, docs, plan ledgers —
  happens in an isolated worktree under `.claude/worktrees/` on the task's
  branch (create once, reuse; check for existence first).
- Explicit pathspecs always — never `git add -A` (stray-staged-files
  gotcha on this machine). Never commit `spokes-up.txt` or other sessions'
  untracked drafts.
- Every commit ends with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- **Merge model — `main` only ever receives complete, green, coherent work.**
  Never merge a half-feature to `main` (a schema field nothing consumes, a
  loop with no caller). Choose by whether each unit stands alone:
  - **Coupled multi-task feature** (the usual ADR-track case — T1 enables T2
    enables T3): use ONE **feature branch** (`adr-NNNN-<slug>`). Execute the
    plan's tasks as commits on it (one task ≈ one or a few commits, per the
    ledger). Land the whole feature as a **single PR to `main`** whose body
    lists every sub-issue it closes (`Closes #a`, `Closes #b`, …). `main`
    sees one working increment; the epic closes after that merge. This is how
    valuesRef (#4) and engine-as-pack (#3) shipped.
  - **Genuinely independent task** (a standalone bug/chore/docs, or an
    ADR-track deliverable that is complete and safe on its own): its own
    small PR straight to `main` with `Closes #N`.
  - Optional within a feature branch: a sub-PR per task merging *into the
    feature branch* (never `main`) when you want per-task review checkpoints.
    The board then tracks at the epic grain (the epic card in `Accepted`
    signals active work); task cards move to Done at the final merge.

## 5. Subagent-driven development (SDD)

Plans are executed one-task-per-fresh-agent, per
`docs/process/sdd-dispatch-template.md`. Non-negotiables:

- The plan is NORMATIVE. No refactoring, renaming, scope-adding beyond it.
  Reality-vs-plan mismatch → minimal correction + FINDINGS entry, or BLOCKED.
- Claim before code; close with evidence — protocol and Outcome fields per
  `docs/process/sdd-ledger-template.md`.
- One task per dispatch, then STOP. Never claim a second task in-session.
- Fresh subagent per task; task review (spec compliance + code quality)
  after each; broad whole-branch review at the end. Fixes re-review.
- Dispatch prompts carry the task brief, interfaces, and constraints —
  never the session's accumulated history.
- Model selection: cheapest model that fits (transcription → cheap;
  integration → standard; design/final review → most capable). State the
  model explicitly in every dispatch.

## 6. Status heartbeat (mandatory during SDD)

Emit the visual status block per `docs/process/sdd-status-template.md`:
at claim, on every task state change, at least every 10 minutes of
wall-clock, immediately on BLOCKED, and as the final report header.
The Integrity line (main untouched · pushed? · commit count · dirty
files) is never omitted.

## 7. Outward actions & owner gates

- Outward = anything leaving this machine: pushing refs, tags, creating/
  editing GitHub issues/labels/milestones/releases/project boards,
  publishing packages. (Board *Status* is never yours to set even with
  outward authorization — see §3.)
- Outward actions require explicit per-dispatch authorization
  (`Outward actions authorized: yes` + scope). Absent that → NEEDS_CONTEXT.
- HARD LIMITS regardless of authorization: never push branches of this
  repo without the dispatch naming them; never force-push; never delete
  remote refs except a failed tag you yourself pushed this session.

## 8. Operational doctrine (hard-won; violating these wedged real runs)

a. **Foreground only.** Never background a long run and wait for a
   notification — it deadlocks the session. Run e2e/conformance/CI-watch
   as foreground Bash, chunked into bounded calls (each under the
   ~10-minute call timeout; loop sleep+check inside one call). Render a
   status heartbeat between chunks.
b. **Live legs are exclusive.** docker + host port 18443: before any
   kind/e2e run, `kind get clusters` must show no conf-*/e2e cluster and
   18443 must be free; poll until true. `CUBE_IDP_E2E_GATEWAY_PORT=18443`.
   One live leg at a time.
c. **Copy, never symlink,** any pack dir you stage (the hasher rejects
   symlinks, CUBE-4001).
d. **Verify with real commands,** never LSP/editor diagnostics (stale-
   diagnostics gotcha). Go gate: `go build ./... && go vet ./... &&
   go test ./... -count=1` in the worktree, all green.
e. **Tags:** exactly ONE tag per `git push` — >3 tags in one push emits
   ZERO GitHub events (CI silently skips).
f. **ghcr:** only tag-triggered CI can write packages (local token
   cannot). A new package may be created private — verify via
   `gh api "orgs/cube-idp/packages/container/<name>"`, record for the
   owner, do NOT flip it, do NOT treat as failure.
g. **go.mod** gains no new module unless the plan's task explicitly says so.
h. **Isolated kubeconfig, always.** Never read or write the user's default
   kubeconfig (`~/.kube/config`). Every cluster-touching command — kind,
   kubectl, helm, flux, `cube-idp` itself, e2e legs — carries an explicit
   per-command inline env var, one file per worktree/leg:
   `KUBECONFIG=<worktree>/.kube/config kind create cluster …`
   `KUBECONFIG=<worktree>/.kube/config go test ./tests/e2e/…`
   Inline on the command, never a session-wide export, never a shell-profile
   edit. kind/k3d honor `KUBECONFIG` for context writes, so contexts land in
   the isolated file; delete the file when the leg's cluster is deleted.
   (`kind get clusters` talks to docker and needs no kubeconfig.)

## 9. Repo map

- `docs/adr/` — decisions (why) · `docs/architecture/` — living system map,
  one file per `area:*`, `cube:doc` markers (how it works now) ·
  `docs/reference/` — user-facing contracts · `docs/process/` — SDD
  templates, plans · `docs/archive/` — frozen history ·
  `.github/ISSUE_TEMPLATE/` — intake forms · `internal/`, `cmd/` — Go code
  · `tests/` — suites.
