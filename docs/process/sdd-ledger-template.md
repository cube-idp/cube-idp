# SDD plan ledger

Every SDD plan embeds two things: a **Task Index** table and a per-task
**Outcome block**. The ledger lives IN the plan file, edited only via the
claim/close protocol. It is the recovery map after compaction or session
loss: trust it and `git log` over memory.

## Task Index

| ID | Task | Depends | Outward? | STATUS |
| --- | --- | --- | --- | --- |
| T1 | <name> | — | no | UNCLAIMED |
| T2 | <name> | T1 | **yes** | UNCLAIMED |

STATUS values: `UNCLAIMED` · `IN_PROGRESS(<session id>, <UTC ts>)` ·
`DONE` · `DONE_WITH_CONCERNS` · `BLOCKED(<one word>)` · `NEEDS_CONTEXT`.
Suffix markers: `OWNER-GATED` (claimable only with explicit per-dispatch
authorization) · `[OUTWARD]` (touches github.com or any external system).

## Claim protocol

1. Re-read the ledger immediately before editing (another session may have
   claimed since your last read).
2. Set ONLY your task's STATUS to `IN_PROGRESS(<session>, <UTC ts>)`.
3. Commit the plan file alone, explicit pathspec:
   `git commit -m "docs: <plan-short> — claim T<N>" -- <plan-path>`
4. Verify HEAD contains your claim.

## Outcome block (one per task, filled at close)

```
#### T<N> Outcome
- STATUS: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
- BRANCH: <branch> (merged: yes|no) in <repo>
- COMMITS: <hash> <message> (one line each)
- FINDINGS: every deviation from the plan, with the evidence that forced
  it. "none" — never dashes, never blank.
- REVIEW: <task-review verdict, or "pending final review">
- BLOCKERS: exact command + actual output + diagnosis ("none" when DONE)
- HANDOFF: discovered values and evidence later tasks consume
  (versions, keys, ports, decisions). Never make a later task re-discover.
```

Evidence is pasted command OUTPUT, not paraphrase.

## Close protocol

1. Gate passes (plan's verification commands, all green, output captured).
2. Tick YOUR task's checkboxes in the plan body. Never touch another
   task's boxes or Outcome.
3. Fill EVERY Outcome field.
4. Commit: `git commit -m "docs: <plan-short> — T<N> complete" -- <plan-path>`
5. Append one line to `.superpowers/sdd/progress.md` if present:
   `Task N: complete (commits <base7>..<head7>, review <verdict>)`.

## Red lines

- Never re-claim or redo a task the ledger marks DONE — after compaction,
  re-verify via `git log`, then trust the ledger.
- Never close a red task. Never soften BLOCKED into DONE_WITH_CONCERNS.
- Ledger edits are separate `docs:` commits — never mixed into code commits.
