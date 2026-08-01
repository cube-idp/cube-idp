# SDD dispatch prompt — {{PLAN_NAME}}

How to use: copy everything below the line into a fresh agent session to
execute exactly ONE task; re-paste for each next task. Fill every
{{PLACEHOLDER}}; delete optional sections that don't apply. Keep the
numbered structure — agents follow it in order. Authorization lines at the
bottom are per-dispatch and default to "no".

---

You are executing exactly ONE task of {{PLAN_NAME}}, then stopping. The
plan is NORMATIVE: you make no changes it does not specify. You do not
refactor, redesign, rename, "improve", or add scope. Where reality
contradicts the plan (an API name, a stale Expected line), use the plan's
escape hatch — verify against the real API/system, apply the minimal
correction, record it as a FINDINGS entry — never your own judgment beyond
that. On any unresolvable mismatch: STATUS: BLOCKED and stop.

Repos (absolute):
{{REPO_VARS e.g. $ROOT = /abs/path · $PACKS = /abs/path}}

0. RULES: $ROOT/CLAUDE.md binds this session — read it first. Its §SDD and
   §Operational-doctrine sections apply to every step below.

1. READ, in this order (this binds every step you take):
   - {{SPEC_PATH — mark RATIFIED sections binding}}
   - {{PLAN_PATH}} — Global Constraints, YOUR task's section, the Task
     Index & Ledger. {{BRANCH_NOTE if plan lives off-main}}
   - The ledger HANDOFF blocks of DONE tasks yours depends on — consume
     discovered values, never re-discover.

2. CURRENT STATE (verify, don't trust): {{STATE_SUMMARY — done/remaining}}.
   Cross-check ledger STATUS lines AND `git log --oneline -15` on the
   feature branch before claiming: if work already exists, do NOT redo it —
   close the ledger from the evidence. Default selection: first UNCLAIMED
   task whose dependencies are all DONE/DONE_WITH_CONCERNS
   {{SELECTION_ORDER if not simple task order}}. A Task id at the bottom
   overrides. {{GATED_TASKS — list OWNER-GATED / OUTWARD tasks}}.

3. WORKTREES/BRANCHES (create once, reuse — check for existence first):
   {{WORKTREE_CMDS one per repo, exact `git worktree add` with base branch}}
   NEVER work in a main checkout — every file you touch, code AND ledger,
   is edited inside the worktree on the task's branch. ALL commits land on
   the feature branch of their repo. Never commit to main. Never push ANY
   ref{{PUSH_EXCEPTIONS e.g. "except the plan's tracking branch to keep
   its PR current"}}.

4. CLAIM before any code: set ONLY your task's ledger STATUS to
   IN_PROGRESS(<session id>, <UTC ts>); commit with explicit pathspec:
   `git commit -m "docs: {{PLAN_SHORT}} — claim T<N>" -- {{PLAN_PATH}}`.
   Re-read the ledger immediately before editing; verify HEAD afterward.

5. EXECUTE the task's steps IN ORDER, TDD as written; every commit uses the
   step's exact message + the CLAUDE.md commit trailer.
   {{TASK_SPECIFIC_DOCTRINE — anything hard-won for THIS plan that
   CLAUDE.md §doctrine doesn't already cover; delete if none}}

6. STATUS HEARTBEAT: emit the docs/process/sdd-status-template.md block at
   claim, at every task-state change, at least every 10 minutes of
   wall-clock (chunk long foreground runs so a heartbeat lands between
   chunks), immediately on BLOCKED, and at final report.

7. On any Expected-mismatch beyond the §5 escape hatch, or any STOP
   condition: stop immediately, STATUS: BLOCKED, BLOCKERS = exact command +
   actual output + diagnosis, commit the ledger, LEAVE worktree and branch
   in place, report. No workarounds. Never close a red task.

8. GATE before closing — in the worktree:
   {{GATE_CMDS e.g. `go build ./... && go vet ./... && go test ./... -count=1`}}
   all green, with output pasted as evidence.
   {{MERGE_PROTOCOL if tasks merge to an integration branch; else delete}}

9. CLOSE the ledger: tick YOUR task's checkboxes; complete EVERY Outcome
   field — STATUS · BRANCH · COMMITS (hashes + messages) · FINDINGS (every
   deviation; "none" over dashes) · BLOCKERS · HANDOFF (discovered values,
   evidence the next task needs) — with pasted command OUTPUT, not
   paraphrase. Commit `docs: {{PLAN_SHORT}} — T<N> complete` (explicit
   pathspec).

10. REPORT and STOP (do not claim another task in this session):
    STATUS / Task / Branch + repo / Commits / Evidence (key commands +
    actual output lines) / Handoff. Statuses: DONE ·
    DONE_WITH_CONCERNS (state the concerns) · NEEDS_CONTEXT (state the
    missing context) · BLOCKED (per §7).

Task id (optional override): ____
Outward actions authorized: no ({{OUTWARD_SCOPE when yes}})
Owner gates authorized: no ({{OWNER_GATE_SCOPE when yes}})
