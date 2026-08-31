# cube-idp

`cube-idp` is a single Go binary for standing up an internal developer
platform from one declarative config document.

**Status: greenfield rebuild — config, cluster, kube, bootstrap, pack,
engine, gateway, and ca domains.** The repository was reset to a
greenfield baseline
on 2026-07-27 and grows in small milestones; today it holds the config
domain (validate/show/scaffold), the cluster domain with a full kind
lifecycle (`init`/`create`/`status`/`delete`), the kube domain
(Kubernetes client access — powering `status`'s API-reachability line),
`bootstrap` (engine-agnostic install machinery), the pack domain
(define, validate, and render packs — including helm packs, which
delegate to a Flux `HelmRelease`), the engine domain (the invariant
Flux substrate plus the tier-2 engine driver seam — the engine is chosen
at day 0 via `spec.engine.provider` and is immutable per cube; Flux is
the only engine today), and the gateway and ca domains (the trust
fabric: ingress gateway, per-cube certificate authority, in-cluster
wildcard DNS, and the `trust` verbs). The previous implementation is
preserved in git history on
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

`cube-idp bootstrap` installs the Flux substrate into the cluster,
installs the prerequisites listed in `spec.prerequisites` — the trust
fabric, by default — applies the sync wiring declared in
`spec.engine.source`, and waits not just for the controllers to be ready
but for the sync to actually **reconcile** — after which the engine owns
steady state. `spec.engine.version`, when set, must use clean SemVer
(`2.9.2`, not `v2.9.2`) and is checked before any cluster contact.
`--timeout` bounds the whole run and defaults to **10m**, because the
gateway's chart is pulled from a registry inside the cluster.

```
$ cube-idp bootstrap
flux 2.9.2 installed — syncing from https://github.com/you/platform (git)
gateway installed — *.dev.cube.test routed to gateway.gateway-system.svc.cluster.local.
cube CA written to /home/you/.cube-idp/dev/ca.crt
```

The `config` verbs read the document without touching a cluster:

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
```

Left like this, `create` applies the gateway-ready kind defaults (the
`ingress-ready` node label and the 8080/8443 host-port mappings).
Writing `forProvider` — any `kind.x-k8s.io/v1alpha4` Cluster fields,
even `{}` — replaces those defaults wholesale: you own ports and
labels entirely.

## Gateway and trust

A bootstrapped cube comes with an ingress gateway, its own certificate
authority, and in-cluster DNS for a wildcard hostname. Inside the
cluster, `https://anything.<cube>.cube.test` reaches the gateway and is
served with a certificate the cube itself issued — CoreDNS points the
whole `*.<cube>.cube.test` space at a stable `gateway` Service, and the
gateway terminates TLS with a wildcard leaf.

The base domain is `spec.gateway.domain`, derived as `<cube>.cube.test`
when you leave it out. The units installed ahead of the engine are
`spec.prerequisites` — an ordered list defaulting to the gateway
platform, the Gateway API CRDs, the CA secrets, and Traefik. Writing it
replaces that list wholesale rather than merging into it.

The CA is minted once per cube and reused on every later bootstrap: only
the leaf is re-signed, and the private key never leaves the cluster.
Bootstrap copies the public half to `~/.cube-idp/<cube>/ca.crt`, and the
`trust` verbs put it where your browser and tools will look:

```
$ cube-idp trust install dev
installed cube "dev"'s CA from /home/you/.cube-idp/dev/ca.crt into the macos-login trust store

$ cube-idp trust list
CUBE  FINGERPRINT (SHA-256)                                             STORE        INSTALLED
dev   3f2a7c1d94b60e8a52f0cbd371e4a98f60d2b5c8813ae7f042916bd35c7e08a1  macos-login  2026-08-31

$ cube-idp trust remove dev
removed cube "dev"'s CA from the macos-login trust store
```

`trust install` writes to **your** trust store only — the macOS login
keychain, or p11-kit on Linux — and never asks for sudo. `remove` is
idempotent and refuses to delete a certificate that is not this cube's.

Reaching the gateway **from the host** takes one more step that M11 does
not do for you. `cube-idp create` maps the gateway's ports to host
`8080` and `8443` by default, so URLs from the host carry the port —
`https://app.<cube>.cube.test:8443`. Writing any
`spec.cluster.forProvider` — including an empty `{}` — replaces that
default wholesale, so it means owning ports and labels yourself, and the
mappings are fixed at cluster create time. cube-idp writes no
`/etc/hosts` entries either, so making that name resolve on your machine
is still yours to arrange.

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
