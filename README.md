# cube-idp

`cube-idp` is a single Go binary for standing up an internal developer
platform from one declarative config document.

**Status: greenfield rebuild — config, cluster, and kube domains.** The
repository was reset to a greenfield baseline on 2026-07-27 and grows in
small milestones; today it holds the config domain
(validate/show/scaffold), the cluster domain with a full kind lifecycle
(`init`/`create`/`status`/`delete`), and the kube domain (Kubernetes
client access — powering `status`'s API-reachability line). The previous
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

The lifecycle is four verbs — `init` writes config, `create`/`delete`
manage the cluster, `status` reports:

```
$ cube-idp init                        # no cube.yaml yet — scaffolds it, nothing else
scaffolded cube.yaml — cube "sunny-walrus"
run "cube-idp create" to provision the cluster

$ cube-idp create                      # needs Docker/Podman
cluster "sunny-walrus" ready — kubeconfig context "cube-idp.dev/sunny-walrus" installed

$ cube-idp status
cluster "sunny-walrus": exists
kubeconfig context "cube-idp.dev/sunny-walrus": installed in /home/you/.kube/config
api server: reachable

$ cube-idp delete
cluster "sunny-walrus" deleted — kubeconfig context "cube-idp.dev/sunny-walrus" removed
```

`init` is config-only: it scaffolds a missing config (`metadata.name`
from `--name`, otherwise a generated docker-style name), validates it,
and reports — re-runs print `config cube.yaml exists — cube "…"` and
exit 0. `--name` never modifies an existing document: a mismatch with its
`metadata.name` fails (`CUBE-CFG-005` — edit the file instead), while a
matching `--name` proceeds.

`create` provisions the cluster declared in `spec.cluster` (kind) and
merges a cube-owned context (`cube-idp.dev/<name>`) into your kubeconfig;
`delete` removes the cluster and cleans that context back out (only
cube-owned entries are touched, and the file is never deleted); `status`
is read-only and exits 0 whenever the report succeeds — an absent
cluster or unreachable API server is a finding, not a failure. All three resolve
the cube from the config document and never scaffold it. Each takes
`--kubeconfig <path>` to target a standalone file instead of the default
location, and `--kubeconfig-context-name` to override the context name.

```
$ cube-idp config validate -f examples/cube.yaml
config "dev" is valid

$ cube-idp config show -f examples/cube.yaml     # round-trips the defaulted config as YAML
```

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
make test-e2e       # kind conformance + kube round-trip against real Docker (opt-in)
```

Agent and contributor rules: [CLAUDE.md](CLAUDE.md). Release notes:
[CHANGELOG.md](CHANGELOG.md).
