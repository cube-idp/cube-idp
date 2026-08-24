# Changelog

## Unreleased

M9 helm packs (epic #139; design gate: `docs/DECISIONS.md` 2026-08-23):

- **`type: helm` renders to a Flux `HelmRelease`, not to expanded
  manifests.** A helm pack is **thin**: chart coordinates and a closed
  `#Values` in `pack.cue`, and **no chart content** — bundled chart files
  are a payload mismatch (`CUBE-PKG-004`), rejected rather than ignored.
  Rendering emits the chart's source CR (`HelmRepository` for a repository
  index, `OCIRepository` for `oci://`) followed by a `HelmRelease` carrying
  the validated values nested verbatim in `spec.values`; the engine's
  helm-controller pulls and templates the chart in cluster. **cube-idp
  never runs Helm**, so rendering stays hermetic and `helm.sh/helm/v4` was
  dropped from the deferred dependency set rather than adopted.
- The `chart` block is a `repo|oci` discriminated shape — `kind`, `url`,
  `name` (repo only), an **exact** `version`, and an optional `digest`
  (oci only). `#Pack` became a **type-discriminated disjunction**, so
  "chart required for helm, forbidden otherwise" is enforced by the schema
  rather than by a hand-written check. Versions are validated by a real
  SemVer parser (`golang.org/x/mod/semver`, a promotion to a direct import
  — no new module, `go.sum` unchanged): ranges, partial versions, a leading
  `v`, and build metadata are all rejected.
- **Bootstrap installs helm-controller.** The embedded, pinned Flux asset
  is regenerated at v2.9.2 with
  `--components=source-controller,kustomize-controller,helm-controller`,
  adding the helm-controller Deployment and the `helmreleases` CRD, and
  re-pinned by sha256. The readiness wait is unchanged — the bootstrap
  kind-set filters by kind, so both join it on their own.
- New scaffold forms: `cube-idp pack new <dir> --from-chart <chartdir>`
  reads a **local** chart's `Chart.yaml` and `values.yaml` (a metadata
  read — nothing fetched, nothing copied) and writes a thin helm pack with
  a reserved-host placeholder url to replace and a lossy, clearly-labelled
  `#Values` derived from the chart's defaults. `--type helm` scaffolds the
  same skeleton with nothing to read it from. Both render immediately.
- **Trade-off, recorded rather than hidden:** `pack render` on a helm pack
  shows the `HelmRelease` — the delegation — not the workload the chart
  expands to, and `pack validate` cannot tell you the chart exists.
- **Scope: public chart sources and non-sensitive values only.** There is
  no private-source authentication and no secret-backed `valuesFrom`;
  values are plaintext in `cube.yaml`, in rendered output, in the delivery
  artifact, and in the CR. A `kind: repo` chart is an honestly-labelled
  **mutable reference** — a repository owner can republish different bytes
  at the same version, so only an OCI `digest` pins content. Trust,
  credentials, and a `lock`/`mirror` operation are deferred to **#142**.
- No new error codes: helm failures land on `CUBE-PKG-003`, `004`, or
  `010`. `CUBE-PKG-020` ("render not implemented") is **retired** — every
  declarable type now renders — with its number never reused.

M8 pack (epic #113; design gate: `docs/DECISIONS.md` 2026-08-21):

- New domain `internal/pack` (`CUBE-PKG-*`, tag `PKG`): a **pack** is a
  self-contained, versioned directory of platform content — a `pack.cue`
  declaring `name`, `version`, and an explicit `type` (`raw|helm|
  kustomize`, **never sniffed** from the payload), plus an optional
  **closed `#Values` definition** that locks down, exposes, and defaults
  the pack's values surface. cube-idp **renders packs; it never applies
  them** — delivery is the engine's, so this domain touches no cluster and
  rendering is a pure function of its inputs.
- New verbs:
  - `cube-idp pack render <ref>` — renders the pack as authored. Only YAML
    reaches stdout (diagnostics go to stderr, no banner, stable order, no
    partial output on failure), so it pipes straight into
    `kubectl apply -f -`.
  - `cube-idp pack render -f <config> --id <id>` — renders one configured
    instance: values merged, external manifests attached, namespace
    applied.
  - `cube-idp pack validate <ref>` — resolves, loads, and **renders and
    discards**, so anything `render` would refuse is reported here.
  - `cube-idp pack new <dir> [--type raw|kustomize] [--name <n>]
    [--from <ref>]` — a real scaffold that renders as written, never
    overwriting an existing directory; `--from` forks an existing pack.
- `spec.packs` config sub-struct: one entry per pack instance with
  `packRef`, `valuesRef`, inline `values` (an RFC 7386 merge patch over the
  referenced document), `externalManifests` (grouped `pre`/`with`), and
  `dependsOn`. Instance identity is a human-readable `id` that defaults to
  the pack's name when unambiguous; `dependsOn` resolves to a deterministic
  install order for a later milestone to execute.
- Namespace injection is one post-render transform for every pack type. A
  pack that forces a namespace forces it over everything it delivers, and
  an object declaring a different one is an error rather than a silent
  override. Scope is decided offline: built-in kinds first, then the
  `spec.scope` of any **CRD the pack itself bundles**, then the default.
- kustomize packs render through the kustomize library and stay hermetic:
  a payload referencing a remote resource is **rejected rather than
  fetched** (`CUBE-PKG-021`), because kustomize resolves remote references
  unconditionally and rendering must remain a function of its inputs.
  `${VAR}` substitution fills values into the built output, and a missing
  variable is an error, never an empty string.
- New shared-infrastructure leaf `internal/ref` (`CUBE-REF-*`, tag `REF`):
  one reference grammar, resolving to a tree or a single file, with
  explicit schemes only (no bare-git guessing). Local paths and `https`
  work today; `git+https`, `oci`, and `s3` are recognized and return their
  own not-implemented codes naming the backend and where it lands.
- Two runtime dependencies join the closed set, each at its own gate and
  each confined: `cuelang.org/go` (pack metadata and values) and
  `sigs.k8s.io/kustomize/api` + `kyaml` (imported by one file).

M7 bootstrap (epic #92; design gate: `docs/DECISIONS.md` 2026-08-06):

- New leaf-ish domain `internal/bootstrap` (`CUBE-BST-*`, tag `BST`): the
  micro-bootstrap applier. It installs the embedded, pinned **Flux**
  distribution (`go:embed` of `flux install --export` v2.9.2, provenance
  verified by sha256), hand-rolling server-side apply on `k8s.io/client-go`
  (field manager `cube-idp`) and a readiness wait over the bootstrap
  kind-set (CRD Established, Deployment/StatefulSet ready, Job complete,
  Namespace Active) read off `unstructured` status — no `fluxcd/pkg/ssa`,
  no kstatus/controller-runtime, **no new runtime dependency**.
- `spec.engine` config sub-struct: `provider` (defaults to `flux`) and a
  **git|oci** discriminated `EngineSource` (`kind`/`url`/`ref`/`path`/
  `interval`), defaulted and validated in `api/` (unknown kind, URL/scheme
  mismatch, and bad interval are `CUBE-CFG-*` document errors).
- New `cube-idp bootstrap` verb: installs Flux, applies the `GitRepository`
  or `OCIRepository` + `Kustomization` CRs from `spec.engine.source`, and
  records a ConfigMap inventory (the seed of a future `down`) — composed at
  the CLI edge, injecting the kube client's `dynamic.Interface` +
  `RESTMapper`. `internal/bootstrap` never imports `internal/kube`. The
  reserved `apply` domain/verb is retired.
- Direction recorded: the gitops engine is mandatory (Flux default,
  installed before all packs) — superseding ADR-0045's ordering — and the
  ROADMAP is re-sequenced M7 bootstrap → M8 pack (delivery-through-engine)
  → M9 engine seam → M10 bus → M11 `up`/`down`.
- `make test-e2e` gains a real Flux round-trip: install + source CRs
  against a kind cluster and a public git source, `GitRepository`
  reconciled `Ready`; the green gate stays hermetic.

M6 kube client access (epic #81; design gate: `docs/DECISIONS.md`
2026-08-04):

- New leaf domain `internal/kube`: Kubernetes clients constructed from
  injected kubeconfig bytes + a context name — REST config, discovery,
  memory-cached RESTMapper, dynamic client, and a bounded `Ping`
  reachability probe — with the `CUBE-KUB-*` error catalog. No driver
  seam: there is exactly one Kubernetes API.
- `cube-idp status` reports API-server reachability as a third line
  (`api server: reachable|unreachable|not checked`), composed at the CLI
  edge via the kube injection contract. An unreachable server is a
  finding, not a failure — exit stays 0.
- `k8s.io/client-go` v0.36.2 (pinned to the apimachinery minor) joins
  the closed dependency set, construction-confined to `internal/kube`;
  consumers may reference its stable interface types.
- `make test-e2e` gains a kube client round-trip against a real kind
  cluster in the new `tests/e2e` package; the green gate stays hermetic.

M5 cluster lifecycle completion (epic #72; operator decisions:
`docs/DECISIONS.md` 2026-08-02 and 2026-08-03):

- `cube-idp delete` removes the cluster through the driver seam (an
  absent cluster is a no-op) and cleans the cube-owned context out of
  the kubeconfig losslessly: map-based removal mirroring the merge,
  atomic write, `current-context` unset only when it pointed at the
  removed context, the file never unlinked, and the write skipped when
  nothing matched. Missing config is the loader's error — `delete`
  never scaffolds.
- `cube-idp status` reports whether the declared cluster exists and
  whether its cube-owned kubeconfig context is installed. Read-only;
  exit 0 whenever the report succeeds — an absent cluster is a finding,
  not a failure.
- `init` split (scope change #78, from operator testing feedback):
  `init` is config-only — scaffold-if-absent, load+validate, report,
  exit 0 and idempotent — and drops its `--kubeconfig*` flags. The new
  `cube-idp create` owns load → provision → kubeconfig context install
  and never scaffolds.
- `CUBE-CLU-005`'s summary generalized to "kubeconfig update failed",
  covering cleanup as well as install.

M4 init bootstrap (design gate: `docs/DECISIONS.md` 2026-08-01):

- `cube-idp init` scaffolds a missing config file before provisioning:
  `metadata.name` from the new `--name` flag, else a generated
  docker-style `<adjective>-<noun>` name. The rendered document is
  validated through the standard load pipeline before an `O_EXCL` create
  (an existing file is never clobbered — `CUBE-CFG-006`; scaffold I/O
  failures are `CUBE-CFG-007`), and a stdout notice names the created
  file and cube.
- `--name` never mutates an existing document: a mismatch with
  `metadata.name` fails with `CUBE-CFG-005` ("edit metadata.name"
  remediation); a matching `--name` proceeds, keeping re-runs idempotent.
- `config validate` now also surfaces provider-side
  `spec.cluster.forProvider` validation via the cluster seam's optional
  `SpecValidator` capability, composed at the CLI edge. Exit contract:
  0 valid, 2 document errors (`CUBE-CFG-*`), 1 provider-payload errors
  (`CUBE-CLU-003`). kind's container-runtime detection is deferred to
  the first provisioning call, so validation works without Docker.

M3 cluster domain (design: `docs/archived/design/2026-07-29-cluster-domain.md`):

- `spec.cluster` API sub-struct: typed `provider` (defaults to `kind`) plus
  opaque `forProvider` passthrough, validated at load time.
- `internal/cluster` domain: `Provisioner` driver seam with an exported
  conformance suite, kubeconfig machinery (`ContextName`/`Rebrand`/`Merge` —
  no client-go), the `Init` operation, and the `CUBE-CLU-*` code catalog.
- kind provider (`sigs.k8s.io/kind v0.32.0`, confined to
  `internal/cluster/kind`); real-cluster conformance is opt-in via
  `make test-e2e`, never part of the green gate.
- CLI: `cube-idp init` creates the cluster and installs a cube-owned
  kubeconfig context (`cube-idp.dev/<name>`), with `--kubeconfig` and
  `--kubeconfig-context-name` overrides.

v0 greenfield baseline (2026-07-27 reset; design:
`docs/archived/design/2026-07-27-back-to-basics-structure.md`):

- `Config` API `cube-idp.dev/v1alpha1`: CRD-ready types (real apimachinery
  `TypeMeta`/`ObjectMeta`), controller-gen deepcopy, name validation.
- Strict loading pipeline: decode (unknown fields rejected) → default →
  validate, with aggregated path-qualified errors.
- Coded error model `CUBE-<TAG>-NNN`: machinery in `internal/cubeerr`,
  code catalogs owned per domain.
- CLI: `cube-idp config validate` and `cube-idp config show`
  (exit 0 valid, 2 config error).
- Gates: `make build/test/lint/generate`; funlen 50 and 300-line file
  limit enforced in CI.

History from before the reset (the previous full implementation) is
preserved in git history on `main`.
