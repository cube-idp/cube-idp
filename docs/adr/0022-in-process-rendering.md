---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 22. All Rendering Happens In-Process with Contained Dependencies

## Context and Problem Statement

cube-idp turns a declarative cluster description into Kubernetes manifests: Helm charts,
kustomize overlays and CUE-validated configuration all have to become plain objects
before anything is delivered to a cluster. There are two ways to do that. The CLI can
shell out to `helm` and `kubectl` binaries, which makes rendering depend on whatever
versions happen to be on the operator's PATH and on ambient `HELM_*` environment state.
Or it can render entirely in-process against pinned library versions.

Rendering in-process raises its own containment problems. A Helm SDK imported freely
across the codebase spreads a large, fast-moving API surface into every package that
happens to need a manifest, and it invites accidental use of Helm's *install* path,
which contacts a cluster and creates release secrets. Helm also reads repository cache
and config locations from process environment by default, so an in-process render can
silently mutate — or be corrupted by — the operator's own `~/.helm` state.

The OCI side had a parallel problem: `github.com/fluxcd/pkg/oci` pulls in a large
go-containerregistry and docker-cli dependency subtree for what amounts to a push of a
tarball layer.

Finally, once rendering is a library call, it needs a clear boundary against
orchestration: a renderer produces manifests from one pack, but pack-to-pack ordering is
composition intent that only the orchestrator knows.

## Decision

All rendering happens in-process, against the **Helm v4 SDK behind a single-file import
boundary (`internal/pack/helm.go`)**, **`sigs.k8s.io/kustomize/api`**, and **`cuelang.org/go`** for schema
validation. No `helm` or `kubectl` binary is shelled out at runtime, and there is no
helm-binary escape hatch in the core.

Helm charts are rendered **client-side** into plain manifests with `DryRunClient` and a
zero `action.Configuration`, so rendering never contacts the cluster and never installs
a release. helm-controller is never installed; cube-idp manages Namespace objects
itself, so `CreateNamespace` stays false. Render failures surface as **CUBE-4005**.

`internal/pack/helm.go` is the **only** file permitted to import the Helm SDK. Every
other consumer, including the cnoe loader, goes through the exported `pack.RenderChart`
wrapper. Helm's chart repository cache and config are pinned under the cube-idp cache
root by setting `cli.EnvSettings` **struct fields** in the `helmSettings()` helper used by
the chart-render path, for all chart packs — never via process environment variables —
falling back to Helm's defaults when the cache directory cannot be resolved.

`Rendered.DependsOn` is set by the orchestrator after graph resolution and never by
`RenderWith`, because dependencies are cube-composition intent rather than render output.

OCI operations use **oras-go v2**. `github.com/fluxcd/pkg/oci` is dropped from `go.mod`;
`github.com/google/go-containerregistry` remains only as a test-only dependency (the
in-process registry used by `internal/oci/ocitest`), while `fluxcd/pkg/ssa` stays.

## Consequences

* Good, because rendering is reproducible: the Helm, kustomize and CUE versions are
 pinned in `go.mod` rather than inherited from the operator's PATH.
* Good, because a client-only render can never accidentally touch a cluster, create a
 release secret, or require cluster credentials to preview a change.
* Good, because pinning cache paths on `EnvSettings` fields keeps renders hermetic and
 leaves the operator's own Helm state untouched — and does so without mutating process
 environment, which would be a global side effect in a concurrent renderer.
* Good, because confining the SDK import to one file keeps a large API surface behind a
 single wrapper, so a Helm major-version bump is a one-file change.
* Good, because dropping `fluxcd/pkg/oci` removes its docker-cli dependency subtree from
 the production build; go-containerregistry survives only in the test binary.
* Bad, because cube-idp is coupled to whatever the Helm SDK supports; charts relying on
 behaviour only the CLI provides cannot be worked around with an escape hatch.
* Bad, because there is no cluster-aware render, so charts whose templates branch on
 `.Capabilities` from a live API server see only client-side defaults.
* Bad, because the single-importer rule is a convention enforced by review and grep, not
 by the compiler.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Rendering happens in-process against pinned libraries — Helm v4 SDK, `sigs.k8s.io/kustomize/api` and `cuelang.org/go`. (No-shellout and no-escape-hatch are asserted by grep in Verification below.) | `go.mod`; render paths `internal/pack/helm.go`, `internal/pack/kustomize.go`, `internal/config/load.go` |
| Helm charts render client-side into plain manifests with `DryRunClient`, never contacting the cluster or installing a release; helm-controller is never installed, `CreateNamespace` stays false, and render failures are CUBE-4005. | `internal/pack/helm.go` |
| `internal/pack/helm.go` is the only file permitted to import the Helm SDK; all other consumers, including the cnoe loader, go through the exported `pack.RenderChart` wrapper. | `internal/pack/helm.go` |
| Helm's chart repository cache and config are pinned under the cube-idp cache root by setting `cli.EnvSettings` fields (never process env) in the `helmSettings()` helper used by the chart-render path, for all chart packs, falling back to Helm defaults if the cache dir cannot be resolved. | `internal/pack/helm.go` |
| `Rendered.DependsOn` is set by the orchestrator after graph resolution and never by `RenderWith`, because dependencies are cube-composition intent rather than render output. | `internal/up/up.go` |
| All OCI operations use oras-go v2; `github.com/fluxcd/pkg/oci` is dropped from `go.mod`, while `fluxcd/pkg/ssa` stays and `github.com/google/go-containerregistry` remains only as a test-only dependency. | `internal/oci/push.go`; `go.mod`; `internal/oci/ocitest/ocitest.go` |
| `installNeedsSSA` gates direct SSA of the rendered engine install: with `selfManage` off every `up` SSAs; with it on, SSA happens only on first install or when the engine is unhealthy at start. | `internal/up/up.go` |
| `packs[].delivery` accepts only `oci` or `repo` (CUE enum); `pack install --via repo` sets the key while `--via oci` leaves it empty. | enum `internal/config/schema.cue`; `--via` write `cmd/pack.go` |
| Gitea repo sync is idempotent by git blob SHA: an unchanged render produces zero commits, and each sync is one commit. | `internal/gitea/client.go` |

### Verification

- [ ] `grep -rln 'helm.sh/helm' internal cmd` returns exactly one file: `internal/pack/helm.go`
- [ ] `go.mod` pins `helm.sh/helm/v4`, `sigs.k8s.io/kustomize/api` and `cuelang.org/go`
- [ ] `grep -n 'fluxcd/pkg/oci' go.mod` returns nothing, while `fluxcd/pkg/ssa` and `oras.land/oras-go/v2` are present; `github.com/google/go-containerregistry` (go.mod:11) is imported only from `_test.go` files and `internal/oci/ocitest`
- [ ] `grep -rn 'exec.Command' internal cmd --include='*.go'` (excluding `_test.go`) yields only `internal/plugin/index.go` (git) and `internal/plugin/exec.go` (plugin binaries) — no `helm` or `kubectl` invocation, and no helm-binary escape hatch
- [ ] `internal/pack/helm.go` exports `RenderChart`, and `internal/cnoe/loader.go` calls it rather than importing the SDK
- [ ] `internal/pack/helm.go` uses a zero `action.Configuration`, sets `DryRunStrategy = action.DryRunClient` and leaves `CreateNamespace` false
- [ ] `internal/diag/codes.go` defines `CodePackChartErr = "CUBE-4005"` and chart render failures wrap it
- [ ] `internal/pack/helm.go` sets `settings.RepositoryCache`/`RepositoryConfig` under the cube-idp cache dir, with no `os.Setenv` of `HELM_*` in the package (pinned by `internal/pack/helm_test.go`)
- [ ] `internal/up/up.go` and `internal/diff/diff.go` are the only assignments of `rendered.DependsOn`
- [ ] `internal/gitea/client.go` skips blob-SHA-identical files and batches all operations into a single change-files request

## History

Engine tuning numbers were originally kept JSON-native end to end and never
int-normalized, on the grounds that unstructured server-side apply forbids plain `int`.
That rule no longer applies: `engine.tuning` was retired in favour of
`spec.engine.values`, whose numeric entries **are** normalized to `int`/`float64` by
`config.Load` (`internal/config/load.go`, documented at
`internal/config/types.go`). The original claim never landed in the code.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the
code before this record was written.

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:165` — in-process rendering, no shelled-out binaries
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase1-mvp.md:2313` — client-side Helm render, CUBE-4005
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:4982` — single Helm SDK importer
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:330` — `EnvSettings` cache pinning
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-pack-depends-and-cubelock-crd.md:536` — `Rendered.DependsOn` ownership
