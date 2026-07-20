# Decision Extraction — GATE 2 Record

**Date:** 2026-07-20  
**Phase:** 2 (Task 8 harvest → clustering → Task 9 validation)

## How this was produced

| Stage | Agents | In | Out |
| --- | --- | --- | --- |
| T8 harvest (1 agent per corpus document) | 31 | 31 docs / 236,377 words | 1,579 candidates |
| T8.5 semantic clustering (1 agent per domain shard) | 19 | 1,579 candidates | 1,168 distinct decisions |
| T9 validation (1 agent per shard, against the truth index + code) | 24 | 621 selected | 621 verdicts |

Zero agent errors across all 74. Coverage verified mechanically at each stage: every
candidate id appears in exactly one cluster (0 dropped, 0 duplicated); every selected
cluster received exactly one verdict (0 missing, 0 duplicated).

## Two plan deviations, both recorded

**1. The plan estimated a review queue of ~15–40 candidates. The harvest returned 1,579** —
roughly 40× over. That estimate was written during design without having read the corpus.
The corpus restates the same decision across successive phase documents (one decision about
`apiVersion` appears in 10 places), which the estimate did not anticipate.

**2. A semantic clustering stage was inserted between T8 and T9.** It is not in the plan.
An initial token-set dedup removed 0 of 1,579 because restatements share meaning, not
vocabulary. Clustering by meaning reduced 1,579 → 1,168.

## Validation scope (operator decision)

Of 1,168 distinct decisions, **621 were validated** and **547 deferred**.

Selection rule: a decision was validated if it was restated in 2+ documents (266 — evidence
it survived repeated review) **or** its canonical statement comes from the current
2026-07-18/19 documents (454). Union = 621.

The 547 deferred are single mentions in early or idea-stage documents that no later
document repeated. They are **recorded, not discarded** — `clusters-deferred.json` holds each
with its exclusion reason, and they can be recovered if this rule proves too aggressive.

**Known limitation:** clustering was sharded by topic domain, so a decision spanning two
domains could remain split. Coverage is provably complete, but the 1,168 likely contains
some cross-domain duplicates.

## Verdicts

| Status | Count | Share |
| --- | --- | --- |
| `binding` | 388 | 62% |
| `abandoned` | 113 | 18% |
| `unverifiable` | 69 | 11% |
| `superseded` | 51 | 8% |
| **total** | **621** | |

- **binding + ADR-worthy: 332** — implemented today AND a contributor could violate them unknowingly.
- binding but implementation detail: 56 — deliberately excluded from ADRs.
- `superseded` (51) feed the ADRs' history sections rather than becoming ADRs themselves.
- `abandoned` (113) and `unverifiable` (69) produce no ADR.

### Evidence quality

- **All 388 binding verdicts carry a codeRef.** The conservative rule (no proof → not binding) held with zero exceptions.
- 403 of 405 `file:line` citations resolve to a real file at a real line. The 2 exceptions
  (`inventory.go`, `ca.go`) were bare basenames; both files exist at `internal/apply/` and
  `internal/trust/` — a citation-formatting flaw, not a fabricated reference.
- 3 randomly sampled binding verdicts were checked against source by hand and matched exactly.

### binding + ADR-worthy by domain

| Domain | Count |
| --- | --- |
| packs-contract | 76 |
| engine-gitops | 61 |
| cluster-provider | 56 |
| cli-ux-output | 42 |
| errors-diag | 32 |
| config-schema | 25 |
| security-trust | 20 |
| gateway-network | 14 |
| other | 6 |

## GATE 2 decision (operator, 2026-07-20)

**332 ADR-worthy decisions would produce an unnavigable ADR set.** Operator directed:
**group into ~25–40 thematic ADRs**, each consolidating its member decisions with their
individual codeRefs listed inside, so no decision is lost — each becomes a clause within a
themed record. The 51 superseded decisions supply those ADRs' history sections.

**GATE 2 is OPEN.** Theming and ADR drafting (Task 10) are authorised. GATE 3 (review of the
drafted ADR set) remains ahead.

