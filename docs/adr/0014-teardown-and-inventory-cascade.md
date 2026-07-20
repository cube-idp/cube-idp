---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 14. Teardown Semantics: Cluster Deletion vs Inventory-Driven Cascade

## Context and Problem Statement

`cube-idp down` has to undo everything `cube-idp up` did, but "everything" means
different things depending on who owns the cluster. When cube-idp created a local
kind or k3d cluster, deleting that cluster is a complete and cheap teardown. When
the user pointed cube-idp at a cluster they already owned (`provider: existing`),
or asked to keep a local one (`--keep-cluster`), deleting the cluster would destroy
resources cube-idp never created.

The delivery engine (Argo CD or Flux) complicates this further: the engine is itself
installed by `up` and recorded in the inventory, so a naive "engine uninstalls itself"
step risks the engine tearing down the very controllers that are executing the
teardown. Beyond in-cluster state, `up` also mutates the developer's machine — a
kubeconfig context, OS trust-store entries from `cube-idp trust`, a CoreDNS rewrite —
and may have created spoke clusters. All of it needs a defined disposal rule.

## Decision

`cube-idp down` deletes a cluster outright only when cube-idp created it. With
`provider: existing` or `--keep-cluster` it instead performs an inventory-driven
cascade that deletes the whole recorded inventory, removing only cube-idp-managed
resources and leaving the cluster in place.

Engine removal is inventory-driven rather than engine-specific: an engine's
`Uninstall` is a no-op where nothing engine-specific is required, and the engine's
own install objects are deleted as part of that bulk cascade. The cascade is
dependency-agnostic — it does not honour declared pack dependency order — and the
argocd engine's self-Application deliberately carries no resources-finalizer, so a
cascade delete cannot tear the engine down from inside.

`down` reverts any `cube-idp trust` changes on both paths, deletes cube-created kind
spoke clusters best-effort after hub teardown — unless `--keep-cluster` was passed,
which preserves spokes too — while leaving `existing` spoke clusters untouched, and
reports the actual cluster provider in its output rather than always naming kind.

## Consequences

* Good, because a user's pre-existing cluster is never destroyed by `down`; the
  blast radius is bounded by what cube-idp actually recorded in the inventory.
* Good, because engine removal needs no per-engine teardown logic — adding an engine
  does not require writing an uninstall path, only being present in the inventory.
* Good, because the missing resources-finalizer on the self-Application makes the
  ordering safe by construction rather than by careful sequencing.
* Good, because `down` is honest about the machine-level state it mutated: trust
  store, CoreDNS, and spoke clusters are all addressed.
* Bad, because the cascade ignores dependency order, so packs with runtime coupling
  may see dependents and dependencies deleted in an arbitrary relative order; only
  reverse-apply order is guaranteed.
* Bad, because anything cube-idp created but failed to record in the inventory
  survives teardown silently.
* Bad, because `existing` spoke clusters retain cube-idp RBAC and namespaces that the
  user must clean up manually.
* Bad, because a CoreDNS revert failure aborts teardown before any inventory deletion
  happens, stranding the whole recorded inventory in the cluster.
* Bad, because a failing `trustUninstall` still exits `down` non-zero even though the
  cluster-side teardown has already succeeded by that point.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Engine removal is inventory-driven: the argocd engine's `Uninstall` is a literal no-op and its objects are removed by the inventory cascade. | `internal/engine/argocd/argocd.go:70-74` |
| The argocd self-Application deliberately carries no resources-finalizer, so a cascade delete cannot tear the engine down from inside (normal pack Applications do carry it). | `internal/engine/argocd/deliverself.go:25-27`, contrast `internal/engine/argocd/deliver.go:48` |
| `down` is dependency-agnostic: `eng.Uninstall` runs first, then `a.DeleteAll` performs bulk inventory deletion with no dependency ordering. | `cmd/down.go:197`, `cmd/down.go:212` |
| Inventory deletion runs in reverse apply order — objects applied later are deleted first. | `internal/apply/inventory.go:158-166` |
| `provider: existing` or `--keep-cluster` takes the cascade arm and returns without touching the cluster; otherwise `prov.Delete` removes the kind/k3d cluster. | `cmd/down.go:168`, `cmd/down.go:222` |
| Output names the actual provider rather than hardcoding "kind". | `cmd/down.go:221`, `cmd/down.go:226` |
| The CoreDNS rewrite is reverted on the keep-cluster path *before* the cascade, and its failure aborts `down` prior to `DeleteAll`. | `cmd/down.go:205-210` |
| `cube-idp trust`'s OS trust-store install is reverted on both teardown paths; an unreadable trust dir/state degrades to a warning, but a failing `trustUninstall` still fails `down` (`cmd/down.go:257-258`). | `cmd/down.go:243-263` |
| Cube-created kind spoke clusters are deleted best-effort after hub teardown, except under `--keep-cluster`, which keeps them; `existing` spokes are left untouched with a manual-cleanup note. | `cmd/down.go:134-155`, `cmd/down.go:139-142` |

### Verification

- [ ] `internal/engine/argocd/argocd.go` defines `(*ArgoCD).Uninstall` as `return nil`
      with the comment "removal is inventory-driven by `down`".
- [ ] `internal/engine/argocd/deliverself.go` sets no `finalizers` on the self-Application,
      while `internal/engine/argocd/deliver.go:48` sets
      `resources-finalizer.argocd.argoproj.io` on pack Applications.
- [ ] `cmd/down.go:168` branches on `cube.Spec.Cluster.Provider == "existing" || keepCluster`
      and that arm returns before any `prov.Delete` call.
- [ ] `grep -n "DependsOn\|dependsOn\|dependency" cmd/down.go` returns no matches
      (dependency ordering lives only in `up`'s wave gate, not teardown).
- [ ] `internal/apply/inventory.go` iterates its deletable set backwards
      (`for i := len(deletable) - 1; i >= 0; i--`).
- [ ] `cmd/down.go` progress/completion messages interpolate
      `cube.Spec.Cluster.Provider` rather than the literal string `kind`.
- [ ] `revertTrust` is called on both the cascade path and the cluster-delete path in
      `runDown`.
- [ ] `downSpokes` deletes `kind` spokes and only emits a note for `existing` spokes.

## More Information

Note the known asymmetry: Flux's `Uninstall`
(`internal/engine/flux/flux.go:54-83`) is *not* a no-op — it deletes delivered
Kustomizations/OCIRepositories and waits for prune finalizers so workloads are removed
while its controllers are still alive. The inventory-driven cascade is the common
mechanism; engines may still do engine-specific pre-work in `Uninstall`.

Kubeconfig context removal is left to the vendored kind/k3d libraries on cluster
delete; cube-idp does not manage it, and the cascade arm (`existing`/`--keep-cluster`)
does not touch the kubeconfig at all. The `down` dry-run preview does mention the
context alongside the cluster it would delete (`cmd/down.go:101-102`).

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during
the 2026-07-20 documentation audit; the underlying statements were validated against
the code before this record was written.

* `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:249`
* `docs/archive/superpowers/plans/2026-07-16-tui-interactive-layer.md:1313`
* `docs/archive/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md:2760`
* `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:99`
