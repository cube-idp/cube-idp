# Reusable dispatch prompt v2 — cube-idp Phase 5 (remaining tasks)

Supersedes `2026-07-18-phase5-agent-prompt.md` for everything still open
after the 2026-07-18 execution day: **P10, A3–A11, then F1 last**. It
carries the mid-phase state, the owner's standing gate authorization, and
the operational doctrine learned in execution (foreground-only runs,
one-tag-per-push, ghcr package ownership, port queue) so a zero-context
agent can resume without inventing anything.

Copy everything below the line into a fresh agent session to execute
exactly one task; re-paste for each next task. Optionally fill the
`Task id` line at the bottom to override auto-selection.

---

You are executing exactly ONE task of the cube-idp Phase 5 plan, then stopping.
The plan is NORMATIVE: you make no changes it does not specify. You do not
refactor, redesign, rename, "improve", or add scope. Where reality contradicts
the plan (an API name, a stale Expected line), you use the plan's own escape
hatch — VERIFY-API + a FINDINGS record — never your own judgment beyond it.
On any unresolvable mismatch you set STATUS: BLOCKED and stop.

Repos (absolute):
  $ROOT    = /Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/neocube
  $PACKS   = /Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/cube-idp-packs
  $PLUGINS = /Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/cube-idp-plugins

1. Read, in this order (this binds every step you take):
   - $ROOT/docs/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md
   - $ROOT/docs/superpowers/plans/2026-07-18-cube-idp-phase5.md — the plan AND
     the ledger: its "Agent Execution Protocol", "Ground truth" (GT1-GT19), and
     your task's section are mandatory. Lane A tasks also read the "Wave A"
     template + their parameter-table row (the row IS the task spec) + A1's and
     A2's Outcome lines (first-execution precedents).
   - The Outcome HANDOFF blocks of DONE tasks your task depends on.

2. CURRENT STATE (verify, don't trust): 20 of 31 tasks are complete — lanes S
   (S1-S4) and U (U1-U5) fully; P1-P9; A1 (crossplane) and A2 (kyverno)
   published. The ONLY remaining tasks are: P10 (plugin install), A3-A11 (nine
   packs, parameter table), then F1 LAST (claimable only when every S/U/P task
   incl. P10 is DONE/DONE_WITH_CONCERNS). Cross-check the ledger STATUS lines
   and `git log --oneline -20` in the target repo before claiming: if work
   already exists, do NOT redo it — close the ledger from the evidence.
   Default selection: the first UNCLAIMED task whose Depends are all
   DONE/DONE_WITH_CONCERNS, preferring P10, then A3..A11 in order, F1 last.
   A specific Task id at the bottom of this prompt overrides.

3. CLAIM before any code: in $ROOT on main with a clean tree, set ONLY your
   task's STATUS to IN_PROGRESS(<your session id>, <UTC ts>), then
   `git add docs/superpowers/plans/2026-07-18-cube-idp-phase5.md &&
    git commit -m "docs: p5 plan — claim <TASK-ID>"`.
   You are the only agent running; keep the discipline anyway (re-read the
   file immediately before editing; verify HEAD afterward). Three untracked
   docs/superpowers drafts in $ROOT (cluster-forprovider*,
   kind-config-reference) belong to ANOTHER session — never add, edit, or
   commit them; use targeted `git add` only, never `git add -A`.

4. Work ONLY in an isolated worktree on the task's exact branch from the Task
   Index / parameter table:
   `git -C <target-repo> worktree add <target-repo>/.claude/worktrees/<slug> -b p5/<task-id>-<slug> main`
   Never edit code in a main checkout; never edit the plan file from a
   worktree. Execute the task's steps IN ORDER, TDD as written; every commit
   uses the step's exact message and ends with the trailer:
   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
   go.mod in $ROOT gains no new module in any task. Never edit anything under
   docs/superpowers/ or .superpowers/ except the plan's checkboxes and YOUR
   task's Outcome. Lane A shared template checkboxes: A1 ticked Steps 1-5 —
   do not re-tick or untick anything.

5. OWNER GATES — STANDING PRE-AUTHORIZATION (ratified by the owner
   2026-07-18, recorded in the ledger's P2 FINDINGS addendum and the A1
   HANDOFF): you run your task's gate steps YOURSELF, exactly as scoped by
   the step: A-task single-tag pushes to cube-idp/packs; the first plugins
   publish `git -C $PLUGINS tag hello/v0.1.0 && git push origin hello/v0.1.0`
   if P10 requires a published hello plugin; watching runs via
   `gh run list/watch`. Record every outward command + output in your
   Outcome. HARD LIMITS regardless of authorization: never push ANY branch
   of $ROOT; never force-push anything; never delete remote refs except a
   failed tag you yourself just pushed this session; anything outward beyond
   your step's stated scope → stop, report NEEDS_CONTEXT.

6. OPERATIONAL DOCTRINE (hard-won in execution; violating these wedged or
   broke prior runs — treat as mandatory):
   a. FOREGROUND ONLY: never background a long run and stop to "wait for a
      notification" — that deadlocks the session. Run conformance/e2e/CI
      watches as foreground Bash, chunked into bounded calls (each under the
      ~10-minute call timeout; loop `sleep`+check inside one call, repeat
      calls as needed).
   b. TAGS: exactly ONE tag per `git push` — a push containing more than 3
      tags emits ZERO GitHub events (no CI runs, silently).
   c. ghcr: only the package-CREATING repo's CI can write a package; nothing
      publishes from this machine (local token lacks write:packages) — all
      publishing goes through tag-triggered Actions. A brand-new package may
      be created private OR public (org default changed mid-day) — VERIFY
      with `gh api "orgs/cube-idp/packages/container/<name>"`; if private,
      record it for the owner, do NOT treat as failure, do NOT flip it.
   d. LIVE LEGS: docker + host port 18443 are exclusive. Before any kind/e2e/
      conformance run: `kind get clusters` must show no conf-*/e2e cluster
      and port 18443 must be free; poll until true. Local e2e uses
      CUBE_IDP_E2E_GATEWAY_PORT=18443 (GT14). Copy — NEVER symlink — any
      pack dir you must stage (the hasher rejects symlinks, CUBE-4001).
   e. The conformance gateway defaults to the PUBLISHED
      oci://ghcr.io/cube-idp/packs/traefik:0.2.0 (publicly pullable);
      override only for offline runs.

7. On any Expected-mismatch or STOP condition: stop immediately, STATUS:
   BLOCKED, BLOCKERS = exact command + actual output + diagnosis, commit the
   ledger, LEAVE the worktree and branch in place, report. No workarounds,
   never merge red work. Merge conflicts ONLY in the append-only shared
   files (internal/config/{types.go,schema.cue}, internal/diag/{codes.go,
   registry.go}, internal/pack/manifests/pack-crd.yaml printer columns, the
   D11 record-writer fields in internal/pack/expose.go) may be resolved by
   taking both sides + re-running the gate + FINDINGS.

8. Task-level gate before merging — in the worktree:
   `go build ./... && go vet ./... && go test ./...` (all pass), plus if the
   task touches cmd/ or internal/ui/:
   `go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence'`
   ($PACKS/$PLUGINS are data-only: the gate is the task's own verification
   commands, including its live conformance where specified.) Then merge in
   the target repo (clean tree, on main):
   `git merge --no-ff p5/<task-id>-<slug> -m "merge: p5 <TASK-ID> <slug> (p5/<task-id>-<slug>)"`,
   post-merge `go test ./...` ($ROOT only), `git worktree remove <path>`.
   Do NOT push branches. Do NOT delete branches.

9. Close the ledger in $ROOT on main: tick YOUR checkboxes, complete EVERY
   Outcome field (STATUS DONE or DONE_WITH_CONCERNS · BRANCH merged: yes ·
   COMMITS · FINDINGS — every deviation, "none" over dashes · REVIEW ·
   BLOCKERS · HANDOFF), commit
   `git commit -m "docs: p5 plan — <TASK-ID> complete"`, and append one line
   to $ROOT/.superpowers/sdd/progress.md if present.

10. Report and STOP (do not claim another task in this session):
    STATUS / Task + Lane / Branch (merged: yes|no) in which repo / Commits /
    Evidence (key commands + actual output lines, incl. conformance verdict,
    tag push + run URL, fence gate where applicable) / Handoff.

Task id (optional override): ____
Owner gates pre-authorized: yes (standing, scope per §5)
