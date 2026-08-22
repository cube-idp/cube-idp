# cube-idp — Agent Rules

These rules bind every AI agent session in this repository. The structural
authority is `docs/ARCHITECTURE.md` (living, updated in place); when this
file and that document disagree, flag the conflict — don't silently pick
one.

## 1. What this repo is

cube-idp is being rebuilt from a greenfield v0 baseline (2026-07-27 reset).
Components are added in small milestones — the queue is `/ROADMAP.md`.
Current domains: **config** (CRD-ready `Config` type, strict loader,
`config validate|show`), **cluster** (M3–M5: `spec.cluster`, `Provisioner`
driver seam, kind provider, full lifecycle CLI), **kube** (M6: leaf
client access from injected kubeconfig bytes + context name), and
**bootstrap** (M7: micro-bootstrap applier — SSA-installs embedded Flux
from `spec.engine` over injected client-go interfaces, waits the bootstrap
kind-set, records an inventory, then hands over to the engine), and
finally **pack** (M8: the self-contained, versioned content unit —
`pack.cue` with a closed `#Values`, `spec.packs` instances with a
`dependsOn` graph,
`pack render|validate|new`; it renders, never applies). M8 also added the
shared-infrastructure leaf **`internal/ref`** (one reference grammar →
tree/file), which is documented inside its only consumer's contract
(`docs/domains/pack.md`) rather than a file of its own — it earns
`docs/domains/ref.md` when a second consumer lands (#136). Per-domain
contracts: `docs/domains/<name>.md`.

### Documentation map (the complete, closed set)

| Document | Purpose | Touched when |
|---|---|---|
| `/ROADMAP.md` | milestone queue + done log | the PR that completes/reorders a milestone |
| `/README.md` | user-facing intro | user-visible behavior changes |
| `/CLAUDE.md` | agent rules + this map | rules or the doc system change |
| `/CHANGELOG.md` | release notes | every milestone PR |
| `docs/ARCHITECTURE.md` | binding cross-cutting architecture (living) | architectural decisions, in the deciding PR |
| `docs/architecture/` | C4 model: `workspace.dsl` + rendered SVGs | when the architecture changes, regenerated from the DSL |
| `docs/domains/<name>.md` | one living contract per domain | the domain's milestones (file created when the domain lands) |
| `docs/DECISIONS.md` | append-only dated decision log | every owner-approved decision |
| `docs/work/` | ephemeral milestone plans/groundwork | created during a milestone, DELETED in its closing PR |
| `docs/archived/` | pre-reset + superseded history | never (read-only, not binding) |

Anti-sprawl rules: dated markdown files are banned outside `docs/archived/`
and `docs/work/`. New markdown may only appear as a new `docs/domains/`
file (with its domain) or inside `docs/work/`; `docs/architecture/` holds
only the C4 model and its rendered assets (DSL + SVGs, regenerated
together, no markdown). Anything else requires editing this map first.
A milestone's "design doc" is a reviewed diff to
`ARCHITECTURE.md`/`domains/<x>.md` plus a `DECISIONS.md` entry —
owner-approved before code, exactly as before.

## 2. Layout and import direction

```
cmd/cube-idp ──▶ internal/cli ──▶ internal/config    ──▶ api/config/v1alpha1
                      │      ├──▶ internal/cluster   ──▶ api/config/v1alpha1
                      │      │        │  └── cluster/kind (driver subpackage)
                      │      ├──▶ internal/kube  (M6 shared-infra leaf: injected
                      │      │        kubeconfig bytes + context name → clients)
                      │      ├──▶ internal/bootstrap (M7: SSA-applies embedded Flux
                      │      │        via injected client-go ifaces → api/config)
                      │      ├──▶ internal/pack (M8: load/validate/render packs
                      │      │        │  → api/config; renders, never applies)
                      │      │        └──▶ internal/ref (M8 shared-infra leaf:
                      │      │               ref grammar → tree/file)
                      └──────┴──▶ internal/cubeerr ◀── (every package above)
```

- Imports flow strictly left to right. `api/` and `internal/cubeerr` are
  leaves: they import nothing from `internal/` — ever. Domains never import
  each other (`internal/config` ↔ `internal/cluster` in particular).
- **Shared-infrastructure leaves are the one sanctioned exception** to
  that rule (2026-08-21), and they are a **closed, listed** set:
  `internal/ref` and `internal/kube`. A component domain MAY import a
  listed leaf; a leaf MAY NOT import a component domain or `api/config` —
  it imports only `internal/cubeerr` and its own backend SDKs. Adding to
  the list is a design-gate event, never an inference from shape. MAY is
  not SHOULD: injection at the CLI edge stays preferred where the value
  crossing is already an interface (`internal/bootstrap` deliberately does
  not import `internal/kube`). Full rule: `docs/ARCHITECTURE.md` §2.
- One component domain = one package under `internal/`. New components add
  a package; they do not grow existing ones. A shared-infrastructure leaf
  is not a component domain and is documented inside its consumer's
  contract until a second consumer earns it a file.
- Driver subpackages (e.g. `internal/cluster/kind`) are the ONLY importers
  of their backend SDK; nothing else may touch `sigs.k8s.io/kind`.
- Factories and composition live at the CLI/orchestrator edge, never inside
  domain packages (every old import-cycle workaround traced to a factory
  importing its implementations). Test seams are injected — a factory is
  passed as a parameter or struct field to the code that uses it, never
  exposed as a mutable package-level `var` that tests overwrite with
  save/restore. Mutable package-level state is banned outside `main`.

## 3. Green definition

Work is done only when, actually run in the worktree:

```
make build && make test && make lint
```

all pass AND `make generate` produces no diff AND `gofmt -l .` prints
nothing. Formatting is part of the lint gate, not a courtesy: the
golangci-lint config enables the `gofmt` and `goimports` formatters, so
unformatted code fails `make lint` in CI, never just in review. CI runs
exactly these gates.

`make test-e2e` (kind driver conformance against real Docker, worktree-local
KUBECONFIG) is opt-in verification — it is never part of the green gate, and
the gate must stay hermetic: no test in `make test` may need Docker.

Size limits are enforced, not aspirational: functions <50 lines (funlen),
files <300 lines (`make filelen`; `zz_generated*` exempt). funlen
excludes `_test.go` files (a table of cases is data, not complexity —
keep tables inside their test function). When a gate trips, refactor the
code — never raise the limit.

## 4. Structure conventions

- `api/` is a pure contract: types, defaults, validation. No I/O, no logic.
- `internal/cli` is cobra wiring only: flag mapping, zero business logic.
- Domains never print. Only `internal/cli/exit.go` renders errors (stderr)
  and maps exit codes: 0 success, 2 config error, 1 anything else.
- Load pipeline order is fixed: strict decode → `Default()` → `Validate()`.
  A non-nil `*Config` is always complete and valid.
- Errors: `internal/cubeerr` is machinery only and must never grow a code
  catalog. Each domain declares its own `CUBE-<TAG>-NNN` codes in its own
  `errors.go` (config owns `CUBE-CFG-*`, cluster owns `CUBE-CLU-*`). A new
  component picks an unused tag and updates the registry table in
  `docs/ARCHITECTURE.md` §5.
- `ConfigSpec` grows one typed sub-struct per component, with its defaults
  and validation beside it; the loading machinery never changes.

## 5. Go rules

- Handle every error exactly once: return it wrapped with `%w` plus
  context the caller doesn't already have
  (`fmt.Errorf("read config %s: %w", path, err)`), or handle it at the
  edge — never both, and never wrap with restated context. Only
  `internal/cli/exit.go` renders.
- `context.Context` is the first parameter on anything that does I/O or can
  block. Tests and conformance suites obtain their context from
  `t.Context()`, never `context.Background()` — cancellation at test end is
  part of the contract being exercised.
- Interfaces are consumer-side by default: defined where used, 1–3 methods,
  and only once a real second consumer or implementation exists. The only
  interfaces so far are designated driver seams (`cluster.Provisioner`) —
  deliberately.
- Swappable backends (cluster providers, gitops engines) use driver seams:
  the interface plus an exported `Run<Seam>Conformance(t, factory)` suite
  live in the domain package; implementations live in subpackages and each
  runs the shared suite.
- Mocks are hand-rolled function-field structs. No mockgen, no generated
  mocks.
- Tests are table-driven wherever cases share one code path; error paths
  are first-class rows, not afterthoughts. Cases needing conditional
  setup, per-case mocking, or branching assertions get separate test
  functions instead of a forced table.
- Assert on errors via `errors.As` into `*cubeerr.Coded` plus code
  equality — never string matching for error identity. For stdlib errors,
  use `errors.Is` with the sentinel (`errors.Is(err, fs.ErrNotExist)`),
  never the legacy `os.IsNotExist`-family helpers, which do not unwrap.
- Coded-error constructors are functions named `new<Thing>Error`
  (exported: `New<Thing>Error`) — the `Err` prefix is reserved for
  sentinel `var`s comparable with `errors.Is`, and this repo has none.
  Error identity is always the `cubeerr.Code`, asserted via `errors.As`.
- Every exported identifier has a doc comment — a full sentence starting
  with the identifier's name. A group comment may cover a const/var block,
  but exported functions and types are documented individually.
- Runtime dependencies are a closed set (`k8s.io/apimachinery`,
  `sigs.k8s.io/yaml`, `github.com/spf13/cobra`, `sigs.k8s.io/kind` —
  confined to `internal/cluster/kind` — and `k8s.io/client-go` —
  construction confined to `internal/kube` — see the dependency
  table in `docs/ARCHITECTURE.md` §8). Adding one requires an
  owner-approved `ARCHITECTURE.md` §8 update + `DECISIONS.md` entry, never
  a plan footnote.

## 6. Milestone flow (epic-driven)

Scale process to the weight of the change. Every milestone is a GitHub
**epic issue** (label `epic`), opened when the milestone is picked up:

1. **Design gate first** where the triggers apply (new domain, new
   dependency, new seam, contract change): a reviewed diff to
   `docs/ARCHITECTURE.md` / `docs/domains/<x>.md` + a `docs/DECISIONS.md`
   entry, operator-approved before any code.
2. **Task breakdown before work starts.** Every task becomes its own
   issue (label `task` + relevant labels below), listed as a checklist of
   issue refs in the epic body; each task issue says "Part of #<epic>".
   The breakdown is aligned with the operator BEFORE work begins.
3. **PRs stay small; a milestone may span several.** Every PR references
   its epic ("Epic: #N") and closes the task issues it completes
   ("Closes #M"). Each PR is green (§3) and delivered as small reviewed
   chunks: implement one reviewable unit, run the gates, present the diff
   for operator/coordinator review of cross-subsystem effects and rule
   conformance, commit — never one big drop.
4. **Scope change == issues.** Adding or dropping work mid-milestone
   requires operator alignment, a new (or closed) task issue reflecting
   it, and the matching doc updates in the same breath. Undeclared work
   does not exist.
5. **The last task of every epic, always:** "Docs & architecture
   consistent and updated" (label `docs`) — verify ROADMAP,
   ARCHITECTURE, domains/, DECISIONS, README, and CHANGELOG reflect
   reality, this milestone's `docs/work/` items are deleted, and every
   pointer resolves. The epic closes only after this issue does.

Labels in use: `epic`, `task`, `docs`, `feature` (new-capability work),
`scope-change` (a task added after the epic's initial alignment), `bug`
(defect found outside milestone flow), `needs-decision` (blocked on an
operator decision), `needs-adr` (requires a DECISIONS.md entry / design
gate before work starts), `wontfix`; `domain:<name>` labels are created
as domains land. PR sizing: `size-s|m|l|xl` on pull requests (`size-xl`
is a signal to split). This is the complete set — new labels require
editing this list. Issue forms in `.github/ISSUE_TEMPLATE/` auto-apply
their label and may only reference labels from this list; the forms are
updated in the same PR as any change to it. PRs carry exactly one
`size-*` label; `domain:*` and type labels live on issues, not PRs.
Small chore/fix/docs work outside any milestone still
goes straight to a PR without an epic.

Update `/ROADMAP.md` in the PR that completes (or reorders) a milestone.
Never work on `main`.

## 7. Cluster work (applies from the cluster milestone on)

`KUBECONFIG` always points to a file inside the session's worktree
(`<worktree>/.kube/config`), set inline on each command:

```
KUBECONFIG=$PWD/.kube/config kind create cluster ...
KUBECONFIG=$PWD/.kube/config go test ./tests/...
```

The global `~/.kube/config` is never read or written by any test, tool, or
action — no session-wide exports, no shell-profile edits. Delete the file
when the cluster it points at is deleted.
