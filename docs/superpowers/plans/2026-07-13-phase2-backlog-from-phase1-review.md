# Phase 2 backlog — carried from Phase 1 reviews

Source: task-scoped reviews + final whole-branch review of `feature/phase1-mvp`
(merged verdict: Ready to merge). These are the FOLLOW-UP items the reviews
deferred; the Phase 2 draft plan's Task 0 reconciliation gate should ingest
this list.

## Behavior / coverage

- **e2e leg for `down --keep-cluster`**: the live-controller prune wait in
  `flux.Uninstall` has envtest proof of mechanics but no end-to-end proof
  (current e2e exercises only the kind down path). Add a `--keep-cluster`
  teardown+assert-empty leg before the final `down`.
- **Multi-cube shared-engine teardown**: `Uninstall` scopes to the cube label,
  but `DeleteAll` removes the shared flux controllers/CRDs from cube A's
  inventory even if cube B still uses them on the same cluster.
- **`down` engine mismatch**: `down --keep-cluster` constructs the engine from
  cube.yaml; if the user edited `engine.type` after `up`, teardown should use
  the engine actually installed (or say so).
- **flux `health.go`**: no unit test (fake-client table test is cheap).
- **`config schema` command**: no test (execute, assert output contains `#Cube`).
- **kindp**: worker-node hostPort==gateway.Port collision only surfaces at
  docker bind time; omitted-role nodes are rejected more strictly than kind's
  own defaulting (error message could mention the omitted-role case);
  `DetectNodeProvider()` error is discarded.
- **existing provider quirks** (wire up when `doctor` lands): `Exists()`
  ignores its name arg; `Diagnose()` only checks the default context;
  `load()` has a dead error return.
- **Cluster-shape drift warning**: `up` silently ignores changes to
  extraPorts/mounts/registry/providerConfig/kubernetesVersion/gateway.port
  when the kind cluster already exists (README caveat exists; a runtime
  warning comparing rendered config against creation-time config would be
  better — requires persisting the rendered config, e.g. in a ConfigMap).

## Hygiene / polish

- CUBE-3004 is overloaded (health timeout, infra failure, status-unready);
  split codes. CUBE-1004's remediation reads oddly from `down --keep-cluster`.
- Pack name validation: `pack.cue` name is unconstrained but becomes an OCI
  repo path and Flux object name; validate early in `up`. Also dedupe delivery
  names (gateway pack listed again in spec.packs yields duplicate objects).
- `tests/packs_render_test.go` never runs in CI (network-gated, e2e job only
  runs tests/e2e) — run it in the e2e job. Add `Gateway` to the
  namespacedKinds guard.
- Pin `sigs.k8s.io/kind` CLI version in CI (currently @latest); cache envtest
  assets; pin `setup-envtest@latest` in the Makefile.
- `pack`: `isLocalRegistryHost` misses `[::1]`; OCI cache doesn't skip the
  registry round-trip; secret-table escaper doesn't escape backslash itself.
- `gatewayContainerPort` (30080) ↔ traefik pack nodePort agreement is
  comment-enforced; add a test reading the pack's chart.yaml.
- A docs-accuracy check (grep remediation strings/README for
  `cube-idp <subcommand>` and verify against the cobra tree) — two phantom
  commands slipped through 13 task reviews before being caught.

## Error-code reservations (reconcile drafts)

Shipped code now uses: CUBE-2006/2007 (apply), CUBE-4012/4013 (pack/up),
CUBE-0004/0006, CUBE-1004, CUBE-3005 (flux uninstall). The Phase 2 draft's
tentative CUBE-3005 reservation must move (3007+). Drafts reserve 4006–4011
(still honored).
