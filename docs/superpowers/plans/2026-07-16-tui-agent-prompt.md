# Reusable dispatch prompt — TUI Interactive Layer (Approach B)

Copy everything below the line into a fresh agent session to execute exactly
one task. Optionally append `Task: W<w>.T<NN>` at the bottom to override
auto-detection.

---

You are executing exactly ONE task of the cube-idp TUI interactive-layer
plan. The repo root (the directory containing go.mod and cube.yaml) is
referred to as $ROOT; resolve it before anything else.

1. Read, in this order:
   - docs/superpowers/specs/2026-07-16-tui-interactive-layer-design.md — the
     design contract. Its §2 "Target Experience" frames and §6 conformance
     matrix are MANDATORY merge gates; its §4 lists contracts you MUST NOT
     break; its §5 lists the only three sanctioned plain-output changes.
   - docs/superpowers/plans/2026-07-16-tui-interactive-layer.md — the plan
     AND the ledger. Its "Agent Execution Protocol" and "Global Constraints"
     sections bind every step you take.

2. Identify your task: the first "### W…T…" section whose Outcome block says
   STATUS: UNCLAIMED, with every earlier task DONE or DONE_WITH_CONCERNS. If
   a task number is given at the bottom of this prompt, it overrides
   auto-detection. Cross-check `git log --oneline -20`: if the task's work
   already exists in git, do NOT redo it — fill the Outcome block from the
   evidence, tick its boxes, commit the ledger, and report DONE with a note.
   If the STATUS is IN_PROGRESS with a timestamp under 24h old, STOP and
   report — another agent owns it.

3. CLAIM before you code. In $ROOT, on main, with a clean tree: set your
   task's STATUS line to IN_PROGRESS(<your session id>, <UTC timestamp>) and
   commit only the plan file:
   git add docs/superpowers/plans/2026-07-16-tui-interactive-layer.md
   git commit -m "docs: tui plan — claim W<w>.T<NN>"

4. Work ONLY in an isolated worktree on the task's named branch:
   git -C $ROOT worktree add $ROOT/.claude/worktrees/<slug> -b tui/w<w>-t<NN>-<slug> main
   The branch name (tui/w<wave>-t<NN>-<slug>) is normative — it comes from
   the plan's Task Index. Never edit code in $ROOT's main checkout; never
   edit the plan file from the worktree.

5. Execute ONLY your task, step by step, in order — TDD as written: failing
   test, verify it fails, implement, verify it passes, commit with the exact
   conventional message the step specifies. Run every verification command
   and compare against its "Expected" line. Where the plan says an API name
   might differ in the pinned library version, check with `go doc` and
   record the real name in FINDINGS — do not guess and do not bump any
   dependency version. Never edit anything under docs/superpowers/ or
   .superpowers/ except the plan's checkboxes and YOUR task's Outcome block.

6. If a result does not match an "Expected" line, or a step's STOP condition
   holds: stop immediately. Set STATUS: BLOCKED, fill BLOCKERS with the
   exact command, its actual output, and your diagnosis, commit the ledger
   on main, LEAVE the worktree and branch in place, and report. No
   workarounds, no force-pushes, never merge red work.

7. When every step is green, finish with the task-level gate inside the
   worktree:
   go build ./... && go vet ./... && go test ./...
   plus, if your task touches a Target Experience frame:
   go test ./internal/ui/... ./cmd/... -run TE
   All must pass. Then merge back:
   cd $ROOT   (tree must be clean, branch must be main)
   git merge --no-ff tui/w<w>-t<NN>-<slug> -m "merge: tui W<w>.T<NN> <slug> (tui/w<w>-t<NN>-<slug>)"
   go test ./...   (post-merge sanity)
   git worktree remove $ROOT/.claude/worktrees/<slug>
   Do NOT git push. Do NOT delete the branch.

8. Close the ledger in $ROOT on main: tick every checkbox of YOUR task, set
   STATUS to DONE (or DONE_WITH_CONCERNS when FINDINGS needs the owner's
   eyes), and complete EVERY Outcome field — BRANCH (mark "merged: yes"),
   COMMITS (hashes + messages), FINDINGS (every deviation, every decision,
   every updated test assertion — write "none" rather than leaving a dash),
   REVIEW (what you verified and how), BLOCKERS (none), HANDOFF (what the
   next agent must know). Commit:
   git add docs/superpowers/plans/2026-07-16-tui-interactive-layer.md
   git commit -m "docs: tui plan — W<w>.T<NN> complete"

9. End with this report:
   - STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
   - Task: <number and name>
   - Branch: <tui/w…-t…-…> (merged to main: yes/no)
   - Commits: <hashes + messages>
   - Evidence: the key verification commands you ran and their actual
     output lines (including the TE gate when applicable)
   - Handoff: anything the next task's agent must know
