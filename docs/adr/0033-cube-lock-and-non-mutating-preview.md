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

Delivery order is deterministic: topological ordering uses Kahn's algorithm with
declared `cube.yaml` order as the tie-break among ready nodes, so cubes with no
dependencies and no repo delivery reproduce the historical order byte-for-byte —
gateway first, declared order after.

Repo-delivered pack manifests are written as `manifests/NN-<kind>-<name>.yaml` in
stable order, and the sync only creates, updates or deletes under `manifests/`.
Gitea repo sync is idempotent by git blob SHA: an unchanged render produces zero
commits, and each sync is at most one commit.

Preview commands mutate nothing. `diff` computes kernel-object changes via SSA
dry-run, pack drift via the lock's rendered hashes, and orphans via the inventory,
calling the same `ResolveOrder` as `up`. `upgrade --plan` classifies each pack as
new, up to date, or update available, and exits 1 when anything would change.
Config-mutating commands only edit `cube.yaml` and validate by round-trip, deferring
all delivery and graph validation to the next `up` or `diff`.

## Consequences

* Good, because the lockfile is reviewable in a pull request, diffable, and
  vendorable into an air-gapped bundle without a cluster.
* Good, because `diff` and `up` share `ResolveOrder`, so a preview cannot fabricate
  drift on dependency-bearing cubes — a property a test asserts directly.
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
| `cube.lock` remains a KRM-shaped `CubeLock` file at apiVersion `cube-idp.dev/v1alpha1` rather than an in-cluster CRD. | `internal/lock/lock.go:16-22` |
| `EngineLock` records the standard `lock.Entry` fields (Ref, Name, Version, resolved pin, RenderedHash, Images) in addition to Type, making the engine install reproducible. | `internal/lock/lock.go:27-45` |
| `cube.lock` entries are written in declared `cube.yaml` order; delivery order is derivable and is not stored. | `internal/up/up.go:341-401` |
| Topological ordering uses Kahn's algorithm with declared order as the tie-break, so dependency-free cubes come out gateway-first then declared order, byte-for-byte. | `internal/pack/depgraph.go:98-123` |
| Repo-delivered manifests are written as `manifests/NN-<kind>-<name>.yaml` in stable order, and the sync only creates/updates/deletes under `manifests/`. | `internal/up/up.go:1157` |
| `cube-idp diff` mutates nothing: kernel changes via SSA dry-run, pack drift via lock rendered hashes, orphans via the inventory. | `internal/diff/diff.go:46-140` |
| `diff.desiredState` calls the same `ResolveOrder` as `up`, so diff previews byte-identical delivery objects and cannot fabricate drift. | `internal/diff/diff.go:257` |
| `upgrade --plan` classifies each pack as "new (not in cube.lock)", "up to date" or "update available", and exits 1 when anything would change. | `internal/upgrade/plan.go:76-83` |
| Config-mutating commands (`pack install`, `spoke add\|list\|remove`) only edit `cube.yaml` and validate by round-trip; no cluster mutation. | `cmd/pack.go:340-379` |
| The install path does not validate the dependency graph; graph validation happens at the next `up` or `diff`. | `cmd/pack.go:340-379` |
| The `up` "packs-crd" step applies and inventories only the `Pack` CRD; step text is "Pack CRD established". | `internal/up/up.go:209-224` |
| `docs/machine-readable-output.md` documents the `encode_error` event — no `ts` field, emitted so a marshal failure surfaces on-stream rather than dropping an event silently. | `internal/ui/render/json.go:31`; `docs/machine-readable-output.md:238-256` |

### Verification

- [ ] `internal/lock/lock.go` defines `File` with `APIVersion`/`Kind`, and `lock.Read`/`lock.Write` are plain file YAML I/O with no Kubernetes client.
- [ ] No `cubelocks.cube-idp.dev` CRD manifest exists in the tree; `internal/pack/manifests/pack-crd.yaml` is the only `cube-idp.dev` CRD.
- [ ] `internal/lock/lock.go` `Entry` has no order or wave field, and `EngineLock.Entry()` projects the same fields as `Entry`.
- [ ] `internal/pack/depgraph.go` picks the lowest-index ready pack each round (Kahn tie-break); `internal/pack/depgraph_test.go:75` (`TestResolveOrderNoDepsDeclaredOrder`) fences declared order.
- [ ] `internal/up/up.go:1157` formats repo files as `manifests/%02d-%s-%s.yaml`; `internal/gitea/client.go` `SyncDir` confines create/update/delete to the passed dir subtree and skips blobs whose SHA matches.
- [ ] `internal/diff/diff.go` checks `prov.Exists` rather than calling `Ensure`, so `diff` never creates a kind cluster.
- [ ] `internal/diff/diff.go:257` calls `pack.ResolveOrder`, the same function `internal/up/up.go:413` reaches via `resolveAndDeliverPacks`; `internal/diff/diff_test.go:148` asserts the two sets match.
- [ ] `internal/upgrade/plan.go:76-83` returns exactly three `Change` strings, and `cmd/upgrade.go` maps drift to exit code 1.
- [ ] `cmd/pack.go:340-379` contains no call to `pack.ResolveOrder` and no cluster client, applier or kube import.
- [ ] `internal/up/up.go:224` emits `con.Step("packs-crd", "Pack CRD established")` — singular.

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
