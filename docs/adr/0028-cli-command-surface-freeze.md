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
what does "upgrade" mean? The planning corpus records the semantics only — `upgrade`
reports, `up` applies — without recording why; the drift argument below is reconstruction,
not a recorded rationale.

Both problems needed a mechanism, not a convention: something that fails a build when the
surface moves, and a single unambiguous command that applies change.

## Decision

The visible CLI surface is fenced, not sealed: any added, removed or renamed command or flag,
any changed default or `Short`, must regenerate `cmd/testdata/clitree.golden` in the same
commit, making the diff reviewable. `TestCommandTreeGolden` enforces this by rendering the
whole tree — one deterministic `<path> | <Short> | flag=default…` line per command — and
comparing it byte-for-byte against the golden. The test must pass without `-update` at every
commit; regenerating the golden is a conscious, reviewable act. Hidden commands and hidden
flags are skipped by the renderer and so sit outside the fence. The golden pins the `-f`/`--file`
and `--yes` spellings and their defaults; prompt behaviour itself is covered by per-command
tests, not by the fence.

Commands added inside the fenced surface keep the fence intact by regenerating the golden as
part of the same change — `cube config render-engine`, the twin of `render-cluster` that prints
the tuned engine manifests for inspection, is the worked example.

Re-running `cube-idp up` is the upgrade path. It must succeed unchanged on an already-provisioned
cube. `upgrade` is read-only by default: without `--plan` it errors and points at `up`; with
`--plan` it reports drift. On an interactive TTY only, a confirmed prompt hands off to the same
`up` pipeline — there is still exactly one apply path.

## Consequences

* Good, because the CLI contract is falsifiable — a surface change fails CI rather than
  reaching users unannounced.
* Good, because the golden file doubles as a readable, always-current index of every command,
  flag and default.
* Good, because there is exactly one apply path — `upgrade`'s interactive hand-off runs the
  same `up` pipeline — so `up` and `upgrade` cannot disagree about what the cluster should
  look like.
* Good, because `--plan` being mandatory makes `upgrade`'s reporting-first nature hard to
  stumble past; applying from `upgrade` requires both a TTY and an explicit confirmation.
* Bad, because every intentional surface change costs an extra `-update` round and a golden
  diff in review.
* Bad, because the golden is byte-exact, so cosmetic edits (a typo fix in a `Short`) are as
  loud as semantic ones.
* Bad, because the golden skips hidden commands and hidden flags, so the fence does not cover
  them at all.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The visible CLI surface is fenced — any added, removed or renamed command or flag, any changed default or `Short`, forces a golden regeneration — enforced by `TestCommandTreeGolden` against `cmd/testdata/clitree.golden`, which must pass without `-update` at every commit. | `cmd/clitree_test.go:40-62` |
| Hidden commands and hidden flags are outside the fence: the renderer skips both. | `cmd/clitree_test.go:70-72`, `cmd/clitree_test.go:88-90` |
| The `-f`/`--yes` spellings are enforced only indirectly: the golden pins the flag names and defaults, while `--yes` prompt behaviour is covered by per-command tests. | `cmd/down_test.go:259-273`, `cmd/trust_test.go:105-127`, `cmd/plugininstall_test.go:120-135` |
| `upgrade` without `--plan` errors with `diag.CodeUpgradeGuard` and a pointer to `up`; with `--plan` it reports, and only on an interactive TTY does a confirmed prompt hand off to the same `up` pipeline. | `cmd/upgrade.go:19-24`, `cmd/upgrade.go:29-46` |
| Re-running `cube-idp up` is the upgrade command: it succeeds unchanged on an already-provisioned cube. | `internal/up/up.go:97` (apply path), `tests/e2e/e2e_test.go:114` (idempotent re-run) |
| `cube config render-engine` exists as a twin of `render-cluster`, printing the tuned engine manifests for inspection from the rendered engine pack, without breaking the golden fence. | `cmd/config.go:75-102` |

### Verification

- [ ] `go test ./cmd/ -run TestCommandTreeGolden` passes with no `-update` flag.
- [ ] `cmd/testdata/clitree.golden` exists and contains one line per non-hidden command;
      `wc -l < cmd/testdata/clitree.golden` reports 41 at the time of writing (root plus
      40 subcommands, joined with a trailing newline). `go test ./cmd/ -run TestCommandTreeGolden`
      is the executable form of this check.
- [ ] `cmd/clitree_test.go` renders `<path> | <Short> | flag=default…` and fails with
      "command tree drifted from golden" on any difference.
- [ ] `cmd/upgrade.go` returns `diag.CodeUpgradeGuard` with the message
      "cube-idp has no separate apply step: re-running `cube-idp up` IS the upgrade."
      when `--plan` is absent.
- [ ] `cube-idp upgrade` declares only `--file`/`-f` and `--plan` as its own non-inherited
      flags (root's `--color`/`--plain`/`--progress` are inherited, and the golden renders
      non-inherited flags only). `upgrade.Plan` reports; the command applies only via the
      TTY-gated confirmation that calls `runUpPipeline`.
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
