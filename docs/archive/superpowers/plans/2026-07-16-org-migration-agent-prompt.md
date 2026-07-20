# Reusable subagent prompt — org-migration plan execution

Paste the block below verbatim to each manually-invoked agent (one task per agent).
Only the optional last line ever changes.

```text
You are executing exactly ONE task of the cube-idp org-migration plan. Work in the repo root (the directory containing go.mod and cube.yaml).

1. Read docs/superpowers/specs/2026-07-16-org-migration-design.md (context, decisions) and docs/superpowers/plans/2026-07-16-org-migration.md (the plan). The plan's "Global Constraints" section binds every step you take.

2. Identify your task: the first "### Task N:" section that still has unchecked boxes (- [ ]). If a task number is given at the bottom of this prompt, that overrides auto-detection. Before starting, cross-check git log --oneline -15 and the plan ticks: if the task's work already exists in git, just tick its boxes, commit the plan update, and report DONE with a note — never redo completed work.

3. Execute ONLY that task, step by step, in order. Run each step's exact command and compare the result against the step's "Expected" line. Do not substitute your own approach, do not skip verification steps, do not start the next task. Never edit anything under docs/superpowers/ or .superpowers/ except ticking checkboxes in this plan file.

4. If a result doesn't match "Expected", or a step says STOP on some condition and that condition holds: stop immediately and report BLOCKED with the command, its actual output, and your diagnosis. No workarounds, no force-pushes, no tag moves. For the destructive step (Task 8 Step 5, package deletion): run the listing command first and delete only packages named cube-idp/packs/<name>, exactly as the step gates it.

5. Commit exactly what the task's commit step specifies (conventional commit message from the plan). Do not git push unless the task's steps say to push.

6. On completion, tick every checkbox of YOUR task only (- [ ] → - [x]) in docs/superpowers/plans/2026-07-16-org-migration.md and commit:
   git add docs/superpowers/plans/2026-07-16-org-migration.md
   git commit -m "docs: plan — Task N complete"
   If the task itself pushed to origin, push this commit too; otherwise leave it local.

7. End with this report:
   - STATUS: DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
   - Task: <number and name>
   - Commits: <hashes + messages> (or "none — GitHub-side only")
   - Evidence: the key verification commands you ran and their actual output lines
   - Handoff: anything the next task's agent must know

Task number (optional override): ____
```
