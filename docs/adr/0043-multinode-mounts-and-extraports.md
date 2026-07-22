# 0043 — Cluster mounts and extraPorts Semantics for Multi-Node Clusters

Status: accepted
Date: 2026-07-20
Accepted: 2026-07-22
Epic: cube-idp/cube-idp#7

## Context

`spec.cluster.extraPorts` / `spec.cluster.mounts` apply to the control-plane
node only (kind: `internal/cluster/kindp/merge.go:143,:163`; k3d:
`internal/cluster/k3dp/merge.go:153,:173`). `providerConfigRef` /
`forProvider` now allow multi-node topologies, where worker-scheduled pods
silently miss hostPath data and hostPort routing becomes provider-dependent.

## Options (from #7)

1. **Mounts scope** — (a) all nodes by default + optional per-role selector
   [CHOSEN — least surprising for hostPath data] · (b) keep
   control-plane-only, documented.
2. **extraPorts semantics** — (a) control-plane only, documented [CHOSEN for
   now] · (b) all nodes (host port conflicts!) [rejected] · (c)
   provider-native LB answer (k3d serverlb) vs kind port-mapping [CHOSEN as
   follow-up — smallest correct step, deferred to its own ADR].
3. **Interaction with per-node conflict checks**
   (`internal/cluster/kindp/merge.go:147-156`) — decision follows 1&2.
4. **k3d specifics** (servers vs agents vs serverlb) — decision follows 2.

## Decision

Adopted 2026-07-22 (merge of this PR = acceptance). Each option group below
states the decision; the reviewer may amend any of them in this PR before
merge.

1. **Mounts scope → all nodes by default, with an optional per-role
   selector** (option 1a). `spec.cluster.mounts` apply to every node in the
   topology unless a mount narrows itself with an explicit role selector
   (e.g. `nodes: [control-plane]` / `nodes: [worker]`). Rationale: hostPath
   data a workload depends on must be present wherever the scheduler places
   the pod; control-plane-only is the surprising default the moment worker
   nodes exist. Backward-compatible for single-node clusters (one node =
   the control plane).

2. **extraPorts semantics → control-plane only now (documented); a
   provider-native load-balancer answer is a follow-up** (option 2a now,
   2c later). `spec.cluster.extraPorts` continue to map on the control-plane
   node only, and this is documented as the current contract. "All nodes"
   (2b) is rejected: identical host ports on multiple nodes collide on the
   single host. The provider-native path — k3d `serverlb` vs kind
   port-mappings (2c) — is deferred to its own ADR/epic rather than bundled
   here, keeping this the smallest correct step.

3. **Per-node conflict checks** (`internal/cluster/kindp/merge.go:147-156`)
   follow from (1) and (2): the existing single-host port-collision guard
   stays authoritative for extraPorts (still control-plane-scoped); mounts
   gain a per-node application step but no new conflict class (hostPath
   mounts do not contend for a shared host resource the way host ports do).

4. **k3d specifics** follow from (2): servers vs agents vs `serverlb` are
   not surfaced as user-facing knobs in this ADR; extraPorts stay on the
   k3d server (control-plane analogue), and the `serverlb` question rides
   with the deferred provider-native follow-up.

**Explicitly out of scope (this ADR):** the provider-native LB/port strategy
(2c), and any change to how single-node clusters behave.

## Implementation Plan

- **Affected paths:** `internal/cluster/kindp/merge.go`,
  `internal/cluster/k3dp/merge.go`, provider contract tests,
  `docs/reference/kind-config-reference.md`, `docs/architecture/cluster.md`.
- **Sub-issues (created at acceptance, each `Closes #<sub>` +
  `Implements ADR-0043`):**
  1. Mounts apply to all nodes by default (kind + k3d), with an optional
     per-role `nodes:` selector on a mount entry.
  2. extraPorts stay control-plane-only; document the contract in
     `docs/reference/kind-config-reference.md` and note the multi-node
     caveat.
  3. Multi-node e2e coverage: hostPath mount visible from a
     worker-scheduled pod; extraPorts asserted per provider in the contract
     suite.
  4. Update `docs/architecture/cluster.md` (same-PR rule) — keep the
     `cube:doc` `code=`/`adrs=` markers current, add `adrs=…,0043`.
- **Follow-up (separate ADR, not this epic):** provider-native
  LB/port-mapping strategy (k3d `serverlb` vs kind port-mappings) — option
  2(c).

## Verification

- [ ] Multi-node e2e: hostPath mount visible from a worker-scheduled pod
- [ ] Port semantics asserted per provider in the contract suite
- [ ] `spec.cluster.*` docs updated
