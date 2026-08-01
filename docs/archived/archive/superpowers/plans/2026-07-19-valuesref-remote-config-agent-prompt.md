# Reusable dispatch prompt — cube-idp RV valuesRef/remote-config

One manually-dispatched agent per task, plan ticks + ledger as the shared
state (the org-migration/phase-5/p6/p7 execution mode). Copy everything
below the line into a fresh agent session to execute exactly one task;
re-paste for each next task. Optionally fill the `Task id` line at the
bottom to override auto-selection.

---

You are executing exactly ONE task of the cube-idp RV plan
(valuesRef/tuningRef remote values + remote -f config), then stopping. The
plan is NORMATIVE: you make no changes it does not specify. You do not
refactor, redesign, rename, "improve", or add scope. Where reality
contradicts the plan (an API name, a line anchor that moved, a fixture
helper that differs from the plan's sketch), you use the plan's own escape
hatch — verify against the real code, apply the minimal correction, and
record it as a FINDINGS entry — never your own judgment beyond that. On any
unresolvable mismatch you set STATUS: BLOCKED and stop.

Repo (absolute, single repo — no $PACKS in this plan):
  $ROOT = /Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/neocube

1. Read, in this order (this binds every step you take):
   - $ROOT/docs/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md
     (the design: ref grammar, merge ladders, pin rules, remote -f contract)
   - $ROOT/docs/superpowers/plans/2026-07-19-valuesref-remote-config.md —
     the Global Constraints, the Amendments section, YOUR task's section
     (including any Step Nb amendment steps), the "Agent Execution
     Protocol", and the Ledger. NOTE: this file lives on branch
     `2026-07-19-valuesref-remote-config`, not main — read it from there.
   - The Ledger HANDOFF blocks of DONE tasks yours depends on (T8 records
     the ACTUAL diag numbers allocated for CodeConfigRemoteReadOnly /
     CodeConfigRemoteFetch — T9/T10/T12 consume them, never re-derive).
   - Context only (do not implement from it): the engine-as-pack spec
     2026-07-19-cube-idp-engine-as-pack-design.md is RATIFIED — that is why
     T7 is GATED_SKIP and Task 11 skips its tuning block.

2. CURRENT STATE (verify, don't trust): execution order is STRICT
   T1→T6, then T8→T12 (T7 is GATED_SKIP — never claim it). Cross-check the
   Ledger STATUS lines AND `git log --oneline -15` on the branch before
   claiming: if work already exists, do NOT redo it — close the ledger from
   the evidence. Default selection: the first UNCLAIMED task whose
   predecessors are all DONE/DONE_WITH_CONCERNS. A Task id at the bottom
   overrides. Also check for p7 (engine-as-pack) merges that moved shared
   files (up.go, config, lock) — reconcile via the escape hatch, FINDINGS.

3. WORKTREE/BRANCH (created once, reused by every subsequent agent — check
   for existence first): all work lands on the EXISTING branch
   `2026-07-19-valuesref-remote-config` (it holds the spec, plan, and
   ledger). If the main checkout already sits on that branch, work there;
   otherwise:
   `git -C $ROOT worktree add $ROOT/.claude/worktrees/rv-valuesref 2026-07-19-valuesref-remote-config`
   (git allows one checkout per branch — if the branch is checked out
   elsewhere, use that checkout). Never commit to main. Never push ANY ref
   before T12 (T12's authorized push + PR are the sole outward acts).

4. CLAIM before any code: set ONLY your task's Ledger STATUS to
   IN_PROGRESS(<your session id>, <UTC ts>), then
   `git commit -m "docs: rv plan — claim T<N>" -- docs/superpowers/plans/2026-07-19-valuesref-remote-config.md`.
   You are the only agent running; keep the discipline anyway (re-read the
   ledger immediately before editing; verify HEAD afterward). Commit with
   EXPLICIT pathspecs everywhere — this machine has a stray-staged-files
   gotcha; never `git add -A` outside your task's stated file set, never
   touch docs/superpowers/ beyond your task's checkboxes + your Ledger
   entry.

5. Execute the task's steps IN ORDER, TDD as written; every commit uses the
   step's exact message and ends with the trailer:
   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
   go.mod gains no new module in any task. DIAG-CODE RENUMBER RULE
   (protocol §): the p7 plan also allocates CUBE-0012/0013 — the plan's
   CONSTANT NAMES and registry entries are normative, the numbers are not.
   At T5/T6/T8 claim time, take the next FREE number in the domain block
   (check internal/diag/codes.go), record the actual number in FINDINGS +
   HANDOFF, and use it consistently in every later test/doc.

6. OPERATIONAL DOCTRINE (hard-won in phase 5/p6; violating these wedged or
   broke prior runs — mandatory):
   a. FOREGROUND ONLY: never background a long run and wait for a
      notification — that deadlocks the session. Run e2e as foreground
      Bash, chunked into bounded calls (each under the ~10-minute call
      timeout; loop sleep+check inside one call).
   b. LIVE LEGS (T12 e2e only): docker + host port 18443 are exclusive.
      Before any kind/e2e run: `kind get clusters` must show no e2e
      cluster and 18443 must be free; poll until true. Always
      CUBE_IDP_E2E_GATEWAY_PORT=18443. One live leg at a time. If docker
      is unavailable, record the deferral in HANDOFF (owner runs live legs)
      — the unit/build gate still must be green.
   c. COPY — never symlink — any pack dir you must stage (the hasher
      rejects symlinks, CUBE-4001).
   d. VERIFY with real commands, never LSP/editor diagnostics (known
      stale-diagnostics gotcha): the gate is
      `go build ./... && go vet ./... && go test ./... -count=1` in the
      worktree, all green, PLUS `go test ./cmd/ -run TestCommandTreeGolden`
      passing WITHOUT -update (the F1 CLI freeze — this plan changes no
      flags), before closing the ledger.
   e. T12 outward acts (pre-authorized by the owner in this dispatch
      form): `git push -u origin 2026-07-19-valuesref-remote-config`, then
      `gh pr create --base main` with a body summarizing the RV lanes and
      ending with the standard generated-with line. ONE push, ONE PR; no
      tags. Record the PR URL in HANDOFF.

7. On any Expected-mismatch you cannot resolve with the §5 escape hatch, or
   any STOP condition: stop immediately, STATUS: BLOCKED, BLOCKERS = exact
   command + actual output + diagnosis, commit the ledger, LEAVE the
   worktree and branch in place, report. No workarounds, never close a red
   task.

8. CLOSE the ledger (same worktree, after the gate passes): tick YOUR
   task's checkboxes in the plan body, complete EVERY Outcome field of your
   Ledger entry — STATUS (DONE or DONE_WITH_CONCERNS) · COMMITS (hashes +
   messages) · FINDINGS (every deviation incl. renumbered diag codes,
   "none" over dashes) · BLOCKERS · HANDOFF (values/evidence the next task
   needs) — with pasted command OUTPUT as evidence, not paraphrase. Commit
   `docs: rv plan — T<N> complete` (explicit pathspec). On T12 also append
   one line to $ROOT/.superpowers/sdd/progress.md (it lives on main — if
   not on your branch, note the line's text in HANDOFF for the owner
   instead).

9. Report and STOP (do not claim another task in this session):
   STATUS / Task / Branch / Commits / Evidence (key commands + actual
   output lines — test summaries, golden check, e2e verdicts) / Handoff.

Task id (optional override): ____
T12 outward steps (branch push + PR creation) authorized: yes (owner
pre-authorized: "agent should end up with PR", dispatch 2026-07-19)
