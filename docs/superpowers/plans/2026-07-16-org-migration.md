# cube-idp Org Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the project from `RafPe/cube-idp` (personal account) to the `cube-idp` GitHub org: repo + fork transfer, Go module rename, GHCR namespace change, pack re-publish, and a v0.1.1 release proving the org pipeline.

**Architecture:** Two GitHub transfers happen first (fork before main repo, so the new module path resolves). Then four mechanical code-change commits land on `main` (module rename → replace directive → GHCR namespace → release surface). Pushing `main` auto-triggers CI and the pack-publish workflow (its own file is in the workflow's `paths:` filter). Finally the old GHCR packages are deleted and `v0.1.1` is tagged.

**Tech Stack:** git, `gh` CLI (GitHub API), macOS `sed -i ''`, Go toolchain from `go.mod`, GoReleaser (runs in CI only), Docker (only to verify a GHCR pull).

**Spec:** `docs/superpowers/specs/2026-07-16-org-migration-design.md`

## Global Constraints

- New module path: `github.com/cube-idp/cube-idp` (exact).
- New fork path: `github.com/cube-idp/go-getter`, version stays `v1.9.0`; import path stays `github.com/hashicorp/go-getter` (fork declares upstream path; the `replace` directive is the mechanism — never `go get` the fork path directly).
- New GHCR namespace: `ghcr.io/cube-idp/packs/<name>` (the redundant `/cube-idp/` segment is dropped).
- Repo stays **private**; user-facing docs say `GOPRIVATE=github.com/cube-idp/*`.
- **Never edit** files under `docs/superpowers/plans/`, `docs/superpowers/specs/` (except this plan's checkboxes), or `.superpowers/` — historical `rafpe` references there are intentional.
- Never hardcode a Go version anywhere; `go.mod` is the single source of truth (repo rule).
- Commit messages follow conventional commits (`feat:`/`fix:`/`docs:`/`ci:`/`chore:`) — GoReleaser's changelog groups on these.
- This is macOS: in-place sed is `sed -i ''` (with the empty string).
- All git pushes go directly to `main` (repo has no PR flow yet).

---

### Task 0: Pre-flight baseline

**Files:** none (verification only)

**Interfaces:**
- Consumes: clean working tree on `main`.
- Produces: a green baseline; `gh` authenticated with scopes needed by later tasks.

- [x] **Step 1: Verify clean tree on main**

Run: `git status --short && git branch --show-current`
Expected: no output from status; branch `main`.

- [x] **Step 2: Verify the test suite is green before touching anything**

Run: `go build ./... && go test ./...`
Expected: build silent; every package `ok` (or `[no test files]`). If anything fails, STOP — fix the baseline first; this plan must not start from red.

- [x] **Step 3: Verify gh auth + org access**

Run: `gh auth status && gh api user --jq .login && gh api orgs/cube-idp --jq .login`
Expected: logged in; `RafPe` (or the account owning the repos); `cube-idp`. A 404 on the org means the account isn't a member — STOP and fix org membership first.

- [x] **Step 4: Verify the org does NOT already have repos with the target names**

Run: `gh repo view cube-idp/cube-idp 2>&1 | head -1; gh repo view cube-idp/go-getter 2>&1 | head -1`
Expected: both print `GraphQL: Could not resolve to a Repository...` (not found). If either exists, STOP — GitHub blocks the transfer; the existing repo must be renamed/deleted by the user first.

---

### Task 1: Transfer the go-getter fork to the org

**Files:** none (GitHub-side). Fork must move FIRST so Task 5's `go mod tidy` can resolve `github.com/cube-idp/go-getter@v1.9.0`.

**Interfaces:**
- Produces: `github.com/cube-idp/go-getter` with tag `v1.9.0` reachable.

- [ ] **Step 1: Transfer via API**

Run: `gh api repos/RafPe/go-getter/transfer -f new_owner=cube-idp`
Expected: HTTP 202-style JSON response. If it errors with permissions, fall back to the browser: RafPe/go-getter → Settings → Danger Zone → Transfer ownership → `cube-idp`, then confirm here before continuing.

- [ ] **Step 2: Verify the transfer landed, fork intact, tag present**

Run: `gh repo view cube-idp/go-getter --json name,isFork,parent --jq '{name,isFork,parent:.parent.nameWithOwner}' && gh api repos/cube-idp/go-getter/git/refs/tags/v1.9.0 --jq .ref`
Expected: `{"name":"go-getter","isFork":true,"parent":"hashicorp/go-getter"}` and `refs/tags/v1.9.0`. (Transfers are async — if 404, wait ~30s and retry.)

---

### Task 2: Transfer the main repo to the org

**Files:** none (GitHub-side).

**Interfaces:**
- Produces: `cube-idp/cube-idp` (private) with issues, tags, the v0.1.0 release + assets, and Actions history; redirect from `RafPe/cube-idp`.

- [ ] **Step 1: Transfer via API**

Run: `gh api repos/RafPe/cube-idp/transfer -f new_owner=cube-idp`
Expected: HTTP 202-style JSON. Browser fallback as in Task 1.

- [ ] **Step 2: Verify repo, visibility, release, and tag all transferred**

Run: `gh repo view cube-idp/cube-idp --json name,visibility --jq '{name,visibility}' && gh release view v0.1.0 -R cube-idp/cube-idp --json tagName,assets --jq '{tag:.tagName,assets:(.assets|length)}'`
Expected: `{"name":"cube-idp","visibility":"PRIVATE"}` and the v0.1.0 release with its asset count (>0).

- [ ] **Step 3: Verify org Actions policy won't break the workflows**

Run: `gh api orgs/cube-idp/actions/permissions --jq '{enabled_repositories,allowed_actions}' && gh api repos/cube-idp/cube-idp/actions/permissions/workflow --jq .default_workflow_permissions`
Expected: Actions enabled for the repo (`all` or the repo selected) and any `default_workflow_permissions` value is fine (`read` is OK — both workflows declare explicit `permissions:` blocks). If Actions are disabled org-wide, STOP and ask the user to enable them in org settings.

---

### Task 3: Point local checkouts at the new origin

**Files:** git config only, in BOTH checkouts:
- `/Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/neocube` (primary)
- `/Users/rafal.pieniazek/github.com/rafpe/neocube` (secondary)

**Interfaces:**
- Produces: `origin` = `https://github.com/cube-idp/cube-idp.git` in both; later tasks push to it.

- [ ] **Step 1: Update the primary checkout's remote**

Run (from the primary checkout):
```bash
git remote set-url origin https://github.com/cube-idp/cube-idp.git
git remote -v && git fetch origin --dry-run
```
Expected: both fetch/push lines show `cube-idp/cube-idp.git`; dry-run fetch succeeds silently (or lists refs).

- [ ] **Step 2: Update the secondary checkout's remote**

Run:
```bash
git -C /Users/rafal.pieniazek/github.com/rafpe/neocube remote set-url origin https://github.com/cube-idp/cube-idp.git
git -C /Users/rafal.pieniazek/github.com/rafpe/neocube remote -v
```
Expected: same URLs. If that path isn't a git checkout, note it and move on.

---

### Task 4: Rename the Go module path

**Files:**
- Modify: `go.mod:1` (module line), all `*.go` files importing `github.com/rafpe/cube-idp` (~160), `.goreleaser.yaml:18-20` (ldflags), `README.md:31-32` (go install / GOPRIVATE lines)

**Interfaces:**
- Consumes: nothing from earlier tasks (pure rename; the old fork replace still resolves).
- Produces: module `github.com/cube-idp/cube-idp` — every later task's build assumes it.

- [ ] **Step 1: Mechanical sweep**

```bash
grep -rl 'github.com/rafpe/cube-idp' --include='*.go' . \
  | xargs sed -i '' 's|github.com/rafpe/cube-idp|github.com/cube-idp/cube-idp|g'
sed -i '' 's|github.com/rafpe/cube-idp|github.com/cube-idp/cube-idp|g' go.mod .goreleaser.yaml README.md
```

- [ ] **Step 2: Fix the README GOPRIVATE guidance to the org wildcard**

In `README.md` (~line 32), the sed above produced `GOPRIVATE=github.com/cube-idp/cube-idp`. Edit it to the wildcard so it also covers any future private org module:

Old: `` private unless you set `GOPRIVATE=github.com/cube-idp/cube-idp` and have git ``
New: `` private unless you set `GOPRIVATE=github.com/cube-idp/*` and have git ``

- [ ] **Step 3: Verify no old module references remain outside history**

Run: `go mod tidy && grep -rn "github.com/rafpe/cube-idp" --exclude-dir=.git --exclude-dir=docs . ; echo "exit=$?"`
Expected: `go mod tidy` silent; grep prints nothing, `exit=1`.

- [ ] **Step 4: Build + full test suite**

Run: `go build ./... && go test ./...`
Expected: all `ok`. The rename is import-path-only; any failure means a missed file — re-run the Step 3 grep.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: rename module to github.com/cube-idp/cube-idp (org migration)"
```

---

### Task 5: Repoint the go-getter replace directive at the org fork

**Files:**
- Modify: `go.mod:307` (replace line), `go.sum` (via tidy), `internal/pack/getter.go:10` and `internal/pack/getter.go:101` (comments naming the fork)

**Interfaces:**
- Consumes: Task 1 (fork lives at `cube-idp/go-getter` with tag `v1.9.0`).
- Produces: `replace github.com/hashicorp/go-getter => github.com/cube-idp/go-getter v1.9.0`.

- [ ] **Step 1: Edit the replace directive**

In `go.mod`, change:
Old: `replace github.com/hashicorp/go-getter => github.com/rafpe/go-getter v1.9.0`
New: `replace github.com/hashicorp/go-getter => github.com/cube-idp/go-getter v1.9.0`

- [ ] **Step 2: Update the two fork-naming comments**

`internal/pack/getter.go:10` —
Old: `	getter "github.com/hashicorp/go-getter" // RafPe fork via replace (go.mod)`
New: `	getter "github.com/hashicorp/go-getter" // cube-idp fork via replace (go.mod)`

`internal/pack/getter.go:101` —
Old: `// RECONCILE (verified against github.com/rafpe/go-getter v1.9.0 client.go):`
New: `// RECONCILE (verified against github.com/cube-idp/go-getter v1.9.0 client.go):`

- [ ] **Step 3: Re-resolve and verify go.sum swapped fork entries**

Run: `go mod tidy && grep "go-getter" go.sum`
Expected: tidy fetches `github.com/cube-idp/go-getter v1.9.0` (fork is public — a fork of a public repo — so no auth needed); go.sum shows `github.com/cube-idp/go-getter v1.9.0 h1:...` lines and NO `rafpe/go-getter` lines. If the proxy 404s on the fresh path, retry with `GOPRIVATE=github.com/cube-idp/* go mod tidy` (direct git fetch).

- [ ] **Step 4: Build + tests (the oci:// getter is the risk surface)**

Run: `go build ./... && go test ./internal/pack/... ./... 2>&1 | tail -20`
Expected: all `ok`.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/pack/getter.go
git commit -m "chore: consume go-getter fork from cube-idp org"
```

---

### Task 6: Switch the GHCR pack namespace

**Files:**
- Modify (exact occurrences of `ghcr.io/rafpe/cube-idp/packs` → `ghcr.io/cube-idp/packs`):
  - `internal/config/types.go:135-136` (default profile refs)
  - `cmd/init.go:93` (non-wizard default pack)
  - `cmd/init_test.go:33,82`
  - `internal/config/load_test.go:197`
  - `internal/up/up_test.go:123,125,139,173,176,212,213`
  - `cube.yaml:17-18` (this repo's own dev config)
  - `README.md:73,476,485,487`
  - `.github/workflows/release-packs.yaml:29` (the `NS=` line)

**Interfaces:**
- Consumes: nothing (string change; the new refs only resolve after Task 8 publishes).
- Produces: default pack refs `oci://ghcr.io/cube-idp/packs/{gitea,argocd}:0.1.0`; workflow publishes to `ghcr.io/cube-idp/packs/<name>`.

- [ ] **Step 1: Mechanical sweep (code + tests change together, so tests stay green)**

```bash
sed -i '' 's|ghcr.io/rafpe/cube-idp/packs|ghcr.io/cube-idp/packs|g' \
  internal/config/types.go cmd/init.go cmd/init_test.go \
  internal/config/load_test.go internal/up/up_test.go \
  cube.yaml README.md .github/workflows/release-packs.yaml
```

- [ ] **Step 2: Refresh the stale comment on the workflow NS line**

`.github/workflows/release-packs.yaml:29` —
Old: `          NS="ghcr.io/cube-idp/packs"   # Owner Decisions #1`
New: `          NS="ghcr.io/cube-idp/packs"   # org namespace — spec docs/superpowers/specs/2026-07-16-org-migration-design.md`

- [ ] **Step 3: Verify zero stragglers and green tests**

Run: `grep -rn "ghcr.io/rafpe" --exclude-dir=.git --exclude-dir=docs . ; echo "exit=$?" && go test ./cmd/... ./internal/config/... ./internal/up/...`
Expected: grep empty with `exit=1`; the three package groups `ok`.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: publish and consume packs under ghcr.io/cube-idp/packs"
```

---

### Task 7: Release surface — GoReleaser owner + README install commands

**Files:**
- Modify: `.goreleaser.yaml:56` (`owner:`), `README.md:21,24,26` (`RafPe/cube-idp` → `cube-idp/cube-idp`)

**Interfaces:**
- Consumes: Task 2 (repo lives in the org, so `owner: cube-idp` is valid).
- Produces: GoReleaser publishes releases to `cube-idp/cube-idp`; README download commands point there. Task 9 relies on this.

- [ ] **Step 1: GoReleaser owner**

`.goreleaser.yaml:56` —
Old: `    owner: RafPe`
New: `    owner: cube-idp`

- [ ] **Step 2: README release/download references**

```bash
sed -i '' 's|RafPe/cube-idp|cube-idp/cube-idp|g' README.md
```
Affects lines 21 (authenticate note), 24 and 26 (`gh release download ... -R ...`).

- [ ] **Step 3: Verify no RafPe remains outside history, and goreleaser config parses**

Run: `grep -rn "RafPe" --exclude-dir=.git --exclude-dir=docs . ; echo "exit=$?" && (command -v goreleaser >/dev/null && goreleaser check || echo "goreleaser not installed locally — CI validates it in Task 9")`
Expected: grep empty, `exit=1`; `goreleaser check` prints `1 configuration file(s) validated` (or the skip note).

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yaml README.md
git commit -m "ci: release under the cube-idp org"
```

---

### Task 8: Push, publish packs under the org, delete old packages

**Files:** none (push + GitHub-side operations).

**Interfaces:**
- Consumes: Tasks 3-7 (remote + all four commits). Pushing `main` triggers `ci` (on push) AND `release-packs` (its `paths:` filter includes `.github/workflows/release-packs.yaml`, which Task 6 modified).
- Produces: `ghcr.io/cube-idp/packs/{argocd,backstage,cert-manager,envoy-gateway,external-secrets,gitea,traefik}` at `0.1.0` + `latest`; old `ghcr.io/rafpe/...` packages gone.

- [ ] **Step 1: Push main**

Run: `git push origin main`
Expected: pushed to `cube-idp/cube-idp`.

- [ ] **Step 2: Watch both workflow runs to completion**

Run: `gh run list -R cube-idp/cube-idp --limit 4` then `gh run watch -R cube-idp/cube-idp <run-id>` for the `ci` and `release-packs` runs.
Expected: both conclude `success`. If `release-packs` did not trigger, run `gh workflow run release-packs -R cube-idp/cube-idp && gh run watch -R cube-idp/cube-idp` (it has `workflow_dispatch`). If it fails on `docker/login-action` or push denied: the org's package-creation permission for `GITHUB_TOKEN` is restricted — surface this to the user (org Settings → Packages), don't work around it with a PAT silently.

- [ ] **Step 3: Verify all seven packages exist under the org and are private + repo-linked**

Run: `gh api "/orgs/cube-idp/packages?package_type=container" --paginate --jq '.[] | {name, visibility, repo: .repository.name}'`
Expected: seven entries `packs/argocd` … `packs/traefik`, each `"visibility":"private"`, `"repo":"cube-idp"`.

- [ ] **Step 4: Verify a pull works with registry auth**

```bash
gh auth token | docker login ghcr.io -u "$(gh api user --jq .login)" --password-stdin
docker manifest inspect ghcr.io/cube-idp/packs/gitea:0.1.0 | head -5
```
Expected: login succeeded; a JSON manifest (mediaType line). This is the same auth path `cube up` users need for private packs — unchanged behavior, new address.

- [ ] **Step 5: Delete the old rafpe-namespace packages**

Deletion needs an extra scope, then delete each of the seven (URL-encode `/` as `%2F`; old package names include the repo segment):
```bash
gh auth refresh -h github.com -s delete:packages,read:packages
gh api "/user/packages?package_type=container" --paginate --jq '.[].name'   # confirm the exact old names first
for p in argocd backstage cert-manager envoy-gateway external-secrets gitea traefik; do
  gh api -X DELETE "/user/packages/container/cube-idp%2Fpacks%2F${p}"
done
```
Expected: the list shows `cube-idp/packs/<name>` entries before; DELETEs return empty (204); re-running the list afterwards shows none of them. **Destructive step:** run the list first, delete only names matching `cube-idp/packs/*`.

---

### Task 9: Cut v0.1.1 from the org

**Files:**
- Modify: `README.md:24,26,31` (pin `v0.1.0` → `v0.1.1` in the install commands) — must land BEFORE tagging so the tagged tree is clean and self-consistent.

**Interfaces:**
- Consumes: Task 7 (`owner: cube-idp`), Task 8 (workflows proven under org).
- Produces: GitHub release `v0.1.1` on `cube-idp/cube-idp` with binaries whose ldflags match the new module path.

- [ ] **Step 1: Bump the README's pinned release version**

```bash
sed -i '' 's|v0\.1\.0|v0.1.1|g' README.md
grep -n "v0.1" README.md
```
Expected: the `gh release download` lines and the `go install github.com/cube-idp/cube-idp@v0.1.1` line show v0.1.1; review the grep output — if any hit is a historical mention rather than an install instruction, revert that one line.

- [ ] **Step 2: Commit and push**

```bash
git add README.md
git commit -m "docs: point install instructions at v0.1.1"
git push origin main
```

- [ ] **Step 3: Tag and push the tag**

```bash
git tag v0.1.1
git push origin v0.1.1
```
Expected: the `release` workflow starts (`on: push: tags: ["v*"]`).

- [ ] **Step 4: Watch the release run**

Run: `gh run watch -R cube-idp/cube-idp $(gh run list -R cube-idp/cube-idp --workflow=release --limit 1 --json databaseId --jq '.[0].databaseId')`
Expected: `success`. GoReleaser publishes to `cube-idp/cube-idp` (Task 7's owner change is what this proves).

- [ ] **Step 5: Verify the release artifacts and the stamped version**

```bash
cd "$(mktemp -d)"
gh release download v0.1.1 -R cube-idp/cube-idp -p "cube-idp_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz"
tar xzf cube-idp_*.tar.gz && ./cube-idp version
```
Expected: version output reports `0.1.1` (+ commit + date). A `dev` version means the ldflags module path didn't match — Task 4 missed `.goreleaser.yaml`; fix and re-tag as v0.1.2 rather than force-moving the tag.

---

### Task 10: Final acceptance sweep

**Files:** none (verification only).

**Interfaces:**
- Consumes: everything above.
- Produces: the spec's acceptance criteria, checked.

- [ ] **Step 1: Reference sweep — old names live only in history**

Run: `grep -rIn "rafpe" -i --exclude-dir=.git . | grep -v "^./docs/superpowers" | grep -v "^docs/superpowers"`
Expected: zero lines. (Case-insensitive, so it covers `RafPe` too. `docs/superpowers/**` hits are the allowed historical record.)

- [ ] **Step 2: Full build + test, one last time**

Run: `go build ./... && go test ./...`
Expected: all `ok`.

- [ ] **Step 3: Default-config smoke — new refs come out of `init`'s defaults**

Run: `go test ./cmd/ -run TestInit -v 2>&1 | tail -15 && go test ./internal/config/ -run TestDefault -v 2>&1 | tail -10`
Expected: PASS — these tests assert the `oci://ghcr.io/cube-idp/packs/...` defaults directly.

- [ ] **Step 4 (optional, user's call): local e2e**

Run: `CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -v -timeout 30m`
(18443 dodges the `testowy` kind cluster squatting 8443.) Requires Docker + registry auth from Task 8 Step 4 for the private packs. Skip if the user doesn't want a full cluster spin-up today.

- [ ] **Step 5: Update the SDD ledger**

Append a dated entry to `.superpowers/sdd/progress.md` noting: org migration executed (spec `2026-07-16-org-migration-design.md`), repo + fork transferred, module renamed, packs republished under `ghcr.io/cube-idp/packs`, old packages deleted, v0.1.1 released. Commit:
```bash
git add .superpowers/sdd/progress.md
git commit -m "docs: ledger — org migration to cube-idp executed"
git push origin main
```
