# Archived planning corpus

Everything under this directory is **historical and non-authoritative**. These plans,
specs, and research notes describe the system as it was being designed, phase by phase —
not as it is. They are kept verbatim for archaeology.

Authoritative sources today:

| Question | Where to look |
| --- | --- |
| What does the product do? | The code, and `hack/truth-index.json` (generated from it) |
| Why does it work that way? | `docs/adr/` |
| How do I use it? | `README.md`, `docs/pack-contract-v1.md`, `docs/cube-yaml-reference.md`, `docs/machine-readable-output.md`, `docs/kind-config-reference.md` |

**Nothing in here may be cited as authority for new work.** If a decision recorded here
still matters and has no ADR, that is a gap in `docs/adr/` — fix it there, not by citing
this tree.

## How this archive came to be

A documentation audit on 2026-07-20 mined these 30 documents (~236,000 words) for design
decisions and validated each against the code:

- 1,579 decision candidates were harvested, clustered into 1,168 distinct decisions
- 621 were validated against the code; 388 proved still binding
- 332 of those were judged ADR-worthy and became **ADRs 0002–0039** in `docs/adr/`
- 547 were deferred without validation (single mentions in early or idea-stage documents
  that no later document repeated)

A 100-item stratified sample of the deferred set was later triaged twice, independently.
Both runs agreed that ~15% were binding and not yet covered; the 11 items where they
agreed item-by-item were folded into existing ADRs as clauses. The remainder were
consciously left: roughly 62% were already covered by an ADR or were implementation
detail, and ~25% were abandoned or unverifiable. The sampling data is preserved in the
audit's scratch artifacts and the reasoning is recorded in
`docs/superpowers/research/2026-07-20-decision-extraction-gate2.md`.

## One file left this tree

`2026-07-18-kind-config-reference.md` was promoted to `docs/kind-config-reference.md`.
It was never a plan — it is a kind v1alpha4 field reference, marked
`Status: REFERENCE (research, no decisions)` — and a user-facing error message in
`internal/cluster/kindp/merge.go` points at it when a `forProvider` field fails strict
decoding. Archiving it would have pointed that error at a document declaring itself
non-authoritative.
