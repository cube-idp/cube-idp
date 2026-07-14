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
| `spec.gateway.port` | int | `8443` | host port mapped to the gateway's `websecure` (HTTPS) listener — see the note below |
| `spec.gateway.ref` | string | — | overrides the pack source `up` fetches for the gateway pack (`oci://…`, a local dir, or an absolute path); falls back to `packs/<pack>` when unset, which only resolves from a checkout — `cube-idp init --local` fills this in |
| `spec.packs` | `[{ref, values}]` | gitea + argocd (D9) | additional packs delivered after the gateway; `ref` is `oci://` or a local dir (git `github.com/...` refs ship in Phase 2); `values` are validated against the pack's `#Values` CUE schema before anything touches the cluster |

Run `cube-idp config render-cluster` to preview the final merged kind
provider config (D10 layer 2) before `up` creates anything.

> **Phase 1 → Phase 2 behavior change:** Phase 1 mapped host
> `spec.gateway.port` (default `8443`) to Traefik's plain-HTTP NodePort
> `30080` while printing an `https://` URL. Phase 2 makes that URL true:
> host `gateway.port` now maps to the `websecure` NodePort `30443` (TLS
> terminated by Traefik with a cube-idp CA-issued cert from `up`), and
> plain HTTP stays available in-cluster on the `web` listener. Existing
> kind clusters need `down`/`up` to pick up the new mapping.

## Node-image cache (warm `up`, spec §3's <60s goal)

Spec §3's "`up` completes in under 60 seconds" is a **warm** goal: it
excludes the time a cold node spends pulling `kindest/node:<version>` and
every pack's images from upstream registries the first time. cube-idp does
no pre-pull engineering itself (no bundled image cache, no background
warmer) — `spec.cluster.mounts` (D10 layer 1) is general enough to build one
yourself:

```yaml
spec:
  cluster:
    provider: kind
    mounts:
      - hostPath: ~/.cache/cube-idp/containerd   # persists across `down && up`
        nodePath: /var/lib/containerd
```

kind's node is itself a container; its containerd content store and
snapshots normally live at `/var/lib/containerd` and vanish with the node
when `cube-idp down` deletes the cluster. Mounting a stable host directory
over that path instead means a subsequent `cube-idp up` (after `down`, or
after switching `cube.yaml` and recreating per the cluster-shape caveat
above) reuses every image layer already pulled into that directory — no
registry round-trip for anything unchanged since the last run. The first
`up` against an empty cache directory is still cold; every one after it, on
the same host, is warm.

Two caveats:

- **Cluster-shape fields apply only at creation** (see the caveat above) —
  `mounts` included. Adding this mount to an existing cluster's `cube.yaml`
  has no effect until the next `down && up`.
- **CI runners are typically ephemeral** — a fresh GitHub-hosted runner has
  no prior `~/.cache/cube-idp/containerd` to mount, so CI's own `up` runs
  are cold by default (`tests/e2e/e2e_test.go` tracks the wall time as a
  metric, not an assertion, for exactly this reason). A self-hosted runner,
  or a CI cache-restore step that seeds that directory before `up` runs,
  gets the same warm-run benefit real hosts do.

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

## Engines

`spec.engine.type: flux | argocd` selects the in-cluster GitOps reconciler.
Both pass the identical contract suite (`make test-engines`, D2) — the same
behavior (install → deliver a pack → report health → uninstall) is asserted
delivery-mechanism-agnostically, so either engine is a drop-in choice:

- **`flux`** (default) delivers packs as a Flux `OCIRepository` + `Kustomization`
  pair per pack.
- **`argocd`** delivers packs as one Argo CD `Application` per pack, sourced
  from the in-cluster OCI registry (`spec.source.repoURL: oci://...`,
  requires an Argo CD build with native OCI application-source support —
  cube-idp vendors v3.4.5). Because `engine.type: argocd` already installs
  Argo CD (UI included), `init --engine argocd` drops the redundant `argocd`
  pack from the default profile (CUBE-0005). Argo CD's repo-server only
  accepts a fixed allow-list of OCI layer media types by default, which does
  not include the one cube-idp's shared pusher writes
  (`application/vnd.cncf.flux.content.v1.tar+gzip` — chosen so the same
  artifact byte-for-byte satisfies Flux's `OCIRepository` reconciler too);
  the vendored `argocd-cmd-params-cm` ConfigMap in
  `internal/engine/argocd/manifests/install.yaml` patches
  `reposerver.oci.layer.media.types` to add it, so the argocd engine accepts
  cube-idp's artifacts out of the box with no extra configuration.

## HTTPS & trust

`cube-idp up` gives you real HTTPS from first boot (D12): a local
certificate authority is generated (or an existing mkcert root is adopted
automatically — same CA, zero prompts, green padlocks if your browser
already trusts mkcert) *before* the cluster is even created, then mounted
into every node's containerd `certs.d` and used to issue the gateway's
leaf certificate. `https://gitea.cube-idp.localtest.me:8443` works
immediately after `up` — no manual cert setup.

The OS trust store itself is **never** touched automatically. Making your
browser trust the generated CA (so it stops just being "not actively
warning because you added an exception") is `cube-idp trust` — opt-in,
consent-prompted (`--yes` to skip the prompt in scripts). `cube-idp trust
--uninstall`, or a plain `cube-idp down`, fully reverts the OS trust store
change (D6).

> **Phase 1 → Phase 2 port-mapping change:** Phase 1 mapped host
> `spec.gateway.port` (default `8443`) to Traefik's plain-HTTP NodePort
> `30080` while merely *printing* an `https://` URL. Phase 2 makes that URL
> true: `gateway.port` now maps to the `websecure` NodePort `30443` (TLS
> terminated by Traefik with the cube-idp-issued cert), and plain HTTP stays
> reachable only in-cluster on the `web` listener. Existing kind clusters
> need `down`/`up` to pick up the new mapping.

## Day 2

- **`cube-idp diff`** — a dry-run server-side-apply diff (what would change
  on the cluster) plus inventory-orphan detection and lock-hash pack drift.
  A converged cube prints nothing and exits 0.
- **`cube-idp upgrade --plan`** — re-resolves every remote pack ref's pin
  (git tags/branches, OCI tags) against `cube.lock` and reports what would
  move, without touching the cluster. Exits 0 on a converged cube.
- **`cube-idp doctor`** — preflight and live diagnostics: container runtime
  present, gateway port free, disk space, inotify limits (Linux), git CLI
  present when git-sourced packs are configured, plus provider/engine health
  — every finding carries a `CUBE-xxxx` code and a copy-pasteable
  remediation.
- **`cube.lock`** — written by `up`, one entry per pack:
  - `resolved` — the concrete ref `up` actually fetched (a resolved git SHA,
    an OCI digest, or a content dirhash for local/http/s3 sources).
  - `renderedHash` — a stable content hash of the rendered manifests, used
    by `diff` to detect pack-level drift without re-rendering everything.
  - `images` — every container image referenced by the rendered objects,
    for offline auditing/vulnerability scanning.

  Commit `cube.lock` alongside `cube.yaml` — it pins what actually shipped,
  the way a lockfile does for a package manager.

## Pack sources

A pack ref (`spec.gateway.ref` / `spec.packs[].ref`) accepts:

| Form | Example | Pin behavior |
| --- | --- | --- |
| local directory | `./mypack`, `packs/gitea` | content dirhash |
| OCI | `oci://ghcr.io/cube-idp/packs/gitea:0.1.0` | digest |
| bare git grammar | `github.com/org/repo//path@v1.2.3` | tag/branch resolved to a commit SHA, or a full SHA passed through |
| explicit go-getter URL | `git::https://example.com/repo.git//path?ref=v1`, `s3::https://s3.amazonaws.com/bucket/pack.tar.gz`, `https://example.com/pack.tar.gz` | dirhash of the fetched tree |

Remote refs must be pinned (a tag, a full commit SHA, or an explicit
`?ref=`) — `HEAD`, a bare branch name with no `@rev`, or a wildcard is
rejected (CUBE-4007) so `cube.lock` always records something reproducible.

Git-sourced packs (the bare grammar and `git::` URLs) shell out to the
system `git` binary (go-getter's `GitGetter`) — every other source form is
binary-pure. `cube-idp doctor` warns (CUBE-0105) if git-sourced packs are
configured but `git` isn't on `PATH`. Every fetched tree, regardless of
source, passes cube-idp's extraction guards (path traversal / symlink
escape, CUBE-4014) before anything is read from it.

## Pack discoverability (D11)

Every delivered pack gets a cluster-scoped `Pack` custom resource
(`packs.cube-idp.dev`), so `kubectl get packs` works with zero cube-idp
tooling on the query path:

```console
$ kubectl get packs
NAME     VERSION   URL                                         AUTH-SECRET             READY
gitea    0.1.0     https://gitea.cube-idp.localtest.me:8443   gitea/gitea-admin-cube-idp   true
```

The columns come straight from the pack's own `pack.cue` **`expose:`**
block — data, not code:

```cue
expose: {
    urls: ["https://gitea.${GATEWAY_HOST}"]         // ${GATEWAY_HOST} -> spec.gateway.host
    authSecretRef: {namespace: "gitea", name: "gitea-admin-cube-idp"}
    impliedFields: {username: "gitea_admin"}         // merged under the secret's own keys
}
```

`${GATEWAY_HOST}` expands to `spec.gateway.host[:port]` — the port is
appended unless it's 443 (HTTPS's default) — so the rendered link is
clickable as-is, without an operator having to know or append the
gateway's actual listening port (default 8443) by hand.

`cube-idp get secrets` follows `expose.authSecretRef` to the referenced
Secret and merges `impliedFields` underneath it (the secret's own keys win
on conflict — `impliedFields` only fills in what the secret itself doesn't
carry, e.g. Argo CD's implicit `admin` username, never actually stored in
`argocd-initial-admin-secret`). The older `cube-idp.dev/cli-secret` label
convention is **deprecated** (one release of grace) in favor of this
`expose:`-driven pivot.

## Terminal output

On a real terminal, `cube-idp` prints styled, colorized status lines
(lipgloss). Piped output, `--plain`, or `$CI` set all force stable,
machine-readable plain lines instead — the plain format is pinned
byte-for-byte to what phase 1 shipped, so scripts/CI never see output
churn across releases. `cube-idp init` runs a short interactive wizard
(huh) when no flags are given on a TTY; any flag short-circuits the wizard
for scripted/CI use.

The output style is one knob: `--progress=auto|plain|live|json` (or the
`CUBE_IDP_PROGRESS` env var; `--plain` is a permanent alias for
`--progress=plain`). `--progress=json` turns long-running commands
(`up`, `down`) into a JSON-lines event stream, and `status`, `doctor`, and
`get secrets` also accept `--output json` for a single gh-style JSON
document. Both schemas are **experimental** until the config v1 freeze —
see [docs/machine-readable-output.md](docs/machine-readable-output.md) for
the full event and document reference.

## Migrating from idpbuilder

`cube-idp cnoe import ./your-idpbuilder-packages` ingests idpbuilder-style
Argo CD `Application`/`ApplicationSet` YAML:

- Plain `Application` manifests translate directly; local-dir sources
  (`cnoe://<relative-dir>`) are pushed as an OCI artifact and delivered
  through whichever engine `cube.yaml` configures (engine-neutral — the
  import doesn't care if you're on flux or argocd).
- `ApplicationSet` **list** generators expand into one `App` per list entry;
  every other generator kind (`git`, `clusters`, `matrix`, …) is rejected
  with a typed error (CUBE-4009) naming the unsupported generator, rather
  than silently dropping entries.
- Git sources with an **unpinned** `targetRevision` (empty, `HEAD`, or a
  glob) are rejected (CUBE-4009) — "set `spec.source.targetRevision` to a
  tag or full commit SHA, then re-import" — the same reproducibility
  requirement `cube.lock` enforces for native pack refs.

## Development

```bash
make build          # CGO_ENABLED=0 go build -o cube-idp .
make test           # go test ./...
make test-apply     # internal/apply against a real envtest API server
                     # (downloads/reuses envtest assets under KUBEBUILDER_ASSETS)
make test-engines   # the engine contract suite (flux + argocd) against envtest
```

Full local verification, mirroring CI:

```bash
go vet ./...
go test ./... -short
make test-apply
make test-engines
CUBE_IDP_E2E=1 CUBE_IDP_E2E_ENGINE=flux   go test ./tests/e2e/ -v -timeout 25m   # real kind cluster; needs docker
CUBE_IDP_E2E=1 CUBE_IDP_E2E_ENGINE=argocd go test ./tests/e2e/ -v -timeout 25m
```

The e2e suite (`tests/e2e/e2e_test.go`) is skipped unless `CUBE_IDP_E2E=1`,
and runs across the `{flux, argocd}` engine matrix via
`CUBE_IDP_E2E_ENGINE` (defaults to `flux`; CI runs both as a matrix job,
spec §5: `{kind} x {flux, argocd} x {up, diff, upgrade, down}`). It builds
the binary, `init --local`s against this checkout, runs `doctor` then `up`
twice (proving idempotency), asserts `cube.lock` was written with a
`renderedHash`, that a converged cube's `diff`/`upgrade --plan` both exit 0,
that `status` and `kubectl get packs` (D11) surface the expected
components/printer columns, that the gateway serves a cube-idp CA-issued
TLS cert, that `cnoe import` round-trips a fixture Application, and that
`get secrets -p gitea` surfaces `gitea_admin` — then `down`s the cluster.
It records the first `up`'s wall-clock time as a tracked metric (`t.Logf`,
plus a `GITHUB_STEP_SUMMARY` line when running under GitHub Actions) —
spec §3's <60s goal is warm, not a hard assertion here, since image-pull
time varies by host and network and this repo's own CI runs are typically
cold (see "Node-image cache" above).

Locally, a host port already bound by an unrelated cluster (e.g. another
kind cluster squatting `0.0.0.0:8443`) can be dodged without touching that
cluster: `CUBE_IDP_E2E_GATEWAY_PORT=18443` rewrites the generated
`cube.yaml`'s `spec.gateway.port` before `up` runs. CI always uses the real
default (`8443`).

See [`docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md`](docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md)
for the full architecture, decision log (D1–D10), and phased roadmap.
