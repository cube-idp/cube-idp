# Reusable dispatch prompt — cube-idp Phase 5

Copy everything below the line into a fresh agent session to execute
exactly one task. Fill the `Lane:` line (S, U, P, A, or F — F is the
single final gate, claimable only when every S/U/P task is DONE) — or a
specific `Task:` id to override auto-detection. Owner-gate
pre-authorization is opt-in per dispatch: leave the last line empty
unless you mean it.

---

You are executing exactly ONE task of the cube-idp Phase 5 plan. The main
repo root (the directory containing go.mod and cube.yaml) is $ROOT; the
packs repo (exists only after task P2) is $PACKS = $ROOT/../cube-idp-packs;
the plugins repo (exists only after task P9) is
$PLUGINS = $ROOT/../cube-idp-plugins. Resolve $ROOT before anything else.

1. Read, in this order:
   - docs/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md —
     the design contract (§2 ratified decisions, §4 engine.values, §5
     spokes scope: registration only, engine takes over).
   - docs/superpowers/plans/2026-07-18-cube-idp-phase5.md — the plan AND
     the ledger. Its "Agent Execution Protocol", "Ground truth", and
     lane/claim rules bind every step you take. Lane A tasks additionally
     read the "Wave A" template + their parameter-table row — the row is
     the task spec.

2. Identify your task: within YOUR lane (given at the bottom), the first
   task whose Outcome says STATUS: UNCLAIMED and whose Depends are all
   DONE or DONE_WITH_CONCERNS. A specific Task id at the bottom overrides
   auto-detection. Cross-check `git log --oneline -20` (in the task's
   target repo): if the work already exists, do NOT redo it — fill the
   Outcome from the evidence, tick the boxes, commit the ledger, report
   DONE with a note. If STATUS is IN_PROGRESS under 24h old, STOP and
   report — another agent owns it. Lanes are parallel; tasks within a
   lane are strictly serial.

3. CLAIM before you code. In $ROOT, on main, clean tree: set your task's
   STATUS to IN_PROGRESS(<your session id>, <UTC timestamp>) and commit
   ONLY the plan file:
   git add docs/superpowers/plans/2026-07-18-cube-idp-phase5.md
   git commit -m "docs: p5 plan — claim <TASK-ID>"

4. Work ONLY in an isolated worktree on the task's named branch, in the
   task's target repo ($ROOT or $PACKS — the task header says which):
   git -C <repo> worktree add <repo>/.claude/worktrees/<slug> -b p5/<task-id>-<slug> main
   Branch names come from the Task Index verbatim. Never edit code in a
   main checkout; never edit the plan file from a worktree.

5. Execute ONLY your task, step by step, in order — TDD as written:
   failing test, verify it fails, implement, verify it passes, commit with
   the exact message the step gives, every commit ending with the trailer:
   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
   Run every verification command and compare against its "Expected" line.
   Steps marked VERIFY-API: check the real symbol with `go doc`/grep
   first, use the real name, record drift in FINDINGS — never guess,
   never bump a dependency version. go.mod gains no new module in any
   task. Never edit anything under docs/superpowers/ or .superpowers/
   except this plan's checkboxes and YOUR task's Outcome block.

6. ⚠ OWNER GATE steps (public repo creation, key generation, publishing,
   tags, any push): STOP there and report NEEDS_CONTEXT with the exact
   commands you would run — unless the bottom of this prompt says
   "Owner gates pre-authorized: yes". Local git init/commit is never
   gated; git push always is.

7. If a result does not match "Expected", or a STOP condition holds: stop
   immediately. Set STATUS: BLOCKED, fill BLOCKERS with the exact command,
   its actual output, and your diagnosis, commit the ledger on main, LEAVE
   the worktree and branch in place, and report. No workarounds, no
   force-pushes, never merge red work. Merge conflicts in
   internal/config/{types.go,schema.cue} or internal/diag/{codes.go,
   registry.go} are the ONE sanctioned exception: append-only — take both
   sides, re-run the gate, note it in FINDINGS.

8. When every step is green, finish with the task-level gate inside the
   worktree:
   go build ./... && go vet ./... && go test ./...
   plus, if your task touches cmd/ or internal/ui/:
   go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence'
   All must pass. Then merge back in the target repo (clean tree, on
   main):
   git merge --no-ff p5/<task-id>-<slug> -m "merge: p5 <TASK-ID> <slug> (p5/<task-id>-<slug>)"
   go test ./...   (post-merge sanity)
   git worktree remove <repo>/.claude/worktrees/<slug>
   Do NOT git push. Do NOT delete the branch.

9. Close the ledger in $ROOT on main: tick every checkbox of YOUR task,
   set STATUS to DONE (or DONE_WITH_CONCERNS when FINDINGS needs the
   owner), complete EVERY Outcome field — BRANCH (merged: yes), COMMITS
   (hashes + messages), FINDINGS (every deviation and decision — write
   "none" rather than a dash), REVIEW (what you verified and how),
   BLOCKERS (none), HANDOFF (what the next agent must know). Commit:
   git add docs/superpowers/plans/2026-07-18-cube-idp-phase5.md
   git commit -m "docs: p5 plan — <TASK-ID> complete"
   Append one line to .superpowers/sdd/progress.md if present (gitignored
   local ledger).

10. End with this report:
   - STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
   - Task: <id and name>  ·  Lane: <S|U|P|A>
   - Branch: <p5/…> (merged to main: yes/no) in <$ROOT|$PACKS>
   - Commits: <hashes + messages>
   - Evidence: the key verification commands and their actual output
     lines (including the fence gate when applicable)
   - Handoff: anything the next task's agent must know

Lane: ____
Task id (optional override): ____
Owner gates pre-authorized: no
