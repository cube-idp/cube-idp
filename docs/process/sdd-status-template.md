# SDD status heartbeat

## When to emit (all mandatory)

1. At task claim (baseline render).
2. On every task state change (DONE, BLOCKED, review verdict, fix dispatched).
3. At least every **10 minutes of wall-clock** while work is in flight.
   Long foreground runs are chunked into bounded calls (CLAUDE.md doctrine)
   — render a heartbeat between chunks.
4. Immediately on BLOCKED / NEEDS_CONTEXT / owner-gate hit.
5. As the final report's header.

## Format (blocks in this order; omit a block only if empty)

```
Overall: <D> of <T> tasks complete (<pct>%) · <n> in flight · <n> blocked
Time <HH:MM TZ> · started <HH:MM> · ETA ~<HH:MM>

Phase <K>  <bar>  <a>/<b> <unit>

  T<id>  <name> [<executor>]  → <STATE>  <detail>
         → <sub-item>            IN FLIGHT (<note, e.g. largest: …>)
         · <sub-item>            queued
         ✓ <sub-item>            done
         ⛔ <sub-item>           BLOCKED (<one-line reason>)

Lane <name> — <scope>   <bar>  <a>/<b>   <state / next>

<pacing: mode · measured rate · outlier caveat>
Discovered values (handoff): <k=v · k=v — only values later tasks consume>
Integrity: <main untouched?> · <pushed?> · <n> commits · <dirty files or "worktrees clean">
```

## Rules

- **Bar:** 10–16 cells, `█` filled = floor(done/total × cells), `░` rest.
- **States:** `✓ DONE` · `→ IN FLIGHT` · `· queued` · `⛔ BLOCKED` ·
  `⏸ OWNER-GATED` · `✗ FAILED (being fixed)`.
- **Executor tag:** what is doing the work — `[WORKFLOW wf_…]`, `[$REPO]`
  lane, `[subagent]`, `[inline]`.
- **ETA is measured, never invented:** after ≥1 completed unit,
  `ETA = now + remaining × measured-rate`; always `~`-prefixed; the pacing
  line states the basis (`~200s/doc measured`) and the biggest outlier
  (`README is biggest so likely slower`). Before any unit completes:
  `ETA: measuring`.
- **Integrity line is never omitted.** It answers: is main untouched, was
  anything pushed, how many commits exist, what is currently dirty.
- **Blocked items float up:** any ⛔ appears in Overall AND its phase block.
- **Discovered values** appear the heartbeat after discovery and persist
  until consumed (they mirror the ledger HANDOFF).
- **No prose padding.** The heartbeat is a render, not a narrative;
  anything needing sentences goes in the report or the ledger.

## Example (multi-lane, mid-run)

```
Overall: 17 of 20 tasks complete (85%) · 1 in flight · 0 blocked
Time 17:23 UTC+3 · started 17:21 · ETA ~17:45

Phase 4  ██░░░░░░░░░░░░░░  0/8 docs committed

  T15  doc fixes [WORKFLOW wf_6e796348-22a]  → IN FLIGHT
         → README.md            IN FLIGHT (largest: 51 residue + 9 findings)
         · pack-contract-v1     queued
         · cube-yaml-reference  queued
         · machine-readable     queued
         · kind-config-ref      queued
         · outstanding-todos    queued
         · tests/e2e/PACKS.md   queued
         · CHANGELOG.md         queued

Phase 7  ███░░░░░░░░░░░░░  2/15

Lane $PACKS — engine packs   ██████████  2/2   COMPLETE (T1 flux, T2 argocd)
Lane $ROOT  — engine seam    ░░░░░░░░░░  0/12  T3 next (fences)
Lane owner  — publish        ░░░░░░░░░░  0/1   T15 OWNER-GATED (not authorized this dispatch)

Sequential (shared tree) · ~200s/doc measured · README is biggest so likely slower
Discovered values (handoff): flux chart 2.19.0 (v1.9.2 controllers) ·
  REPLICA_KNOB = kustomizeController.resources.requests.cpu · argocd chart 10.1.4
Integrity: main untouched · nothing pushed · 25 commits · README.md currently modified
```
