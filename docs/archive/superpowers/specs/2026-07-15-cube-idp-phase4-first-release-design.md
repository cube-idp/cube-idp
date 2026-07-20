# cube-idp Phase 4 — "First Release (v0.1.0)" Design

**Date:** 2026-07-15
**Status:** Approved design (owner brainstorm 2026-07-15), ground truth for the Phase 4 implementation plan
**Parent spec:** `docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md` (decisions D1–D15)
**Prior record:** Phase 3 executed 2026-07-14/15 — plan with findings register F1–F11 and the final-review Phase 4 backlog at `docs/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md`; execution ledger `.superpowers/sdd/progress.md`

## 1. Goal

Ship cube-idp **v0.1.0** as a **private** release: versioned multi-platform binaries, a generated changelog, install documentation — on the **unchanged `cube-idp.dev/v1alpha1` config schema** — with the entire Phase 3 review backlog burned down and known behavioral warts fixed **without breaking any existing cube.yaml**.

## 2. Decisions (owner, 2026-07-15)

| # | Decision | Choice |
|---|----------|--------|
| P4-D1 | Phase 4 identity | **Road to first release**, not new capabilities. Spec §6's Phase 4 candidates (Talos/vcluster providers, Extism hooks, in-cluster operator) remain gated on demand evidence and are OUT of scope |
| P4-D2 | Release reach | **Private.** Repo stays private; binaries/packs consumable by authorized users only. Public launch is a separate later decision. Release machinery is built and exercised for real regardless |
| P4-D3 | Schema | **Stay on `v1alpha1`.** Fix bugs and behavior, never the wire schema. The D5 freeze (`v1` + `cube-idp migrate`) is deferred until post-publication findings accumulate. No schema field is added, renamed, or removed in this phase unless purely additive AND optional (one addition is sanctioned: the `pack.cue` `gatewayService` block, §5.7 — pack format, not cube.yaml) |
| P4-D4 | Version tag | **v0.1.0** — conservative first-release semver; v1.0.0 is reserved for the D5 freeze |
| P4-D5 | Backlog scope | **Full burn-down** of the Phase 3 final-review FOLLOW-UP list (all seven clusters), plus release engineering |
| P4-D6 | Execution shape | **Sequential single lane** (owner's explicit choice over parallel streams), ten tasks, release pipeline FIRST so every later merge is release-candidate-testable |

## 3. Non-goals

- No public launch work (positioning README, announcement, brew tap, install script for strangers).
- No schema freeze, no `cube-idp migrate`, no `v1` apiVersion.
- No new cluster providers, no plugin RPC/Wasm, no in-cluster operator.
- No new packs; catalog stays as shipped in Phase 3.

## 4. Standing constraints (inherited, binding on every task)

These are the Phase 1–3 invariants; the implementation plan restates them as its Global Constraints:

- Module `github.com/rafpe/cube-idp`, Go per `go.mod` (currently 1.26.x); never hardcode a Go version in CI (`go-version-file: go.mod`).
- Every user-facing failure is a typed `CUBE-xxxx` `diag.Error` with remediation; every code is a constant in `internal/diag/codes.go`; `TestNoCubeLiteralsOutsideCatalog` bans literals. **Range `8xxx` is reserved for this phase** (release/bundle-integrity codes) — grep the catalog before allocating.
- Plain-mode output is byte-stable and golden/e2e-pinned; new output is additive and routed through `internal/ui` (`Console`/`Printer`/event stream per the UX design `docs/superpowers/specs/2026-07-14-cube-idp-ux-design.md`).
- TDD per step; `go build ./... && go vet ./... && go test ./... -short -count=1` green before every commit; conventional commits ending `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- e2e: gated by `CUBE_IDP_E2E=1`; locally ALWAYS set `CUBE_IDP_E2E_GATEWAY_PORT=18443` (8443 is squatted on the dev machine); never touch clusters named `testowy`, `airbyte*`, `xxx`, `envoy-dbg`; test clusters use `e2e-`/`fix-` prefixes and must never leak.
- SSA field manager `cube-idp`; inventory merge semantics (RecordInventory unions); prune opt-out annotation unchanged.

## 5. The ten tasks

### 5.1 Task R1 — Release pipeline (FIRST)

**Purpose:** every subsequent merge is buildable into a real release candidate.

- **goreleaser** config (`.goreleaser.yaml`): builds for `darwin/arm64`, `darwin/amd64`, `linux/arm64`, `linux/amd64`; `CGO_ENABLED=0` static binaries; archives `tar.gz` + `checksums.txt` (sha256).
- **Version stamping:** ldflags `-X` into the existing version surface — `cmd/version.go` currently prints a compile-time value; RECONCILE its exact variable path and stamp `version`, `commit`, `date`. `cube-idp version` output format is part of the release smoke assertion.
- **Changelog:** goreleaser's built-in changelog with conventional-commit grouping (`feat`, `fix`, `docs`, others); no new tooling. Seed `CHANGELOG.md` for v0.1.0 from the Phase 1–4 history (curated summary, not 200 raw commits — headline features per phase).
- **CI:** new `.github/workflows/release.yaml` triggered on `v*` tags: checkout, setup-go via `go-version-file`, `goreleaser release` with `GITHUB_TOKEN` (private-repo Release). Snapshot mode (`goreleaser release --snapshot --clean`) must run green in the normal CI on every push to main (cheap validation, no publish).
- **Install docs (private consumption):** README section — `gh release download v0.1.0 -R RafPe/cube-idp -p '<pattern>'`, checksum verify, chmod, PATH. Note `go install` does not work for private modules without GOPRIVATE setup; document that alternative in two lines.
- **Acceptance:** `goreleaser release --snapshot --clean` produces all four artifacts locally; `dist/*/cube-idp version` prints the stamped snapshot version.

### 5.2 Task R2 — Bundle integrity

**Purpose:** restore the "verified/untampered" guarantee that Phase 3's fix wave honestly tempered (findings register: Verify is presence+size only).

- **Manifest formatVersion 2** (`internal/bundle/bundle.go` `Manifest`): adds `packHashes map[string]string` (pack name → `h1:` dirhash of the staged pack dir, computed with `golang.org/x/mod/sumdb/dirhash` — already the pin format used for `dir:` pins in `internal/lock`) and `imageHashes map[string]string` (original image ref → sha256 of the tar file). `Vendor` writes v2. `Open` REJECTS v1 bundles with the existing `CUBE-7003` and remediation "re-run `cube-idp vendor` — bundle format upgraded" (bundles are ephemeral transport artifacts; no compatibility shim).
- **`Verify()`** recomputes both hash sets and compares; any mismatch → `CUBE-7004` naming the exact entry. Docstring and `up`'s bundle step line updated back to an honest "verified" (they were tempered in commit `949dca6` — restore the strong claim only after the code earns it).
- **Extraction caps** in `extractTarGz`: per-file `io.LimitReader` and a total-bytes cap (constant, generous — e.g. 16 GiB total, 4 GiB per file), exceeded → `CUBE-7003` wrap. Same for the plugin-index style consistency.
- **Tests:** tamper each surface (flip a byte in a pack file, in an image tar, truncate) → `CUBE-7004` naming it; v1-bundle rejection; cap enforcement with a small synthetic limit override (test seam, not exported config).

### 5.3 Task R3 — Event-stream migration

**Purpose:** complete the UX design's stage plan — `--progress=json` covers every command.

- Migrate `cube-idp vendor`, `sync` (one-shot summary path), `repo create`, `plugin list/trust/install`, `pack push` from direct `ui.Printer` calls onto the typed event stream (`internal/ui/event` + `ui.RunPipeline`), exactly as `up`/`down` were migrated in 14b: Console facade, plain projection byte-identical to today's lines (their tests assert bytes — the migration must keep them green, adapting only call plumbing).
- **Live view:** only `vendor` gains the LiveRenderer step-tree (it is long-running: per-pack + per-image progress events). `sync --watch` keeps its current loop (its Printer routing was fixed in `949dca6`); converting watch to a live single-pane view stays deferred (post-public, per the ratified UX decision it may be revisited with usage data).
- **JSON:** every migrated command emits the `"v":1` event stream under `--progress=json`; `docs/machine-readable-output.md` updated in the SAME commits (the final review praised it for being accurate because written against code — keep that property).
- **Tests:** per-command golden event-slice tests (plain projection byte-pinned, JSON one-event-per-line), mirroring 14b's pattern.

### 5.4 Task R4 — Diag taxonomy sweep

All from the final review's Minor list, one task:

- `CUBE-1003` (`CodeClusterSetupFailed`): its only use is a config-load error (`internal/config/load.go:99` area) — re-scope comment + rename constant if the use is genuinely config (verify at plan time), drop its stale `(RECONCILE: Task 0 use unclear)` comment.
- `CUBE-7002` reuse for the consume-side node-load in kindp/k3dp: allocate a dedicated load-side code (7xxx family) and migrate the two call sites; remediation stays the post-`949dca6` transient-aware wording.
- `CUBE-3007` Poke overload (non-NotFound Get/Update failures reuse the "target missing" code): allocate a transient-engine-IO code or rewrap with an existing engine code; distinguishing summaries already exist.
- `internal/diag/diag.go` header range list: extend to cover 6xxx/7xxx/8xxx.
- Ban-test hole: extend `TestNoCubeLiteralsOutsideCatalog` to catch backtick raw-string literals (`` `CUBE-`` ) too.
- New exhaustiveness test: every `Code` constant used somewhere in non-test code OR explicitly annotated reserved (CUBE-3006 precedent), and every used code defined — used⊆defined∧defined⊆(used∪reserved).

### 5.5 Task R5 — Plugin polish

- Trust-store keys canonicalized via `filepath.Abs`+`EvalSymlinks` on record AND lookup (today a relative PATH entry yields cwd-dependent keys; fail-safe but re-prompts).
- `http.Client` with a timeout (60s) for index archive downloads (today only ctx-bound; cobra ctx is un-deadlined).
- Plugin name charset guard (`^[a-z0-9][a-z0-9-]*$`) on `plugin install`/`trust` args — closes the `../`-shaped-name path escape (self-inflicted only, still).
- Document the flag-before-plugin-name limitation (`cube-idp --plain myplugin` doesn't dispatch) in the README plugin section.

### 5.6 Task R6 — D15 kustomize substitution

`internal/pack/render.go` kustomize path (`RenderDir`) gets the same `${GATEWAY_HOST}/${GATEWAY_FQDN}/${GATEWAY_PACK}` byte-level substitution the manifests path has (pre-parse), with tests mirroring `TestRenderForSubstitutesGatewayHost` on a kustomize fixture. Packs without tokens render byte-identically (existing kustomize tests stay green untouched). This closes the documented D15 asymmetry rather than erroring on tokens — full substitution chosen because the mechanism is small and already proven on two other paths.

### 5.7 Task R7 — Gateway coherence (the one design-bearing task; design fixed HERE)

Two related changes:

**(a) De-fang the pack/ref trap non-breakingly** (F11 hardened it with CUBE-0008; this removes the foot-gun at the source):
- `cube-idp init` (both wizard and flags) writes **exactly one** gateway source: `--local` mode writes `gateway.ref` pointing at `packs/<chosen-pack>` AND `gateway.pack: <chosen-pack>` consistently derived from the same choice (never a ref for pack A with pack name B); published mode writes `gateway.pack` only.
- `gateway.ref` remains optional and valid (schema unchanged, P4-D3); CUBE-0008 validation stays as the backstop.
- README documents the precedence explicitly.

**(b) Close the envoy CoreDNS in-cluster gap** (KNOWN GAP in `packs/envoy-gateway/chart.yaml` since F9):
- Root understanding (from F9's live diagnosis): the CoreDNS rewrite targets the hardcoded `<gateway.pack>.<gateway.pack>.svc.cluster.local`; for envoy that's the CONTROLLER service, not the data plane, so in-cluster `*.<host>` clients bypass the proxy. The data-plane service can be stably named — the F9 hijack happened because the pack named it identically to the controller's service (`envoy-gateway`), not because naming is unsafe.
- **Design:** a new OPTIONAL `pack.cue` field for gateway packs — `gatewayService: {name: string, namespace: string}` (D11-style data contract; pack format addition, not cube.yaml). `up`'s CoreDNS rewrite target derives: declared `gatewayService` if present, else today's `<pack>.<pack>.svc` default (traefik unchanged, zero migration).
- The envoy pack then: sets `EnvoyProxy.spec.provider.kubernetes.envoyService.name: cube-idp-gateway` (stable, NON-colliding — verified safe naming mechanism, the NodePort patch is name-agnostic) and declares `gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}` in its pack.cue.
- Plumbing: `pack.Pack` gains the parsed field (like `Expose`/`Images`); `internal/up`'s CoreDNS step consumes it from the resolved gateway pack; validation error (existing 4xxx pack-cue code) on malformed blocks.
- **Proof:** unit tests on parsing/derivation + the envoy e2e smoke extended with one in-cluster curl through `gitea.<host>` resolving via CoreDNS to the data plane (the exact flow that is broken today).
- Remove the KNOWN GAP comment + the README caveat once green.

### 5.8 Task R8 — Hygiene + test hardening

From the final review, verbatim list (file:line references as of `e06890a` — plan reconciles):
- `isLocalRegistryHost` triplication (`internal/pack/source.go:159`, `internal/oci/pushdir.go:213`, `internal/bundle/vendor.go:363`) → one helper in `internal/oci` (or a tiny shared package — implementer picks the cycle-free home), all three call sites migrated, byte-equal behavior test.
- `internal/oci/pushdir.go:118` `time.Now()` created-annotation → content-derived (e.g. fixed epoch or digest-derived) so identical content republishes to an identical digest; CI republish becomes a true no-op. Update the round-trip test to assert digest stability across two pushes.
- Syncer synthesized-pack temp-dir leak (`internal/syncer/syncer.go:141`) → cleanup after render.
- `deployRepo` triple `diag.Wrap` dedup (`cmd/repo.go`) via a small helper.
- `pr` loop-var shadowing (`internal/up/up.go:250` area) rename.
- Test hardening: `TestTrustConsentFallsBackWithoutConfig` gains a positive assertion (generic wording present, not just old-host absent); flux uninstall test's unfiltered `flux-system` list scoped by label (removes the cross-test coupling Task 10a papered with t.Cleanup); the two `tests/e2e/e2e_test.go:329`-area lint items (slices.Contains/FieldsSeq); `hack/inject-argocd-cmd-params.awk` header note about positional fragility on version bumps.

### 5.9 Task R9 — Docs pass

- README: add missing command docs (`config schema`, `down --keep-cluster`, `vendor --lock` — verify the actual missing set at plan time), install section from R1, gateway precedence from R7a, drop the envoy caveat after R7b, prune any stale claims (sweep against `--help` output).
- `docs/machine-readable-output.md`: add the `encode_error` event type (missed in 14c review), re-verify field tables against code post-R3.
- CHANGELOG.md curated per R1.

### 5.10 Task R10 — Cut v0.1.0 (exit gate)

1. Full suite + `make test-engines` (envtest) green at HEAD.
2. Full local e2e sweep: the five Phase 3 arbiters, run per the deferred-commands convention (port 18443).
3. Tag `v0.1.0`, push tag (THIS phase's only push — requires owner go-ahead at execution time), release workflow produces the GitHub Release.
4. **Release smoke from the artifact** (not a local build): `gh release download` into a clean temp dir, checksum verify, then `init → up → status → get secrets → down` against real Docker; `cube-idp version` prints `v0.1.0` + commit.
5. Ledger + plan closed out; findings register updated if the smoke finds anything (fix-wave loop as in Phase 3).

## 6. Testing strategy

- Unit/TDD per task as specified inline; every RED documented before GREEN (Phase 3 convention).
- The e2e matrix is NOT extended this phase except R7b's one in-cluster CoreDNS assertion inside the existing envoy smoke.
- Release pipeline validated three ways: snapshot build in CI on every main push (R1), the tag-triggered real release (R10), and the downloaded-artifact smoke (R10).

## 7. Risks

- **goreleaser on a private repo:** GITHUB_TOKEN Release creation works on private repos, but artifact download requires auth — install docs must use `gh` (authed), never raw URLs. Accepted per P4-D2.
- **Manifest v2 hard-rejects old bundles:** deliberate (ephemeral artifacts); the typed error names the fix.
- **Event-stream migration byte-stability:** five commands' pinned outputs must survive; 14b's facade pattern proved the technique, and the plan will require per-command golden proofs BEFORE behavior changes.
- **EnvoyProxy stable service name:** naming the data-plane service is the same mechanism that caused F9's hijack — safety comes from the non-colliding name; the plan must include the explicit collision check (name ≠ any existing Service in the pack's namespace at render/review time) and the e2e proof.

## 8. Zero-context enablement (what the implementation plan MUST carry)

The plan (written next, via the writing-plans skill) must be executable by an agent with no access to this conversation. It therefore must include:

1. **A Task 0 reconciliation gate**: verify every file:line and seam named in §5 against the then-current tree (Phase 3's Task 0 is the template); all §5 RECONCILE markers resolved before execution.
2. **A ground-truth section** pre-answering: module path; the ui/event API surface (Console, RunPipeline, renderer contracts); the lock pin formats (`oci:sha256:…`, `git+<sha>`, `dir:h1:…`); the bundle Manifest v1 shape; the diag catalog state incl. reserved codes; `cmd/version.go`'s current variable; the goreleaser-relevant repo facts (no CGO, module path, private repo owner `RafPe`).
3. **Environment facts**: Docker available on the dev machine (v29+); local e2e port 18443; protected cluster names (`testowy`, `airbyte*`, `xxx`, `envoy-dbg`); agent worktrees may spawn stale — ALWAYS `git reset --hard main` first and verify a named recent file exists; the ledger lives at `.superpowers/sdd/progress.md` and every task appends to it.
4. **Conventions**: conventional commits + the Claude trailer; TDD steps with exact RED/GREEN commands; per-task review gates (implementer report → spec+quality review → fix loop) if executed subagent-driven; sequential order R1→R10 is BINDING (P4-D6) — no parallel streams.
5. **CUBE-code allocations** declared up front (8xxx for anything release-new; specific 7xxx additions from R4) with the literal-ban reminder.
6. **The R10 push gate**: tagging/pushing requires explicit owner confirmation at execution time — the plan must stop there, not assume.

## 9. Success criteria

v0.1.0 exists as a GitHub Release with four checksummed artifacts; the downloaded binary passes the clean-environment smoke; the Phase 3 backlog register shows zero FOLLOW-UP items open; `--progress=json` works on every command; a tampered bundle cannot pass `Verify`; envoy's in-cluster `*.<host>` path works; no cube.yaml written by any prior phase breaks.
