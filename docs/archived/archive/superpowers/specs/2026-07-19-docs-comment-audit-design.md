# Documentation & Comment Audit — Design

**Date:** 2026-07-19
**Status:** Approved, not yet executed
**Repos in scope:** `cube-idp`, `cube-idp-web`, `packs`, `plugins`
**Repos out of scope:** `go-getter` (fork of hashicorp/go-getter; we do not own upstream docs)

---

## 1. Problem

Documentation across the cube-idp repos is suspected of contradicting the current
implementation, and code comments carry references to internal planning artifacts
(task IDs, phase numbers, gate numbers) rather than explaining the code.

The suspicion is confirmed by sampling, not assumed:

- `docs/outstanding-todos.md` cites `docs/superpowers/plans/2026-07-18-cluster-forprovider.md`
  and `docs/superpowers/specs/2026-07-18-cluster-forprovider-design.md` as authority for
  deferred work. **Neither file exists.**
- `README.md:85` documents a `cube-idp migrate` command. No such command exists in `cmd/`.
  The same line carries `(D5)`, a planning-decision marker, in user-facing prose.
- `internal/diag/codes.go` — the canonical diagnostic-code table — annotates codes with
  `(Phase 2)`, leaking process vocabulary into the product's error-code reference.

### Measured scope

Documentation volume:

| Location | Files | Words |
| --- | --- | --- |
| `cube-idp/docs/superpowers/**` (plans, specs, research) | 30 | ~230,000 |
| `cube-idp` user-facing (README, `docs/*.md`, CHANGELOG, `tests/e2e/PACKS.md`) | 8 | ~11,000 |
| `packs` | 18 | ~10,000 |
| `plugins` | 3 | ~3,300 |
| `cube-idp-web` | 2 | ~2,300 |

Planning references in code comments (`cube-idp`, Go comment lines only): **~790**.
By pattern across Go/shell/YAML/Makefile: `Task N` 119, `P1`–`P9` 120, `Phase N` 118,
`GT16` 39, `GT15` 21, `GT1` 16, `GT18` 14, `GT17` 13, plus `checkpoint 0.10`,
`decision 11`, `TE-3.4 / R3`, `plan P2 Step 2`.

Sibling repos and CI are comparatively light: `packs` 7 CI/hack hits + 15 in `CONTRACT.md`,
`plugins` 5, `cube-idp-web` ~26 (largely legitimate diagnostic codes rendered on the docs site).

### The sharp edge

`CUBE-XXXX` is **not** a task-ID namespace. It is the product's diagnostic-code system —
100+ codes declared in `internal/diag/codes.go` and surfaced to users via
`cube-idp explain CUBE-XXXX`. It accounts for 572 of the regex hits. A naive strip of
"ticket-looking identifiers" would gut the error-code system. Every mechanical step in this
design treats `CUBE-XXXX` as an allowlisted product identifier.

---

## 2. Goals

1. Every user-facing documentation claim either matches the code or is corrected.
2. Code comments explain *why* the code does what it does, with no reference to planning
   artifacts, task IDs, phase numbers, or gate numbers.
3. Still-binding design decisions have a durable, falsifiable home (ADRs) instead of being
   buried in 230k words of historical plans.
4. The cleanup is enforced by CI so it does not decay.

## 3. Non-goals

- **No behaviour changes.** Where code appears wrong, it is reported, not fixed.
- **No rewriting of historical plans/specs.** They are mined and archived, not edited to
  match today's code. A plan dated 2026-07-13 legitimately describes the world of that date.
- **No unrelated refactoring.**
- `go-getter` upstream documentation.

---

## 4. Decisions taken

Numbered `AUD-n` deliberately: the codebase already uses a bare `D`*n* convention for its own
planning decisions (`(D5)` in `README.md:85` is one of the leaks this project removes), and
reusing that prefix here would collide.

| # | Decision | Rationale |
| --- | --- | --- |
| AUD-1 | `docs/superpowers/**` is mined for binding decisions, then archived — not audited line-by-line, not rewritten | 230k words; the value is the decisions, not the prose |
| AUD-2 | Binding decisions become ADRs in `docs/adr/` (via `adr-skill`); reference docs are trimmed to describe only the current surface | Rationale needs a home; reference docs need to stay about *now* |
| AUD-3 | Code comments are rewritten per-site with judgement, not mechanically stripped | A `sed` pass leaves orphan citations that say nothing, and keeps comments that restate the code |
| AUD-4 | Code is ground truth; doc/code disagreements are corrected in the doc — **except** where the doc describes deliberate specified behaviour, which is reported as a suspected bug | Prevents silently documenting bugs as intended behaviour |
| AUD-5 | Findings report is a hard gate before any edit | ~150–400 findings; misjudged resolutions are cheaper to catch pre-commit |
| AUD-6 | `CUBE-XXXX` is allowlisted everywhere as a product identifier | It is the diagnostic-code namespace, not a ticket namespace |
| AUD-7 | ADR extraction lands **before** comment cleanup | Rewritten comments may cite `docs/adr/NNNN` where rationale is too long to inline; the ADR must exist to be cited |
| AUD-8 | Comment rewrites are derived from the passage the planning ID cites, never reconstructed from memory | `"Task 9"` → prose is only trustworthy if it says what Task 9 actually said |
| AUD-9 | CHANGELOG: planning IDs stripped and factual errors corrected; released entries otherwise verbatim | A published changelog is a record; rewriting it wholesale is its own kind of falsification |

---

## 5. Components

### A. Truth Index

A machine-extracted ground truth of the product surface, derived from code rather than prose.
Auditing prose against prose is the failure mode where an agent hallucinates agreement; the
index makes claims checkable.

| Surface | Extracted from |
| --- | --- |
| Commands, subcommands, flags | `cobra` `Use:` / `Flags()` in `cmd/` |
| Diagnostic codes + meanings | `internal/diag/codes.go` |
| Config schema (fields, enums, defaults) | `internal/config/types.go` |
| Pack contract | `packs/**/pack.cue` + loader in `internal/pack/` |
| Machine output shapes | JSON structs behind `--output` |
| Exit codes | `cmd/root.go` + error mapping |

**Extraction is a small Go tool** (`hack/truthindex/`) that imports the real packages —
walking the registered cobra command tree for commands/flags and reading the `diag` and
`config` declarations directly — not a regex scrape of source text. Regex approximation of
what the code does is exactly the failure mode this project exists to eliminate; the oracle
must not be built out of it. The pack contract is read via CUE evaluation of `pack.cue`.

Emitted as a checked-in artifact (`hack/truth-index.json` plus a human-readable rendering).
CI regenerates the index on every run and fails on drift, so the checked-in copy cannot go
stale — a generated-but-committed file without that check would itself become the next
stale doc. Sibling repos (`packs`, `plugins`, `cube-idp-web`) consume the index as a
published release asset of `cube-idp`, pinned by version, so their guards need no Go
toolchain. It is reused as the input to component E, so it is not throwaway scaffolding.

**Consequence:** any doc claim that cannot be checked against the index is flagged
`unverifiable` rather than silently passing.

### B. Contradiction Audit

Every doc claim is classified into exactly one bucket:

| Bucket | Meaning | Fix |
| --- | --- | --- |
| `stale-doc` | Doc describes old behaviour | Edit the doc |

The audited surface includes **user-facing strings in code**: the `diag` registry summaries
(`internal/diag/registry.go`) are rendered to users by `cube-idp explain` and carry the same
planning leaks as the comments they mirror (e.g. `"cube.lock unreadable or corrupt (Phase 2)"`).
They are claims, and they are audited like any doc sentence.
| `suspected-bug` | Doc describes deliberate specified behaviour the code does not implement | **Report only.** No change without approval |
| `planning-leak` | Process residue: `(D5)`, `Phase 2`, `GT16`, plan-file references | Rewrite or delete |
| `dangling` | Points at something that does not exist | Remove or replace with prose |
| `unverifiable` | Could not be checked against the Truth Index | Escalate for human judgement |

Report row format:

```text
repo · file:line · claim · code reality (with code file:line) · bucket · proposed resolution · confidence
```

Written to `docs/superpowers/research/2026-07-20-docs-audit-findings.md` (dated at
execution). Audit artifacts
(this spec, the findings report) are exempt from the archive move in component C while the
project runs; they are archived at close-out, once nothing references them.

### C. Decision Extraction → ADRs → Archive

1. **Harvest.** Sweep plans/specs/research for decision-shaped statements. The corpus
   self-marks them (`decision 11`, `D5`, `GT16`, `spec decision`, "we chose", "instead of").
   Each candidate carries its source `file:line`.
2. **Validate** each candidate against the Truth Index:
   - **binding** — true in today's code → becomes an ADR
   - **superseded** — a later decision overrode it → recorded as context inside the
     superseding ADR
   - **abandoned** — never built or since removed → one-line tombstone, no ADR
   - **unverifiable** — cannot be proven from code → goes to the review queue, **not** an ADR
3. **Write** one ADR per binding decision in `docs/adr/`, using `adr-skill` for format and
   index. Each ADR cites both its origin plan and the code implementing it, so it is
   falsifiable later.
4. **Archive** originals to `docs/archive/superpowers/` with a README stating they are
   historical and non-authoritative, superseded by `docs/adr/`. Git history preserves
   everything regardless.

"Still binding" is the one genuinely subjective step in this project. The default is
conservative: unproven means `unverifiable`, not an ADR. Expected review queue: ~15–40 candidates.

### D. Comment Cleanup

~800 sites (≈790 Go comment lines, plus stragglers in shell/YAML/Makefile/CI), three
per-site outcomes:

- **rewrite** — comment explains non-obvious behaviour; the planning ID becomes plain prose
  (`"Task 9"` → `"the sha256-pinned git-index path"`), or cites the ADR now holding the
  rationale where it is too long to inline (AUD-7)
- **delete** — comment exists only to cite a plan, or merely restates the code
- **keep verbatim** — `CUBE-XXXX` diagnostic codes

Rewrite discipline (AUD-8): replacement prose is derived from the passage the ID points at
(still readable in `docs/archive/superpowers/` — the archive move precedes this phase).
If the cited passage cannot be located and the comment carries no self-contained meaning,
delete it; if it appears to carry unique meaning that cannot be verified, escalate rather
than paraphrase blind.

Batched for reviewable diffs: `internal/diag`, `internal/config`, `internal/pack`,
`internal/up`, `cmd/`, `tests/e2e`, remaining packages, then a final batch for
`Makefile` + `.github/workflows` + `hack/`.

Verification per batch:

- `go build ./... && go test ./...` pass
- the set of declared diagnostic-code IDs (the `CUBE-NNNN` string constants — **not** the
  comment prose, which this component deliberately edits) is identical before and after;
  any delta fails the batch
- `cube-idp explain` output is byte-identical pre/post **except** for codes whose registry
  summary was itself an approved phase-1 `planning-leak` finding — for those, the output
  must match the approved replacement text exactly (the registry mirrors `codes.go`
  comments verbatim, so summary and comment change together)
- `codes.go` trailing comments are load-bearing: `// reserved:` markers are parsed by
  `internal/diag/codes_test.go` — cleanup must preserve them
- diff review confirming zero non-comment lines changed

### E. Recurrence Guard

`hack/check-docs.sh`, wired into CI in every in-scope repo. Fails on:

- new planning-ID patterns in comments (`CUBE-XXXX` allowlisted)
- dangling markdown links
- documented commands/flags absent from the Truth Index
- Truth Index drift (`cube-idp` CI regenerates it; the result must reproduce the
  checked-in copy)

The pattern list is written once, in phase 1, and shared: the same patterns that inventory
comment sites for component D are what the guard enforces afterwards, so the audit and the
guard cannot disagree about what counts as a planning reference. The guard runs in
report-only mode from phase 1 onward and flips to enforcing in its own phase.

**Honest limit:** the guard catches mechanical recurrence — patterns, dead links, phantom
commands. It cannot catch a semantically stale sentence. That class is bounded by keeping
reference docs small and pointing rationale at ADRs, not by CI.

This is what makes the cleanup hold rather than recur.

---

## 6. Execution phases

| Phase | Output | Gate |
| --- | --- | --- |
| 1 | Truth Index + full findings report + suspected-bug list + shared pattern list | **Approval required before any edit** |
| 2 | PR: ADRs + archive move | review of the ADR set |
| 3 | PR: comment cleanup (`cube-idp`, batched by package) | build + tests + code-ID guard + `explain` sample |
| 4 | PR: reference-doc fixes (README, `pack-contract-v1.md`, `machine-readable-output.md`, CHANGELOG per AUD-9, `outstanding-todos.md` re-pointed at ADRs) | re-run audit → clean |
| 5 | PR: recurrence guard enforcing in CI | guard green on the cleaned tree |
| 6 | PRs: `packs`, `plugins`, `cube-idp-web` | same guard, consuming the published index |
| 7 | Close-out: archive audit artifacts (this spec, findings report) | definition of done below |

ADRs land before comment cleanup (AUD-7): a rewritten comment may cite an ADR where the
rationale is too long to inline, and the ADR must exist to be cited.

`suspected-bug` findings are never auto-resolved. They land as a standalone list at the end
of phase 1 and are triaged separately from this project.

**Definition of done:** guard green in all four repos; zero planning-pattern hits outside
allowlists; zero dangling links; every phase-1 finding resolved or explicitly deferred with
an owner; suspected-bug list handed off for triage.

---

## 7. Risks

| Risk | Mitigation |
| --- | --- |
| Mass comment edit damages the diagnostic-code system | Code-set extracted pre/post and compared; any delta fails the batch (component D) |
| Agent "confirms" a doc claim it did not actually verify | Truth Index makes claims machine-checkable; unverifiable claims are flagged, not passed (component A) |
| A real bug gets documented as intended behaviour | `suspected-bug` bucket; code is never edited to resolve a finding (AUD-4) |
| Invented ADRs for decisions never actually made | Every ADR cites origin `file:line` **and** implementing code; unproven candidates are escalated, not written (component C) |
| Archived plans still cited as live authority | Component E fails CI on dangling links; `outstanding-todos.md` citations are re-pointed to ADRs or severed |
| Review fatigue across ~800 comment edits | Batched per package; each batch independently verifiable |
| A rewritten comment garbles the original meaning | AUD-8: prose derived from the cited passage; unlocatable sources are deleted or escalated, never paraphrased from memory |
| The Truth Index itself goes stale after cleanup | CI regenerates and diffs it on every run (component E) |

---

## 8. Execution mode (resolved 2026-07-20)

**Multi-agent, opted in by the operator.** Phase 1 runs as a coordinated parallel workflow:
auditors fan out per doc-cluster, every finding is adversarially verified by independent
agents before it enters the report, and comment-site classification fans out per package.
The token cost is accepted. Later phases use parallel agents where work items are
independent (comment batches, ADR drafting) and stay sequential where they are not
(archive move, guard flip).

---

## 9. Note

This spec lives in `docs/superpowers/specs/` and is therefore itself a candidate for the
archive described in component C. It is exempt from the phase-2 archive move and is
archived at close-out (phase 7). Any binding decision it contains (the `CUBE-XXXX`
allowlist rule and AUD-9's changelog policy, in particular) should be promoted to an ADR
rather than left here.
