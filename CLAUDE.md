# cube-idp — Agent Rules

These rules bind every AI agent session in this repository. The structural
authority is `docs/design/2026-07-27-back-to-basics-structure.md`; when this
file and that design disagree, flag the conflict — don't silently pick one.

## 1. What this repo is

cube-idp is being rebuilt from a greenfield v0 baseline (2026-07-27 reset).
Components are added in small milestones — done/queued state lives in
`docs/plans/ROADMAP.md`; direction and rationale in
`docs/plans/2026-08-01-roadmap-direction.md`. Current domains:

- **config**: CRD-ready `Config` type (`cube-idp.dev/v1alpha1`), strict
  loader, CLI `config validate` and `config show`.
- **cluster** (M3): `spec.cluster` sub-struct, `Provisioner` driver seam +
  conformance suite, kind provider, CLI `init` installing cube-owned
  kubeconfig contexts (`cube-idp.dev/<name>`).
  Design: `docs/design/2026-07-29-cluster-domain.md`.

Active documentation is `docs/design/` (approved designs) and `docs/plans/`
(implementation plans + ROADMAP). Everything else under `docs/` — `adr/`,
`architecture/`, `process/`, `reference/`, `archive/`, `vhs/` — is pre-reset
history: read-only, NOT binding, never extended. Do not follow the old
ADR/board/SDD process those files describe.

## 2. Layout and import direction

```
cmd/cube-idp ──▶ internal/cli ──▶ internal/config  ──▶ api/config/v1alpha1
                      │      └──▶ internal/cluster ──▶ api/config/v1alpha1
                      │                │  └── cluster/kind (driver subpackage)
                      └──────▶ internal/cubeerr ◀──── (config, cluster)
```

- Imports flow strictly left to right. `api/` and `internal/cubeerr` are
  leaves: they import nothing from `internal/` — ever. Domains never import
  each other (`internal/config` ↔ `internal/cluster` in particular).
- One component domain = one package under `internal/`. New components add
  a package; they do not grow existing ones.
- Driver subpackages (e.g. `internal/cluster/kind`) are the ONLY importers
  of their backend SDK; nothing else may touch `sigs.k8s.io/kind`.
- Factories and composition live at the CLI/orchestrator edge, never inside
  domain packages (every old import-cycle workaround traced to a factory
  importing its implementations).

## 3. Green definition

Work is done only when, actually run in the worktree:

```
make build && make test && make lint
```

all pass AND `make generate` produces no diff. CI runs exactly these gates.

`make test-e2e` (kind driver conformance against real Docker, worktree-local
KUBECONFIG) is opt-in verification — it is never part of the green gate, and
the gate must stay hermetic: no test in `make test` may need Docker.

Size limits are enforced, not aspirational: functions <50 lines (funlen),
files <300 lines (`make filelen`; `zz_generated*` exempt). When a gate
trips, refactor the code — never raise the limit.

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
  component picks an unused tag and adds a row to the registry in the
  design doc §5.2.
- `ConfigSpec` grows one typed sub-struct per component, with its defaults
  and validation beside it; the loading machinery never changes.

## 5. Go rules

- Wrap every error hop with `%w` plus context:
  `fmt.Errorf("read config %s: %w", path, err)`.
- `context.Context` is the first parameter on anything that does I/O or can
  block.
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
- Tests are table-driven; error paths are first-class rows, not
  afterthoughts.
- Assert on errors via `errors.As` into `*cubeerr.Coded` plus code
  equality — never string matching.
- Runtime dependencies are a closed set (`k8s.io/apimachinery`,
  `sigs.k8s.io/yaml`, `github.com/spf13/cobra`, and `sigs.k8s.io/kind` —
  the latter confined to `internal/cluster/kind`, per the 2026-07-29
  cluster design §8). Adding one requires a design doc in `docs/design/`,
  never a plan footnote.

## 6. Milestone flow

Scale process to the weight of the change; every milestone lands as ONE
green PR to `main`:

- Real architectural choice (new domain, new dependency, new seam, contract
  change) → short design doc in `docs/design/` first, owner-approved.
- Multi-task implementation → checkbox plan in `docs/plans/`.
- Small chore/fix/docs → straight to a PR.

Update `docs/plans/ROADMAP.md` in the same PR that completes (or reorders)
a milestone. Never work on `main`.

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
