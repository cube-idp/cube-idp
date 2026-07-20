# Outstanding TODOs

Deferred follow-ups surfaced during implementation but intentionally not yet
fixed. Each item records the symptom, the code site, and a suggested fix so the
work can be scoped later.

## Cluster `forProvider` / `providerConfigRef`

Surfaced while replacing `spec.cluster.providerConfig` with `providerConfigRef`
+ `forProvider`. Both are non-blocking (minor).

- [ ] **M1 — Spoke `provider: existing` does not reject node-creation fields (`forProvider` / `providerConfigRef`), while the hub does.**
  - **Symptom:** A spoke like `{provider: existing, context: x, forProvider: {…}}` loads clean and silently ignores `forProvider` (no cluster is created for an `existing` spoke). The identical *hub* config errors with `CUBE-1003` (`CodeClusterFieldsConflict`). Inconsistent validation feedback for a nonsensical config.
  - **Root cause / code site:** [`internal/config/load.go`](internal/config/load.go) — the existing-provider node-field guard lives in `crossValidate` (applied only to `c.Spec.Cluster`). `validateSpokes` validates the provider enum + `context` but never the node-field conflict.
  - **Why deferred:** the existing-provider guard was only extended for the **hub**; adding a spoke guard was out of scope. It also extends a **pre-existing** gap: `kubernetesVersion` was already spoke-allowed and equally unguarded.
  - **Suggested fix:** extend the existing-provider node-field check to spokes in `validateSpokes` (mirror the hub condition: reject `ExtraPorts`/`Mounts`/`ProviderConfigRef`/`ForProvider`/`KubernetesVersion` when `provider == "existing"`), with a spoke-scoped test alongside `TestLoadForProviderRejectedForExisting`.

- [ ] **M2 — `RenderConfig` calls `pack.DefaultCacheDir()` unconditionally, regressing the pure-render contract when no ref is set.**
  - **Symptom:** Even with no `providerConfigRef`, `RenderConfig` calls `pack.DefaultCacheDir()`, which `os.MkdirAll($HOME/.cache/cube-idp/packs)` — a filesystem side effect + a new failure mode if `$HOME` is unset. The pre-change render was side-effect-free; `cube-idp config render-cluster`'s contract is pure, pipeable YAML.
  - **Root cause / code site:** [`internal/cluster/kindp/merge.go`](internal/cluster/kindp/merge.go) and [`internal/cluster/k3dp/merge.go`](internal/cluster/k3dp/merge.go) — `cacheDir, err := pack.DefaultCacheDir()` is called at the top of `RenderConfig`, before the `spec.ProviderConfigRef == ""` short-circuit inside `compose.Compose`.
  - **Why deferred:** the unconditional call was left in place at the time; a lazy-resolution fix was deferred as a follow-up.
  - **Suggested fix:** resolve the cache dir lazily — only when `spec.ProviderConfigRef != ""` — so a no-ref render performs no filesystem work. Guard with a test that renders with `$HOME` unset and no ref. The purity claim is in the `RenderConfig` doc comment ("Pure except the providerConfigRef fetch").
