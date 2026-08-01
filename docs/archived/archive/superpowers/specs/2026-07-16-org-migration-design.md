# cube-idp: Migration to the `cube-idp` GitHub Organization

**Date:** 2026-07-16
**Status:** Approved design, pending implementation plan
**Context:** v0.1.0 shipped 2026-07-15 from `RafPe/cube-idp` (private). A new
GitHub org `cube-idp` now exists; the project moves there before Phase 5 work
(F12) begins. The Phase 3 spec anticipated this: the GHCR namespace decision
was recorded as "org migration is a one-line ref change".

## Decisions

| # | Decision | Choice |
|---|----------|--------|
| 1 | How the repo moves | **GitHub Transfer ownership** of `RafPe/cube-idp` → `cube-idp` org (keeps issues, releases, tags, Actions history; old URLs redirect) |
| 2 | Go module path | **Rename now** to `github.com/cube-idp/cube-idp` (no external importers exist yet; the redirect keeps `go get` on the old path working regardless) |
| 3 | go-getter fork | **Transfer `RafPe/go-getter` to the org too** and update the `replace` directive to `github.com/cube-idp/go-getter v1.9.0` |
| 4 | Published packs | **Re-publish the same 0.1.0 content** under `ghcr.io/cube-idp/packs/<name>`, then **delete** the `ghcr.io/rafpe/...` packages (no external users pin them) |
| 5 | CLI release | **Cut v0.1.1** immediately after migration to prove the org release pipeline and ship binaries that self-identify with the new module path |
| 6 | Visibility | **Stay private.** Going public is a separate, later decision. `GOPRIVATE` docs are updated, not removed. |

New GHCR namespace drops the redundant repo segment:
`ghcr.io/rafpe/cube-idp/packs/gitea` → `ghcr.io/cube-idp/packs/gitea`.

## Phase A — GitHub-side (manual, no code)

Order matters: the fork moves **first** so the module-rename commit can resolve
`github.com/cube-idp/go-getter@v1.9.0` during `go mod tidy`.

1. Transfer `RafPe/go-getter` → `cube-idp` org. The fork relationship and the
   `v1.9.0` tag travel with the transfer; the redirect keeps the existing
   `replace` working until step B2 updates it.
2. Transfer `RafPe/cube-idp` → `cube-idp` org. Issues, the v0.1.0 release and
   its assets, tags, and Actions history all transfer. Old git URLs redirect.
3. Org checks: Actions enabled; org `GITHUB_TOKEN` policy must permit the
   `contents: write` / `packages: write` the workflows declare (both workflows
   carry explicit `permissions:` blocks, so restrictive org defaults are fine).
   No custom Actions secrets exist (verified 2026-07-16 — both workflows use
   only `secrets.GITHUB_TOKEN`), so nothing to recreate.
4. Update `origin` in both local checkouts (the Dropbox checkout and
   `~/github.com/rafpe/neocube`):
   `git remote set-url origin https://github.com/cube-idp/cube-idp.git`.
   Redirects would mask a stale remote until someone recreates
   `rafpe/cube-idp`, so set it explicitly.

**Preconditions:** the org must not already contain repos named `cube-idp` or
`go-getter`, and the transferring account needs repo-creation rights in the org.

## Phase B — code changes (one commit series in the transferred repo)

1. **Module rename.** `go.mod` module line plus mechanical replacement of
   `github.com/rafpe/cube-idp` → `github.com/cube-idp/cube-idp` across all Go
   imports (~160 files) and the three goreleaser ldflags
   (`.goreleaser.yaml:18-20`). Then `go mod tidy`, `go build ./...`, full
   `go test ./...`.
2. **go-getter replace.** `go.mod:307` becomes
   `replace github.com/hashicorp/go-getter => github.com/cube-idp/go-getter v1.9.0`.
   The fork's declared module path stays `github.com/hashicorp/go-getter`, so
   the replace mechanism is unchanged. Also update the fork-path comment at
   `internal/pack/getter.go:101`. Expect `go.sum` churn for the fork path;
   private modules are outside the sumdb, so `GOPRIVATE` covers it.
3. **GHCR namespace** `ghcr.io/rafpe/cube-idp/packs` → `ghcr.io/cube-idp/packs`
   in:
   - `NS=` in `.github/workflows/release-packs.yaml:29`
   - default profile refs in `internal/config/types.go:135-136` and
     `cmd/init.go:93`
   - the tests asserting those refs (`cmd/init_test.go`,
     `internal/config/load_test.go`, `internal/up/up_test.go`, and any others
     `grep` surfaces)
   - the repo's own `cube.yaml:17-18`
   - README pack-ref examples
4. **GoReleaser owner.** `.goreleaser.yaml:56` `owner: RafPe` → `owner: cube-idp`.
5. **README private-repo docs.** `gh release download -R cube-idp/cube-idp`,
   `GOPRIVATE=github.com/cube-idp/*` (the wildcard now also covers the fork),
   `go install github.com/cube-idp/cube-idp@...`.
6. **Historical docs stay untouched.** Plans/specs under `docs/superpowers/`
   and the SDD ledger (`.superpowers/sdd/progress.md`) are dated records;
   rewriting them would falsify history. Acceptance: after this phase,
   `grep -rI rafpe --exclude-dir=.git` hits only `docs/superpowers/**` (and
   this spec's Context section).

## Phase C — publish and verify

1. Run **release-packs** via `workflow_dispatch` → publishes every
   `packs/*/` at its `pack.cue` version (all currently 0.1.0) plus `latest`
   under `ghcr.io/cube-idp/packs`. Packages created by `GITHUB_TOKEN` are
   auto-linked to the repo and stay private.
2. **Delete** the old `ghcr.io/rafpe/cube-idp/packs/*` packages (GitHub UI or
   `gh api`).
3. **Tag `v0.1.1`** → release workflow runs goreleaser under the org. Verify
   `gh release download -R cube-idp/cube-idp` works and `cube version` reports
   the new module path.
4. **Smoke:** `cube init` emits `ghcr.io/cube-idp/packs/...` refs; local e2e
   passes (`CUBE_IDP_E2E_GATEWAY_PORT=18443` locally to dodge the kind-cluster
   port squat).

## Risks and accepted trade-offs

- **GHCR does not redirect.** Any existing `cube.yaml`/lock pinning
  `ghcr.io/rafpe/...` breaks once the old packages are deleted. Accepted:
  v0.1.0 is one day old with no external users.
- **The old module path dies for importers** (none exist). The GitHub redirect
  keeps `git`-level access on the old URL working.
- **Stale remotes elsewhere** (other machines, CI) keep silently working via
  redirect — until the old name is reused. Mitigation: update every known
  checkout in Phase A step 4.
- **Transfer edge case:** GitHub blocks transferring a fork into an org that
  already has a fork in the same network. Not expected here; if hit, the
  fallback is a fresh org fork of `hashicorp/go-getter` plus cherry-pick and
  re-tag of `v1.9.0`.

## Out of scope

- Going public (separate decision, later).
- Upstreaming or vendoring the go-getter `oci://` getter (tracked as a
  possible Phase 5+ item).
- Renaming the local `neocube` folder(s); folder names carry no references.
