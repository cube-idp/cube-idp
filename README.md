# cube-idp

`cube-idp` is a single Go binary for standing up an internal developer
platform from one declarative config document.

**Status: greenfield rebuild — config, cluster, kube, bootstrap, and pack
domains.** The repository was reset to a greenfield baseline on 2026-07-27
and grows in small milestones; today it holds the config domain
(validate/show/scaffold), the cluster domain with a full kind lifecycle
(`init`/`create`/`status`/`delete`), the kube domain (Kubernetes client
access — powering `status`'s API-reachability line), `bootstrap`
(installs Flux and hands over), and the pack domain (define, validate, and
render packs — including helm packs, which delegate to a Flux
`HelmRelease`). The previous implementation is preserved in git history on
`main`. Structure and rationale:
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
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

`cube-idp bootstrap` installs the gitops engine (Flux) declared in
`spec.engine` into the cluster, waits for it to be ready, and applies the
source and sync resources it should watch — after which the engine owns
steady state.

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

## Packs

A **pack** is a self-contained, versioned unit of platform content: a
`pack.cue` declaring `name`, `version`, and an explicit `type` — never
sniffed — plus whatever that type needs. cube-idp **renders** packs; the
gitops engine applies them.

| `type` | Payload | Renders to |
|---|---|---|
| `raw` | manifests under `manifests/` | those manifests |
| `kustomize` | a kustomization root | the built output, with `${VAR}` substituted |
| `helm` | **none** — chart coordinates in `pack.cue` | a Flux `HelmRelease` + its source CR |

```
$ cube-idp pack new ./hello              # a pack that renders as written
created pack hello 0.1.0 (raw) in ./hello
run "cube-idp pack render ./hello" to see what it produces

$ cube-idp pack render ./hello | kubectl apply -f -
$ cube-idp pack validate ./hello         # loads, renders, and discards
pack hello 0.1.0 (raw) is valid
```

Only rendered YAML reaches stdout — diagnostics go to stderr and nothing is
written when rendering fails — so the pipe above is safe.

### Helm packs delegate

A `type: helm` pack carries the chart's **coordinates**, never the chart.
Rendering it emits a `HelmRelease` and the source CR it pulls through, and
**helm-controller templates the chart in the cluster** — cube-idp never
runs Helm, so rendering stays a pure function of its inputs.

```
$ cube-idp pack new ./podinfo --from-chart ./charts/podinfo
created pack podinfo 0.1.0 (helm) in ./podinfo
replace the placeholder chart url in ./podinfo/pack.cue before installing this pack

$ cube-idp pack render ./podinfo         # the delegation, not the expanded chart
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
...
```

`--from-chart` reads a local chart's `Chart.yaml` and `values.yaml` — a
metadata read, nothing fetched — and derives a starting `#Values` you then
narrow. `--type helm` scaffolds the same skeleton from scratch. Both leave
the repository url for you to fill in, because a local directory does not
say where a chart is published.

M9 supports **public chart sources and non-sensitive values only**:
private-registry credentials and secret-backed values are not implemented
yet, and a chart addressed by repository + version is a *mutable*
reference — only an OCI digest pins content.

Packs are installed by listing them in `spec.packs`, each entry naming the
pack and the values for *this* copy of it:

```yaml
spec:
  packs:
    - packRef: ./packs/traefik
      values:
        replicas: 2
```

`cube-idp pack render -f cube.yaml --id traefik` renders that entry as
configured — values merged, external manifests attached — which is what a
later milestone delivers to the cluster. Full contract, including
`#Values` lockdown, `dependsOn`, and the reference grammar:
[docs/domains/pack.md](docs/domains/pack.md).


## Development

```
make test           # go vet + go test ./... -count=1 (hermetic, no Docker)
make lint           # golangci-lint (funlen 50) + 300-line file gate
make generate       # controller-gen deepcopy (output is committed)
make test-e2e       # kind conformance + kube round-trip against real Docker (opt-in)
```

Agent and contributor rules: [CLAUDE.md](CLAUDE.md). Release notes:
[CHANGELOG.md](CHANGELOG.md).
