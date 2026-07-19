# Reusable dispatch prompt — cube-idp p7 engine-as-pack

One manually-dispatched agent per task, plan ticks + ledger as the shared
state (the org-migration/phase-5 execution mode, v2 doctrine carried
over). Copy everything below the line into a fresh agent session to
execute exactly one task; re-paste for each next task. Optionally fill
the `Task id` line at the bottom to override auto-selection.

---

You are executing exactly ONE task of the cube-idp p7 engine-as-pack plan,
then stopping. The plan is NORMATIVE: you make no changes it does not
specify. You do not refactor, redesign, rename, "improve", or add scope.
Where reality contradicts the plan (an API name, a stale Expected line, a
chart values key that differs from the plan's `expected` note), you use the
plan's own escape hatch — verify against the real API/chart, apply the
minimal correction, and record it as a FINDINGS entry — never your own
judgment beyond that. On any unresolvable mismatch you set STATUS: BLOCKED
and stop.

Repos (absolute):
  $ROOT  = /Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/neocube
  $PACKS = /Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/cube-idp-packs

1. Read, in this order (this binds every step you take):
   - $ROOT/docs/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md
     (RATIFIED — decisions D1-D7 and §9 resolutions are binding)
   - $ROOT/docs/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md —
     the Global Constraints, YOUR task's section, the "Agent Execution
     Protocol", and the Ledger. NOTE: on a fresh main checkout this file
     may be absent — it lives on branch `2026-07-19-valuesref-remote-config`
     (and on `p7/engine-as-pack` once created); read it from there.
   - The Ledger HANDOFF blocks of DONE tasks yours depends on (T1/T2
     record the discovered CHART_PIN / MEDIA_TYPES / REPLICA_KNOB values —
     later tasks consume them, never re-discover).

2. CURRENT STATE (verify, don't trust): execution order is STRICT T1→T15.
   Cross-check the Ledger STATUS lines AND `git log --oneline -15` on the
   feature branch of the target repo before claiming: if work already
   exists, do NOT redo it — close the ledger from the evidence. Default
   selection: the first UNCLAIMED task whose predecessors are all
   DONE/DONE_WITH_CONCERNS. A Task id at the bottom overrides. T15 is
   OWNER-GATED: claim it only if the dispatch message explicitly
   authorizes the tag pushes.

3. WORKTREES/BRANCHES (created once, reused by every subsequent agent —
   check for existence first):
   - $ROOT: `git -C $ROOT worktree add $ROOT/.claude/worktrees/p7-engine-as-pack -b p7/engine-as-pack 2026-07-19-valuesref-remote-config`
     (the base branch holds the ratified spec + the plan; if the worktree
     already exists, just use it).
   - $PACKS: `git -C $PACKS worktree add $PACKS/.claude/worktrees/p7-engine-packs -b p7/engine-packs main`
   ALL commits — code AND ledger — land on the feature branch of their
   repo. Never commit to main. Never push ANY ref (T15's authorized tag
   pushes are the sole exception, per §2). Plan/ledger edits are made in
   the $ROOT p7 worktree (the plan lives on that branch) as separate
   `docs:` commits.

4. CLAIM before any code: in the $ROOT p7 worktree, set ONLY your task's
   Ledger STATUS to IN_PROGRESS(<your session id>, <UTC ts>), then
   `git add docs/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md`
   + `git commit -m "docs: p7 plan — claim T<N>" -- docs/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md`.
   You are the only agent running; keep the discipline anyway (re-read the
   ledger immediately before editing; verify HEAD afterward). Commit with
   EXPLICIT pathspecs everywhere — this machine has a stray-staged-files
   gotcha; never `git add -A` outside your task's stated file set, never
   touch docs/superpowers/ beyond your task's checkboxes + your Ledger
   entry.

5. Execute the task's steps IN ORDER, TDD as written; every commit uses
   the step's exact message and ends with the trailer:
   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
   go.mod gains no new module in any task (the helm v4 SDK is already
   vendored). Where a step says "discover" (CHART_PIN, MEDIA_TYPES,
   REPLICA_KNOB), the discovery command + criterion in the step is
   authoritative; record the discovered value in your Ledger HANDOFF —
   network access to helm repos is in-scope for T1/T2 only.

6. OPERATIONAL DOCTRINE (hard-won in phase 5; violating these wedged or
   broke prior runs — mandatory):
   a. FOREGROUND ONLY: never background a long run and wait for a
      notification — that deadlocks the session. Run e2e/conformance as
      foreground Bash, chunked into bounded calls (each under the
      ~10-minute call timeout; loop sleep+check inside one call).
   b. LIVE LEGS (T14/T15): docker + host port 18443 are exclusive. Before
      any kind/e2e run: `kind get clusters` must show no conf-*/e2e
      cluster and 18443 must be free; poll until true. Always
      CUBE_IDP_E2E_GATEWAY_PORT=18443 and
      CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>. One live leg at a time.
   c. COPY — never symlink — any pack dir you must stage (the hasher
      rejects symlinks, CUBE-4001).
   d. Until T15 publishes, oci://…cube-engine-*:0.1.0 does NOT resolve —
      every test needs spec.engine.ref pointing at the local $PACKS p7
      worktree pack dir (the plan's tasks already say where).
   e. VERIFY with real commands, never LSP/editor diagnostics (known
      stale-diagnostics gotcha): the $ROOT gate is
      `go build ./... && go vet ./... && go test ./... -count=1` in the
      worktree, all green, before closing the ledger.
   f. T15 only: exactly ONE tag per `git push` (>3 tags in one push =
      zero GitHub events); only tag-triggered CI can write ghcr packages
      (local token cannot); a new package may be created private — VERIFY
      via `gh api "orgs/cube-idp/packages/container/<name>"`, record for
      the owner, do NOT flip it yourself, do NOT treat as failure.

7. On any Expected-mismatch you cannot resolve with the §5 escape hatch,
   or any STOP condition: stop immediately, STATUS: BLOCKED, BLOCKERS =
   exact command + actual output + diagnosis, commit the ledger, LEAVE the
   worktree and branch in place, report. No workarounds, never close a red
   task.

8. CLOSE the ledger (same worktree, after the gate passes): tick YOUR
   task's checkboxes in the plan body, complete EVERY Outcome field of
   your Ledger entry — STATUS (DONE or DONE_WITH_CONCERNS) · BRANCH ·
   COMMITS (hashes + messages) · FINDINGS (every deviation, "none" over
   dashes) · BLOCKERS · HANDOFF (discovered values, evidence the next
   task needs) — with pasted command OUTPUT as evidence, not paraphrase.
   Commit `docs: p7 plan — T<N> complete` (explicit pathspec). Append one
   line to $ROOT/.superpowers/sdd/progress.md if present (main checkout —
   if that file is not on your branch, note it in HANDOFF instead).

9. Report and STOP (do not claim another task in this session):
   STATUS / Task / Branch + repo / Commits / Evidence (key commands +
   actual output lines — test summaries, e2e verdicts, discovered values)
   / Handoff.

Task id (optional override): ____
T15 outward steps (tag pushes) authorized: no (owner authorizes per-dispatch)
