---
status: accepted
date: 2026-07-20
decision-makers:
---

# Diagnostic-Code Identifiers Are Stable Product Surface, Not Planning Residue

## Context and Problem Statement

cube-idp emits typed diagnostics of the form `CUBE-NNNN` (see
[ADR-0030](0030-typed-cube-diagnostics.md) and
[ADR-0031](0031-diagnostic-code-catalog.md)). During the 2026-07-20 documentation
audit, docs and comments were swept for *planning residue* — internal
identifiers such as `D6`, `GT15`, `Task 3`, `R2`, `F10`, and `Phase 5` that had
leaked from the design process into shipped text.

Several of those planning patterns are shaped almost exactly like a diagnostic
code (a letter-ish prefix plus a number). A naive residue pattern — or a careless
edit — could match `CUBE-4016` and either flag it for removal or rewrite it.
Diagnostic codes are the opposite of residue: they are a **contract**. Users
grep for them, scripts branch on them, and `cube-idp explain CUBE-NNNN` resolves
them. Altering or deleting one silently breaks that contract.

We need a durable rule that separates product identifiers from planning
identifiers, so that the audit (and the CI guard that enforces its results) can
never treat a diagnostic code as residue.

## Decision

**`CUBE-[0-9]{4}` identifiers are stable product surface and are excluded, by
construction, from every planning-residue pattern and cleanup pass.**

- No pattern in `hack/docaudit/patterns.txt` may match `CUBE-[0-9]{4}`; the file
  documents this as an invariant at its head.
- Documentation edits (the audit and any future cleanup) must never renumber,
  reletter, or delete a `CUBE-NNNN` code. A doc that cites the wrong code for a
  behavior is corrected *toward* the code the code actually emits — never the
  reverse.
- The audit's verification gate treats the sorted set of diagnostic-code IDs
  (`go run ./hack/truthindex -codes-only`) as a fixture: it must be
  **byte-identical** before and after any documentation change. A diff is a
  failed gate, not an accepted edit.

## Consequences

* Good, because the diagnostic-code contract cannot be eroded by a docs sweep —
  the byte-identical gate makes any drift a hard failure rather than a silent
  regression.
* Good, because the residue-pattern set has one clearly documented forbidden
  match, so future patterns are written with the exclusion in mind.
* Good, because it draws a bright line: `CUBE-NNNN` is product, everything of the
  `D#`/`GT#`/`Task N`/`R#`/`F#`/`Phase N` shape is process.
* Neutral, because it constrains how the recurrence guard may be extended — new
  patterns must be checked against the code catalog before being added.

## Alternatives Considered

* **Rely on reviewer vigilance.** Trust that no edit touches a `CUBE-NNNN` code.
  Rejected: the audit already found 103 residue sites missed by the original
  pattern set; human vigilance at that scale is exactly what the gate replaces.
* **Allowlist codes case-by-case in each pattern.** Add negative lookarounds per
  pattern. Rejected: fragile and easy to forget; a single stated invariant plus a
  byte-identical fixture is simpler and total.
