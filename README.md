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

## Install

Releases are private — authenticate `gh` to RafPe/cube-idp first.

```bash
gh release download v0.1.0 -R RafPe/cube-idp -p "cube-idp_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz"
tar xzf cube-idp_*.tar.gz
shasum -a 256 -c <(gh release download v0.1.0 -R RafPe/cube-idp -p checksums.txt -O - | grep "$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')")
chmod +x cube-idp && mv cube-idp ~/bin/   # or anywhere on PATH
cube-idp version
```

`go install github.com/rafpe/cube-idp@v0.1.0` does NOT work while the repo is
private unless you set `GOPRIVATE=github.com/rafpe/cube-idp` and have git
auth to the repo; prefer `gh release download`.

> **Known limitation (v0.1.0, F12):** the default profile resolves the gateway
> pack from the repo-relative path `packs/traefik`, so `cube-idp init && cube-idp up`
> must currently run from a cube-idp checkout (clone the repo, run the binary at
> its root). Outside a checkout, point `spec.gateway.ref` at a gateway pack
> directory you provide. A future release publishes the gateway packs as
> `oci://` refs so the downloaded binary works standalone.

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
checkout's `packs/` directory rather than `oci://ghcr.io/rafpe/cube-idp/packs/...`
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
| `metadata.name` | string | *(required)* | Cube identity; also the `kind`/`k3d` cluster name for the local providers. `^[a-z0-9][a-z0-9-]{0,30}$` |
| `spec.cluster.provider` | `kind` \| `k3d` \| `existing` | `kind` | `kind` and `k3d` create a local cluster; `existing` targets any kubeconfig context (see "k3d provider" below) |
| `spec.cluster.context` | string | — | kubeconfig context, for `provider: existing` |
| `spec.cluster.kubernetesVersion` | string | `v1.33.1` | local providers only (kind node image `kindest/node:<ver>`, k3d node image `rancher/k3s:<ver>-k3s1`); rejected for `existing` (CUBE-1003) |
| `spec.cluster.extraPorts` | `[{hostPort, nodePort}]` | — | D10 layer 1: extra host→node port mappings beyond the gateway's |
| `spec.cluster.registry.mirrors` | map | — | D10 layer 1: registry mirror rewrites for the node's containerd |
| `spec.cluster.registry.insecure` | `[string]` | — | D10 layer 1: registries the node's containerd treats as HTTP/self-signed |
| `spec.cluster.mounts` | `[{hostPath, nodePath}]` | — | D10 layer 1: host paths mounted into the node |
| `spec.cluster.providerConfig` | string | — | D10 layer 2 escape hatch: a file path or inline provider-native config (e.g. a full kind config). cube-idp merges in only what it *requires* and fails with a typed error on real conflicts; inspect the merged result with `cube-idp config render-cluster` |
| `spec.engine.type` | `flux` \| `argocd` | `flux` | GitOps reconciler; `argocd` ships in Phase 2 (D2) |
| `spec.gateway.pack` | `traefik` \| `envoy-gateway` (any pack name is accepted when paired with `spec.gateway.ref`) | `traefik` | Gateway API implementation; `cube-idp init --gateway-pack` writes this and `spec.gateway.ref` coherently |
| `spec.gateway.host` | string | `cube-idp.localtest.me` | routable hostname for delivered packs |
| `spec.gateway.port` | int | `8443` | host port mapped to the gateway's `websecure` (HTTPS) listener — see the note below |
| `spec.gateway.ref` | string | — | overrides the pack source `up` fetches for the gateway pack (`oci://…`, a local dir, or an absolute path); falls back to `packs/<pack>` when unset, which only resolves from a checkout — `cube-idp init --local` fills this in |
| `spec.packs` | `[{ref, values}]` | gitea + argocd (D9) | additional packs delivered after the gateway; `ref` is `oci://` or a local dir (git `github.com/...` refs ship in Phase 2); `values` are validated against the pack's `#Values` CUE schema before anything touches the cluster |

**Precedence:** when both `spec.gateway.ref` and `spec.gateway.pack` are
set, the REF decides what is fetched; `up` verifies the ref'd pack.cue name
equals `gateway.pack` and fails with CUBE-0008 on mismatch. `cube-idp init`
always writes the two coherently (`--gateway-pack`).

Run `cube-idp config render-cluster` to preview the final merged kind
provider config (D10 layer 2) before `up` creates anything. Run `cube-idp
config schema` to print the CUE schema `cube.yaml` is validated against —
every CUBE-0002 (config validation failure) remediation points here.

> **Phase 1 → Phase 2 behavior change:** Phase 1 mapped host
> `spec.gateway.port` (default `8443`) to Traefik's plain-HTTP NodePort
> `30080` while printing an `https://` URL. Phase 2 makes that URL true:
> host `gateway.port` now maps to the `websecure` NodePort `30443` (TLS
> terminated by Traefik with a cube-idp CA-issued cert from `up`), and
> plain HTTP stays available in-cluster on the `web` listener. Existing
> kind clusters need `down`/`up` to pick up the new mapping.

## k3d provider

`spec.cluster.provider: k3d` stands the platform up on k3d (k3s-in-docker,
D4) instead of kind — same single-binary flow, same everything-else. It is a
drop-in alternative to `kind`: both are cluster-creating providers that
node-load images (so both support air-gapped `up --bundle`, below), both map
the host `gateway.port` onto the gateway's pinned NodePort `30443`, and both
honor the D10 layer-1/2 cluster-shape fields (`extraPorts`, `mounts`,
`registry`, `providerConfig`). The e2e suite runs the full `{kind, k3d}`
provider matrix in CI.

```yaml
spec:
  cluster:
    provider: k3d
    kubernetesVersion: v1.33.1   # -> rancher/k3s:v1.33.1-k3s1 node image
```

`cube-idp config render-cluster` previews the final merged provider config
for k3d exactly as it does for kind — pipe it out and inspect the k3d
`SimpleConfig` before `up` creates anything. The `--local` node-image cache
recipe below applies to k3d too (mount over the k3s containerd store).

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

A gateway pack may also declare an optional `gatewayService:` block, the
in-cluster Service `up` should point the `*.<gateway.host>` CoreDNS rewrite
at:

```cue
gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}
```

Most gateway packs need nothing here: `up` falls back to the
`<pack>.<pack>.svc.cluster.local` convention (traefik's chart installs
release `traefik` into namespace `traefik`, so the pack name doubles as
both). `gatewayService:` exists for packs where the controller's own
Service is not the data-plane Service that actually terminates traffic —
envoy-gateway is the one shipped example: its Gateway API controller
spawns a separate Envoy proxy Service at Gateway-attach time, and
`gatewayService:` names that Service so CoreDNS resolves `*.<host>` to the
data plane instead of the controller. A malformed block is rejected
(CUBE-4003, the same code the `images:` list uses).

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
- **`cube-idp status --details`** — adds every inventory object
  (kind/namespace/name) to the health summary; plain `cube-idp status`
  prints only the component/inventory-count roll-up.
- **`cube-idp down --keep-cluster`** — deletes cube-idp's resources
  (inventory-driven cascade) but leaves the cluster itself running; useful
  for iterating on `cube.yaml` without paying kind/k3d cluster-creation cost
  each time. Requires the cluster to already exist (it never creates one as
  a side effect).
- **`cube.lock`** — written by `up`, one entry per pack:
  - `resolved` — the concrete ref `up` actually fetched (a resolved git SHA,
    an OCI digest, or a content dirhash for local/http/s3 sources).
  - `renderedHash` — a stable content hash of the rendered manifests, used
    by `diff` to detect pack-level drift without re-rendering everything.
  - `images` — every container image referenced by the rendered objects,
    for offline auditing/vulnerability scanning.

  Commit `cube.lock` alongside `cube.yaml` — it pins what actually shipped,
  the way a lockfile does for a package manager.

## Delivering your own work

Two ways to get *your* manifests onto a running cube, beyond the packs in
`cube.yaml`:

### `cube-idp sync <dir>` — push a directory (D7)

`sync` renders a local directory as a pack, pushes it to the cube's
in-cluster registry, delivers it through the configured engine, and pokes
the engine to reconcile now. A directory with a `pack.cue` is treated as a
full pack; a bare directory of `*.yaml`/`*.yml` manifests is synthesized
into one (named after the directory). It targets an already-`up` cube and
never creates a cluster as a side effect.

```bash
cube-idp sync ./my-manifests           # one-shot: render, push, deliver, exit
cube-idp sync ./my-manifests --watch   # re-sync on every debounced change until Ctrl-C
```

`--watch` is the sanctioned long-running **foreground** mode — a fast local
edit loop, not a daemon: it runs once immediately, then re-syncs on every
debounced filesystem change under `dir` until interrupted, and a sync
failure mid-watch is printed in full without stopping the watch (fix the
file and save again). **Boundary (D7):** `sync` pushes OCI artifacts
directly to the registry; it is *not* a git-push flow. The git-push
delivery flow lives in the gitea pack — see `repo create` below.

### `cube-idp repo create <name> [--deploy]` — git-push delivery

Creates a repository in the cube's built-in Gitea for the admin user (with
`auto_init`, public so the in-cluster engine needs no pull secret). With
`--deploy` it also registers the repo as a continuously-synced engine
delivery source — the classic "empty repo to deployed" loop:

```bash
cube-idp repo create app --deploy
# clone: https://gitea.cube-idp.localtest.me:8443/gitea_admin/app.git
# push:  git push <clone-url> main
#   ...push a manifest, and the engine (cloning the repo from *inside* the
#   cluster via the gitea Service) applies it — no laptop tunnel involved
```

A human clones/pushes over the **gateway** URL (real TLS via the cube-idp
CA); the engine reaches the gitea Service directly in-cluster. Re-running is
idempotent (`--deploy` re-registers the same source). Admin credentials come
from `cube-idp get secrets -p gitea`.

## Air-gapped install (`vendor` + `up --bundle`)

For a host with no registry access, split the install into a connected
*vendor* step and an offline *up* step (spec §4.1):

```bash
# On a connected machine (reads cube.lock; pure lock consumer, no cluster):
cube-idp vendor -o cube-bundle.tar.gz              # host platform
cube-idp vendor -o cube-bundle.tar.gz --platform linux/amd64   # cross-arch
cube-idp vendor --lock ./other/cube.lock -o cube-bundle.tar.gz # non-default lock path

# Carry the tarball to the air-gapped host, then:
cube-idp up --bundle cube-bundle.tar.gz
```

`vendor` pulls every pack source and container image pinned in `cube.lock`
(or the file `--lock` points at, when `cube.lock` isn't in the working
directory) into one self-contained tarball — a bundle is complete or an
error (any pull failure aborts rather than shipping a partial bundle).
`up --bundle` is offline-honest: after the cluster exists it node-loads
every bundled image into the nodes (so pods start with no registry pull),
rewrites every pack ref to its bundle-local source before fetching, and
fails **loudly** (CUBE-7004) on any ref missing from the bundle rather than
silently falling through to a network fetch. It requires an image-loading
provider (`kind` or `k3d`); `provider: existing` cannot node-load images
and is rejected up front (CUBE-7005).
Bundle extraction is capped (4 GiB per tar entry, 16 GiB total per bundle);
exceeding either limit fails extraction with CUBE-7003.

## Plugins

cube-idp is extensible via exec-plugins (spec §4.4 tier 2): any executable
named `cube-idp-<name>` on `$PATH` (or in the plugin install dir) is
invokable as `cube-idp <name>`, and `cube-idp plugin list` shows every one
discovered.

**Environment contract.** cube-idp runs a plugin with the parent environment
plus these variables (Owner Decisions #5). Each is set **only** when
available — an omitted field is absent from the child's environment entirely
(a stale `CUBE_IDP_*` in your shell never leaks through as if cube-idp set
it), so a cluster-independent plugin keeps working with no cube.yaml around:

| Variable | Value | Set when |
| --- | --- | --- |
| `CUBE_IDP_CUBE_NAME` | the cube's `metadata.name` | a loadable `cube.yaml` is present |
| `CUBE_IDP_KUBECONFIG` | path to a temp kubeconfig for the cube's cluster (0600, removed on exit) | the cluster exists |
| `CUBE_IDP_REGISTRY` | the in-cluster zot registry URL | the cluster exists |
| `CUBE_IDP_CA` | path to the cube-idp local CA cert (`ca.crt`) | a CA has been generated by a prior `up` |

A plugin reaches the registry either through its own port-forward or, on a
host where the gateway hostname resolves, at `https://registry.<gateway.host>`
(the same `internal/registry` gateway route the host-side `docker`/`oras`
push uses) — with `CUBE_IDP_CA` as the trust anchor. So zot is reachable
from the host, not just in-cluster.

**Trust model.** A discovered plugin runs only after its current sha256 is
approved: `cube-idp plugin trust <name>` records the hash so it runs without
prompting; an untrusted plugin prompts interactively (CUBE-7104) or, in a
non-TTY, is refused. Any change to the binary invalidates the recorded hash
and re-prompts.

**Install.** `cube-idp plugin install <name> --index <url>` fetches a plugin
from a sha256-pinned git index and records its trust in one step. `--index`
is required (Owner Decisions #8) — there is no implicit default index.

Global flags go AFTER the plugin name: `cube-idp myplugin --plain` dispatches
to the plugin, but `cube-idp --plain myplugin` does not (the plugin
fallthrough inspects only the first argument).

## Pack sources

A pack ref (`spec.gateway.ref` / `spec.packs[].ref`) accepts:

| Form | Example | Pin behavior |
| --- | --- | --- |
| local directory | `./mypack`, `packs/gitea` | content dirhash |
| OCI | `oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0` | digest |
| bare git grammar | `github.com/org/repo//path@v1.2.3` | tag/branch resolved to a commit SHA, or a full SHA passed through |
| explicit go-getter URL | `git::https://example.com/repo.git//path?ref=v1`, `s3::https://s3.amazonaws.com/bucket/pack.tar.gz`, `https://example.com/pack.tar.gz` | dirhash of the fetched tree |

Remote refs must be pinned (a tag, a full commit SHA, or an explicit
`?ref=`) — `HEAD`, a bare branch name with no `@rev`, or a wildcard is
rejected (CUBE-4007) so `cube.lock` always records something reproducible.

The catalog packs under `packs/` are published to
`ghcr.io/rafpe/cube-idp/packs/<name>` by `.github/workflows/release-packs.yaml`
on every push to `main` that touches `packs/**`: it runs `cube-idp pack push
--also-tag latest <dir> oci://ghcr.io/rafpe/cube-idp/packs/<name>` for each
pack directory, tagging the pushed artifact with both the pack's
`pack.cue` version and a moving `latest`.

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
# real cluster; needs docker. Provider x engine matrix (spec §5):
CUBE_IDP_E2E=1 CUBE_IDP_E2E_PROVIDER=kind CUBE_IDP_E2E_ENGINE=flux   go test ./tests/e2e/ -v -timeout 35m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_PROVIDER=k3d  CUBE_IDP_E2E_ENGINE=flux   go test ./tests/e2e/ -v -timeout 35m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_PROVIDER=kind CUBE_IDP_E2E_ENGINE=argocd go test ./tests/e2e/ -v -timeout 35m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_PROVIDER=k3d  CUBE_IDP_E2E_ENGINE=argocd go test ./tests/e2e/ -v -timeout 35m
```

The e2e suite is skipped unless `CUBE_IDP_E2E=1`, and runs across the
`{kind, k3d} x {flux, argocd}` matrix via `CUBE_IDP_E2E_PROVIDER` (default
`kind`) and `CUBE_IDP_E2E_ENGINE` (default `flux`); CI runs all four legs as
a matrix job (spec §5: `{kind, k3d} x {flux, argocd} x {up, diff, upgrade,
down}`). The Phase 1 loop (`tests/e2e/e2e_test.go`) builds the binary,
`init --local`s against this checkout, runs `doctor` then `up` twice
(proving idempotency), asserts `cube.lock` was written with a `renderedHash`,
that a converged cube's `diff`/`upgrade --plan` both exit 0, that `status`
and `kubectl get packs` (D11) surface the expected components/printer
columns, that the gateway serves a cube-idp CA-issued TLS cert, that `cnoe
import` round-trips a fixture Application, and that `get secrets -p gitea`
surfaces `gitea_admin` — then `down`s the cluster. The Phase 3 scenarios
(`tests/e2e/phase3_test.go`) add: k3d up/down, `vendor` → offline `up
--bundle` (asserting the image node-load ran and that every per-pack
`fetching <source>` output line resolves into the bundle staging dir, never
an `oci://` ref), `sync` one-shot, `repo create --deploy` (git push over the
gateway → engine syncs → ConfigMap appears), and an envoy-gateway smoke test.
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
