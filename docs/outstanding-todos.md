# Outstanding TODOs

Deferred follow-ups surfaced during implementation but intentionally NOT fixed
in their originating branch (they conflicted with plan-mandated text, so the
operator deferred them). Each item cites the exact source finding, the code
site, and the plan/spec authority so the fix can be scoped later.

## From: cluster forProvider / providerConfigRef (branch `worktree-forprovider`, b827e09..fdd19e3)

Surfaced by the final whole-branch code review of the 8-task feature that
replaced `spec.cluster.providerConfig` with `providerConfigRef` + `forProvider`.
Both were rated **Minor** (non-blocking; branch shipped ready-to-merge).

- [ ] **M1 — Spoke `provider: existing` does not reject node-creation fields (`forProvider` / `providerConfigRef`), while the hub does.**
  - **Symptom:** A spoke like `{provider: existing, context: x, forProvider: {…}}` loads clean and silently ignores `forProvider` (no cluster is created for an `existing` spoke). The identical *hub* config errors with `CUBE-1003` (`CodeClusterFieldsConflict`). Inconsistent validation feedback for a nonsensical config.
  - **Root cause / code site:** [`internal/config/load.go`](internal/config/load.go) — the existing-provider node-field guard lives in `crossValidate` (applied only to `c.Spec.Cluster`, ~line 137). `validateSpokes` (~line 234) validates the provider enum + `context` but never the node-field conflict.
  - **Why deferred:** The plan only extended the **hub** existing-guard (Task 3, [`docs/superpowers/plans/2026-07-18-cluster-forprovider.md`](docs/superpowers/plans/2026-07-18-cluster-forprovider.md)). Adding a spoke guard is scope beyond the plan. It also extends a **pre-existing** gap: `kubernetesVersion` was already spoke-allowed and equally unguarded before this branch.
  - **Spec authority:** spoke parity for `forProvider`/`providerConfigRef` is intentional — [`docs/superpowers/specs/2026-07-18-cluster-forprovider-design.md`](docs/superpowers/specs/2026-07-18-cluster-forprovider-design.md) §3. The guard, not the field, is what's missing for `existing` spokes.
  - **Suggested fix:** extend the existing-provider node-field check to spokes in `validateSpokes` (mirror the hub condition: reject `ExtraPorts`/`Mounts`/`ProviderConfigRef`/`ForProvider`/`KubernetesVersion` when `provider == "existing"`), with a spoke-scoped test alongside `TestLoadForProviderRejectedForExisting`.

- [ ] **M2 — `RenderConfig` calls `pack.DefaultCacheDir()` unconditionally, regressing the pure-render contract when no ref is set.**
  - **Symptom:** Even with no `providerConfigRef`, `RenderConfig` calls `pack.DefaultCacheDir()`, which `os.MkdirAll($HOME/.cache/cube-idp/packs)` — a filesystem side effect + a new failure mode if `$HOME` is unset. The pre-change render was side-effect-free; `cube-idp config render-cluster`'s contract is pure, pipeable YAML.
  - **Root cause / code site:** [`internal/cluster/kindp/merge.go`](internal/cluster/kindp/merge.go) and [`internal/cluster/k3dp/merge.go`](internal/cluster/k3dp/merge.go) — `cacheDir, err := pack.DefaultCacheDir()` is called at the top of `RenderConfig`, before the `spec.ProviderConfigRef == ""` short-circuit inside `compose.Compose`.
  - **Why deferred:** The plan's Task 4 Step 4 and Task 5 Step 4 code ([`docs/superpowers/plans/2026-07-18-cluster-forprovider.md`](docs/superpowers/plans/2026-07-18-cluster-forprovider.md)) write this exact unconditional call. A lazy-resolution fix diverges from the plan's written code, so it was left plan-faithful.
  - **Spec authority:** stdout/render purity — spec §8 and Task 6 (`TestRenderClusterPrintsOverrideWarnings`). The purity claim is in the `RenderConfig` doc comment ("Pure except the providerConfigRef fetch").
  - **Suggested fix:** resolve the cache dir lazily — only when `spec.ProviderConfigRef != ""` — so a no-ref render performs no filesystem work. Guard with a test that renders with `$HOME` unset and no ref.
