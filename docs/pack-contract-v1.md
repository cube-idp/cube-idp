# cube-idp pack contract — v1

Status: **FROZEN** (Phase 5 P1, 2026-07-18). This document is the public
API of the cube-idp pack format. The copy at `docs/pack-contract-v1.md` in
the cube-idp main repo is normative; `CONTRACT.md` in the
`github.com/cube-idp/packs` monorepo is a verbatim copy of it (GT12).
`internal/pack/contract_conformance_test.go`
(`TestReposPacksSatisfyContractV1`) enforces the mechanical clauses against
every pack in the tree; the packs-repo conformance harness (W0.T3) runs the
same gate per pack.

Within v1 this contract only ever changes additively (§6).

## 1. Layout

A pack is a **data-only directory**. No code ships in a pack; cube-idp
fetches it, validates it, and renders it client-side — engines receive
rendered manifests only (helm-controller is never installed in-cluster).

```text
<name>/
  pack.cue             REQUIRED  metadata + optional values schema (§2)
  manifests/*.yaml     optional  raw multi-doc YAML manifests
  kustomization.yaml   optional  kustomize overlay rooted at the pack dir
  chart.yaml           optional  helm chart reference, rendered client-side
```

**Raw-manifest source — exactly one of two paths:**

- If `kustomization.yaml` exists at the pack **root**, it is the **sole**
  source of raw manifests. `manifests/` is consumed through it (listed as
  `resources:`), never walked independently — objects are not
  double-rendered. The kustomize build output is `${GATEWAY_*}`-substituted
  (§3) as raw bytes before parsing. A failed build is typed CUBE-4008.
- Otherwise `manifests/` is walked directly: every `*.yaml` / `*.yml` file
  at its top level, in **sorted filename order** (subdirectories and other
  extensions are skipped). Number your files (`00-namespace.yaml`,
  `10-secret.yaml`, …) to control apply order. Each file's raw bytes are
  `${GATEWAY_*}`-substituted (§3) before YAML parsing; parse errors are
  typed CUBE-4004.

**Helm is orthogonal and appended.** If `chart.yaml` exists, its chart is
rendered client-side (a dry-run install against no cluster) and the
resulting objects are **appended after** the raw manifests, whichever
raw-manifest path was taken. `chart.yaml` shape:

```yaml
chart: traefik                            # chart name, or oci://registry/chart
repo: https://traefik.github.io/charts    # omitted for oci:// charts
version: "34.1.0"
releaseName: traefik
namespace: traefik
values:                                   # pack defaults, merged UNDER user values (§4)
  ...
```

Helm render semantics (all verified behavior):

- The render targets the same default Kubernetes version cube-idp
  provisions clusters with, so charts see a realistic
  `Capabilities.KubeVersion`.
- Chart **hooks become plain resources**: install-relevant hook manifests
  (pre-install before the manifest objects, post-install after, mirroring
  helm's own order) are emitted into the stream with their `helm.sh/hook*`
  annotations stripped; hooks that would not fire on a fresh install
  (test, delete/rollback-only, upgrade-only) are skipped. The rendered
  artifact is static GitOps content — there is no out-of-band hook runner.
- If `chart.yaml` names a `namespace` and no rendered object already
  creates it, a `v1/Namespace` object is prepended.
- Chart load/render failures are typed CUBE-4005.

**A pack must render at least one object.** A pack whose render produces
zero objects is rejected (CUBE-4004): a pack needs `manifests/` (directly
or via `kustomization.yaml`) and/or `chart.yaml`.

Only directories and regular files are part of a pack. Symlinks and
irregular files are not packaged (§5) and must not be relied on.

## 2. pack.cue fields

`pack.cue` is a single CUE file at the pack root. A missing or
non-compiling `pack.cue`, or one whose declared blocks are malformed, is
typed CUBE-4003 (CUBE-4011 for `expose:`). Fields:

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | yes | Pack name. MUST match `^[a-z0-9][a-z0-9-]{0,30}$` and MUST equal both the pack's directory name and its artifact name (`oci://ghcr.io/cube-idp/packs/<name>`). |
| `version` | yes | Pack version. MUST be semver (`X.Y.Z`, optional pre-release/build suffix) and MUST equal the publish tag (§5). |
| `description` | v1 packs: yes (loader: optional) | One-line human description, NEW in v1. Surfaced by the catalog index artifact and `cube-idp pack list --available`. The loader accepts its absence (packs predating v1 still load); the conformance gate requires it for every pack published from the packs monorepo. |
| `#Values` | no | CUE schema for user `values:`. When declared, user values are unified with it — defaults (`*`) are filled in, violations are typed CUBE-4002. Without it, values pass through unvalidated. |
| `expose` | no | D11 discoverability block: `urls?: [...string]` (may carry `${GATEWAY_*}` tokens, §3), `authSecretRef?: {namespace, name}` (both required when the ref is present), `impliedFields?: {<k>: <v>}` (e.g. an implicit `username: "admin"`). Malformed → CUBE-4011. |
| `images` | no | `[...string]` — runtime images the pack pulls that never appear in its rendered manifests (e.g. a controller's dynamically-provisioned proxy image). Consumed by lockfile assembly and `cube-idp vendor` for air-gapped bundles. |
| `gatewayService` | no | `{name, namespace}` (both required if the block is present) — the pack's data-plane Service, used by `up` to point DNS at the data plane instead of the controller Service. Gateway packs only. |

Unknown top-level fields are permitted (CUE compiles them; the loader
ignores them) — but new *meaningful* fields are added to this contract
only by a contract revision (§6). Do not squat field names.

## 3. Substitution

Three tokens are substituted wherever noted below, from the cube's
`spec.gateway`:

| Token | Expands to |
| --- | --- |
| `${GATEWAY_HOST}` | `host[:port]` — the port is omitted when the gateway listens on 443, so rendered links are clickable. |
| `${GATEWAY_FQDN}` | The bare `host` (no port) — for Gateway API `hostnames:` fields, which cannot carry ports. |
| `${GATEWAY_PACK}` | The gateway pack's name — which is also its namespace by convention. Use it in `HTTPRoute.spec.parentRefs` so routes attach to whichever gateway pack the cube runs. |

Where substitution applies:

- `manifests/*.yaml` — on each file's **raw bytes, before parsing** (plain
  text replacement).
- Kustomize output — on the built YAML bytes, before parsing.
- Helm values — on every **string leaf** of the merged values map
  (recursing through nested maps and lists), applied **AFTER** the
  defaults-merge (§4, D15) so tokens resolve whichever side they came
  from. Non-string leaves pass through unchanged.
- `expose.urls` — when URLs are displayed or recorded.

Rendering without a gateway (empty host) performs **no** substitution: the
literal tokens pass through untouched.

## 4. Values — the stone (GT15)

> **`values:` are helm values, only, always — consumed exclusively by the
> pack's `chart.yaml` render.**

Setting `values:` on a pack that has no `chart.yaml` is a **typed error,
CUBE-4016**, raised at render time (a pack's layout is unknowable until
the ref is fetched). Values never parametrize `manifests/` or kustomize
output.

**Merge order for helm packs** (each later layer wins; maps deep-merge,
scalars and lists replace):

1. the chart's own built-in defaults (`values.yaml` inside the chart);
2. `chart.yaml`'s `values:` block (the pack author's defaults);
3. the user's `packs[].values` from cube.yaml — first unified with
   `#Values` when the pack declares one, which fills CUE defaults and
   rejects violations (CUBE-4002);
4. `${GATEWAY_*}` substitution over the merged result's string leaves
   (§3).

Numeric values are normalized to `int` / `float64` (never `int64`, CUE's
raw decode type) by config loading, so round-tripped cube.yaml files and
in-process values compare equal.

### extraManifests

The uniform extras mechanism for **every** pack kind is
`packs[].extraManifests`: a multi-doc YAML **string** in cube.yaml. It is
parsed, `${GATEWAY_*}`-substituted (§3), **appended** to the pack's
rendered objects, and inventoried like them. Invalid YAML is a typed
error, **CUBE-4017**.

### CUSTOMIZED

A pack installed with non-empty `values` or `extraManifests` is
**CUSTOMIZED**: recorded on its in-cluster Pack record and shown as a
printer column in `kubectl get packs`. A vanilla install shows no marker.

Vocabulary triad, fixed: **values → helm render · tuning → engine patches
(`spec.engine.tuning`, not packs) · extraManifests → appended objects.**
Manifests-only packs parametrize via `${GATEWAY_*}` tokens,
`extraManifests`, or by growing a chart — never via `values:`.

## 5. Artifact

A published pack is an OCI artifact of the pack **source directory** (the
input side — not rendered manifests):

- **Manifest:** packed with ORAS `PackManifest` v1.0; artifact config
  media type `application/vnd.cncf.flux.config.v1+json`.
- **One layer**, media type
  `application/vnd.cncf.flux.content.v1.tar+gzip`: a gzip-compressed tar
  of the pack directory tree. Entry names are slash-separated paths
  relative to the pack root in deterministic lexical walk order;
  directories are mode `0755`, regular files `0644`; symlinks and
  irregular files are skipped.
- **Manifest annotations:**
  `org.opencontainers.image.created: "1970-01-01T00:00:00Z"` (a fixed
  epoch, NOT wall time — identical content republishes to an **identical
  digest**, so CI republish of an unchanged pack is a true no-op),
  `org.opencontainers.image.source: "cube-idp"`, and
  `org.opencontainers.image.revision: <primary tag>`.
- **Tag = pack version:** the artifact tag MUST equal `pack.cue`'s
  `version` (the CLI defaults the tag from it). Additional tags (e.g.
  `latest`) may point at the same manifest.
- **Digest immutability:** a published `<name>:<version>` digest is
  immutable; consumers pin by digest (index digests, `packs.lock`).
  Republishing different content under an existing version tag is a
  contract violation.
- **Naming (GT9):** artifacts live at
  `oci://ghcr.io/cube-idp/packs/<name>:X.Y.Z`; the packs monorepo tags
  releases `<name>/vX.Y.Z`; the catalog index artifact is
  `oci://ghcr.io/cube-idp/packs/index`.
- **Provenance (GT10):** CI attests every published pack digest with
  GitHub-native artifact attestations (keyless OIDC). Verification is
  `gh attestation verify oci://ghcr.io/cube-idp/packs/<name>:<ver> --owner cube-idp`.
  In-binary signature verification is a deliberate non-goal; the binary's
  pull integrity rests on digest pinning over TLS.

## 6. Compatibility

- Within v1, changes are **additive only**: new optional `pack.cue`
  fields, new optional layout entries, new annotations. Existing fields,
  media types, substitution tokens, merge order, and render precedence
  never change meaning.
- Any breaking change bumps the contract version (v2, a new
  `docs/pack-contract-v2.md`) **and** the consuming cube-idp minor
  version.
- Loader compatibility promise: every pack valid under this document
  loads and renders identically under every cube-idp release that
  declares contract v1.

## Verifying pack provenance

Every artifact under `ghcr.io/cube-idp/packs/` is published by the
`cube-idp/packs` GitHub workflow and carries a GitHub-native provenance
attestation. To verify one (requires `gh` ≥ 2.49, logged in):

    gh attestation verify oci://ghcr.io/cube-idp/packs/gitea:0.2.0 --owner cube-idp

Expected: `✓ Verification succeeded!` naming the cube-idp/packs workflow
as the builder. cube-idp itself pins digests (catalog index, e2e
packs.lock) and does not re-verify attestations at pull time.
