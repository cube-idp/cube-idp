---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 33. cube.lock as a Local KRM Lockfile and Non-Mutating Preview Commands

## Context and Problem Statement

`cube-idp up` fetches, renders and delivers a cube's packs and records what it did
in `cube.lock`. Two questions had to be settled together, because they share the
same substrate.

First, where does the reproducibility record live? A lockfile could plausibly be a
cluster resource — a `CubeLock` custom resource reconciled alongside the `Pack`
records the tool already writes — or a plain file next to `cube.yaml`. A cluster
resource makes the record unreadable without a live API server, unreviewable in a
pull request, and impossible to vendor into an air-gapped bundle. It also couples
the lock's schema to CRD upgrade mechanics.

Second, what may a preview command do? Operators reach for `diff`, `upgrade --plan`
and the config-editing commands (`pack install`, `spoke add|list|remove`) precisely
when they do *not* yet want to touch the cluster. If those commands create a kind
cluster, push to a Git repo, or apply objects as a side effect of "just looking",
the preview is a lie and the operator has lost the ability to inspect before acting.
A preview is also worthless if it disagrees with the thing it previews: if `diff`
computes delivery objects by a different path than `up`, it fabricates drift on
every cube that uses pack dependencies.

Both questions bottom out in ordering. The lock, the n-of-m progress enumeration
and the golden-output fences all need a delivery order that is stable across runs,
and cubes that predate pack dependencies must keep the order they have always had.

## Decision

`cube.lock` is a KRM-shaped `CubeLock` document at apiVersion `cube-idp.dev/v1alpha1`,
written as a local file — not an in-cluster CRD. It records entries in declared
`cube.yaml` order; delivery order is derivable and is therefore not stored. Its
`EngineLock` carries the standard `lock.Entry` fields alongside `Type`, so the engine
install is as reproducible as any pack.

Because delivery order is derivable rather than recorded, the lock stays stable
under dependency changes. See ADR-0005 for the authoritative statement of the
delivery-ordering algorithm and its tie-break rule.

Repo-delivered pack manifests are written as `manifests/NN-<kind>-<name>.yaml` in
stable order, and the sync only creates, updates or deletes under `manifests/`.
Gitea repo sync is idempotent by git blob SHA: an unchanged render produces zero
commits, and each sync is at most one commit.

`diff` mutates nothing: it computes kernel-object changes via SSA dry-run, pack
drift via the lock's rendered hashes, and orphans via the inventory, resolving the
delivery graph through the same `pack.ResolveOrder` `up` uses. `upgrade --plan`
classifies each pack as new, up to date, or update available, and exits 1 when
anything would change — except on an interactive TTY, where it first offers to
run `up` immediately and exits 0 after applying if the operator accepts.
`pack install` and `spoke add` only edit `cube.yaml` and validate by round-trip,
deferring all delivery and graph validation to the next `up` or `diff`. The other
config commands are not pure previews: `spoke list` additionally reads live cluster
state (degrading to declared config on any failure), and `spoke remove
--delete-cluster` deletes a kind spoke cluster behind a consent prompt.

## Consequences

* Good, because the lockfile is reviewable in a pull request, diffable, and
 vendorable into an air-gapped bundle without a cluster.
* Good, because `diff` and `up` both call `pack.ResolveOrder`, so a preview cannot
 fabricate drift on dependency-bearing cubes; the object-identity half of that
 property is fenced by a test asserting diff's desired set matches `up.Run`'s
 applied set, while order parity rests on the shared call rather than on a test.
* Good, because declared-order lock entries keep the file stable under dependency
 changes: adding a `dependsOn` reorders delivery but leaves `cube.lock` untouched.
* Good, because idempotent Gitea sync means a re-run of `up` leaves no commit churn,
 and the repo outside `manifests/` stays an editable working copy.
* Bad, because the lock is not authoritative in-cluster: two operators can drift
 their local `cube.lock` files against the same cluster with nothing to arbitrate.
* Bad, because `pack install` accepts a ref whose dependencies cannot be satisfied;
 the failure surfaces later, at the next `up` or `diff`, not at the point of edit.
* Bad, because delivery order must be recomputed on every read of the lock rather
 than looked up, and any change to the ordering rule silently changes past runs'
 reproduction.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `cube.lock` is defined by the KRM-shaped `File` struct with `APIVersion`/`Kind` and read/written as a local file, not an in-cluster CRD; the `cube-idp.dev/v1alpha1` / `CubeLock` values are stamped by `up`. | `internal/lock/lock.go`; `internal/up/up.go` |
| `EngineLock` records the standard `lock.Entry` fields (Ref, Name, Version, resolved pin, RenderedHash, Images) in addition to Type, making the engine install reproducible. | `internal/lock/lock.go` |
| `cube.lock` entries are accumulated in declared `cube.yaml` order and written as-is; the `Entry` struct carries no order field, so delivery order is derivable rather than stored. | `internal/up/up.go`; `internal/up/up.go`; `internal/lock/lock.go` |
| Repo-delivered manifests are written as `manifests/NN-<kind>-<name>.yaml` in stable order, and the sync only creates/updates/deletes under the passed dir subtree. | `internal/up/up.go`; `internal/gitea/client.go` |
| `cube-idp diff` mutates nothing: it gates on `prov.Exists` rather than creating a cluster, then computes kernel changes via SSA dry-run, pack drift via lock rendered hashes, and orphans via the inventory. | `internal/diff/diff.go` (gate) |
| `diff.desiredState` calls the same `pack.ResolveOrder` as `up`, so diff previews byte-identical delivery objects and cannot fabricate drift. | `internal/diff/diff.go`; `internal/diff/diff_test.go` |
| `upgrade --plan` classifies each pack as "new (not in cube.lock)", "up to date" or "update available". | `internal/upgrade/plan.go` |
| On drift, `upgrade --plan` exits 1 — except when prompts are allowed (real TTY), where it offers to apply immediately and runs `runUpPipeline` on consent. | `cmd/upgrade.go` |
| `pack install` and `spoke add` only edit `cube.yaml` and validate by round-trip; no cluster mutation. | `cmd/pack.go`; `cmd/spoke.go` |
| `spoke list` reads live cluster state read-only and degrades to declared config on any failure. | `cmd/spoke.go`; `cmd/spoke.go` |
| `spoke remove --delete-cluster` deletes a kind spoke cluster via `prov.Delete`, behind a consent prompt that refuses non-interactively without `--yes`. | `cmd/spoke.go`; `cmd/spoke.go` |
| The install path does not validate the dependency graph; graph validation happens at the next `up` or `diff`. | `cmd/pack.go` |
| The `up` "packs-crd" step applies and inventories only the `Pack` CRD; step text is "Pack CRD established". | `internal/up/up.go` |
| `docs/machine-readable-output.md` documents the `encode_error` event — no `ts` field, emitted so a marshal failure surfaces on-stream rather than dropping an event silently. | `internal/ui/render/json.go`; `docs/machine-readable-output.md` |

### Verification

- [ ] `internal/lock/lock.go` defines `File` with `APIVersion`/`Kind`, and `lock.Read`/`lock.Write` are plain file YAML I/O with no Kubernetes client.
- [ ] No `cubelocks.cube-idp.dev` CRD manifest exists in the tree; `internal/pack/manifests/pack-crd.yaml` is the only `cube-idp.dev` CRD.
- [ ] `internal/lock/lock.go` `Entry` has no order or wave field, and `EngineLock.Entry()` projects the same fields as `Entry`.
- [ ] Delivery ordering itself is ADR-0005's; this ADR only relies on it being derivable. `internal/pack/depgraph.go` and `internal/pack/depgraph_test.go` (`TestResolveOrderNoDepsDeclaredOrder`) are the ordering owner's evidence.
- [ ] `internal/up/up.go` formats repo files as `manifests/%02d-%s-%s.yaml`; `internal/gitea/client.go` `SyncDir` confines create/update/delete to the passed dir subtree and skips blobs whose SHA matches.
- [ ] `internal/diff/diff.go` checks `prov.Exists` rather than calling `Ensure`, so `diff` never creates a kind cluster.
- [ ] `internal/diff/diff.go` calls `pack.ResolveOrder`, the same function `internal/up/up.go` reaches via `resolveAndDeliverPacks`; `internal/diff/diff_test.go` (`TestDesiredStateMatchesUpAppliedSet`) asserts diff's desired object set matches `up.Run`'s applied set.
- [ ] `internal/upgrade/plan.go` returns exactly three `Change` strings, and `cmd/upgrade.go` maps drift to exit code 1 on the non-TTY / declined-prompt path.
- [ ] `cmd/pack.go` contains no call to `pack.ResolveOrder` and no cluster client, applier or kube import.
- [ ] `internal/up/up.go` emits `con.Step("packs-crd", "Pack CRD established")` — singular.

## History

The `up` "packs-crd" step was originally specified to apply and inventory both the
`Pack` and `CubeLock` CRDs, with step text "record CRDs established". Because
`cube.lock` stayed a file-based KRM document rather than an in-cluster CRD, that
step now applies only the `Pack` CRD and its text reads "Pack CRD established";
`internal/ui/render/json_test.go` and `plain_test.go` assert the singular text.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during
the 2026-07-20 documentation audit; the underlying statements were validated against
the code before this record was written.

- `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:203` — cube.lock stays a KRM file, not a CRD.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:174` — Kahn ordering with declared-order tie-break.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:219` — lock entries in declared order.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:2078` — `diff` mutates nothing.
- `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:253` — config-mutating commands edit cube.yaml only.
