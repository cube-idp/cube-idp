---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 28. CLI Command Surface Freeze and the up-as-Upgrade Path

## Context and Problem Statement

`cube-idp` is a single binary whose command tree is its entire public contract. Users
script against it, documentation quotes it, and error messages point at it. A silently
added flag, a renamed subcommand or a reworded `Short` text breaks that contract without
any compiler or test noticing — the change simply ships.

The tree also had to answer a second question that CLIs of this shape usually get wrong:
what does "upgrade" mean? A separate mutating `upgrade` command creates two apply paths
that can drift, and users then have to guess which one is authoritative.

Both problems needed a mechanism, not a convention: something that fails a build when the
surface moves, and a single unambiguous command that applies change.

## Decision

The full CLI surface is frozen. No command may be added, removed or renamed; no cobra flag
may be added or renamed; no default value or `Short` text may change. `TestCommandTreeGolden`
enforces this by rendering the whole tree — one deterministic `<path> | <Short> | flag=default…`
line per command — and comparing it byte-for-byte against `cmd/testdata/clitree.golden`. The
test must pass without `-update` at every commit; regenerating the golden is a conscious,
reviewable act. The accompanying conventions audit covers `-f`/`--file`, `--yes` and prompt
doctrine, so those spellings stay uniform across the tree.

Commands added inside the frozen surface keep the fence intact by regenerating the golden as
part of the same change — `cube config render-engine`, the twin of `render-cluster` that prints
the tuned engine manifests for inspection, is the worked example.

Re-running `cube-idp up` is the upgrade path. It must succeed unchanged on an already-provisioned
cube. `upgrade` never mutates: it only reports. Invoking `upgrade` without `--plan` is an error
that points the user at `up`.

## Consequences

* Good, because the CLI contract is falsifiable — a surface change fails CI rather than
  reaching users unannounced.
* Good, because the golden file doubles as a readable, always-current index of every command,
  flag and default.
* Good, because there is exactly one apply path, so `up` and `upgrade` can never disagree
  about what the cluster should look like.
* Good, because `--plan` being mandatory makes the read-only nature of `upgrade` impossible
  to stumble past.
* Bad, because every intentional surface change costs an extra `-update` round and a golden
  diff in review.
* Bad, because the golden is byte-exact, so cosmetic edits (a typo fix in a `Short`) are as
  loud as semantic ones.
* Bad, because users arriving from tools where `upgrade` applies must unlearn that habit; the
  guard error is the only teacher.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The full CLI surface is frozen — no command added, removed or renamed, no new or renamed cobra flags, no changed defaults or `Short` texts — enforced by `TestCommandTreeGolden` against `cmd/testdata/clitree.golden`, which must pass without `-update` at every commit. | `cmd/clitree_test.go:39-61` |
| `TestCommandTreeGolden` is the permanent CLI-surface fence, and the accompanying conventions audit covers `-f`, `--yes` and prompt doctrine. | `cmd/clitree_test.go:24-60` |
| Re-running `cube-idp up` is the upgrade command: it succeeds unchanged on an already-provisioned cube, `upgrade` never mutates, and `upgrade` without `--plan` errors with a pointer to `up`. | `cmd/upgrade.go:19-24` |
| `cube config render-engine` exists as a twin of `render-cluster`, printing the tuned engine manifests for inspection from the rendered engine pack, without breaking the golden fence. | `cmd/config.go:75-102` |

### Verification

- [ ] `go test ./cmd/ -run TestCommandTreeGolden` passes with no `-update` flag.
- [ ] `cmd/testdata/clitree.golden` exists and contains one line per non-hidden command
      (41 lines at the time of writing).
- [ ] `cmd/clitree_test.go` renders `<path> | <Short> | flag=default…` and fails with
      "command tree drifted from golden" on any difference.
- [ ] `cmd/upgrade.go` returns `diag.CodeUpgradeGuard` with the message
      "cube-idp has no separate apply step: re-running `cube-idp up` IS the upgrade."
      when `--plan` is absent.
- [ ] `cube-idp upgrade` declares only `--file`/`-f` and `--plan`; `upgrade.Plan` reports
      and does not mutate.
- [ ] `cmd/config.go` registers `render-engine` under the `config` parent, sourcing objects
      from `pack.FetchRenderEngine(...)` and marshalling them as multi-doc YAML.
- [ ] `cmd/testdata/clitree.golden` lists both `cube-idp config render-cluster` and
      `cube-idp config render-engine`.
- [ ] Every command in the golden that takes a config file spells it `file=cube.yaml`
      (shorthand `f`), and confirmation-bearing commands (`down`, `trust`, `spoke remove`,
      `plugin install`) spell it `yes=false`.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member provenance:

- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:2269` — `up` as the upgrade path.
- `docs/archive/superpowers/plans/2026-07-19-valuesref-remote-config.md:15` — the golden-enforced surface freeze.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:343` — the fence plus the `-f`/`--yes`/prompt conventions audit.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:150` — `config render-engine` as `render-cluster`'s twin.

Related: ADR 0007 (engine as a pack), which `config render-engine` renders from.
