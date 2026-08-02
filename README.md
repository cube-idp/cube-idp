# cube-idp

`cube-idp` is a single Go binary for standing up an internal developer
platform from one declarative config document.

**Status: greenfield rebuild — config + cluster domains.** The repository
was reset to a greenfield baseline on 2026-07-27 and grows in small
milestones; today it holds the config domain (validate/show/scaffold) and
the cluster domain (kind provisioning via `init`). The previous
implementation is preserved in git history on `main`. Structure and
rationale: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
What's next: [ROADMAP.md](ROADMAP.md). Decision history:
[docs/DECISIONS.md](docs/DECISIONS.md).

## Build

```
make build          # → ./cube-idp
```

Requires Go 1.26+.

## Usage

All commands operate on a `Config` document (`cube-idp.dev/v1alpha1`):

```
$ cube-idp config validate -f examples/cube.yaml
config "dev" is valid

$ cube-idp config show -f examples/cube.yaml     # round-trips the defaulted config as YAML

$ cube-idp init -f examples/cube.yaml            # needs Docker/Podman
cluster "dev" ready — kubeconfig context "cube-idp.dev/dev" installed
```

`init` creates the cluster declared in `spec.cluster` (kind) and merges a
cube-owned context (`cube-idp.dev/<name>`) into your kubeconfig;
`--kubeconfig <path>` writes a standalone file instead of merging, and
`--kubeconfig-context-name` overrides the context name.

When the config file does not exist, `init` scaffolds it first — with
`metadata.name` from `--name`, otherwise a generated docker-style name —
prints a notice naming the created file and cube, then provisions from
it:

```
$ cube-idp init          # no cube.yaml yet
scaffolded cube.yaml — cube "sunny-walrus"
cluster "sunny-walrus" ready — kubeconfig context "cube-idp.dev/sunny-walrus" installed
```

`--name` never modifies an existing document: a mismatch with its
`metadata.name` fails (`CUBE-CFG-005` — edit the file instead), while a
matching `--name` proceeds, so re-runs stay idempotent.

`config validate` also checks the provider-specific
`spec.cluster.forProvider` payload against the selected provider — no
Docker needed.

Exit codes: `0` success, `2` config document error (`CUBE-CFG-NNN` code
with a remediation hint), `1` anything else (cluster and provider-payload
errors render as `CUBE-CLU-NNN`).

Minimal config ([examples/cube.yaml](examples/cube.yaml)):

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
    forProvider:          # optional kind.x-k8s.io/v1alpha4 Cluster fields
      nodes:
        - role: control-plane
```

## Development

```
make test           # go vet + go test ./... -count=1 (hermetic, no Docker)
make lint           # golangci-lint (funlen 50) + 300-line file gate
make generate       # controller-gen deepcopy (output is committed)
make test-e2e       # kind driver conformance against real Docker (opt-in)
```

Agent and contributor rules: [CLAUDE.md](CLAUDE.md). Release notes:
[CHANGELOG.md](CHANGELOG.md).
