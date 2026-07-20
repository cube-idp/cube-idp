---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 31. Central Append-Only Diagnostic Code Catalog with Domain-Partitioned Ranges

## Context and Problem Statement

Every user-facing failure in cube-idp carries a `CUBE-XXXX` code so an operator, a
support thread or a CI job can identify the exact failure without parsing prose. That
contract only holds if the codes behave like a stable public surface rather than like
ordinary string literals. Three failure modes threaten it.

First, drift: if any package can write `"CUBE-4019"` inline, the same number ends up
meaning two things in two files, and nothing catches the collision. Second,
undocumented codes: a code that exists in an error path but has no human-readable
summary is useless to the person reading it. Third, reuse: if a retired code's number
is later handed to a different failure, every archived log line, runbook and issue
citing that number silently becomes wrong.

The codes also need to be findable. A flat, first-come numbering makes it impossible
to tell from `CUBE-3011` alone which subsystem failed, so codes need a numeric
partition that maps to the concern that raises them.

## Decision

Every CUBE code is declared as an exported sentinel constant in `internal/diag/codes.go`
matching `^CUBE-[0-9]{4}$`. No non-test Go file outside that catalog may contain a
`CUBE-` string literal — including backtick raw strings — and this is enforced by the
literal-ban test.

Every declared code must also carry a non-empty summary entry in
`internal/diag/registry.go`, enforced by `TestRegistryCoversEveryDeclaredCode`.

The code surface is append-only. Retired codes are marked in place by comment or
registry annotation and are never deleted, and their numbers are not reused.

CUBE code ranges are partitioned by domain and enumerated as section headers in
`internal/diag/codes.go`. New diagnostics are allocated inside the numeric family of
the concern they belong to, so config-family, wait-family and pack-dependency failures
each stay in their own range.

## Consequences

* Good, because a code number is a durable identifier: an operator can search a
  five-month-old log line and still find the right constant, call site and summary.
* Good, because the literal ban makes collisions structurally impossible — two
  meanings for one number cannot both compile past the test.
* Good, because the registry-coverage test is bidirectional, so a code cannot ship
  without documentation and documentation cannot outlive its code.
* Good, because the numeric partition lets a reader infer the subsystem from the code
  alone, before opening any source.
* Bad, because the number space is consumed permanently; retired numbers stay as dead
  weight in the catalog and registry forever.
* Bad, because allocating a code is a two-file edit plus a domain-range judgement,
  which is friction on every new error path.
* Bad, because the append-only rule is a convention over deletion, not a mechanical
  guarantee: a code removed from `codes.go` in the same commit as its registry entry
  leaves no in-place marker, and the tests stay green. `CUBE-1002` and `CUBE-3002` are
  absent with no marker, showing this gap.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Every CUBE code is an exported sentinel constant in `internal/diag/codes.go` matching `^CUBE-[0-9]{4}$`, and no non-test Go file outside the catalog may contain a `CUBE-` string literal. | `internal/diag/codes_test.go:37,44-48` |
| CUBE codes live in a central sentinel catalog enforced by `TestNoCubeLiteralsOutsideCatalog`, whose regex also catches backtick raw-string literals. | `internal/diag/codes_test.go:44` |
| Every new diag code must have a summary registered in `internal/diag/registry.go`. | `internal/diag/registry_test.go:41-57` |
| CUBE diagnostic codes are append-only: retired codes are marked in place by comment edit and never deleted or reused. | `internal/diag/codes.go:78` |
| The code surface is append-only in the registry too: retired codes such as CUBE-3009 keep their registry entry marked retired rather than being removed. | `internal/diag/registry.go:89` |
| Pack dependency diagnostics use CUBE-4018, CUBE-4019 and CUBE-4020, and the argocd dependency wait failure uses CUBE-3011. | `internal/diag/codes.go:112` |
| A new config-family diagnostic covers engine pack ref mismatch, mirroring CUBE-0008. | `internal/pack/enginepack.go:39` |
| (Superseded) A missing local path that is not ref-shaped fails with CUBE-0001, and a remote fetch or single-YAML failure fails with CUBE-0013. | `internal/config/load.go:30-31` |

### Verification

- [ ] `internal/diag/codes_test.go:38` anchors the single exemption to the exact
      repo-relative path `internal/diag/codes.go`, not to the basename.
- [ ] `internal/diag/codes_test.go:44` compiles `cubeLiteralRe` from the character class
      of both quote characters, so a backtick raw string containing `CUBE-` is an
      offender, not only a double-quoted one.
- [ ] `TestNoCubeLiteralsOutsideCatalog` (`internal/diag/codes_test.go:90`) fails when a
      non-test `.go` file outside the catalog holds a `CUBE-` literal.
- [ ] `TestRegistryCoversEveryDeclaredCode` (`internal/diag/registry_test.go:41`) fails
      both when `Describe()` misses a declared code and when
      `len(AllCodes()) != len(declared)`.
- [ ] `internal/diag/codes.go:78` and `:84` still declare CUBE-3003 and CUBE-3009 with
      `(RETIRED 2026-07-19 …)` annotations, and `internal/diag/registry.go:83` and `:89`
      carry the matching retired summaries.
- [ ] `internal/diag/codes.go` carries range section headers (`0xxx: preflight/config`,
      `3xxx: engine`, `4xxx: pack`, …) and CUBE-4018/4019/4020 sit under
      `Pack dependencies` at `internal/diag/codes.go:112-114`.
- [ ] CUBE-4018/4019/4020 are live at `internal/pack/depgraph.go:34,56,77`, `:142` and
      `:41`; CUBE-3011 at `internal/up/up.go:682`.
- [ ] `internal/diag/codes.go:18` declares `CodeEnginePackMismatch Code = "CUBE-0013"` in
      the 0xxx config family and it is raised at `internal/pack/enginepack.go:39`.

## History

The config-load family was originally split: a missing local path that is not
ref-shaped failed with CUBE-0001, while a remote fetch or single-YAML failure was to
fail with CUBE-0013. The CUBE-0013 remote branch was never built.
`internal/config/load.go:28-32` now has a single unconditional read-failure path
emitting CUBE-0001 with the `cube-idp init` remediation, and no ref-shape test precedes
it. The number CUBE-0013 was reallocated to mean engine pack ref mismatch
(`CodeEnginePackMismatch`, `internal/diag/codes.go:18`).

That reallocation predates the append-only rule reaching its current form. Under the
rule as it now stands, superseded numbers keep a retired or reassigned marker in the
catalog and registry rather than being removed or silently re-pointed.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during
the 2026-07-20 documentation audit; the underlying statements were validated against
the code before this record was written.

- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:870` — central catalog and literal ban
- `docs/archive/superpowers/specs/2026-07-15-cube-idp-phase4-first-release-design.md:80` — backtick-aware literal-ban test
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:310` — mandatory registry summary
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:221` — append-only retirement in place
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-pack-depends-and-cubelock-crd.md:39` — pack-dependency range allocation
