---
status: accepted
date: 2026-07-20
decision-makers:
---

# Changelog Records Released Behavior, Not Internal Process

## Context and Problem Statement

`CHANGELOG.md` is user-facing: it tells someone upgrading what changed and how it
affects them. During development, the changelog accreted internal process
vocabulary — a version header cut "by Phase 4 R10", entries tagged with decision
IDs (`(D12)`, `(D4/D10/D12)`, `D15 …`), and roadmap-phase groupings used as
internal milestones.

The 2026-07-20 documentation audit had to decide how far to clean the changelog.
A changelog is unlike other docs in one respect: **released entries are a
historical record.** Rewriting the substance of what a release said it shipped is
revisionism, even when the prose is awkward. But the internal identifiers help no
reader and are exactly the planning residue the audit exists to remove.

We need a policy that says what may and may not be edited in the changelog.

## Decision

**The changelog records released behavior for users. Strip internal process
identifiers and fix factual errors; never rewrite the substance of a released
entry.**

Permitted edits:
- Remove internal decision/gate/work-package/finding identifiers
  (`D#`, `GT#`, `Task N`, `R#`, `WP#`, `P#`, "cut by Phase N R#") from entry text.
- Correct factual errors (a command that never shipped, a wrong flag, a wrong
  `CUBE-NNNN` code) against the code and the oracle.

Forbidden edits:
- Rewording or restructuring the *substance* of an entry for an already-released
  version. If it shipped, the record of what shipped stays.
- Altering any `CUBE-NNNN` code (see
  [ADR-0040](0040-diagnostic-code-identifier-stability.md)).

Structure:
- Entries are grouped by version. Dated development-milestone subheadings that
  already exist in released sections are left as-is (they are historical), but new
  releases are organized by version, not by internal phase.

## Consequences

* Good, because upgraders get an accurate, jargon-free record and the historical
  integrity of released entries is preserved.
* Good, because the edit boundary is unambiguous — "process label or factual
  error → edit; released substance → leave" — so the rule is mechanically
  applicable in review and in the recurrence guard.
* Neutral, because the existing phase-grouped historical entries remain slightly
  inconsistent with the version-first structure going forward; this is the cost of
  not rewriting released records.

## Alternatives Considered

* **Rewrite the changelog into a clean version-first structure.** Rejected:
  rewriting released entries falsifies the historical record and the audit's own
  principle is that shipped statements are evidence, not drafts.
* **Leave the changelog untouched.** Rejected: the internal identifiers are
  reader-hostile residue of exactly the kind the audit removes everywhere else;
  exempting the changelog would be arbitrary.
