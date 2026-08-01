# 0043 — Cluster mounts and extraPorts Semantics for Multi-Node Clusters

Status: proposed
Date: 2026-07-20
Epic: cube-idp/cube-idp#7

## Context

`spec.cluster.extraPorts` / `spec.cluster.mounts` apply to the control-plane
node only (kind: `internal/cluster/kindp/merge.go:143,:163`; k3d:
`internal/cluster/k3dp/merge.go:153,:173`). `providerConfigRef` /
`forProvider` now allow multi-node topologies, where worker-scheduled pods
silently miss hostPath data and hostPort routing becomes provider-dependent.

## Options (from #7)

1. **Mounts scope** — (a) all nodes by default + optional per-role selector
   [RECOMMENDED (agent): least surprising for hostPath data] · (b) keep
   control-plane-only, documented.
2. **extraPorts semantics** — (a) control-plane only, documented ·
   (b) all nodes (host port conflicts!) · (c) provider-native LB answer
   (k3d serverlb) vs kind port-mapping [RECOMMENDED (agent): (a) now,
   (c) as follow-up — smallest correct step].
3. **Interaction with per-node conflict checks**
   (`internal/cluster/kindp/merge.go:147-156`) — decision follows 1&2.
4. **k3d specifics** (servers vs agents vs serverlb) — decision follows 2.

## Decision

_Pending PR review — the merge of this PR is the acceptance._

## Implementation Plan

- **Affected paths:** `internal/cluster/kindp/merge.go`,
  `internal/cluster/k3dp/merge.go`, provider contract tests.
- **Sub-issues (created at acceptance):** one per decided option group +
  e2e coverage on a multi-node topology.

## Verification

- [ ] Multi-node e2e: hostPath mount visible from a worker-scheduled pod
- [ ] Port semantics asserted per provider in the contract suite
- [ ] `spec.cluster.*` docs updated
