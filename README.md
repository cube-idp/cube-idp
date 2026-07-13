# cube-idp

cube-idp is a single static Go binary that stands up a complete internal
developer platform on a local or existing Kubernetes cluster in under a
minute — and then gets out of the way.

**Core thesis: cube-idp is a pusher, not an operator.** The binary does four
things: (1) ensures a cluster exists, (2) server-side-applies a GitOps engine
plus a tiny in-cluster OCI registry, (3) renders and delivers data-only
*packs*, (4) diagnoses loudly and exits. Continuous reconciliation is the
GitOps engine's job in-cluster. Re-running `cube-idp up` **is** the upgrade
command. The inventory makes `cube-idp down` a true cascading delete.

There is no in-process controller-runtime manager, no cube-idp CRDs, no
daemon left running on your laptop after `up` exits. The full design
rationale lives in the spec:
[`docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md`](docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md).

## Quickstart

Requires a container runtime (docker or podman) for the `kind` cluster
provider. Nothing else — cube-idp fetches everything it needs itself.

```bash
go build -o cube-idp .

./cube-idp init --name dev          # writes cube.yaml (D9 default profile:
                                     # kind + flux + traefik + gitea + argocd)
./cube-idp up                       # cluster + engine + registry + packs, <60s goal
./cube-idp status                   # component health + inventory size
./cube-idp get secrets -p gitea     # gitea_admin credentials (D9)
./cube-idp down                     # cascading delete, then the cluster
```

`cube-idp up` is idempotent — re-running it after editing `cube.yaml` (or
just re-running it unchanged) **is** the upgrade command; there is no
separate `upgrade` verb in Phase 1.

**Caveat — cluster-shape fields apply only at cluster creation.** For
`provider: kind`, the fields that shape the node itself (`extraPorts`,
`mounts`, `registry`, `providerConfig`, `kubernetesVersion`, and
`gateway.port`) are baked into the cluster when it is first created;
re-running `up` against an existing cluster will not apply changes to them.
To change any of these, recreate the cluster:
`cube-idp down && cube-idp up`.

Developing against an unreleased checkout (no published OCI packs yet)?
Use `init --local <path-to-this-repo>` instead of `init --name dev`, which
writes `gateway.ref` and pack `ref`s as absolute local paths into this
checkout's `packs/` directory rather than `oci://ghcr.io/cube-idp/packs/...`
refs (see `tests/e2e/e2e_test.go` for a full example).

## `cube.yaml` reference

```yaml
apiVersion: cube-idp.dev/v1alpha1   # frozen pre-1.0 (D5); `cube-idp migrate` at v1
kind: Cube
metadata:
  name: dev
spec:
  cluster: {...}
  engine: {type: flux}
  gateway: {...}
  packs: [...]
```

| Field | Type | Default | Notes |
|---|---|---|---|
| `metadata.name` | string | *(required)* | Cube identity; also the `kind` cluster name for `provider: kind`. `^[a-z0-9][a-z0-9-]{0,30}$` |
| `spec.cluster.provider` | `kind` \| `existing` | `kind` | `existing` targets any kubeconfig context |
| `spec.cluster.context` | string | — | kubeconfig context, for `provider: existing` |
| `spec.cluster.kubernetesVersion` | string | `v1.33.1` | `provider: kind` only; rejected for `existing` (CUBE-1003) |
| `spec.cluster.extraPorts` | `[{hostPort, nodePort}]` | — | D10 layer 1: extra host→node port mappings beyond the gateway's |
| `spec.cluster.registry.mirrors` | map | — | D10 layer 1: registry mirror rewrites for the node's containerd |
| `spec.cluster.registry.insecure` | `[string]` | — | D10 layer 1: registries the node's containerd treats as HTTP/self-signed |
| `spec.cluster.mounts` | `[{hostPath, nodePath}]` | — | D10 layer 1: host paths mounted into the node |
| `spec.cluster.providerConfig` | string | — | D10 layer 2 escape hatch: a file path or inline provider-native config (e.g. a full kind config). cube-idp merges in only what it *requires* and fails with a typed error on real conflicts; inspect the merged result with `cube-idp config render-cluster` |
| `spec.engine.type` | `flux` \| `argocd` | `flux` | GitOps reconciler; `argocd` ships in Phase 2 (D2) |
| `spec.gateway.pack` | string | `traefik` | Gateway API implementation |
| `spec.gateway.host` | string | `cube-idp.localtest.me` | routable hostname for delivered packs |
| `spec.gateway.port` | int | `8443` | host port mapped to the gateway |
| `spec.gateway.ref` | string | — | overrides the pack source `up` fetches for the gateway pack (`oci://…`, a local dir, or an absolute path); falls back to `packs/<pack>` when unset, which only resolves from a checkout — `cube-idp init --local` fills this in |
| `spec.packs` | `[{ref, values}]` | gitea + argocd (D9) | additional packs delivered after the gateway; `ref` is `oci://` or a local dir (git `github.com/...` refs ship in Phase 2); `values` are validated against the pack's `#Values` CUE schema before anything touches the cluster |

Run `cube-idp config render-cluster` to preview the final merged kind
provider config (D10 layer 2) before `up` creates anything.

## Pack format

A pack (`internal/pack`) is a directory, fetched from a local dir or
`oci://registry/pack:tag` (git `github.com/org/repo//path@ref` sources ship
in Phase 2). It is **data only** — no code runs from a pack beyond CUE/Helm
rendering, entirely client-side:

```
mypack/
  pack.cue          required: name, version, optional #Values schema
  manifests/*.yaml  optional: raw multi-doc YAML, applied as-is
  chart.yaml        optional: a helm chart reference, rendered client-side
```

**`pack.cue`** — CUE metadata and (optionally) a values contract:

```cue
name:    "gitea"
version: "0.1.0"
#Values: {
    replicas: int & >0 | *1   // schema; values from cube.yaml are validated
                              // against this before anything touches the cluster —
                              // edit spec.packs[].values and re-run `cube-idp up`
}
```

Packs without a `#Values` schema accept any values map unchecked. Values
supplied in `cube.yaml`'s `spec.packs[].values` are unified against
`#Values` (CUE) — the defaulted, concrete result is what actually reaches
rendering.

**`manifests/`** — plain multi-document YAML, parsed and applied via
server-side apply. Files are applied in lexical filename order (hence the
`00-`, `10-`, `20-` prefixes in the shipped packs), which matters when one
manifest depends on another existing first (e.g. a `Namespace` before
objects that live in it).

**`chart.yaml`** — a reference to an external helm chart, template-rendered
in-process (Helm SDK, `DryRun`/`ClientOnly`, no cluster access and no
helm-controller in the loop — engines only ever receive rendered manifests):

```yaml
chart: traefik
repo: https://traefik.github.io/charts   # or `oci://registry/chart` (repo omitted)
version: "41.0.2"
releaseName: traefik
namespace: traefik
values:                                  # chart-level defaults
  deployment:
    replicas: 1
```

**Values merge semantics**, most-specific wins: `chart.yaml`'s `values:` are
the base, deep-merged under the caller's CUE-validated `spec.packs[].values`
(the caller's keys win on conflict; nested maps merge recursively, not
replace-wholesale). If `chart.yaml` sets `namespace:` and the rendered
manifests don't already include that `Namespace` object, one is synthesized
so a chart can't leave dependents in a namespace that doesn't exist yet.

Rendered objects (raw manifests + chart render) are pushed as an OCI
artifact to the in-cluster zot registry and delivered via the configured
`GitOpsEngine` (a Flux `OCIRepository` + `Kustomization` in Phase 1) — the
engine, not cube-idp, owns continuous reconciliation from then on.

## Development

```bash
make build          # CGO_ENABLED=0 go build -o cube-idp .
make test           # go test ./...
make test-apply     # internal/apply against a real envtest API server
                     # (downloads/reuses envtest assets under KUBEBUILDER_ASSETS)
```

Full local verification, mirroring CI:

```bash
go vet ./...
go test ./... -short
make test-apply
CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 25m   # real kind cluster; needs docker
```

The e2e suite (`tests/e2e/e2e_test.go`) is skipped unless `CUBE_IDP_E2E=1` —
it builds the binary, `init --local`s against this checkout, runs `up` twice
(proving idempotency), asserts `status` and `get secrets -p gitea` surface
the expected components/credentials, then `down`s the cluster. It logs the
first `up`'s wall-clock time; the sub-60s goal (spec §3) is a tracked
metric there, not a hard assertion, since image-pull time varies by host and
network.

See [`docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md`](docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md)
for the full architecture, decision log (D1–D10), and phased roadmap.
