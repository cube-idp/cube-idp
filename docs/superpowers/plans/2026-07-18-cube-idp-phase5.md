# cube-idp Phase 5 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **This file is the persistent ledger** — see "Agent Execution Protocol" below. The dispatch prompt lives in [2026-07-18-phase5-agent-prompt.md](2026-07-18-phase5-agent-prompt.md).

**Goal:** Deliver Phase 5 of
[docs/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md](../specs/2026-07-18-cube-idp-phase5-roadmap-design.md):
standalone binary via a public attested packs monorepo (closes F12), visible
provisioning, opt-in HTTP gateway port, `engine.tuning` typed knobs, remote
pack catalog, per-pack Gitea delivery, and hub/spoke registration — as
independently dispatchable, idempotent tasks in three parallel lanes plus a
pack-authoring template.

**Architecture:** Every task plugs into existing seams and adds no new
subsystem: spokes reuse `internal/cluster` providers + `internal/apply`;
provider logs reuse the W1 `StepLog` event vocabulary via `ui.Console.Log`;
`engine.tuning` patches the embedded engine manifests in memory before SSA
(no helm exists in the engine path); Gitea delivery reuses
`internal/syncer.SyncOnce` + `engine.DeliverGit`; provenance uses
GitHub-native artifact attestations generated in CI (keyless — no key
custody, no crypto code in the binary at all).

**Tech Stack:** Go (stdlib + existing pinned deps ONLY — `go.mod` gains no
new module in any task), kind/k3d libraries already vendored, GitHub
artifact attestations in the packs-repo CI (keyless — no signing keys
anywhere), GitHub Actions in the packs repo.

---

## Agent Execution Protocol (normative — read fully before any work)

Every task is executed by one dispatched agent in an **isolated git
worktree** on a **task-specific feature branch**, with this plan file as the
**shared ledger** committed on `main`. Any agent must be able to pick up a
task cold, with no context beyond this file, the spec, and git history.

**Repo root** (contains `go.mod` and `cube.yaml`):
`/Users/rafal.pieniazek/Library/CloudStorage/Dropbox/github.com/rafpe/neocube`
— referred to as `$ROOT`.

**Packs repo root** (exists only after P2; created as a SIBLING of $ROOT):
`$ROOT/../cube-idp-packs` — referred to as `$PACKS`. **Plugins repo root**
(exists only after P9; also a sibling): `$ROOT/../cube-idp-plugins` —
referred to as `$PLUGINS`. Tasks marked `[repo: $PACKS]` / `[repo:
$PLUGINS]` do their code work there; the ledger (this file) is STILL
committed in `$ROOT` on `main`.

### Lanes and ordering (differs from the TUI plan — read carefully)

Tasks are grouped into parallel **lanes**: `S` (spokes), `U` (CLI UX), `P`
(pack + plugin platform), `A` (pack authoring), and `F` (the single final
coherence gate — claimable only when every S, U, and P task is DONE).
Lanes are independent: an S task never waits for a U or P task. **Within a lane tasks are strictly serial.**
Every task's header lists `Depends:` — ALL its dependencies must be `DONE`
or `DONE_WITH_CONCERNS` before claiming, and they are always within the
task's own lane except where the header says otherwise (A tasks depend on
P3).

**Claim rule:** you are dispatched FOR A LANE (or a specific task). Claim
the first task in that lane whose STATUS is `UNCLAIMED` and whose Depends
are all DONE. Two agents may hold IN_PROGRESS tasks simultaneously only if
they are in different lanes.

**Merge-conflict doctrine:** lanes touch disjoint files by design; the
deliberate shared files are `internal/config/types.go` /
`internal/config/schema.cue` (S1, U2, U3, P7 all add fields),
`internal/diag/codes.go` + `internal/diag/registry.go` (most tasks append
codes), and `internal/pack/manifests/pack-crd.yaml` printer columns + the
D11 record-writer fields in `internal/up/up.go` (U4 CUSTOMIZED, P7
DELIVERY). These are APPEND-ONLY additions — on merge conflict, take both
sides, run the task-level gate, and note it in FINDINGS. Any other
conflict: STOP, set BLOCKED.

### Branch & worktree naming (mandatory)

- Branch: `p5/<task-id>-<slug>` — e.g. `p5/s1-spoke-config`. Task id and
  slug come from the Task Index verbatim.
- Worktree: `$ROOT/.claude/worktrees/<task-id>-<slug>` (`.claude/worktrees/`
  is gitignored). `[repo: $PACKS]` tasks use
  `$PACKS/.claude/worktrees/<task-id>-<slug>` and branch in $PACKS.

### Task lifecycle (mandatory, in this order)

1. **Identify your task** per the Claim rule above. Cross-check
   `git log --oneline -20` (in the repo the task targets): if the task's
   work already exists, do NOT redo it — fill the Outcome block from the
   evidence, tick the boxes, commit the ledger, report DONE.
2. **Claim it (before any code).** In `$ROOT` on `main` with a clean tree:
   edit ONLY your task's `STATUS:` line to
   `IN_PROGRESS(<agent-or-session-id>, <UTC timestamp>)`, then:
   `git add docs/superpowers/plans/2026-07-18-cube-idp-phase5.md && git commit -m "docs: p5 plan — claim <TASK-ID>"`.
   If STATUS is already `IN_PROGRESS` with a timestamp under 24h old, STOP
   and report — another agent owns it.
3. **Create the worktree** from the target repo's current `main`:
   `git -C $ROOT worktree add $ROOT/.claude/worktrees/<slug> -b p5/<task-id>-<slug> main`
   (substitute $PACKS for `[repo: $PACKS]` tasks). Never edit code in the
   main checkout; never edit this plan file from a worktree.
4. **Execute the steps in order** — TDD as written: failing test → verify
   fail → implement → verify pass → commit with the exact message given,
   ending every commit with the trailer
   `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`. Run every
   verification command and compare against its "Expected" line. Where a
   step says VERIFY-API, the named symbol may drift from the pinned
   library's docs — check with `go doc <pkg>` first, use the real name, and
   record it in FINDINGS. Never guess, never bump a dependency.
5. **Task-level gate** — inside the worktree:
   `go build ./... && go vet ./... && go test ./...` — all pass. Tasks
   touching `cmd/` or `internal/ui/` additionally run
   `go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence'`
   — the frozen-output fences MUST stay green.
6. **Merge back** (only when green): in the target repo root, clean tree,
   on `main`:
   `git merge --no-ff p5/<task-id>-<slug> -m "merge: p5 <TASK-ID> <slug> (p5/<task-id>-<slug>)"`,
   post-merge `go test ./...`, then `git worktree remove <path>`. Do NOT
   push. Do NOT delete the branch.
7. **Close the ledger** in `$ROOT` on `main`: tick YOUR checkboxes, fill
   the complete Outcome block, then:
   `git add docs/superpowers/plans/2026-07-18-cube-idp-phase5.md && git commit -m "docs: p5 plan — <TASK-ID> complete"`.
   Also append one line to `.superpowers/sdd/progress.md` (local ledger, if
   present — it is gitignored).
8. **Report:** STATUS / Task / Branch (merged: yes/no) / Commits / Evidence
   (key commands + actual output) / Handoff.

### Outcome block rules

Statuses: `UNCLAIMED` · `IN_PROGRESS(<who>, <UTC ts>)` (stale after 24h) ·
`DONE` · `DONE_WITH_CONCERNS` (FINDINGS needs owner) · `BLOCKED` (worktree
and branch left in place; BLOCKERS says what failed, its output, diagnosis).
FINDINGS records every deviation (API drift, renamed symbols, different
insertion points), every decision, everything the next agent needs. Never
leave a field `—` on a non-UNCLAIMED status: write `none`.

### Owner gates

Steps marked **⚠ OWNER GATE** perform outward-facing or destructive
actions (creating the public GitHub repo, uploading signing keys, pushing).
STOP at such a step, report NEEDS_CONTEXT with exactly what you would run,
and wait — unless the dispatch prompt explicitly pre-authorized that gate.

---

## Ground truth (pre-answered decisions — spec §8 and beyond)

Binding for every task. Items marked ⭑ are new decisions taken by this
plan with the simplest viable default — PENDING OWNER RATIFICATION; record
any owner override in FINDINGS of the affected task and update this block.
(GT5, GT6, GT7 and the GT10 mechanism were ratified by the owner on
2026-07-18.)

- GT1 (ratified) Engine knobs live at **`spec.engine.tuning`** — renamed
  from `engine.values` per GT15's vocabulary stone: the word *values* is
  reserved for helm. v1 knobs: **`components.<name>.replicas` and
  `components.<name>.resources` only**, no args escape hatch; unknown
  component → typed error listing valid names. Tuning entries are patches
  over the pre-rendered embedded engine manifests — there is no chart at
  up-time to merge anything into (see U3's rationale).
- GT2 Spokes support **both engines from day 1** (each is one hub Secret).
- GT3 **OpenChoreo is out of Phase 5 entirely** (owner, 2026-07-18): no
  spike tasks run; the research branch `spike/openchoreo-plan` stays
  parked, and `kgateway`/`openbao` are parked with it. The CNOE stacks
  harvest via `cnoe import` remains sanctioned (spec §3 Spike) — it is
  dispatched ad hoc, not a numbered task of this plan.
- GT4 Spoke bootstrap naming: namespace **`cube-idp-system`**, SA
  **`cube-idp-<engine>`** (`cube-idp-flux` / `cube-idp-argocd`), CRB
  **`cube-idp-<engine>-admin`** → ClusterRole `cluster-admin`. Hub-side
  Secret: **`cube-idp-spoke-<name>`** in ns `argocd` (engine argocd,
  labeled `argocd.argoproj.io/secret-type: cluster`) or `flux-system`
  (engine flux, key `value` = kubeconfig).
- GT5 (ratified) Spoke credentials use the **TokenRequest API** (works in envtest,
  no kube-controller-manager needed), `expirationSeconds: 315360000` (10y,
  server may clamp); every `up` re-run re-issues and re-writes the hub
  Secret, so a clamped token self-heals on re-run.
- GT6 (ratified) Spoke cluster providers v1: **`kind` and `existing` only** — k3d
  spokes are deferred (k3d's per-cluster docker network needs a
  shared-network cluster-shape field; kind clusters all join the `kind`
  docker network already). The k3d HUB remains fully supported.
- GT7 (ratified) Spoke kind cluster name: **`<cube-name>-spoke-<spoke-name>`**; its
  API server URL for hub secrets comes from kind's **internal kubeconfig**
  (`provider.KubeConfig(name, true)` → `https://<cluster>-control-plane:6443`).
- GT8 Spoke CUBE codes: new **8xxx range** (“spoke”), first entries
  CUBE-8001…CUBE-8007 as assigned in Lane S tasks. Engine-values error:
  **CUBE-3009**. All must be registered in `internal/diag/registry.go`
  (the completeness fence `TestRegistryCoversEveryDeclaredCode` enforces
  this). No pack-signature codes exist — see GT10.
- GT9 Packs repo: **`github.com/cube-idp/packs`**, local path `$PACKS`
  (sibling `cube-idp-packs`). Pack dirs live at `$PACKS/packs/<name>`.
  Release tags: **`<name>/vX.Y.Z`**. Artifacts:
  `oci://ghcr.io/cube-idp/packs/<name>:X.Y.Z`. Catalog index:
  `oci://ghcr.io/cube-idp/packs/index:latest` (also digest-pinned tags).
- GT10 (ratified — "make this simple") Provenance: **GitHub-native
  artifact attestations** (`actions/attest-build-provenance`, keyless
  OIDC) — zero repo secrets, zero key custody, zero crypto code in the
  binary. CI attests each published pack digest and the index digest.
  Verification is the documented command
  `gh attestation verify oci://ghcr.io/cube-idp/packs/<name>:<ver> --owner cube-idp`
  (contract doc + README). In-binary cryptographic verification is a
  deliberate NON-goal (would need sigstore-go, a new dependency — parked
  in spec §6); the binary's pull integrity rests on **digest pinning**
  (index digests, e2e packs.lock) over TLS.
- GT11 ⭑ HTTP gateway port maps host `spec.gateway.httpPort` →
  **NodePort 30080** (`config.GatewayHTTPNodePort`), which BOTH gateway
  packs already pin in-cluster (`packs/traefik/chart.yaml` ports.web,
  `packs/envoy-gateway/manifests/10-gatewayclass.yaml`). Opt-in: absent
  field = no mapping, no behavior change.
- GT12 Contract doc: `docs/pack-contract-v1.md` in $ROOT (P1); copied
  verbatim into `$PACKS/CONTRACT.md` by P2 — $ROOT's copy is normative
  until the repos merge policy says otherwise.
- GT13 Frozen surfaces (from the TUI plan, still binding): plain
  projection byte-frozen (R1/R2 only), JSONL additive-only, CUBE codes
  append-only, prompt doctrine (flag twin + non-TTY CUBE-0010 refusal),
  `TestModeMatrixFence` and `TestPromptFence*` are merge gates.
- GT14 Local e2e: a squatting kind cluster owns 8443 on the dev machine —
  export `CUBE_IDP_E2E_GATEWAY_PORT=18443` for any local e2e leg. Unit
  tests + envtest are the default gate; live kind/e2e legs run ONLY where
  a step says so.
- GT15 (ratified — the values stone, owner 2026-07-18): **`values:` means
  helm values, only, always** — consumed exclusively by a pack's
  `chart.yaml` render. Setting `values:` on a pack without `chart.yaml`
  is a typed error **CUBE-4016** (raised at render time — pack layout is
  unknowable until the ref is fetched). The uniform extras mechanism is
  **`packs[].extraManifests`** (multi-doc YAML string, valid for EVERY
  pack kind): parsed, `${GATEWAY_*}`-substituted, appended to the pack's
  objects, inventoried; invalid YAML → **CUBE-4017**. A pack installed
  with non-empty `values` or `extraManifests` is **CUSTOMIZED** — recorded
  on its D11 Pack record and shown as a `kubectl get packs` printer
  column. Vocabulary triad: *values → helm render · tuning → engine
  patches · extraManifests → appended objects.* (U4 implements; P1's
  contract doc states it.)
- GT16 (ratified mechanism, owner 2026-07-18): engine self-management is
  **opt-in `spec.engine.selfManage: true`, sourced from zot — never
  Gitea**. `up` pushes the rendered (tuned) engine manifests as
  `oci://<zot>/cube-engine` and attaches an engine-native self-source
  with pruning disabled; the engine reconciles itself from then on.
  Three rules: (1) first install is always direct SSA of the rendered
  manifests; (2) once the self-source exists, later `up`s render → push →
  poke and never SSA; (3) engine unhealthy at `up` start → direct-SSA
  fallback of freshly rendered manifests, then resume. Works with gitea
  absent, offline bundles included. (P8 implements; its four-scenario
  semantics are normative.)
- GT17 (owner, 2026-07-18) Plugins platform mirrors the packs platform:
  repo **`github.com/cube-idp/plugins`** ($PLUGINS sibling
  `cube-idp-plugins`), one dedicated folder per plugin
  (`plugins/<name>/` with source + `plugin.yaml`
  {name, version, description}), tags **`<name>/vX.Y.Z`**. Plugins are
  binaries, so artifacts are **per-platform**:
  `oci://ghcr.io/cube-idp/plugins/<name>:<ver>-<os>-<arch>` (single-layer
  blob artifact; oras CLI in CI only), plus a discovery index
  `oci://ghcr.io/cube-idp/plugins/index:latest` —
  `{schemaVersion: 1, plugins: [{name, version, description, platforms:
  {"<os>-<arch>": {ref, digest}}}]}`. Provenance = GT10 verbatim
  (keyless GitHub attestations per digest, `gh attestation verify
  --owner cube-idp` documented, no in-binary crypto). Binary-side
  integrity = **digest pinning from the index** + the EXISTING sha256
  plugin trust-store consent flow (CUBE-71xx doctrine unchanged).
- GT18 (owner, 2026-07-18) Doctor is a tri-state checklist: EVERY
  registered check renders exactly one row — green `✔` passed (with a
  one-line detail), yellow `⚠` warning (+CUBE code), red `✗` error
  (+CUBE code) — glyph and word always paired (semantic-color doctrine,
  GT13). Exit semantics preserved: exit 1 iff any red. `-o json` gains an
  additive `checks` array. Passing checks are SHOWN, not silent — that is
  the point.
- GT19 (owner, 2026-07-18) Delivery visibility: every pack's D11 record
  carries `delivery: oci|repo` (the record writer maps an empty
  `PackRef.Delivery` to `oci`), and the Pack CRD gains an
  `additionalPrinterColumns` entry **DELIVERY** — repo-delivered packs
  are visible to the operator in `kubectl get packs`, beside U4's
  CUSTOMIZED column. (P7 implements.)

---

## Task Index

| Task | Lane | Repo | Branch | Depends | Delivers |
|------|------|------|--------|---------|----------|
| S1 | S | $ROOT | `p5/s1-spoke-config` | — | `spec.spokes` schema + `spoke add/list/remove` |
| S2 | S | $ROOT | `p5/s2-spoke-bootstrap` | S1 | `internal/spoke` bootstrap: ns/SA/CRB + TokenRequest |
| S3 | S | $ROOT | `p5/s3-spoke-register` | S2 | hub secrets, `up` reconcile, `down` cascade |
| S4 | S | $ROOT | `p5/s4-spoke-status` | S3 | status rows, doctor probes, live `spoke list` |
| U1 | U | $ROOT | `p5/u1-provider-logs` | — | kind/k3d + engine-wait logs → `StepLog` |
| U2 | U | $ROOT | `p5/u2-http-port` | U1 | opt-in `gateway.httpPort` → host mapping of 30080 |
| U3 | U | $ROOT | `p5/u3-engine-tuning` | U2 | `engine.tuning` knobs + `config render-engine` |
| U4 | U | $ROOT | `p5/u4-values-stone` | U3 | values stone: helm-only enforcement, `extraManifests`, CUSTOMIZED column |
| U5 | U | $ROOT | `p5/u5-doctor-checklist` | U4 | doctor tri-state checklist — every check as a green/yellow/red row (GT18) |
| P1 | P | $ROOT | `p5/p1-pack-contract` | — | pack contract v1 doc + conformance test + `description` field |
| P2 | P | $PACKS* | `p5/p2-packs-repo` | P1 | packs repo scaffold, `pack publish`/`pack index`, publish CI, signing CI |
| P3 | P | $PACKS | `p5/p3-conformance` | P2 | per-pack conformance harness (CI + local script) |
| P4 | P | both | `p5/p4-migrate-f12` | P3 | move 7 packs, oci:// gateway default, F12 closed, e2e digest-pinned |
| P5 | P | $PACKS+docs | `p5/p5-pack-attest` | P2 | GitHub attestations in publish CI + `gh attestation verify` docs |
| P6 | P | $ROOT | `p5/p6-remote-catalog` | P2 | index-backed catalog: `pack list --available`, wizard, install |
| P7 | P | $ROOT | `p5/p7-gitea-delivery` | P4 | per-pack `delivery: repo` via SyncOnce + DeliverGit; gitea validation + ordering + readiness gate; DELIVERY column (GT19) |
| P8 | P | $ROOT | `p5/p8-engine-selfmanage` | P7 + U3 | opt-in engine self-management from zot (GT16) |
| P9 | P | $PLUGINS* | `p5/p9-plugins-repo` | P8 | plugins repo scaffold: per-platform artifacts, index, attestations (GT17) |
| P10 | P | $ROOT | `p5/p10-plugin-install` | P9 | `plugin install` from the official index → digest pull → existing trust consent |
| A1–A11 | A | $PACKS | `p5/a<N>-<pack>` | P3 | one new pack each — see Wave A template + parameter table |
| F1 | F | $ROOT | `p5/f1-cli-coherence` | ALL S+U+P | CLI coherence gate: command-tree golden fence, conventions audit, docs sweep |

\* P2 also adds the `pack publish` / `pack index` commands in $ROOT.

---

## Lane S — hub/spoke registration

### S1: `spec.spokes` config + `spoke add/list/remove`  `[repo: $ROOT]`

**Branch:** `p5/s1-spoke-config` · **Depends:** none

**Files:**
- Modify: `internal/config/types.go` (SpokeSpec + Spec.Spokes; near
  ClusterSpec), `internal/config/schema.cue` (spokes block),
  `internal/config/load.go` (cross-validation), `internal/diag/codes.go`
  (+ `internal/diag/registry.go`)
- Create: `cmd/spoke.go`, `cmd/spoke_test.go`
- Test: `internal/config/load_test.go` (extend)

**Interfaces:**
- Produces: `config.SpokeSpec{Name string; Cluster ClusterSpec}`,
  `Spec.Spokes []SpokeSpec` (yaml `spokes,omitempty`);
  `diag.CodeSpokeProviderUnsupported = "CUBE-8001"`. Command surface:
  `cube-idp spoke add <name> [--provider kind|existing] [--context <ctx>] [-f cube.yaml]`,
  `spoke list`, `spoke remove <name> [--delete-cluster] [--yes]`.
- Consumes: existing `config.Load` round-trip validation, `pack install`'s
  config-mutating pattern (`cmd/pack.go`), prompt doctrine seams
  (`ui.PromptsAllowed`/`Confirm`).

- [x] **Step 1: Failing config tests** — append to
  `internal/config/load_test.go`:

```go
func TestSpokesRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
  spokes:
    - name: staging
      cluster: {provider: kind}
    - name: prod-eu
      cluster: {provider: existing, context: eks-prod-eu}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	cube, err := Load(p)
	if err != nil {
		t.Fatalf("valid spokes rejected: %v", err)
	}
	if len(cube.Spec.Spokes) != 2 || cube.Spec.Spokes[0].Name != "staging" {
		t.Fatalf("spokes not decoded: %+v", cube.Spec.Spokes)
	}

	// k3d spokes are deferred (GT6): must fail with CUBE-8001.
	bad := strings.Replace(base, "provider: kind", "provider: k3d", 1)
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(p)
	if err == nil || !strings.Contains(err.Error(), "CUBE-8001") {
		t.Fatalf("k3d spoke must be CUBE-8001, got: %v", err)
	}

	// existing spoke without context must fail (CUBE-8001 family).
	bad2 := strings.Replace(base, "context: eks-prod-eu", "", 1)
	if err := os.WriteFile(p, []byte(bad2), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(p); err == nil {
		t.Fatal("existing spoke without context must be rejected")
	}

	// duplicate spoke names must fail.
	dup := strings.Replace(base, "prod-eu", "staging", 1)
	if err := os.WriteFile(p, []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(p); err == nil {
		t.Fatal("duplicate spoke names must be rejected")
	}
}
```

- [x] **Step 2: Verify fail** — Run:
  `go test ./internal/config/ -run TestSpokes -v`
  Expected: FAIL — `cube.Spec.Spokes` undefined / CUE rejects `spokes`.

- [x] **Step 3: Implement config.** In `internal/config/types.go`, after
  the `PackRef` type:

```go
// SpokeSpec declares a managed spoke cluster (spec §5, Phase 5). cube-idp
// only bootstraps and registers spokes — delivering workloads to them is
// engine content, never packs. Provider is limited to kind|existing in v1
// (GT6); k3d spokes need a shared docker network and are deferred.
type SpokeSpec struct {
	Name    string      `yaml:"name" json:"name"`
	Cluster ClusterSpec `yaml:"cluster" json:"cluster"`
}
```

Add to `Spec` (alongside `Packs`):

```go
	Spokes []SpokeSpec `yaml:"spokes,omitempty" json:"spokes,omitempty"`
```

In `internal/config/schema.cue`, inside `spec:` after `packs?`:

```cue
		spokes?: [...{
			name: =~"^[a-z0-9][a-z0-9-]{0,30}$"
			cluster: {
				provider: *"kind" | "existing"
				context?: string
				kubernetesVersion?: string
			}
		}]
```

In `internal/diag/codes.go`, append a new range block (after 73xx):

```go
// 8xxx: spoke (Phase 5)
const (
	CodeSpokeProviderUnsupported Code = "CUBE-8001" // spoke cluster.provider invalid for spokes (k3d deferred; existing needs context; duplicate name)
)
```

Register CUBE-8001 in `internal/diag/registry.go` following the file's
existing entry format (summary verbatim from the comment above). In
`internal/config/load.go`, extend the existing cross-validation section
(where CUBE-1003 is raised) with:

```go
	seen := map[string]bool{}
	for _, s := range cube.Spec.Spokes {
		if seen[s.Name] {
			return nil, diag.New(diag.CodeSpokeProviderUnsupported,
				fmt.Sprintf("duplicate spoke name %q", s.Name),
				"spoke names must be unique within a cube")
		}
		seen[s.Name] = true
		switch s.Cluster.Provider {
		case "kind":
		case "existing":
			if s.Cluster.Context == "" {
				return nil, diag.New(diag.CodeSpokeProviderUnsupported,
					fmt.Sprintf("spoke %q: provider \"existing\" requires cluster.context", s.Name),
					"set spec.spokes[].cluster.context to the spoke's kubeconfig context")
			}
		default:
			return nil, diag.New(diag.CodeSpokeProviderUnsupported,
				fmt.Sprintf("spoke %q: provider %q is not supported for spokes", s.Name, s.Cluster.Provider),
				"spokes support provider: kind or existing in this release (k3d spokes are deferred)")
		}
	}
```

Note: CUE also constrains the provider enum; the Go check exists so the
error is a typed CUBE-8001 with a spoke-specific fix line rather than a
generic CUBE-0002 schema failure. Keep both; the CUE `*"kind"|"existing"`
must NOT be widened.

- [x] **Step 4: Verify pass** — Run:
  `go test ./internal/config/ -run TestSpokes -v`
  Expected: PASS (all four sub-assertions).

- [x] **Step 5: Commit** —
  `git add internal/config/ internal/diag/ && git commit -m "feat(config): spec.spokes schema + CUBE-8001 validation"`

- [x] **Step 6: Failing command tests** — create `cmd/spoke_test.go`:

```go
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpokeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSpokeAddWritesConfig(t *testing.T) {
	p := writeSpokeFixture(t)
	out, err := runCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p)
	if err != nil {
		t.Fatalf("spoke add: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "spokes:") || !strings.Contains(string(b), "name: staging") {
		t.Fatalf("cube.yaml missing spoke:\n%s", b)
	}
	// Idempotent: adding the same name again fails cleanly, file unchanged.
	if _, err := runCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p); err == nil {
		t.Fatal("duplicate spoke add must fail")
	}
}

func TestSpokeListAndRemove(t *testing.T) {
	p := writeSpokeFixture(t)
	mustRunCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p)
	out := mustRunCLI(t, "spoke", "list", "-f", p)
	if !strings.Contains(out, "staging") || !strings.Contains(out, "kind") {
		t.Fatalf("spoke list missing row:\n%s", out)
	}
	mustRunCLI(t, "spoke", "remove", "staging", "-f", p)
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "staging") {
		t.Fatalf("spoke not removed:\n%s", b)
	}
}
```

VERIFY-API: `runCLI`/`mustRunCLI` — `cmd/pack_test.go` and
`cmd/init_test.go` already contain the package's CLI-execution helper
(building the root command and capturing output). Use THAT helper's real
name and signature; if none is exported for reuse, lift the one
`pack_test.go` uses. Record the actual name in FINDINGS.

- [x] **Step 7: Verify fail** — Run: `go test ./cmd/ -run TestSpoke -v`
  Expected: FAIL — `unknown command "spoke"`.

- [x] **Step 8: Implement `cmd/spoke.go`** — follow `cmd/pack.go`'s
  config-mutating pattern exactly (load → mutate → validate by round-trip
  → write). Complete file:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui"
)

func newSpokeCmd() *cobra.Command {
	parent := &cobra.Command{Use: "spoke", Short: "Manage spoke clusters registered with this cube's engine"}
	parent.AddCommand(newSpokeAddCmd(), newSpokeListCmd(), newSpokeRemoveCmd())
	return parent
}

func newSpokeAddCmd() *cobra.Command {
	var file, provider, kubeContext string
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Declare a spoke in cube.yaml (applied on the next `cube-idp up`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			for _, s := range cube.Spec.Spokes {
				if s.Name == name {
					return diag.New(diag.CodeSpokeProviderUnsupported,
						fmt.Sprintf("spoke %q already declared", name),
						"pick another name or `cube-idp spoke remove` it first")
				}
			}
			cube.Spec.Spokes = append(cube.Spec.Spokes, config.SpokeSpec{
				Name:    name,
				Cluster: config.ClusterSpec{Provider: provider, Context: kubeContext},
			})
			if err := config.SaveValidated(file, cube); err != nil {
				return err
			}
			p := ui.NewFor(c)
			p.Notef("spoke %q declared (provider %s) — run `cube-idp up` to bootstrap and register it", name, provider)
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().StringVar(&provider, "provider", "kind", "spoke cluster provider (kind|existing)")
	c.Flags().StringVar(&kubeContext, "context", "", "kubeconfig context (required for --provider existing)")
	return c
}

func newSpokeListCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "list",
		Short: "List spokes declared in cube.yaml",
		RunE: func(c *cobra.Command, args []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			p := ui.NewFor(c)
			if len(cube.Spec.Spokes) == 0 {
				p.Notef("no spokes declared")
				return nil
			}
			for _, s := range cube.Spec.Spokes {
				ctx := s.Cluster.Context
				if ctx == "" {
					ctx = "-"
				}
				p.Linef("%-20s %-10s %s", s.Name, s.Cluster.Provider, ctx)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}

func newSpokeRemoveCmd() *cobra.Command {
	var file string
	var deleteCluster, yes bool
	c := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a spoke declaration (hub registration prunes on next `up`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			idx := -1
			for i, s := range cube.Spec.Spokes {
				if s.Name == name {
					idx = i
					break
				}
			}
			if idx < 0 {
				return diag.New(diag.CodeSpokeProviderUnsupported,
					fmt.Sprintf("spoke %q is not declared", name),
					"`cube-idp spoke list` shows declared spokes")
			}
			spoke := cube.Spec.Spokes[idx]
			cube.Spec.Spokes = append(cube.Spec.Spokes[:idx], cube.Spec.Spokes[idx+1:]...)
			if err := config.SaveValidated(file, cube); err != nil {
				return err
			}
			p := ui.NewFor(c)
			p.Notef("spoke %q removed — the hub registration secret prunes on the next `cube-idp up`", name)
			if deleteCluster && spoke.Cluster.Provider == "kind" {
				return spokeDeleteCluster(c, cube.Metadata.Name, spoke, yes)
			}
			if spoke.Cluster.Provider == "kind" {
				p.Notef("kind cluster %s-spoke-%s left running — delete with `cube-idp spoke remove --delete-cluster` or `kind delete cluster --name %s-spoke-%s`",
					cube.Metadata.Name, name, cube.Metadata.Name, name)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&deleteCluster, "delete-cluster", false, "also delete a kind spoke cluster now")
	c.Flags().BoolVar(&yes, "yes", false, "skip the delete confirmation (required non-interactively with --delete-cluster)")
	return c
}
```

`spokeDeleteCluster` lands in S3 (it needs the provider plumbing); in S1
stub it to honor the prompt doctrine so the flag is wired and fenced:

```go
// spokeDeleteCluster deletes a kind spoke's cluster after consent. The
// provider call arrives in S3; until then the consent path is real and the
// deletion reports a clear not-yet error so --delete-cluster is never a
// silent no-op.
func spokeDeleteCluster(c *cobra.Command, cubeName string, s config.SpokeSpec, yes bool) error {
	if !yes {
		ok, err := ui.Confirm(c, fmt.Sprintf("delete kind cluster %s-spoke-%s?", cubeName, s.Name))
		if err != nil {
			return err
		}
		if !ok {
			return diag.New(diag.CodeConfirmRequired, "spoke cluster deletion not confirmed", "re-run with --yes to skip the prompt")
		}
	}
	return diag.New(diag.CodeSpokeProviderUnsupported,
		"spoke cluster deletion ships in a later task of this plan (S3)",
		"delete manually: kind delete cluster --name "+cubeName+"-spoke-"+s.Name)
}
```

VERIFY-API before implementing: (a) `config.SaveValidated` — `cmd/pack.go`
(W2.T11) already writes cube.yaml with validate-by-round-trip; reuse its
exact helper (it may be a cmd-local func like `savePackConfig` rather than
a config export — if so, lift it into `internal/config.SaveValidated` as
part of this step and rewire pack.go's call site). (b) `ui.Confirm` /
`ui.PromptsAllowed` signatures are from W1.T06 — check `internal/ui/prompt.go`.
(c) `ui.NewFor`, `p.Notef`/`p.Linef` — check `internal/ui/ui.go` Printer
surface; use the real method names (FINDINGS records drift). Register
`newSpokeCmd()` in `cmd/root.go` alongside the other `AddCommand` calls.

- [x] **Step 9: Verify pass** — Run: `go test ./cmd/ -run TestSpoke -v`
  Expected: PASS.

- [x] **Step 10: Fences + gate** — Run:
  `go build ./... && go vet ./... && go test ./...` then
  `go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence'`
  Expected: all PASS (spoke add/list/remove emit only static Printer
  output; no new prompt reaches a producer).

- [x] **Step 11: Commit** —
  `git add cmd/ internal/config/ && git commit -m "feat(cmd): spoke add/list/remove — declarative spokes in cube.yaml"`

#### Outcome

```
STATUS: DONE_WITH_CONCERNS
BRANCH: p5/s1-spoke-config (merged: yes)
COMMITS: bc32ee5 feat(config): spec.spokes schema + CUBE-8001 validation; 22ab114 feat(cmd): spoke add/list/remove — declarative spokes in cube.yaml; 7fe48f6 merge: p5 S1 spoke-config (p5/s1-spoke-config)
FINDINGS: (1) OWNER: the plan's crossValidate placement can never yield CUBE-8001 for a k3d spoke — CUE validates BEFORE decode, so the narrow spoke enum fails first with CUBE-0002 ("2 errors in empty disjunction", observed). Adaptation: validateSpokes runs as a best-effort pre-CUE probe pass in Load (typed yaml decode of spec.spokes from raw; malformed docs skip it and get CUE's canonical CUBE-0002); the CUE enum stays *"kind" | "existing" exactly as ordered; absent provider aliases the CUE default ("", "kind"). (2) OWNER: spoke cluster CUE block additionally allows `registry?:` (verbatim mirror of the hub's) — ClusterSpec.Registry is a non-pointer struct that ALWAYS marshals as `registry: {}` (types.go documents omitempty is a no-op there), so without it every spoke SaveValidated round-trip failed CUE "field not allowed"; alternative (pointer *RegistrySpec) would change hub marshal output. extraPorts/mounts/providerConfig stay disallowed for spokes. (3) config.SaveValidated did not exist: pack.go carried the save inline — lifted into internal/config.SaveValidated (load.go) and rewired packInstallRefs, per this task's VERIFY-API note (a). (4) ui drift: ui.NewFor takes io.Writer, not *cobra.Command; Printer has no Notef/Linef — Notef mapped to p.Step("spoke", ...) (state changes) and p.Warn (advisory); list rows via fmt.Fprintf. ui.Confirm is (in, out, ConfirmOpts) — spokeDeleteCluster adapted; non-TTY returns Default=false → CUBE-0010 with the --yes twin (doctrine intact). (5) No runCLI/mustRunCLI helper existed in cmd tests — defined spoke_test.go-local helpers wrapping NewRootCmd + SetOut/SetErr/SetIn + Execute (pack_test.go mechanics). (6) Added a TestPromptFenceNeverBlocksOnBufferStdin row "spoke remove --delete-cluster" (the stub's consent path is fenced per the table's doctrine). (7) diag.go package comment called 8xxx "release/bundle-integrity (reserved, unallocated)" — updated to "8xxx spoke (Phase 5)" per GT8; registry.go also needed ranges["8"] (TestRangeMeaningCoversAllCodes). (8) Steps 5/11 commit messages used verbatim plus the mandated Co-Authored-By trailer.
REVIEW: TDD observed fail→pass both legs (Step 2: "cube.Spec.Spokes undefined"; Step 7: unknown command "spoke"; Steps 4/9: PASS). Task gate in worktree: go build && go vet && go test ./... all green; fence run go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence' green (incl. the new spoke fence row). Post-merge on main: go test ./... exit 0, 29 packages ok. Verified duplicate-add fails cleanly with file untouched, remove prunes the spokes key entirely (omitempty), k3d/dup/no-context all CUBE-8001.
BLOCKERS: none
HANDOFF: S2 creates internal/spoke (nothing exists there yet). Reuse config.SaveValidated for any config mutation. CUBE-8001 is taken (all S1 misuse cases); 8002/8003 are next and the 8xxx codes.go block + registry section + ranges["8"] already exist — append inside them. The spoke provider check lives in Load's pre-CUE probe (validateSpokes), NOT crossValidate — extend the probe if S3/S4 add spoke config rules. spokeDeleteCluster in cmd/spoke.go is the S1 stub returning a typed not-yet error after real consent — S3 replaces its tail with the provider call (keep the Confirm path and the promptfence row). Spoke kind cluster naming <cube>-spoke-<name> is already user-visible in remove's messages (GT7).
```

---

### S2: `internal/spoke` — bootstrap RBAC + TokenRequest credential  `[repo: $ROOT]`

**Branch:** `p5/s2-spoke-bootstrap` · **Depends:** S1

**Files:**
- Create: `internal/spoke/bootstrap.go`, `internal/spoke/bootstrap_test.go`
  (envtest — follow `internal/up/crd_wait_envtest_test.go`'s build-tag and
  harness pattern)
- Modify: `internal/diag/codes.go` + `internal/diag/registry.go`

**Interfaces:**
- Produces:

```go
package spoke

// Credential is everything the hub needs to reach a spoke as
// cube-idp-<engine>: the SA bearer token and the cluster CA. The server
// URL is chosen by the CALLER (S3) — internal kubeconfig URL for kind
// spokes, the context's own URL for existing spokes.
type Credential struct {
	Token  string
	CAData []byte
}

// Bootstrap idempotently applies namespace cube-idp-system, ServiceAccount
// cube-idp-<engineType>, and ClusterRoleBinding cube-idp-<engineType>-admin
// (→ cluster-admin) on the spoke behind conn, then mints a 10-year
// TokenRequest token (GT5; server may clamp — re-issued on every up).
func Bootstrap(ctx context.Context, conn *kube.Conn, engineType string, timeout time.Duration) (*Credential, error)
```

- Consumes: `kube.Conn{Kubeconfig []byte; Context string; REST *rest.Config}`
  (`internal/kube`), `apply.New(conn.REST, name)` + `Applier.Apply(ctx,
  objs, wait, timeout)` (`internal/apply/applier.go:91`) — SSA gives
  idempotency for free. New codes: `CodeSpokeBootstrapFailed = "CUBE-8002"`,
  `CodeSpokeTokenFailed = "CUBE-8003"`.

- [x] **Step 1: Failing envtest** — `internal/spoke/bootstrap_test.go`
  (same build tag the existing envtest files use — check the first line of
  `internal/up/crd_wait_envtest_test.go` and copy it verbatim):

```go
package spoke

import (
	"context"
	"testing"
	"time"
)

// startEnv boots envtest exactly the way internal/up's envtest harness
// does — copy that helper here (envtest.Environment start/stop, REST
// config → kube.Conn). VERIFY-API: reuse, do not reinvent.

func TestBootstrapIdempotentAndTokenIssued(t *testing.T) {
	conn, stop := startEnv(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cred1, err := Bootstrap(ctx, conn, "flux", 30*time.Second)
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if cred1.Token == "" || len(cred1.CAData) == 0 {
		t.Fatalf("empty credential: %+v", cred1)
	}
	// Second run must succeed cleanly (SSA idempotency) and re-issue.
	cred2, err := Bootstrap(ctx, conn, "flux", 30*time.Second)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if cred2.Token == "" {
		t.Fatal("re-issued token empty")
	}
}
```

- [x] **Step 2: Verify fail** — Run (with the envtest env the repo's
  Makefile/CI uses for the other envtest suites — check `Makefile` for the
  `KUBEBUILDER_ASSETS`/setup-envtest incantation and reuse it):
  `go test ./internal/spoke/ -v`
  Expected: FAIL — package does not exist.

- [x] **Step 3: Implement `internal/spoke/bootstrap.go`:**

```go
// Package spoke bootstraps and registers spoke clusters (Phase 5 spec §5).
// cube-idp is a pusher here too: apply RBAC, mint a token, hand the
// credential to the hub engine, exit. No controller, no CRD, no daemon.
package spoke

import (
	"context"
	"fmt"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/kube"
)

const (
	Namespace = "cube-idp-system"
	// tokenTTL is 10 years (GT5). Servers clamp silently; every `up`
	// re-issues, so a clamped token never strands a spoke.
	tokenTTL int64 = 315360000
)

func saName(engineType string) string { return "cube-idp-" + engineType }

// objects returns the three bootstrap objects. Data-only unstructured so
// the existing SSA Applier handles them like everything else cube-idp
// pushes.
func objects(engineType string) []*unstructured.Unstructured {
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": Namespace},
	}}
	sa := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ServiceAccount",
		"metadata": map[string]any{"name": saName(engineType), "namespace": Namespace},
	}}
	crb := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding",
		"metadata": map[string]any{"name": saName(engineType) + "-admin"},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "cluster-admin",
		},
		"subjects": []any{map[string]any{
			"kind": "ServiceAccount", "name": saName(engineType), "namespace": Namespace,
		}},
	}}
	return []*unstructured.Unstructured{ns, sa, crb}
}

func Bootstrap(ctx context.Context, conn *kube.Conn, engineType string, timeout time.Duration) (*Credential, error) {
	a, err := apply.New(conn.REST, "spoke-bootstrap")
	if err != nil {
		return nil, err
	}
	if err := a.Apply(ctx, objects(engineType), true, timeout); err != nil {
		return nil, diag.Wrap(err, diag.CodeSpokeBootstrapFailed,
			"spoke RBAC bootstrap failed",
			"check the spoke is reachable and your credentials can create namespaces and clusterrolebindings")
	}
	cs, err := kubernetes.NewForConfig(conn.REST)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeSpokeBootstrapFailed, "cannot build client for spoke", "verify the spoke kubeconfig")
	}
	ttl := tokenTTL
	tr, err := cs.CoreV1().ServiceAccounts(Namespace).CreateToken(ctx, saName(engineType),
		&authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: &ttl}}, metav1.CreateOptions{})
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeSpokeTokenFailed,
			fmt.Sprintf("token issuance for %s failed", saName(engineType)),
			"the spoke API server must support the TokenRequest API (any supported Kubernetes version does)")
	}
	return &Credential{Token: tr.Status.Token, CAData: conn.REST.TLSClientConfig.CAData}, nil
}
```

Add to `internal/diag/codes.go` (inside the 8xxx block from S1) and
register both in `registry.go`:

```go
	CodeSpokeBootstrapFailed Code = "CUBE-8002" // spoke RBAC bootstrap apply failed
	CodeSpokeTokenFailed     Code = "CUBE-8003" // spoke ServiceAccount token issuance failed
```

VERIFY-API: (a) `apply.New`'s exact signature at
`internal/apply/applier.go` — the second arg is the cube name used for
inventory labeling; passing a constant here is correct BUT confirm Apply
does not write inventory unless `RecordInventory` is called (it does not —
`RecordInventory` is a separate method; state this check's result in
FINDINGS). (b) If `conn.REST.TLSClientConfig.CAData` is empty under
envtest (CA served via CAFile), fall back to reading
`conn.REST.TLSClientConfig.CAFile`; implement the fallback
unconditionally:

```go
	ca := conn.REST.TLSClientConfig.CAData
	if len(ca) == 0 && conn.REST.TLSClientConfig.CAFile != "" {
		ca, err = os.ReadFile(conn.REST.TLSClientConfig.CAFile)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeSpokeBootstrapFailed, "cannot read spoke CA file", "check the kubeconfig's certificate-authority path")
		}
	}
	return &Credential{Token: tr.Status.Token, CAData: ca}, nil
```

- [x] **Step 4: Verify pass** — Run: `go test ./internal/spoke/ -v`
  Expected: PASS both bootstrap runs; token non-empty.

- [x] **Step 5: Gate + commit** —
  `go build ./... && go vet ./... && go test ./...` → all PASS, then
  `git add internal/spoke/ internal/diag/ && git commit -m "feat(spoke): bootstrap RBAC + TokenRequest credential (CUBE-8002/8003)"`

#### Outcome

```
STATUS: DONE
BRANCH: p5/s2-spoke-bootstrap (merged: yes)
COMMITS: 5b1c0b5 feat(spoke): bootstrap RBAC + TokenRequest credential (CUBE-8002/8003); bd0e512 merge: p5 S2 spoke-bootstrap (p5/s2-spoke-bootstrap)
FINDINGS: (1) Plan Step 1 says "same build tag the existing envtest files use" — internal/up/crd_wait_envtest_test.go has NO build tag: its gate is a per-test KUBEBUILDER_ASSETS-unset t.Skip plus its own env.Start; copied that exact pattern (startEnv helper local to bootstrap_test.go returning kube.Conn{REST: cfg} + stop func). (2) VERIFY-API (a) confirmed: apply.New(cfg *rest.Config, cubeName string) at applier.go:39; Apply (applier.go:91) only labels + ApplyAllStaged + optional WaitForSet — inventory is written solely by the separate RecordInventory (inventory.go:27), so Bootstrap's constant cube name "spoke-bootstrap" creates no inventory ConfigMap. (3) VERIFY-API (b): unconditional CAFile fallback implemented; under envtest CAData was already embedded (test asserts non-empty CAData and passed), so the fallback serves file-referencing kubeconfigs (existing-provider spokes). (4) Makefile test-apply's package list does not include ./internal/spoke/ and this task's Files don't sanction a Makefile edit — left untouched; the spoke envtest leg runs via KUBEBUILDER_ASSETS=$(setup-envtest use 1.33 -p path) go test ./internal/spoke/. (5) spoke.Namespace duplicates apply.SystemNamespace's "cube-idp-system" value by design (plan-specified local const; package stays self-describing). (6) Step 5 commit message verbatim plus the mandated Co-Authored-By trailer. (7) Ledger close waited out P2's uncommitted plan-file edit per the claim-serialization discipline (background poll until porcelain-clean, then re-read).
REVIEW: TDD observed: Step 2 red = "undefined: Bootstrap ... FAIL [build failed]" (package absent); Step 4 green = TestBootstrapIdempotentAndTokenIssued PASS (4.66s) against a real envtest API server — both bootstrap runs succeed (SSA idempotency), token and CAData non-empty, re-issued token non-empty. Task gate in worktree: go build ./... && go vet ./... && go test ./... exit 0 (30 test packages ok, incl. cmd + ui fences). Merge --no-ff clean, no conflicts (P2's merge on main touched disjoint files); post-merge go test ./... exit 0.
BLOCKERS: none
HANDOFF: S3 consumes spoke.Bootstrap(ctx, *kube.Conn, engineType, timeout) → *Credential{Token string; CAData []byte}; the server URL is deliberately NOT in Credential — S3 chooses it (kind internal kubeconfig URL vs existing context URL). spoke.Namespace ("cube-idp-system") is exported. CUBE-8002/8003 are taken; S3's 8004/8005 append inside the same codes.go 8xxx block + registry section (ranges["8"] already exists). bootstrap_test.go's startEnv is the package envtest harness — reuse it for register tests needing an API server (KUBEBUILDER_ASSETS skip semantics included). Bootstrap's Applier is throwaway (cube name "spoke-bootstrap", never RecordInventory); S3's hub secrets must go through the HUB applier + RecordInventory so removal prunes. ./internal/spoke/ is not in Makefile test-apply — owner may want it added once S3/S4 grow the suite.
```

---

### S3: hub registration, `up` reconcile, `down` cascade  `[repo: $ROOT]`

**Branch:** `p5/s3-spoke-register` · **Depends:** S2

**Files:**
- Create: `internal/spoke/register.go`, `internal/spoke/register_test.go`
- Modify: `internal/up/up.go` (spoke loop after `waitHealthy`, before the
  summary), `internal/cluster/kindp/kind.go` (+`InternalKubeconfig`),
  `internal/cluster/kindp/merge.go` or the config renderer
  (skip host port mapping when `gw.Port == 0`), `internal/cluster/provider.go`
  (optional interface), `cmd/down.go` (cascade + preview lines),
  `cmd/spoke.go` (real `spokeDeleteCluster`), `internal/diag/codes.go` +
  `registry.go`
- Test: `internal/spoke/register_test.go` (pure), `internal/up/up_test.go`
  (extend the existing fake-provider test seam if one exists — FINDINGS
  records what was reusable), `internal/cluster/kindp/kind_test.go`
  (render-config port-skip)

**Interfaces:**
- Produces:

```go
// BuildKubeconfig renders a self-contained kubeconfig for server with the
// bearer token and CA — the flux hub secret's `value` payload.
func BuildKubeconfig(clusterName, server string, caData []byte, token string) ([]byte, error)

// HubSecrets returns the engine-native registration secret(s) for one
// spoke: argocd → argocd cluster secret in ns "argocd"; flux → kubeconfig
// secret (key "value") in ns "flux-system". Both named cube-idp-spoke-<name>.
func HubSecrets(engineType, spokeName, server string, cred *Credential) ([]*unstructured.Unstructured, error)
```

  plus `cluster.InternalKubeconfiger interface { InternalKubeconfig(ctx
  context.Context, name string) ([]byte, error) }` in
  `internal/cluster/provider.go`, implemented by kindp via
  `provider.KubeConfig(name, true)`.
- Consumes: S2 `spoke.Bootstrap`/`Credential`; `cluster.New(spec, gw)`
  (`internal/cluster/provider.go:61`); `apply.Applier.Apply` +
  `RecordInventory` (hub secrets MUST be inventoried so removal prunes,
  `internal/up/up.go:178` shows the call pattern); Console events
  (`con.ProgressN("spoke", …, i, n)`).
- New codes: `CodeSpokeEnsureFailed = "CUBE-8004"` (spoke cluster
  create/connect failed), `CodeSpokeRegisterFailed = "CUBE-8005"` (hub
  secret apply failed).

- [ ] **Step 1: Failing register tests** — `internal/spoke/register_test.go`
  (pure unit, no cluster):

```go
package spoke

import (
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

func testCred() *Credential {
	return &Credential{Token: "tok-123", CAData: []byte("CADATA")}
}

func TestBuildKubeconfigShape(t *testing.T) {
	kc, err := BuildKubeconfig("dev-spoke-staging", "https://dev-spoke-staging-control-plane:6443", testCred().CAData, testCred().Token)
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip through client-go itself: the kubeconfig must be loadable.
	if _, err := clientcmd.RESTConfigFromKubeConfig(kc); err != nil {
		t.Fatalf("kubeconfig does not load: %v", err)
	}
	s := string(kc)
	for _, want := range []string{"dev-spoke-staging-control-plane:6443", "token: tok-123", "certificate-authority-data:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("kubeconfig missing %q:\n%s", want, s)
		}
	}
}

func TestHubSecretsArgocd(t *testing.T) {
	objs, err := HubSecrets("argocd", "staging", "https://x:6443", testCred())
	if err != nil || len(objs) != 1 {
		t.Fatalf("objs=%v err=%v", objs, err)
	}
	o := objs[0]
	if o.GetNamespace() != "argocd" || o.GetName() != "cube-idp-spoke-staging" {
		t.Fatalf("wrong target: %s/%s", o.GetNamespace(), o.GetName())
	}
	if o.GetLabels()["argocd.argoproj.io/secret-type"] != "cluster" {
		t.Fatalf("missing argocd cluster label: %v", o.GetLabels())
	}
	data, _, _ := unstructuredNestedStringMap(o, "stringData")
	if data["server"] != "https://x:6443" || !strings.Contains(data["config"], "bearerToken") {
		t.Fatalf("bad cluster secret payload: %v", data)
	}
}

func TestHubSecretsFlux(t *testing.T) {
	objs, err := HubSecrets("flux", "staging", "https://x:6443", testCred())
	if err != nil || len(objs) != 1 {
		t.Fatalf("objs=%v err=%v", objs, err)
	}
	o := objs[0]
	if o.GetNamespace() != "flux-system" || o.GetName() != "cube-idp-spoke-staging" {
		t.Fatalf("wrong target: %s/%s", o.GetNamespace(), o.GetName())
	}
	data, _, _ := unstructuredNestedStringMap(o, "stringData")
	if !strings.Contains(data["value"], "token: tok-123") {
		t.Fatalf("flux secret must embed kubeconfig under key value: %v", data)
	}
}
```

(`unstructuredNestedStringMap` is a 5-line test helper over
`unstructured.NestedStringMap(o.Object, "stringData")` — write it at the
bottom of the test file.)

- [ ] **Step 2: Verify fail** — Run:
  `go test ./internal/spoke/ -run 'TestBuild|TestHubSecrets' -v`
  Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement `internal/spoke/register.go`:**

```go
package spoke

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func BuildKubeconfig(clusterName, server string, caData []byte, token string) ([]byte, error) {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[clusterName] = &clientcmdapi.Cluster{Server: server, CertificateAuthorityData: caData}
	cfg.AuthInfos[clusterName] = &clientcmdapi.AuthInfo{Token: token}
	cfg.Contexts[clusterName] = &clientcmdapi.Context{Cluster: clusterName, AuthInfo: clusterName}
	cfg.CurrentContext = clusterName
	return clientcmd.Write(*cfg)
}

// argocdClusterConfig is argocd's cluster-secret `config` JSON payload.
type argocdClusterConfig struct {
	BearerToken     string `json:"bearerToken"`
	TLSClientConfig struct {
		CAData []byte `json:"caData"`
	} `json:"tlsClientConfig"`
}

func HubSecrets(engineType, spokeName, server string, cred *Credential) ([]*unstructured.Unstructured, error) {
	name := "cube-idp-spoke-" + spokeName
	switch engineType {
	case "argocd":
		cc := argocdClusterConfig{BearerToken: cred.Token}
		cc.TLSClientConfig.CAData = cred.CAData
		cj, err := json.Marshal(cc)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeSpokeRegisterFailed, "cannot encode argocd cluster config", "report this as a bug")
		}
		return []*unstructured.Unstructured{{Object: map[string]any{
			"apiVersion": "v1", "kind": "Secret",
			"metadata": map[string]any{
				"name": name, "namespace": "argocd",
				"labels": map[string]any{"argocd.argoproj.io/secret-type": "cluster"},
			},
			"type": "Opaque",
			"stringData": map[string]any{
				"name":   spokeName,
				"server": server,
				"config": string(cj),
			},
		}}}, nil
	case "flux":
		kc, err := BuildKubeconfig(spokeName, server, cred.CAData, cred.Token)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeSpokeRegisterFailed, "cannot render spoke kubeconfig", "report this as a bug")
		}
		return []*unstructured.Unstructured{{Object: map[string]any{
			"apiVersion": "v1", "kind": "Secret",
			"metadata":   map[string]any{"name": name, "namespace": "flux-system"},
			"type":       "Opaque",
			"stringData": map[string]any{"value": string(kc)},
		}}}, nil
	default:
		return nil, diag.New(diag.CodeSpokeRegisterFailed,
			fmt.Sprintf("unknown engine type %q for spoke registration", engineType),
			"engine.type must be flux or argocd")
	}
}
```

Add codes to the 8xxx block + registry:

```go
	CodeSpokeEnsureFailed   Code = "CUBE-8004" // spoke cluster create/connect failed
	CodeSpokeRegisterFailed Code = "CUBE-8005" // hub registration secret build/apply failed
```

- [ ] **Step 4: Verify pass** — Run:
  `go test ./internal/spoke/ -run 'TestBuild|TestHubSecrets' -v`
  Expected: PASS ×3.

- [ ] **Step 5: Commit** —
  `git add internal/spoke/ internal/diag/ && git commit -m "feat(spoke): hub registration secrets for flux and argocd (CUBE-8004/8005)"`

- [ ] **Step 6: Port-skip guard + internal kubeconfig in kindp.** Failing
  test first, in `internal/cluster/kindp/kind_test.go` (append; the file
  already tests `RenderConfig` — mirror its fixture style):

```go
func TestRenderConfigZeroGatewaySkipsHostPorts(t *testing.T) {
	cfg, err := RenderConfig("dev-spoke-staging", config.ClusterSpec{Provider: "kind"}, config.GatewaySpec{}, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "hostPort") {
		t.Fatalf("spoke render must not map host ports (hub owns them):\n%s", cfg)
	}
}
```

Run `go test ./internal/cluster/kindp/ -run TestRenderConfigZero -v` —
Expected: FAIL (today the gateway port is always mapped). Then guard the
`extraPortMappings` emission in `RenderConfig` (find where `gw.Port` is
written into the kind config; wrap gateway-port AND
`config.GatewayNodePort` mapping in `if gw.Port > 0`) and add:

```go
// InternalKubeconfig returns the docker-network-internal kubeconfig
// (server https://<name>-control-plane:6443) — what hub engine pods must
// use to reach a kind spoke (GT7).
func (k *Kind) InternalKubeconfig(ctx context.Context, name string) ([]byte, error) {
	kc, err := k.provider.KubeConfig(name, true)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKindKubeconfigGet, "cannot get internal kubeconfig from kind", "retry; if it persists the spoke cluster may be gone")
	}
	return []byte(kc), nil
}
```

and in `internal/cluster/provider.go`:

```go
// InternalKubeconfiger is implemented by providers whose clusters have a
// second, container-network-internal API endpoint (kind). Spoke
// registration prefers it (GT7).
type InternalKubeconfiger interface {
	InternalKubeconfig(ctx context.Context, name string) ([]byte, error)
}
```

Re-run the test — Expected: PASS. Also run the kindp contract test:
`go test ./internal/cluster/kindp/ -run TestKind -short` (the live-kind
contract leg only runs where CI/the arbiter says so, per GT14).

- [ ] **Step 7: Commit** —
  `git add internal/cluster/ && git commit -m "feat(kindp): zero-gateway render skips host ports; InternalKubeconfig for spokes"`

- [ ] **Step 8: `up` spoke loop.** In `internal/up/up.go`, insert AFTER
  the `waitHealthy` call (line ~360; anchor: right after the block that
  writes pack discoverability records — grep `RecordInventory` and the
  health summary emission to find the exact seam, FINDINGS records the
  chosen line) and BEFORE the final summary/epilogue:

```go
	// Phase 5 spec §5: spokes — bootstrap and register, then the engine
	// takes over. Failure of one spoke aborts up (fail loud, spec thesis);
	// re-running up is the retry path and re-issues tokens (GT5).
	for i, sp := range cube.Spec.Spokes {
		pr := con.ProgressN("spoke", fmt.Sprintf("spoke %q (%s)", sp.Name, sp.Cluster.Provider), i+1, len(cube.Spec.Spokes))
		if err := ensureSpoke(ctx, cube, sp, a, con); err != nil {
			pr.Stop()
			return err
		}
		pr.Done("spoke %q registered with %s", sp.Name, cube.Spec.Engine.Type)
	}
```

and add to the same file:

```go
// ensureSpoke creates/connects one spoke, bootstraps cube-idp RBAC on it,
// and applies the engine-native registration secret on the HUB (recorded
// in inventory so `spoke remove` + `up` prunes it, and `down` cascades).
func ensureSpoke(ctx context.Context, cube *config.Cube, sp config.SpokeSpec, hub *apply.Applier, con *ui.Console) error {
	spokeName := cube.Metadata.Name + "-spoke-" + sp.Name
	prov, err := cluster.New(sp.Cluster, config.GatewaySpec{}) // zero gw: no host ports (S3 kindp guard)
	if err != nil {
		return diag.Wrap(err, diag.CodeSpokeEnsureFailed, fmt.Sprintf("spoke %q: unusable provider", sp.Name), "spokes support provider kind or existing")
	}
	sctx, cancel := context.WithTimeout(ctx, clusterTimeout)
	defer cancel()
	conn, err := prov.Ensure(sctx, spokeClusterName(cube, sp), sp.Cluster)
	if err != nil {
		return diag.Wrap(err, diag.CodeSpokeEnsureFailed, fmt.Sprintf("spoke %q: cluster ensure failed", sp.Name), "`cube-idp doctor` preflights the runtime; for provider existing check the context name")
	}
	cred, err := spoke.Bootstrap(ctx, conn, cube.Spec.Engine.Type, applyTimeout)
	if err != nil {
		return err
	}
	server, err := spokeServerURL(ctx, prov, spokeClusterName(cube, sp), sp, conn)
	if err != nil {
		return err
	}
	secrets, err := spoke.HubSecrets(cube.Spec.Engine.Type, sp.Name, server, cred)
	if err != nil {
		return err
	}
	if err := hub.Apply(ctx, secrets, true, applyTimeout); err != nil {
		return diag.Wrap(err, diag.CodeSpokeRegisterFailed, fmt.Sprintf("spoke %q: hub registration apply failed", sp.Name), "is the hub engine namespace present? re-run `cube-idp up`")
	}
	if err := hub.RecordInventory(ctx, secrets); err != nil {
		return err
	}
	con.Log("spoke", "%s: server %s, sa cube-idp-%s", sp.Name, server, cube.Spec.Engine.Type)
	_ = spokeName
	return nil
}

// spokeClusterName: kind spokes get <cube>-spoke-<name> (GT7); existing
// spokes are whatever the context points at — Ensure ignores the name.
func spokeClusterName(cube *config.Cube, sp config.SpokeSpec) string {
	if sp.Cluster.Provider == "existing" {
		return sp.Name
	}
	return cube.Metadata.Name + "-spoke-" + sp.Name
}

// spokeServerURL picks the hub-reachable API endpoint: kind → internal
// kubeconfig's server (shared docker network); existing → the connection's
// own server URL (reachability is the operator's contract, doctor probes it).
func spokeServerURL(ctx context.Context, prov cluster.Provider, clusterName string, sp config.SpokeSpec, conn *kube.Conn) (string, error) {
	if ik, ok := prov.(cluster.InternalKubeconfiger); ok && sp.Cluster.Provider == "kind" {
		kc, err := ik.InternalKubeconfig(ctx, clusterName)
		if err != nil {
			return "", err
		}
		cfg, err := clientcmd.RESTConfigFromKubeConfig(kc)
		if err != nil {
			return "", diag.Wrap(err, diag.CodeSpokeEnsureFailed, "internal kubeconfig invalid", "recreate the spoke: cube-idp spoke remove --delete-cluster && cube-idp up")
		}
		return cfg.Host, nil
	}
	return conn.REST.Host, nil
}
```

VERIFY-API while wiring: the imports `cluster`, `spoke`, `clientcmd` and
the `clusterTimeout`/`applyTimeout` consts already exist in/for this file;
`_ = spokeName` is temporary — remove it and use `spokeName` if your final
code references it (vet must be clean). Unit-test `ensureSpoke`'s pieces:
`spokeClusterName` and `spokeServerURL` (existing-provider arm) get direct
table tests in `internal/up/up_test.go`; the full loop is covered by the
e2e leg below.

- [ ] **Step 9: `down` cascade + real `spokeDeleteCluster`.** In
  `cmd/down.go`: the preview (W1.T07) gains one line per spoke
  (`spoke <name> (<provider>)` — kind spokes annotated `cluster will be
  deleted`); after the hub teardown succeeds, for each `provider: kind`
  spoke call `cluster.New` + `Delete(ctx, <cube>-spoke-<name>)`, wrapping
  failures as CUBE-8004 Warn lines (best-effort — down must not strand the
  hub teardown on a half-dead spoke); `provider: existing` spokes get a
  Note: `spoke <name>: existing cluster left untouched — cube-idp-<engine>
  RBAC remains; remove with kubectl delete ns cube-idp-system && kubectl
  delete clusterrolebinding cube-idp-<engine>-admin --context <ctx>`.
  Replace S1's `spokeDeleteCluster` stub body with the same
  `cluster.New(…).Delete` call (consent path unchanged). Extend
  `cmd/down_test.go`'s preview assertions with a spokes fixture, run
  `go test ./cmd/ -run TestDown -v` — Expected: PASS with the new lines,
  and the TE-3 consent goldens UNCHANGED for spoke-less cubes (frozen
  surface, GT13).

- [ ] **Step 10: e2e leg (gated).** Append to `tests/e2e/e2e_test.go` a
  `TestSpokeKindRegistration` following the file's existing gating pattern
  (env-gated; honors `CUBE_IDP_E2E_GATEWAY_PORT`, GT14): cube.yaml with
  one kind spoke, `up`, assert hub secret `cube-idp-spoke-<name>` exists
  in the engine namespace with a non-empty token/config payload, assert
  `kubectl --context kind-<cube>-spoke-<name> get ns cube-idp-system`
  succeeds, then `down --yes` and assert the spoke kind cluster is gone.
  Do NOT run it locally by default; note in FINDINGS whether the arbiter
  ran it live.

- [ ] **Step 11: Gate + fences + commit** —
  `go build ./... && go vet ./... && go test ./...` and
  `go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence'`
  Expected: all PASS. Then:
  `git add internal/up/ internal/spoke/ cmd/ tests/e2e/ && git commit -m "feat(up,down): spoke reconcile loop + cascade — engine takes over (spec §5)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/s3-spoke-register (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### S4: spoke rows in status, doctor probes, live `spoke list`  `[repo: $ROOT]`

**Branch:** `p5/s4-spoke-status` · **Depends:** S3

**Files:**
- Modify: `cmd/status.go` (`statusSnapshot` + collector + render),
  `cmd/spoke.go` (list gains Registered/Reachable columns, graceful
  degradation), `internal/doctor/doctor.go` (spoke reachability check),
  `internal/diag/codes.go` + `registry.go` (`CodeSpokeUnreachable =
  "CUBE-8006"`), `docs/machine-readable-output.md` (additive `spokes`
  field)
- Test: `cmd/status_test.go`, `cmd/spoke_test.go`, `internal/doctor/doctor_test.go`

**Interfaces:**
- Produces: `statusSnapshot.Spokes []spokeStatus` with
  `spokeStatus{Name, Provider string; Registered, Reachable bool}`; JSON
  status doc gains additive top-level `"spokes": [...]` (additive-only —
  GT13 allows it); doctor check id `spoke-reachability`.
- Consumes: S3 hub secret naming (`cube-idp-spoke-<name>` in
  `argocd`/`flux-system`), `statusCollector` seam (`cmd/status.go:242`,
  `statusConnect` var at `:248` — the W2.T12 fake-collector test pattern).

- [ ] **Step 1: Failing status test** — in `cmd/status_test.go`, extend
  the existing fake-collector test (find the test that stubs
  `statusConnect`; copy its arrangement) with a snapshot carrying
  `Spokes: []spokeStatus{{Name: "staging", Provider: "kind", Registered:
  true, Reachable: false}}` and assert the rendered output contains a
  `spokes` section row `staging` with a paired glyph+word for each state
  (`✔ registered` / `✗ unreachable` — semantic-color doctrine: word always
  present). Also assert `-o json` output contains
  `"spokes":[{"name":"staging"` (additive field).
  Run: `go test ./cmd/ -run TestStatus -v` — Expected: FAIL (no Spokes
  field).

- [ ] **Step 2: Implement.** (a) Add the struct + field; (b) in
  `connectStatus`'s collector closure: after component collection, if
  `cube.Spec.Spokes` is non-empty, for each spoke read the hub secret
  (Registered = secret exists in the engine's namespace) and probe
  reachability: build a REST config from the secret's own payload (argocd:
  `server`+`config` JSON; flux: the `value` kubeconfig) and GET
  `/readyz` with a **2-second** per-spoke timeout, all spokes probed in
  parallel (`sync.WaitGroup`), errors → `Reachable: false` (never an
  error — status must render a dead spoke, not fail on it); (c) render the
  section in `renderStatusOnce` after components, gated on
  `len(snap.Spokes) > 0`; (d) add the JSON field to `statusDoc`
  (`cmd/status.go:412`). Re-run — Expected: PASS. The `--watch` path needs
  NO change (it re-runs the same collector).

- [ ] **Step 3: Doctor + spoke list + docs.** Doctor: add a
  `spoke-reachability` check (skip silently when no spokes declared; warn
  with CUBE-8006 naming each unreachable spoke). `spoke list`: attempt the
  same collector; when the hub is unreachable print the S1 config-only
  table with a trailing `hub unreachable — showing declared config only`
  Note (graceful, no error). `docs/machine-readable-output.md`: document
  the additive `spokes` array. Tests: doctor fake for both arms; spoke
  list degradation test (point `-f` at a cube whose cluster doesn't
  exist).
  Run: `go test ./cmd/ ./internal/doctor/ -v -run 'TestSpoke|TestDoctor'`
  Expected: PASS.

- [ ] **Step 4: Gate + fences + commit** — full gate + fence commands (as
  S3 Step 11). Expected: PASS; JSONL fence green (additive only). Commit:
  `git add cmd/ internal/doctor/ internal/diag/ docs/machine-readable-output.md && git commit -m "feat(status,doctor): spoke representation — rows, probes, live list (CUBE-8006)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/s4-spoke-status (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

## Lane U — CLI UX

### U1: provider + engine-wait logs → `StepLog`  `[repo: $ROOT]`

**Branch:** `p5/u1-provider-logs` · **Depends:** none

**Files:**
- Modify: `internal/cluster/provider.go` (LogSink + Loggable),
  `internal/cluster/kindp/kind.go` (logger adapter + SetLogSink),
  `internal/cluster/k3dp/k3d.go` (logrus forwarder), `internal/up/up.go`
  (wire sink; ticker inside `waitHealthy` at `internal/up/up.go:498`)
- Create: `internal/cluster/kindp/kindlog.go`, `internal/cluster/kindp/kindlog_test.go`
- Test: `internal/up/up_test.go` (waitHealthy ticker), k3dp forwarder test

**Interfaces:**
- Produces: `cluster.LogSink func(line string)`;
  `cluster.Loggable interface { SetLogSink(cluster.LogSink) }` —
  implemented by BOTH local providers; up wires
  `sink → con.Log("cluster", "%s", line)`. waitHealthy emits
  `con.Log("engine", "waiting on: <comma-joined not-ready components>")`
  every 15s while unhealthy.
- Consumes: `ui.Console.Log(stage, format, args...)`
  (`internal/ui/console.go:57`, W1.T02 StepLog event — live mode renders
  it as the dim log tail; machine modes already project it per the frozen
  matrix).

- [x] **Step 1: Failing kind adapter test** —
  `internal/cluster/kindp/kindlog_test.go`:

```go
package kindp

import (
	"strings"
	"testing"
)

func TestKindLogAdapterForwardsInfoAndWarns(t *testing.T) {
	var got []string
	l := newKindLogger(func(line string) { got = append(got, line) })
	l.V(0).Infof("Ensuring node image (%s) ...", "kindest/node:v1.33.1")
	l.Warn("a warning")
	l.Error("an error")
	l.V(3).Info("debug noise") // must be dropped
	joined := strings.Join(got, "\n")
	for _, want := range []string{"Ensuring node image", "a warning", "an error"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "debug noise") {
		t.Fatalf("V(3) must be dropped: %q", joined)
	}
}
```

- [x] **Step 2: Verify fail** — Run:
  `go test ./internal/cluster/kindp/ -run TestKindLog -v`
  Expected: FAIL — `newKindLogger` undefined.

- [x] **Step 3: Implement `internal/cluster/kindp/kindlog.go`.**
  VERIFY-API first: `go doc sigs.k8s.io/kind/pkg/log Logger` and
  `go doc sigs.k8s.io/kind/pkg/cluster ProviderWithLogger` — the interface
  below matches kind's pinned version at time of writing; use the real
  method set and record drift in FINDINGS:

```go
package kindp

import (
	kindlog "sigs.k8s.io/kind/pkg/log"
)

// kindLogger adapts kind's log.Logger to a plain line sink so `up` can
// stream cluster provisioning into the StepLog event channel. Verbosity
// >0 is dropped: kind's V(0) is its user-facing progress narration.
type kindLogger struct{ sink func(string) }

func newKindLogger(sink func(string)) kindLogger { return kindLogger{sink: sink} }

func (k kindLogger) Warn(message string)                 { k.sink(message) }
func (k kindLogger) Warnf(format string, args ...any)    { k.sink(sprintf(format, args...)) }
func (k kindLogger) Error(message string)                { k.sink(message) }
func (k kindLogger) Errorf(format string, args ...any)   { k.sink(sprintf(format, args...)) }
func (k kindLogger) V(level kindlog.Level) kindlog.InfoLogger {
	if level > 0 {
		return nopInfo{}
	}
	return infoLogger{sink: k.sink}
}

type infoLogger struct{ sink func(string) }

func (i infoLogger) Info(message string)               { i.sink(message) }
func (i infoLogger) Infof(format string, args ...any)  { i.sink(sprintf(format, args...)) }
func (i infoLogger) Enabled() bool                     { return true }

type nopInfo struct{}

func (nopInfo) Info(string)          {}
func (nopInfo) Infof(string, ...any) {}
func (nopInfo) Enabled() bool        { return false }

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
```

(add the `fmt` import). In `internal/cluster/provider.go`:

```go
// LogSink receives one human-readable provisioning line at a time.
type LogSink func(line string)

// Loggable providers can stream their provisioning narration (kind's
// "Ensuring node image ..." etc.) into the caller's sink. Optional: up
// type-asserts and wires it to StepLog events.
type Loggable interface{ SetLogSink(LogSink) }
```

In `internal/cluster/kindp/kind.go`: store the sink on `Kind` and rebuild
the provider with the logger option —

```go
func (k *Kind) SetLogSink(sink cluster.LogSink) {
	np, _ := kindcluster.DetectNodeProvider()
	opts := []kindcluster.ProviderOption{kindcluster.ProviderWithLogger(newKindLogger(sink))}
	if np != nil {
		opts = append(opts, np)
	}
	k.provider = kindcluster.NewProvider(opts...)
}
```

CAREFUL: importing `internal/cluster` from kindp would cycle (the
provider.go comment at `internal/config/types.go:78` explains the
existing cycle-avoidance). `cluster.LogSink` is just `func(string)` — so
declare the method as `func (k *Kind) SetLogSink(sink func(line string))`
in kindp and let it satisfy `cluster.Loggable` structurally. Same in k3dp.

In `internal/cluster/k3dp/k3d.go`: VERIFY-API
`go doc github.com/k3d-io/k3d/v5/pkg/logger` — k3d logs through a global
logrus instance. Implement:

```go
// SetLogSink forwards k3d's global logrus output (Info and above) to sink.
// The hook is installed once per process; subsequent calls only swap the
// destination (k3d's logger is global — two concurrent K3d values share it).
func (k *K3d) SetLogSink(sink func(line string)) { installK3dHook(sink) }
```

with `installK3dHook` guarding via package-level `sync.Once` + an atomic
sink pointer, hooking `l.Log().AddHook(...)` (logrus hook whose `Levels()`
returns Info/Warn/Error and whose `Fire(e)` sends `e.Message` to the
current sink). Test with a fake sink asserting a `l.Log().Info("x")`
arrives.

In `internal/up/up.go` right after `cluster.New` (line ~119):

```go
	if lg, ok := prov.(cluster.Loggable); ok {
		lg.SetLogSink(func(line string) { con.Log("cluster", "%s", line) })
	}
```

- [x] **Step 4: Verify pass** — Run:
  `go test ./internal/cluster/... -run 'TestKindLog|TestK3dLog' -v`
  Expected: PASS.

- [x] **Step 5: waitHealthy ticker.** Failing test in
  `internal/up/up_test.go`: call `waitHealthy` with a stub engine whose
  `Health` reports one not-ready component for >15s (drive a fake clock
  ONLY if the file already has one; otherwise shrink the ticker via a
  package-level `var healthLogEvery = 15 * time.Second` overridden to
  30ms in the test) and a Console whose event recorder the test can
  inspect (the W1 tests show the recorder pattern); assert at least one
  `StepLog` event with stage `engine` containing the component name. Then
  implement inside `waitHealthy`'s poll loop (`internal/up/up.go:498`):
  track `lastLog time.Time`; on each unhealthy poll where
  `time.Since(lastLog) >= healthLogEvery`, `con.Log("engine", "waiting
  on: %s", strings.Join(notReadyNames, ", "))` and reset. Run:
  `go test ./internal/up/ -run TestWaitHealthy -v` — Expected: PASS.

- [x] **Step 6: Gate + fences + commit** — full gate + fence run (S3
  Step 11 commands). CRITICAL: `TestModeMatrixFence` must be green —
  StepLog is an existing event type with frozen projections; U1 adds
  emissions, not rendering. Commit:
  `git add internal/cluster/ internal/up/ && git commit -m "feat(up): stream kind/k3d provisioning + engine-wait narration via StepLog"`

#### Outcome

```
STATUS: DONE_WITH_CONCERNS
BRANCH: p5/u1-provider-logs (merged: yes)
COMMITS: 6f5e701 feat(up): stream kind/k3d provisioning + engine-wait narration via StepLog; 03bd237 merge: p5 U1 provider-logs (p5/u1-provider-logs)
FINDINGS: (1) VERIFY-API kind: sigs.k8s.io/kind/pkg/log Logger/InfoLogger and cluster.ProviderWithLogger match the plan's method set exactly — no drift. (2) VERIFY-API k3d: pkg/logger exposes Log() *logrus.Logger; forwarder is a logrus Hook (Levels Info/Warn/Error, Fire → atomic sink pointer, sync.Once AddHook) in k3d.go; sirupsen/logrus v1.9.4 was already pinned (indirect) — direct import adds NO new module, go.mod/go.sum untouched. (3) Deviation: the plan's `type LogSink func(line string)` (defined type) cannot be satisfied structurally from kindp/k3dp — Go needs identical method signatures and a defined func type ≠ func(string). Declared as type ALIAS `type LogSink = func(line string)`; Loggable then equals interface{ SetLogSink(func(line string)) } and both providers satisfy it with plain-signature methods; added `var _ Loggable` assertions in provider.go per the ImageLoader precedent. (4) Deviation: internal/ui/console.go gained exported NewConsole(ch chan<- event.Event) *Console — the step-5 "Console whose event recorder the test can inspect" is unbuildable from package up otherwise (Console.ch unexported, only RunPipeline constructs; StepLog is zero bytes in plain/JSON so rendered output can't be asserted). Additive export; no frozen surface touched. (5) Deviation: healthPoll moved const→package var (value unchanged) beside the new healthLogEvery var so the narration test can shrink both (5ms/30ms); with healthPoll const the test would need >5s wall time. (6) Narration emits after the allReady check and BEFORE the deadline check (a timing-out wait still narrates); lastLog starts at Now, so the first line lands after healthLogEvery (15s) of unhealthiness; an empty component list narrates "waiting on: no components reported yet". (7) CONCERN for owner: the live renderer shows log tails only under an open step of the SAME stage; waitHealthy's open Progress is stage "health" while the narration stage is "engine" (plan-normative), so these lines do not display in today's live tail (the "cluster" narration DOES display — its stage matches the open cluster step). Emissions-not-rendering was U1's contract; if display is wanted, either the narration stage becomes "health" (one word) or the renderer learns cross-stage tails — owner's call, follow-up not self-authorized.
REVIEW: TDD fail→pass verified for all three test groups (kindlog: undefined newKindLogger → PASS; k3dlog: undefined SetLogSink → PASS incl. hook single-install + sink swap; up: undefined healthLogEvery/ui.NewConsole → PASS asserting ≥1 StepLog{Stage:"engine"} naming kustomize-controller and CUBE-3004 on timeout). Task gate in worktree: go build ./... && go vet ./... && go test ./... all PASS (34 pkgs); fence run go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence' all PASS, TestModeMatrixFence explicitly green. gofmt clean on every touched file; go.mod/go.sum diff empty. Post-merge go test ./... on main: all PASS.
BLOCKERS: none
HANDOFF: U2 (p5/u2-http-port) is unblocked — U1 merged at 03bd237. For U2+: kindp/k3dp now expose SetLogSink(func(line string)) and provider.go carries Loggable assertions — kindp.New's provider field is REBUILT by SetLogSink (same DetectNodeProvider dance as New), keep that in sync if RenderConfig/port work touches provider construction. healthPoll is now a var (tests may shrink it). ui.NewConsole is the sanctioned recorder seam for producer-side event assertions from any package. k3d.go imports logrus directly; if a later task runs `go mod tidy` the // indirect marker moves — harmless, same module list.
```

---

### U2: opt-in `gateway.httpPort`  `[repo: $ROOT]`

**Branch:** `p5/u2-http-port` · **Depends:** U1

**Files:**
- Modify: `internal/config/types.go` (HTTPPort + const),
  `internal/config/schema.cue`, `internal/config/load.go` (collision
  check), `internal/cluster/kindp/kind.go` (RenderConfig mapping),
  `internal/cluster/k3dp/k3d.go` (port mapping), `internal/doctor/doctor.go`
  (port preflight), `README.md` (cluster-shape caveat table row)
- Test: `internal/config/load_test.go`, `internal/cluster/kindp/kind_test.go`,
  `internal/cluster/k3dp/k3d_test.go`

**Interfaces:**
- Produces: `GatewaySpec.HTTPPort int` (yaml `httpPort,omitempty`),
  `config.GatewayHTTPNodePort = 30080`. Absent → byte-identical behavior
  to today (opt-in, decision 3).
- Consumes: both gateway packs' EXISTING in-cluster pins of 30080
  (`packs/traefik/chart.yaml` ports.web.nodePort,
  `packs/envoy-gateway/manifests/10-gatewayclass.yaml:83`) — NO pack
  change is needed or allowed in this task.

- [ ] **Step 0: U1 follow-up (orchestrator amendment, 2026-07-18) — make
  the engine-wait narration visible live.** U1 landed `waitHealthy`
  narration with stage `"engine"`, but the live renderer shows log tails
  only under an open step of the SAME stage, and the step open during
  the wait is stage `"health"` — so the lines never render live (U1
  FINDINGS). Fix by adopting the open step's stage: in U1's
  `TestWaitHealthyNarratesUnhealthyWait` change the asserted
  `StepLog{Stage:…}` from `"engine"` to `"health"` → verify FAIL → change
  the one word in `waitHealthy`'s `con.Log("engine", …)` call to
  `"health"` → verify PASS. JSONL stays additive (stage values are not
  frozen), plain/JSON unaffected (StepLog renders zero bytes there).
  Commit: `git add internal/ && git commit -m "fix(up): engine-wait narration uses the open health step's stage"`

- [ ] **Step 1: Failing tests.** (a) `internal/config/load_test.go`:
  cube.yaml with `gateway: {…, httpPort: 8080}` loads and round-trips;
  `httpPort: 8443` equal to `port` fails validation; omitted → zero. (b)
  `internal/cluster/kindp/kind_test.go`:

```go
func TestRenderConfigMapsHTTPPortWhenSet(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443, HTTPPort: 8080}
	cfg, err := RenderConfig("dev", config.ClusterSpec{Provider: "kind"}, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(cfg)
	if !strings.Contains(s, "hostPort: 8080") || !strings.Contains(s, "containerPort: 30080") {
		t.Fatalf("http mapping missing:\n%s", s)
	}
	// And absent → absent (opt-in contract).
	gw.HTTPPort = 0
	cfg, _ = RenderConfig("dev", config.ClusterSpec{Provider: "kind"}, gw, CertsD{})
	if strings.Contains(string(cfg), "30080") {
		t.Fatalf("httpPort must be opt-in:\n%s", cfg)
	}
}
```

  (c) equivalent k3d test asserting the `30080:8080` port map in the
  rendered SimpleConfig. Run all three — Expected: FAIL.

- [ ] **Step 2: Implement.** `types.go`: add `HTTPPort int
  `yaml:"httpPort,omitempty" json:"httpPort,omitempty"`` to GatewaySpec
  and `const GatewayHTTPNodePort = 30080` next to `GatewayNodePort`
  (`internal/config/types.go:86`) with a comment naming both gateway
  packs' pins. `schema.cue`: `httpPort?: int & >0 & <65536` in the gateway
  block. `load.go` cross-validation: `httpPort == port` or `httpPort`
  colliding with any `extraPorts.hostPort` → CUBE-0002-family config error
  (use the existing invalid-config wrap the file uses, message
  `gateway.httpPort must differ from gateway.port and extraPorts`).
  kindp `RenderConfig`: inside the existing `if gw.Port > 0` block (S3's
  guard — if S3 is not yet merged this task creates the same guard;
  APPEND-ONLY conflict doctrine applies), add a second mapping when
  `gw.HTTPPort > 0`: hostPort `gw.HTTPPort` → containerPort
  `config.GatewayHTTPNodePort`. k3dp: mirror in its config renderer
  (find where `GatewayNodePort` is mapped; add the HTTP twin under the
  same condition). doctor: the port-in-use preflight (CUBE-0102) also
  probes `httpPort` when set. README: add the `gateway.httpPort` row to
  the cluster-shape caveat table.

- [ ] **Step 3: Verify pass** — Run:
  `go test ./internal/config/ ./internal/cluster/... ./internal/doctor/ -v -run 'HTTP|TestRenderConfig|TestDoctor'`
  Expected: PASS.

- [ ] **Step 4: Gate + commit** — full gate + fences. Commit:
  `git add internal/ README.md && git commit -m "feat(gateway): opt-in httpPort — host mapping onto pinned NodePort 30080"`

#### Outcome

```
STATUS: IN_PROGRESS(b67ed6f3-6188-4eca-ba99-88e5dad89e07, 2026-07-18T09:25:28Z)
BRANCH: p5/u2-http-port (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### U3: `engine.tuning` typed knobs → patched embedded manifests  `[repo: $ROOT]`

**Branch:** `p5/u3-engine-tuning` · **Depends:** U2

**Files:**
- Create: `internal/engine/tune.go`, `internal/engine/tune_test.go`
- Modify: `internal/config/types.go` (EngineTuning), `internal/config/schema.cue`,
  `internal/engine/factory/factory.go` (+ every `enginefactory.New` call
  site — enumerate with `grep -rn "enginefactory.New" cmd/ internal/`),
  `internal/engine/flux/flux.go` + `internal/engine/argocd/argocd.go`
  (apply tuning in `InstallManifests`), `cmd/config.go` (render-engine),
  `internal/diag/codes.go` + `registry.go` (CUBE-3009)
- Test: `internal/engine/tune_test.go`, `cmd/config_test.go`

**Interfaces:**
- Produces:

```go
// config side
type EngineSpec struct {
	Type   string        `yaml:"type" json:"type"`
	Tuning *EngineTuning `yaml:"tuning,omitempty" json:"tuning,omitempty"`
}
type EngineTuning struct {
	Components map[string]ComponentTuning `yaml:"components,omitempty" json:"components,omitempty"`
}
type ComponentTuning struct {
	Replicas  *int           `yaml:"replicas,omitempty" json:"replicas,omitempty"`
	Resources map[string]any `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// engine side
// ApplyTuning patches Deployments named in v.Components: spec.replicas and
// every container's resources. Unknown component → CUBE-3009 listing the
// Deployment names that exist. nil v is a no-op.
func engine.ApplyTuning(objs []*unstructured.Unstructured, v *config.EngineTuning) error
```

  `enginefactory.New(spec config.EngineSpec)` (was `New(engineType
  string)`) — engines carry values into `InstallManifests`. New command:
  `cube-idp config render-engine [-f cube.yaml]` printing the tuned
  install YAML.
- Consumes: embedded manifests seams `flux.InstallManifests`
  (`internal/engine/flux/flux.go:38`), `ArgoCD.InstallManifests`
  (`internal/engine/argocd/argocd.go:94`); `config render-cluster`
  precedent in `cmd/config.go` (CUBE-0004 pattern).

- [ ] **Step 1: Failing tune tests** — `internal/engine/tune_test.go`:

```go
package engine

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
)

func deployment(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": name, "namespace": "x"},
		"spec": map[string]any{
			"replicas": int64(1),
			"template": map[string]any{"spec": map[string]any{
				"containers": []any{map[string]any{"name": "main"}},
			}},
		},
	}}
}

func intp(i int) *int { return &i }

func TestApplyTuningPatchesReplicasAndResources(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("kustomize-controller"), deployment("source-controller")}
	v := &config.EngineTuning{Components: map[string]config.ComponentTuning{
		"kustomize-controller": {
			Replicas:  intp(2),
			Resources: map[string]any{"limits": map[string]any{"memory": "512Mi"}},
		},
	}}
	if err := ApplyTuning(objs, v); err != nil {
		t.Fatal(err)
	}
	rep, _, _ := unstructured.NestedInt64(objs[0].Object, "spec", "replicas")
	if rep != 2 {
		t.Fatalf("replicas = %d, want 2", rep)
	}
	cs, _, _ := unstructured.NestedSlice(objs[0].Object, "spec", "template", "spec", "containers")
	res := cs[0].(map[string]any)["resources"].(map[string]any)
	if res["limits"].(map[string]any)["memory"] != "512Mi" {
		t.Fatalf("resources not patched: %v", res)
	}
	// Untouched deployment stays untouched.
	rep2, _, _ := unstructured.NestedInt64(objs[1].Object, "spec", "replicas")
	if rep2 != 1 {
		t.Fatalf("source-controller must be untouched, replicas=%d", rep2)
	}
}

func TestApplyTuningUnknownComponentIsCube3009(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("source-controller")}
	v := &config.EngineTuning{Components: map[string]config.ComponentTuning{"nope": {Replicas: intp(2)}}}
	err := ApplyTuning(objs, v)
	if err == nil || !strings.Contains(err.Error(), "CUBE-3009") || !strings.Contains(err.Error(), "source-controller") {
		t.Fatalf("want CUBE-3009 naming valid components, got: %v", err)
	}
}

func TestApplyTuningNilIsNoop(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("a")}
	if err := ApplyTuning(objs, nil); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Verify fail** — Run:
  `go test ./internal/engine/ -run TestApplyTuning -v`
  Expected: FAIL — ApplyTuning undefined (config types too).

- [ ] **Step 3: Implement.** config types + CUE exactly per the
  Interfaces block (CUE:
  `engine: {type: *"flux" | "argocd", tuning?: {components?: {[=~"^[a-z0-9-]+$"]: {replicas?: int & >0, resources?: {...}}}}}`
  — replaces the current single-line `engine: type:` form). Nil-map
  round-trip discipline identical to `PackRef.Values`
  (`internal/config/types.go:115` comment). `internal/engine/tune.go`:

```go
package engine

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// ApplyTuning implements GT1: the closed engine.tuning knob set (replicas,
// resources) patched over the embedded install manifests in memory, before
// SSA. Plain manifests are the only engine install path (no helm) — this
// is a walk-and-set, not a re-render.
func ApplyTuning(objs []*unstructured.Unstructured, v *config.EngineTuning) error {
	if v == nil || len(v.Components) == 0 {
		return nil
	}
	deployments := map[string]*unstructured.Unstructured{}
	for _, o := range objs {
		if o.GetKind() == "Deployment" {
			deployments[o.GetName()] = o
		}
	}
	for name, tune := range v.Components {
		d, ok := deployments[name]
		if !ok {
			valid := make([]string, 0, len(deployments))
			for n := range deployments {
				valid = append(valid, n)
			}
			sort.Strings(valid)
			return diag.New(diag.CodeEngineTuningUnknown,
				fmt.Sprintf("engine.tuning.components.%s: no such engine component", name),
				"valid components for this engine: "+strings.Join(valid, ", "))
		}
		if tune.Replicas != nil {
			if err := unstructured.SetNestedField(d.Object, int64(*tune.Replicas), "spec", "replicas"); err != nil {
				return diag.Wrap(err, diag.CodeEngineTuningUnknown, "cannot set replicas", "report this as a bug")
			}
		}
		if len(tune.Resources) > 0 {
			cs, found, err := unstructured.NestedSlice(d.Object, "spec", "template", "spec", "containers")
			if err != nil || !found || len(cs) == 0 {
				return diag.New(diag.CodeEngineTuningUnknown,
					fmt.Sprintf("engine.tuning.components.%s: deployment has no containers to patch", name),
					"report this as a bug — the embedded manifest changed shape")
			}
			for i := range cs {
				c := cs[i].(map[string]any)
				c["resources"] = deepCopyJSON(tune.Resources)
				cs[i] = c
			}
			if err := unstructured.SetNestedSlice(d.Object, cs, "spec", "template", "spec", "containers"); err != nil {
				return diag.Wrap(err, diag.CodeEngineTuningUnknown, "cannot set resources", "report this as a bug")
			}
		}
	}
	return nil
}

// deepCopyJSON keeps the caller's map unshared (SetNestedSlice requires
// JSON-compatible values; config.Load already normalized numbers).
func deepCopyJSON(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if mm, ok := v.(map[string]any); ok {
			out[k] = deepCopyJSON(mm)
		} else {
			out[k] = v
		}
	}
	return out
}
```

Code: `CodeEngineTuningUnknown Code = "CUBE-3009"` in the 3xxx block +
registry entry. Factory: change `New(engineType string)` →
`New(spec config.EngineSpec)`; engines store `values *config.EngineTuning`
and their `InstallManifests()` calls `engine.ApplyTuning(objs, values)`
before returning (flux at `flux.go:38`'s function end, argocd at
`argocd.go:94`'s). Every `enginefactory.New` call site passes the full
`cube.Spec.Engine` (grep lists them: `internal/up/up.go:166`,
`cmd/cnoe.go`, down/status/sync/repo sites — update ALL; the compiler is
the checklist). `cmd/config.go`: add `render-engine` subcommand cloning
`render-cluster`'s shape: load config, `enginefactory.New(cube.Spec.Engine)`,
`InstallManifests()`, marshal all objects as a `---`-separated YAML stream
to stdout. Test in `cmd/config_test.go`: with `tuning: {components:
{"source-controller": {replicas: 2}}}` (engine flux) the rendered stream
contains `replicas: 2`; with an unknown component the command fails
mentioning CUBE-3009.

- [ ] **Step 4: Verify pass** — Run:
  `go test ./internal/engine/... ./internal/config/ ./cmd/ -run 'TestApplyTuning|TestRenderEngine|TestEngineTuning' -v`
  Expected: PASS.

- [ ] **Step 5: Gate + fences + commit** — full gate + fences (factory
  signature change touches many packages — the build IS the migration
  checklist). Commit:
  `git add internal/ cmd/ && git commit -m "feat(engine): engine.tuning typed knobs — replicas/resources patched pre-SSA (CUBE-3009)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/u3-engine-tuning (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### U4: the values stone — helm-only enforcement, `extraManifests`, CUSTOMIZED  `[repo: $ROOT]`

**Branch:** `p5/u4-values-stone` · **Depends:** U3

Implements GT15. Three deliverables: (1) `values:` on a chartless pack is
a typed error, (2) `packs[].extraManifests` appends raw resources to any
pack kind, (3) customized installs are visible in `kubectl get packs`.

**Files:**
- Modify: `internal/config/types.go` (PackRef.ExtraManifests),
  `internal/config/schema.cue` (`extraManifests?: string & !=""`),
  `internal/pack/render.go` (RenderWith + guard),
  `internal/pack/manifests/pack-crd.yaml` (CUSTOMIZED printer column),
  `internal/up/up.go` (D11 record writer sets `customized`; pack loop
  calls RenderWith — grep `.RenderFor(` for every call site: up, diff,
  others go to FINDINGS), `internal/diag/codes.go` + `registry.go`
  (CUBE-4016, CUBE-4017)
- Test: `internal/pack/render_test.go`, `internal/config/load_test.go`

**Interfaces:**
- Produces:

```go
// PackRef gains the GT15 extras channel (any pack kind):
ExtraManifests string `yaml:"extraManifests,omitempty" json:"extraManifests,omitempty"`

// RenderWith is RenderFor plus the values stone: non-empty values on a
// pack without chart.yaml → CUBE-4016; extraManifests parsed as
// multi-doc YAML, ${GATEWAY_*}-substituted, appended (CUBE-4017 on
// invalid YAML). RenderFor keeps its exact current behavior for tests.
func (p *Pack) RenderWith(values map[string]any, extraManifests string, gw config.GatewaySpec) (*Rendered, error)

// HasChart reports whether the pack carries a chart.yaml (stone guard).
func (p *Pack) HasChart() bool
```

  Pack record field `customized: "yes"|"no"` + CRD
  `additionalPrinterColumns` entry `CUSTOMIZED`.
- Consumes: `substitute` + `apply.ParseMultiDoc` exactly as the
  manifests walk uses them (`internal/pack/render.go:82`); the D11
  record writer in `internal/up/up.go` (~:364).

- [ ] **Step 1: Failing render tests** — append to
  `internal/pack/render_test.go` (mirror its fixture helpers):

```go
func TestRenderWithValuesOnChartlessPackIsCube4016(t *testing.T) {
	p := loadFixturePack(t, "manifests-only") // reuse/build a chartless fixture
	_, err := p.RenderWith(map[string]any{"x": 1}, "", config.GatewaySpec{})
	if err == nil || !strings.Contains(err.Error(), "CUBE-4016") {
		t.Fatalf("values on chartless pack must be CUBE-4016, got: %v", err)
	}
}

func TestRenderWithExtraManifestsAppendsAndSubstitutes(t *testing.T) {
	p := loadFixturePack(t, "manifests-only")
	extra := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: seed, namespace: x}\ndata: {URL: \"https://app.${GATEWAY_HOST}\"}\n"
	r, err := p.RenderWith(nil, extra, config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443})
	if err != nil {
		t.Fatal(err)
	}
	last := r.Objects[len(r.Objects)-1]
	if last.GetKind() != "ConfigMap" || last.GetName() != "seed" {
		t.Fatalf("extras not appended: %v", last)
	}
	data, _, _ := unstructured.NestedStringMap(last.Object, "data")
	if !strings.Contains(data["URL"], "cube-idp.localtest.me") {
		t.Fatalf("extras not substituted: %v", data)
	}
	// Invalid YAML → CUBE-4017.
	if _, err := p.RenderWith(nil, "{not yaml", config.GatewaySpec{}); err == nil || !strings.Contains(err.Error(), "CUBE-4017") {
		t.Fatalf("bad extras must be CUBE-4017, got: %v", err)
	}
}
```

  VERIFY-API: the fixture-loading helper name in render_test.go; the
  `substitute` function's exact name/location (expose.go). Record drift
  in FINDINGS.

- [ ] **Step 2: Verify fail** — Run:
  `go test ./internal/pack/ -run TestRenderWith -v`
  Expected: FAIL — RenderWith undefined.

- [ ] **Step 3: Implement.** `HasChart` (stat chart.yaml), `RenderWith`:

```go
func (p *Pack) RenderWith(values map[string]any, extraManifests string, gw config.GatewaySpec) (*Rendered, error) {
	if len(values) > 0 && !p.HasChart() {
		return nil, diag.New(diag.CodePackValuesChartless,
			fmt.Sprintf("pack %s has no chart.yaml — values: are helm values only (GT15)", p.Name),
			"use extraManifests to add raw resources, or remove values")
	}
	r, err := p.RenderFor(values, gw)
	if err != nil {
		return nil, err
	}
	if extraManifests != "" {
		objs, err := apply.ParseMultiDoc([]byte(substitute(extraManifests, gw)))
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackExtraManifests,
				fmt.Sprintf("pack %s: extraManifests is not valid YAML", p.Name), "fix the extraManifests block in cube.yaml")
		}
		r.Objects = append(r.Objects, objs...)
	}
	return r, nil
}
```

  Codes (4xxx block, after 4015):

```go
	CodePackValuesChartless Code = "CUBE-4016" // values: set on a pack without chart.yaml (values are helm-only, GT15)
	CodePackExtraManifests  Code = "CUBE-4017" // packs[].extraManifests is not valid multi-doc YAML
```

  (+ registry entries). Config field + CUE. Rewire the up pack loop (and
  `diff`) from `RenderFor(values, gw)` to
  `RenderWith(ref.Values, ref.ExtraManifests, gw)` — the compiler and
  grep are the checklist; list every touched call site in FINDINGS.

- [ ] **Step 4: Verify pass** — Run:
  `go test ./internal/pack/ ./internal/config/ -run 'TestRenderWith|ExtraManifests' -v`
  Expected: PASS.

- [ ] **Step 5: CUSTOMIZED surface.** Add to the Pack CRD
  (`internal/pack/manifests/pack-crd.yaml`) an `additionalPrinterColumns`
  entry `CUSTOMIZED` (JSONPath onto the record field), and set
  `customized: yes|no` in the D11 record writer
  (`len(ref.Values) > 0 || ref.ExtraManifests != ""`). Unit-test the
  record object's field; the visual `kubectl get packs` check rides the
  existing e2e (no new leg). Note: the CRD is applied by `up` (D11 —
  wait=true Established); a changed CRD re-applies idempotently.

- [ ] **Step 6: Gate + fences + commit** — full gate + fences. Commit:
  `git add internal/ && git commit -m "feat(pack): values stone — helm-only values, extraManifests, CUSTOMIZED (CUBE-4016/4017)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/u4-values-stone (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### U5: doctor tri-state checklist  `[repo: $ROOT]`

**Branch:** `p5/u5-doctor-checklist` · **Depends:** U4

Implements GT18: doctor shows EVERY check as one green/yellow/red row —
what was checked, what passed, what didn't. Today `doctor` renders only
findings (problems); passes are invisible.

**Files:**
- Modify: `internal/doctor/doctor.go` (Check registry wrapping the
  existing `CheckRuntime`/`CheckPortFree`/`CheckGitCLI`/disk/inotify
  funcs — do NOT rewrite the checks, wrap them), `cmd/doctor.go`
  (checklist render + `-o json` additive `checks`),
  `docs/machine-readable-output.md`
- Test: `internal/doctor/doctor_test.go`, `cmd/doctor_test.go`

**Interfaces:**
- Produces:

```go
// Check is one named doctor probe. Run returns nil for green; a Finding
// with SeverityWarning for yellow; SeverityError for red (GT18).
type Check struct {
	Name   string           // stable id, e.g. "container-runtime"
	Detail string           // one-line "what passed looks like", filled by Run on green
	Run    func() *diag.Finding
}

// All assembles every check for this cube: container-runtime,
// gateway-port (and http-port when U2's field is set), disk-space,
// inotify (linux), git-cli (when git-sourced refs exist),
// spoke-reachability (when spokes declared — S4's probe).
func All(cube *config.Cube, clusterExists bool) []Check
```

  Rendered rows (styled: theme.OK/Warn/Err, glyph+word ALWAYS paired;
  plain: same rows with `ok`/`warn`/`fail` words, no glyphs):

```text
✔ ok    container-runtime   docker 27.x on PATH
✔ ok    gateway-port        8443 free
⚠ warn  disk-space          cache dir has 3.1G free — CUBE-0103
✗ fail  spoke-reachability  spoke "staging" unreachable — CUBE-8006
```

- Consumes: existing check funcs + `Render` exit contract
  (`internal/doctor/doctor.go:100` — returns hasErrors; VERIFY the
  doctor command's current exit path and PRESERVE it: exit 1 iff any
  red; record the current mechanism in FINDINGS), `ui.Printer` static
  surface, S4's spoke probe.

- [ ] **Step 1: Failing tests.** (a) `internal/doctor/doctor_test.go`:
  `All(cube, false)` on a minimal cube returns ≥4 checks with unique
  names; a stubbed all-green run yields zero findings and every Detail
  non-empty. (b) `cmd/doctor_test.go`: with a stubbed check set (one
  green, one warn, one fail — seam: a package-level `var doctorChecks =
  doctor.All` overridable in tests, same pattern as `statusConnect`),
  output contains all three rows with paired glyph+word (styled) /
  word-only (plain), and the command exits non-zero (fail present); with
  green+warn only → exit 0. (c) `-o json` contains
  `"checks":[{"name":"container-runtime","status":"ok"` — additive.
  Run: `go test ./internal/doctor/ ./cmd/ -run 'TestDoctor' -v` —
  Expected: FAIL.
- [ ] **Step 2: Implement** the registry (wrap existing funcs; green
  Detail strings come from what each check already knows), the renderer
  (one row per check, summary line unchanged in meaning), the JSON
  field, the docs section. Re-run — Expected: PASS.
- [ ] **Step 3: Gate + fences + commit** — full gate + fences (doctor's
  human rows are NOT part of the frozen event-mode matrix — verify
  `TestModeMatrixFence` scope stays green untouched; `-o json` additive
  only, GT13). Commit:
  `git add internal/doctor/ cmd/ docs/ && git commit -m "feat(doctor): tri-state checklist — every check as a green/yellow/red row (GT18)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/u5-doctor-checklist (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

## Lane P — pack platform (W0 → catalog → Gitea)

### P1: pack contract v1 — normative doc + conformance test + `description`  `[repo: $ROOT]`

**Branch:** `p5/p1-pack-contract` · **Depends:** none

**Files:**
- Create: `docs/pack-contract-v1.md`
- Modify: `internal/pack/pack.go` (parse optional `description`),
  `internal/pack/pack_test.go`, all 7 `packs/*/pack.cue` (add
  `description`), `internal/pack/contract_conformance_test.go` (create)

**Interfaces:**
- Produces: `Pack.Description string` (from optional `description: string`
  in pack.cue — used by P2's index and P6's catalog);
  `docs/pack-contract-v1.md` — the frozen public API (GT12);
  `TestReposPacksSatisfyContractV1` walking `packs/*`.
- Consumes: current pack.cue semantics (`internal/pack/pack.go` parse,
  `expose.go`, D15 values order in `internal/pack/helm.go:138`).

- [x] **Step 1: Failing description test** — in
  `internal/pack/pack_test.go` add a fixture pack.cue containing
  `description: "in-cluster git server"` and assert the parsed
  `Pack.Description` matches; a pack.cue WITHOUT description parses with
  `Description == ""` (optional, backward-compatible).
  Run: `go test ./internal/pack/ -run TestPack.*Description -v`
  Expected: FAIL.

- [x] **Step 2: Implement + pass.** Add the field to the pack.cue schema
  the loader compiles (find where `name`/`version` are read in
  `internal/pack/pack.go`; description is read the same way, optional).
  Re-run — Expected: PASS.

- [x] **Step 3: Write `docs/pack-contract-v1.md`** — normative, complete,
  no TBDs. Sections, each stating today's ACTUAL behavior (verify each
  claim in code as you write; FINDINGS records any surprise):
  1. **Layout** — `pack.cue` (required) + exactly one of `manifests/`
     (plain YAML, ordered by filename) or `chart.yaml` (helm, client-side
     rendered) or `kustomize` entry (per `internal/pack/kustomize.go`).
  2. **pack.cue fields** — `name` (required, `^[a-z0-9][a-z0-9-]{0,30}$`,
     MUST equal the directory and artifact name), `version` (required
     semver, MUST equal the publish tag), `description` (optional, one
     line, NEW in v1), `expose: {urls, authSecretRef, impliedFields}`
     (optional, per `internal/pack/expose.go` semantics).
  3. **Substitution** — `${GATEWAY_HOST}` and `${GATEWAY_FQDN}` in
     manifests and values, applied AFTER defaults-merge (D15,
     `internal/pack/helm.go:138`).
  4. **Values (the stone, GT15)** — merge order for helm packs: chart
     defaults ← pack.cue/chart.yaml defaults ← user `values:` ←
     substitution; numbers normalized int/float64. MUST state the stone
     verbatim: **`values:` are helm values only, consumed exclusively by
     the `chart.yaml` render**; on a pack without `chart.yaml` they are a
     typed error (CUBE-4016 — raised at render time, since layout is
     unknown until the ref is fetched; U4 enforces). Document
     **`extraManifests`** in its own short section: a multi-doc YAML
     string appended to ANY pack kind after `${GATEWAY_*}` substitution
     (CUBE-4017 on invalid YAML), and the **CUSTOMIZED** marker on
     `kubectl get packs` (set when values or extraManifests are present).
     Manifests-only parametrization = `${GATEWAY_*}` variables,
     `extraManifests`, or add a chart.
  5. **Artifact** — OCI media types exactly as `internal/oci/pushdir.go`
     produces (name them from the source), tag = pack version, digest
     immutability, `<name>/vX.Y.Z` git-tag convention (GT9).
  6. **Compatibility** — additive-only within v1; any breaking change
     bumps the contract version and the consuming cube-idp minor.
- [x] **Step 4: Conformance test** — create
  `internal/pack/contract_conformance_test.go`:

```go
package pack

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReposPacksSatisfyContractV1 walks the repo's packs/ tree and
// enforces docs/pack-contract-v1.md mechanically: every pack loads, has
// name==dir, semver version, and (v1) a non-empty description. This test
// moves to $PACKS with the packs in P4 — P3's harness runs it there.
func TestReposPacksSatisfyContractV1(t *testing.T) {
	root := filepath.Join("..", "..", "packs")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("no packs/ tree at %s (post-P4 layout): %v", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		p, err := Load(dir) // VERIFY-API: use the real single-pack load func from pack.go
		if err != nil {
			t.Errorf("%s: does not load: %v", e.Name(), err)
			continue
		}
		if p.Name != e.Name() {
			t.Errorf("%s: pack.cue name %q != directory", e.Name(), p.Name)
		}
		if p.Description == "" {
			t.Errorf("%s: contract v1 requires description", e.Name())
		}
	}
}
```

  Add one-line `description:` to all 7 `packs/*/pack.cue` (reuse the
  `cmd/pack.go` packCatalog wording for gitea/argocd; write apt one-liners
  for the rest). Run: `go test ./internal/pack/ -run Contract -v` —
  Expected: PASS with all 7 packs green.

- [x] **Step 5: Gate + commit** — full gate. Commit:
  `git add docs/pack-contract-v1.md internal/pack/ packs/ && git commit -m "docs+feat(pack): contract v1 frozen — description field + mechanical conformance"`

#### Outcome

```
STATUS: DONE
BRANCH: p5/p1-pack-contract (merged: yes)
COMMITS: 95e9e2c docs+feat(pack): contract v1 frozen — description field + mechanical
  conformance; 8ee519e merge: p5 P1 pack-contract (p5/p1-pack-contract)
FINDINGS: (1) claim commit rode 66d63f1 ("claim S1") — the sibling's git add
  picked up my staged plan edit; HEAD verified showing P1 IN_PROGRESS per the
  dispatch note, so no separate "claim P1" commit exists in history. (2)
  VERIFY-API: the plan snippet's Load(dir) does not exist — the real
  single-pack loader is unexported loadMeta(dir) in pack.go (Fetch is the
  exported ref-resolving wrapper); the conformance test is in package pack and
  calls loadMeta directly. (3) The conformance test also asserts the name
  pattern ^[a-z0-9][a-z0-9-]{0,30}$ and semver version — named by the
  snippet's own comment and doc §2; all 7 packs pass. (4) Doc §1 states
  ACTUAL layout semantics: raw-manifest source is exactly one of
  kustomization.yaml (sole source, governs manifests/) OR the manifests/
  walk; chart.yaml is orthogonal and APPENDED in both cases — the plan
  sketch's "exactly one of manifests/ or chart.yaml or kustomize" is not
  today's behavior. Also documented: the third substitution token
  ${GATEWAY_PACK} (F9), and the optional #Values/images/gatewayService
  pack.cue fields (completeness). (5) description is loader-OPTIONAL
  (backward compat, Description=="" when absent) but conformance-REQUIRED
  for repo packs — doc §2 states the split. (6) "numbers normalized
  int/float64" verified: config.Load normalizePackValues (load.go:65)
  rewrites CUE's int64. (7) No new CUBE codes in P1; CUBE-4016/4017 appear
  in the doc as GT15 contract statements — U4 implements them. (8) pack.cue
  name/version/description lines are column-aligned (cue fmt style) in all
  7 packs.
REVIEW: TDD red→green verified for both steps: description tests failed
  compile (p.Description undefined) then passed; conformance test failed red
  on all 7 packs missing description, green after adding them. Every doc
  claim checked against source while writing: render.go (precedence, sorted
  walk, zero-object error, namespace injection), helm.go (D15 merge order
  L138-142, hook flattening), expose.go (3 tokens, port-omit-on-443),
  pushdir.go (media types, fixed-epoch annotation, tar layout), load.go
  (int64 normalization). Worktree gate: go build && go vet && go test ./...
  all green; post-merge go test ./... on main all green (cmd + ui fence
  suites included in both full runs).
BLOCKERS: none
HANDOFF: P2 reads Pack.Description for the index artifact — it is parsed and
  populated for all 7 packs. docs/pack-contract-v1.md in $ROOT is normative
  (GT12): copy VERBATIM to $PACKS/CONTRACT.md. TestReposPacksSatisfyContractV1
  (internal/pack/contract_conformance_test.go) walks ../../packs and
  t.Skip()s once packs/ leaves the main repo (post-P4) — P3's harness must
  run it in $PACKS. cmd/pack.go packCatalog descriptions (gitea "in-cluster
  git server", argocd "delivery UI") match the pack.cue descriptions
  verbatim — keep them in sync until P6 replaces the hardcoded catalog.
```

---

### P2: packs repo scaffold + `pack publish`/`pack index` + publish CI  `[repo: $PACKS — creates it; commands land in $ROOT]`

**Branch:** `p5/p2-packs-repo` (in BOTH repos — same name) · **Depends:** P1

**Files:**
- Create in $PACKS (new git repo): `README.md`, `CONTRACT.md` (copy of
  docs/pack-contract-v1.md), `.github/workflows/publish.yml`,
  `.github/workflows/conformance.yml` (stub — P3 fills it), `hack/`,
  `packs/.gitkeep`
- Create in $ROOT: `cmd/pack_publish.go`, `cmd/pack_publish_test.go`
- Modify in $ROOT: `cmd/pack.go` (register subcommands)

**Interfaces:**
- Produces: `cube-idp pack publish <dir> --ref oci://<host>/<repo>:<tag>`
  (wraps `oci.PushPackDir`, `internal/oci/pushdir.go:54` — prints the
  digest, exits per existing error doctrine);
  `cube-idp pack index build <packs-dir> -o index.json` and
  `cube-idp pack index push index.json --ref oci://…/index:latest`.
  Index schema (consumed by P5's CI attestation, P6's catalog):

```json
{"schemaVersion": 1, "packs": [
  {"name": "gitea", "version": "0.2.0", "description": "in-cluster git server",
   "ref": "oci://ghcr.io/cube-idp/packs/gitea:0.2.0", "digest": "sha256:…"}
]}
```

- Consumes: P1 `Pack.Description`; `oci.PushPackDir(ctx, dir, ociRef,
  alsoTags...)` returning the digest; docker credential chain for ghcr
  auth (fixed in 3d7f4cd).

- [x] **Step 1: Failing publish/index tests** ($ROOT) —
  `cmd/pack_publish_test.go`: (a) `pack publish` against the repo's
  ocitest fake registry (`internal/oci/ocitest` — reuse its harness the
  way `internal/oci/pushdir_test.go` does) publishes `packs/gitea` and
  prints a `sha256:` digest; (b) `pack index build` over a temp dir with
  two minimal packs writes index.json matching the schema above
  (marshal-compare after normalizing digests); (c) `pack index push`
  pushes index.json to the fake registry and prints its digest.
  Run: `go test ./cmd/ -run TestPackPublish -v` — Expected: FAIL.

- [x] **Step 2: Implement `cmd/pack_publish.go`** — three cobra commands
  under `pack`; `publish` validates the dir loads as a pack (P1 loader)
  and its version equals the ref tag before pushing (mismatch → CUBE-4001
  with a fix line); `index build` loads every pack dir, requires
  descriptions (contract v1), and needs each pack's digest — compute it
  the way `pushdir.go` computes/returns digests WITHOUT pushing
  (VERIFY-API: if no offline digest helper exists, `index build` takes
  `--digest name=sha256:…` repeatable flags AND a `--from-registry` mode
  that HEADs the fake/real registry; implement the flag form first, note
  the choice in FINDINGS — CI passes digests from publish output);
  `index push` wraps `oci.PushPackDir` over a temp dir containing only
  index.json. Re-run — Expected: PASS.

- [x] **Step 3: Commit ($ROOT)** —
  `git add cmd/ && git commit -m "feat(pack): publish + index build/push — the packs-repo CI toolchain"`

- [ ] **Step 4: ⚠ OWNER GATE — create the public repo.** STOP and report
  NEEDS_CONTEXT listing exactly:
  `gh repo create cube-idp/packs --public --description "cube-idp packs — data-only platform packs, published as attested OCI artifacts"`
  plus the ONE repo secret the CI bootstrap needs while the main repo is
  private: `CUBE_IDP_READ_TOKEN` (a read-only PAT for checking out
  cube-idp/cube-idp in workflows; deleted when the main repo goes public
  or a public release exists). No signing keys exist anywhere — GT10 uses
  GitHub's keyless attestations. Proceed past this gate ONLY if the
  dispatch prompt pre-authorized it; otherwise continue with Steps 5-7
  locally (git init, no remote) and leave pushing to the owner.

- [x] **Step 5: Scaffold $PACKS.** `git init cube-idp-packs` as $ROOT's
  sibling; commit README.md (what the repo is, how to add a pack —
  pointing at CONTRACT.md and the A-task template in this plan),
  CONTRACT.md (verbatim copy; header notes $ROOT's copy is normative,
  GT12), `packs/.gitkeep`, and `hack/publish-changed.sh`:

```bash
#!/usr/bin/env bash
# Publish every pack whose <name>/vX.Y.Z tag matches this git ref, then
# rebuild and push the index. Requires: cube-idp on PATH, ghcr login.
set -euo pipefail
REF="${GITHUB_REF_NAME:?set GITHUB_REF_NAME (e.g. gitea/v0.2.0)}"
NAME="${REF%%/v*}"; VERSION="${REF##*/v}"
test -d "packs/$NAME" || { echo "no such pack: $NAME"; exit 1; }
DIGEST=$(cube-idp pack publish "packs/$NAME" --ref "oci://ghcr.io/cube-idp/packs/$NAME:$VERSION" | grep -o 'sha256:[a-f0-9]*')
echo "published $NAME:$VERSION @ $DIGEST"
echo "$NAME=$DIGEST" >> digests.env
```

- [x] **Step 6: `.github/workflows/publish.yml`:**

```yaml
name: publish
on:
  push:
    tags: ['*/v*']
permissions:
  contents: read
  packages: write
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: '1.24'}
      # Bootstrap: build the publisher from the cube-idp repo until a
      # public release exists (F12 closes in P4; then swap to a pinned
      # release download and delete these two steps).
      - uses: actions/checkout@v4
        with: {repository: cube-idp/cube-idp, path: cube-idp-src, token: '${{ secrets.CUBE_IDP_READ_TOKEN }}'}
      - run: cd cube-idp-src && go build -o /usr/local/bin/cube-idp . 
      - run: echo '${{ secrets.GITHUB_TOKEN }}' | docker login ghcr.io -u '${{ github.actor }}' --password-stdin
      - run: hack/publish-changed.sh
      - name: attest (P5 wires GitHub attestation here)
        run: echo "attestation added by P5 — digests in digests.env"
      - name: rebuild index
        run: |
          source <(sed 's/^/DIGEST_/' digests.env) || true
          cube-idp pack index build packs -o index.json $(sed 's/^/--digest /' digests.env)
          cube-idp pack index push index.json --ref oci://ghcr.io/cube-idp/packs/index:latest
```

  NOTE for the agent: this workflow CANNOT run until the owner gate (repo
  + secrets) is done — commit it as authored; a `workflow_dispatch`
  trigger may be added for a dry run. The index rebuild rebuilds from ALL
  packs — `index build` must therefore accept `--digest` for the ones
  published this run and `--from-registry` for the rest, OR the workflow
  keeps a committed `digests.lock` — implement the simplest one your Step
  2 chose and make the workflow match; FINDINGS records it.

- [x] **Step 7: Commit ($PACKS)** —
  `git add -A && git commit -m "chore: packs repo scaffold — contract, publish CI, index"`
  Then in $ROOT close the ledger normally. HANDOFF must state: $PACKS
  path, whether the owner gate ran (repo exists? secrets set?), and the
  digest-passing mode chosen for `index build`.

#### Outcome

```
STATUS: DONE_WITH_CONCERNS
BRANCH: p5/p2-packs-repo (merged: yes — in BOTH repos: $ROOT 8f10fb2, $PACKS 9a30593)
COMMITS: $ROOT: 7fad66c feat(pack): publish + index build/push — the
  packs-repo CI toolchain; 8f10fb2 merge: p5 P2 packs-repo
  (p5/p2-packs-repo). $PACKS (NEW repo, sibling cube-idp-packs): 0a01aa7
  chore: repo init (empty root commit so the feature branch had a merge
  base — a fresh repo cannot branch/merge otherwise); 2f89f85 chore: packs
  repo scaffold — contract, publish CI, index; 9a30593 merge: p5 P2
  packs-repo (p5/p2-packs-repo).
FINDINGS: (1) Step 4 OWNER GATE NOT run (dispatch: not pre-authorized) —
  its box is deliberately unticked: no GitHub repo created, no secret set,
  nothing pushed anywhere; Steps 5-7 executed locally per the gate's own
  fallback. Exact owner commands in HANDOFF. (2) VERIFY-API digest
  sourcing: internal/oci exports NO offline digest helper (pushPackDirTo
  is an unexported oras.Target seam, and P2's Files list excludes
  internal/oci), so per the plan's stated fallback `index build` takes
  repeatable `--digest name=sha256:…` AND `--from-registry`
  (pack.ResolveRemote — HEAD, never pull; digest = pin minus its "oci:"
  prefix). publish.yml's rebuild-index step matches: `--from-registry
  $(sed 's/^/--digest /' digests.env)`; the plan draft's dead `source <(sed
  …)` line was dropped (it set unused DIGEST_* vars). No digests.lock. (3)
  CUBE-4001 = diag.CodePackRefInvalid ("unsupported pack ref scheme")
  reused per plan text for the version≠tag mismatch; NO new codes minted
  (none were assigned to P2). Index-build contract violations (missing
  description, pack.cue name ≠ directory) reuse CUBE-4003
  CodePackCueInvalid; packs-dir read failures, zero-pack dirs, and
  index-file read/parse errors reuse CUBE-4004 CodePackManifestErr. (4)
  `pack publish` defaults a tagless --ref to :<pack.cue version> (mirrors
  `pack push`); an @digest ref fails the same mismatch check (a digest
  target cannot satisfy tag==version). Zero-pack `index build` is a typed
  error, not an empty artifact — an accidental empty index would wipe the
  published catalog for P6 consumers. (5) Index entries sorted by name;
  index.json byte-deterministic for identical inputs (republish no-op
  property preserved end to end). (6) runCLI/mustRunCLI helpers from S1
  (cmd/spoke_test.go) reused, as the plan's S1 VERIFY-API anticipated. (7)
  TestPackPublishIndexBuildNeedsDigestSource asserts the fix line via
  errors.As → diag.Error.Remediation: Error() carries only code+summary,
  remediations render through diag.Render. (8) $PACKS extras beyond the
  listed files: .gitignore (.claude/worktrees/ for the P3/A-task worktree
  protocol; digests.env + index.json publish scratch) and repo-local git
  identity (fresh repo had none). CONTRACT.md needed no prepended header:
  P1's doc already opens with the GT12 normativity note — diff-verified
  verbatim. (9) conformance.yml committed as an explicit stub for P3
  (pull_request + workflow_dispatch, echo only). No workflow_dispatch added
  to publish.yml — hack/publish-changed.sh requires a tag-shaped
  GITHUB_REF_NAME, so a dispatch dry run would always fail. (10) Workflows
  reviewed against the Actions injection doctrine: the tag name reaches
  the script only as the quoted GITHUB_REF_NAME default env var, never via
  ${{ }} interpolation into run:; triggers are tag-push and pull_request
  (not pull_request_target).
REVIEW: TDD red→green verified: all 7 TestPackPublish* tests failed
  (unknown command/flag) before implementation, all PASS after. Round
  trips proven against the in-process registry: publish → pack.Fetch
  returns name demo/1.0.0; `index push` → pack.ResolveRemote pin equals
  the printed digest exactly; --from-registry index digest equals the
  publish-printed digest. Worktree gate green: go build ./... && go vet
  ./... && go test ./... (29 pkgs ok, 0 FAIL) plus the fence run
  (./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|
  TestPromptFence') green — no frozen surface moved. Post-merge go test
  ./... on $ROOT main green. $PACKS verified: CONTRACT.md diff-identical
  to docs/pack-contract-v1.md, bash -n on publish-changed.sh (and +x
  mode), both workflow YAMLs parse.
BLOCKERS: none
HANDOFF: $PACKS exists at the $ROOT sibling path `../cube-idp-packs`,
  main at 9a30593 (scaffold merged), branch p5/p2-packs-repo kept, git
  history entirely local — NO remote configured. OWNER GATE STILL OPEN;
  the owner must run: (a) `gh repo create cube-idp/packs --public
  --description "cube-idp packs — data-only platform packs, published as
  attested OCI artifacts"`; (b) mint a read-only PAT that can check out
  cube-idp/cube-idp and `gh secret set CUBE_IDP_READ_TOKEN --repo
  cube-idp/packs` (delete the secret when the main repo goes public or a
  public release exists); (c) `git -C ../cube-idp-packs remote add origin
  git@github.com:cube-idp/packs.git && git push -u origin main` (plus the
  branch if wanted). Until then the publish workflow cannot run — but P3
  proceeds fully locally: repo, hack/, packs/.gitkeep, and the
  conformance.yml stub are all in place. For P3/P5/P6: index digest modes
  are `--digest name=sha256:…` (from `pack publish` output — CI writes
  digests.env) and/or `--from-registry`; index schema is {schemaVersion:1,
  packs:[{name,version,description,ref,digest}]}, entries sorted by name;
  `pack index build` REQUIRES contract-v1 descriptions and name==dir.
  cmd/pack.go's hardcoded packCatalog is untouched (P6 replaces it).
```

---

### P3: conformance harness — CI + local runner  `[repo: $PACKS]`

**Branch:** `p5/p3-conformance` · **Depends:** P2

**Files:**
- Create in $PACKS: `hack/conformance.sh`, finalize
  `.github/workflows/conformance.yml`
- Create in $PACKS: `hack/conformance_config.tmpl.yaml`

**Interfaces:**
- Produces: `hack/conformance.sh <pack-name>` — the ONE command every A
  task and CI runs: kind cluster + `cube-idp up` with only that pack +
  health gate + teardown. Exit 0 = conformant.
- Consumes: `cube-idp init --local` semantics (absolute local refs — see
  README "Developing against an unreleased checkout" and
  `tests/e2e/e2e_test.go`), `cube-idp status --exit-status` (W2.T12) as
  the health gate, GT14 port override.

- [ ] **Step 1: `hack/conformance.sh`:**

```bash
#!/usr/bin/env bash
# Conformance: one pack, one throwaway kind cluster, hard health gate.
# Usage: conformance.sh <pack-name> [cube-idp-binary]
set -euo pipefail
PACK="${1:?usage: conformance.sh <pack-name>}"
BIN="${2:-cube-idp}"
PORT="${CUBE_IDP_E2E_GATEWAY_PORT:-18443}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"; trap 'cd /; "$BIN" down --yes -f "$WORK/cube.yaml" >/dev/null 2>&1 || true; rm -rf "$WORK"' EXIT
NAME="conf-${PACK//[^a-z0-9]/}"
sed -e "s|{{NAME}}|$NAME|" -e "s|{{PORT}}|$PORT|" -e "s|{{PACK_DIR}}|$ROOT/packs/$PACK|" \
  "$ROOT/hack/conformance_config.tmpl.yaml" > "$WORK/cube.yaml"
cd "$WORK"
"$BIN" up -f cube.yaml
"$BIN" status -f cube.yaml --exit-status
echo "CONFORMANT: $PACK"
```

  with `hack/conformance_config.tmpl.yaml`:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: "{{NAME}}"}
spec:
  cluster: {provider: kind}
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: {{PORT}},
            ref: "oci://ghcr.io/cube-idp/packs/traefik:0.2.0"}
  packs:
    - {ref: "{{PACK_DIR}}"}
```

  NOTE: until P4 publishes the gateway pack, CI must check out the
  cube-idp source and point `gateway.ref` at its local
  `packs/traefik` — the workflow below does exactly that and P4's agent
  swaps it to the oci ref + removes the checkout. The gateway pack under
  test-by-name (traefik/envoy) instead uses `gateway.ref: {{PACK_DIR}}`
  and drops the packs list — the script special-cases
  `PACK in (traefik, envoy-gateway)` with a second template
  `conformance_config_gateway.tmpl.yaml` (same file, no `packs:` list,
  `gateway.pack` substituted). Write both templates.

- [ ] **Step 2: `.github/workflows/conformance.yml`:**

```yaml
name: conformance
on:
  pull_request:
    paths: ['packs/**']
jobs:
  changed:
    runs-on: ubuntu-latest
    outputs: {packs: '${{ steps.diff.outputs.packs }}'}
    steps:
      - uses: actions/checkout@v4
        with: {fetch-depth: 0}
      - id: diff
        run: |
          PACKS=$(git diff --name-only origin/${{ github.base_ref }}... -- packs/ | cut -d/ -f2 | sort -u | jq -Rnc '[inputs]')
          echo "packs=$PACKS" >> "$GITHUB_OUTPUT"
  conformance:
    needs: changed
    if: needs.changed.outputs.packs != '[]'
    runs-on: ubuntu-latest
    strategy:
      matrix: {pack: '${{ fromJSON(needs.changed.outputs.packs) }}'}
      fail-fast: false
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: '1.24'}
      - uses: actions/checkout@v4
        with: {repository: cube-idp/cube-idp, path: cube-idp-src, token: '${{ secrets.CUBE_IDP_READ_TOKEN }}'}
      - run: cd cube-idp-src && go build -o /usr/local/bin/cube-idp .
      - run: hack/conformance.sh '${{ matrix.pack }}'
        env: {CUBE_IDP_E2E_GATEWAY_PORT: '18443'}
```

- [ ] **Step 3: Local verification** — run the harness once against a
  REAL pack to prove the loop closes (this is the task's live leg;
  requires docker locally, GT14 port):
  `cd $PACKS && bash hack/conformance.sh gitea $ROOT_BUILT_BINARY` — but
  `packs/gitea` only exists in $PACKS after P4. Until then verify with a
  symlink: `ln -s $ROOT/packs/gitea packs/gitea` (remove after). Expected:
  `CONFORMANT: gitea` and the cluster gone afterwards
  (`kind get clusters` does not list `conf-gitea`). Record actual output
  in FINDINGS. If docker is unavailable: BLOCKED per protocol — do not
  fake the leg.

- [ ] **Step 4: Commit ($PACKS)** —
  `git add hack/ .github/ && git commit -m "ci: per-pack conformance harness — kind + up + exit-status gate"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p3-conformance (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### P4: migrate the 7 packs, oci:// gateway default, close F12, digest-pin e2e  `[repo: both]`

**Branch:** `p5/p4-migrate-f12` (both repos) · **Depends:** P3

**Files:**
- $PACKS: `packs/<7 packs>` (moved from $ROOT), version bumps to `0.2.0`
- $ROOT modify: `internal/config/types.go` (`Default()` gateway Ref +
  pack refs to 0.2.0), `cmd/init.go` (same refs), `README.md` (drop the
  F12 caveat), `tests/e2e/e2e_test.go` (packs source),
  `internal/pack/contract_conformance_test.go` (now skips gracefully —
  Step 1 of P1 planned for this), delete `$ROOT/packs/`
- $ROOT create: `tests/e2e/PACKS.md` (how e2e finds packs)

**Interfaces:**
- Produces: `config.Default` writes
  `gateway: {pack: traefik, …, ref: "oci://ghcr.io/cube-idp/packs/traefik:0.2.0"}`
  and pack refs `…:0.2.0` — the standalone-binary contract (F12 CLOSED).
  e2e resolves packs from `CUBE_IDP_E2E_PACKS_DIR` (a $PACKS checkout,
  hermetic default) — digest-pinned online leg gated separately.
- Consumes: P2 publish toolchain, P3 harness, GT9 naming.

- [ ] **Step 1: Move packs → $PACKS.** In the $PACKS worktree:
  `cp -R $ROOT/packs/* packs/ && rm packs/.gitkeep`; bump every pack.cue
  `version` to `0.2.0` (first packs-repo release line); run
  `bash hack/conformance.sh gitea <built cube-idp>` for ONE pack as a
  smoke (live leg, GT14). Commit ($PACKS):
  `git add packs/ && git commit -m "feat: adopt the seven cube-idp packs at 0.2.0 (contract v1)"`

- [ ] **Step 2: ⚠ OWNER GATE — publish 0.2.0.** Publishing to
  ghcr.io/cube-idp requires the P2 owner gate to have run (repo + auth).
  Report NEEDS_CONTEXT with the exact commands (`git tag <name>/v0.2.0`
  ×7 + `git push --tags`, or local `cube-idp pack publish` ×7 with owner
  credentials) unless pre-authorized. The $ROOT half of this task (Steps
  3-6) does NOT depend on the publish having happened — only the final
  online e2e leg does.

- [ ] **Step 3: $ROOT defaults.** Change `config.Default`
  (`internal/config/types.go:125`): gateway gains
  `Ref: "oci://ghcr.io/cube-idp/packs/traefik:0.2.0"`; both default pack
  refs bump `:0.1.0` → `:0.2.0`. Mirror in `cmd/init.go`'s non-default
  path (`cmd/init.go:94` region) and anywhere else grep finds
  `packs/gitea:0.1.0`. Failing test first: extend the existing
  `config.Default`/init tests to assert the gateway Ref is the oci:// URL
  (grep `oci://ghcr.io/cube-idp/packs/gitea` in tests shows where the
  expectations live — `internal/config/load_test.go:197`). Run
  `go test ./internal/config/ ./cmd/ -run 'Default|Init' -v` — Expected:
  PASS after the change.

- [ ] **Step 4: Delete `$ROOT/packs/`, rewire e2e.**
  `git rm -r packs/`. e2e (`tests/e2e/e2e_test.go`) currently builds
  `init --local`-style configs pointing into the checkout's `packs/` —
  switch the source dir to
  `os.Getenv("CUBE_IDP_E2E_PACKS_DIR")`, defaulting to
  `../cube-idp-packs/packs` relative to the repo root, and `t.Skip` with
  an actionable message when absent. Document in `tests/e2e/PACKS.md`
  (clone command + env var + the digest-pinned online leg below). Any
  other repo-relative `packs/` reference (`grep -rn '"packs/' cmd/
  internal/ tests/`) is rewired the same way or deleted with
  justification in FINDINGS — EXCEPT `GatewaySpec.PackRef()`'s fallback
  string, which stays (it is the documented last-resort for checkout
  users and now simply fails cleanly outside one).

- [ ] **Step 5: Digest-pinned online leg.** Append to e2e a
  `TestPublishedPacksByDigest` gated on `CUBE_IDP_E2E_ONLINE=1`: reads
  `tests/e2e/packs.lock` (JSON: name → `oci://…@sha256:…` — committed;
  seeded by the owner after Step 2's publish; the test SKIPS with a clear
  message while the file is absent), runs `up` with gateway+gitea by
  digest ref, asserts health, `down --yes`. This is decision 2's
  digest-pin: e2e consumes the packs repo pinned by digest, never by
  mutable tag.

- [ ] **Step 6: README.** Remove the v0.1.0 F12 caveat block (README
  "Known limitation (v0.1.0, F12)"); replace with two sentences: packs
  come from `ghcr.io/cube-idp/packs` by default; `init --local
  <packs-checkout>` for offline/dev (note the flag now points at a PACKS
  checkout, not the cube-idp repo — update the flag's help text in
  `cmd/init.go` accordingly).

- [ ] **Step 7: Gate + fences + commits.** Full gate in $ROOT (unit
  suites must be green with packs/ GONE — that is the point). Fences
  green. Commit $ROOT:
  `git add -A && git commit -m "feat!: packs live in cube-idp/packs — oci gateway default closes F12; e2e digest-pinned"`
  Close ledger; HANDOFF states whether 0.2.0 is actually published and
  whether packs.lock is seeded.

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p4-migrate-f12 (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### P5: GitHub attestations in publish CI + verification docs  `[repo: $PACKS + docs in $ROOT]`

**Branch:** `p5/p5-pack-attest` · **Depends:** P2 (parallel with P4 — different files)

Per GT10 ("make this simple"): provenance comes from GitHub's keyless
artifact attestations — no keys, no secrets, no Go code, no CUBE codes.
This task is CI + documentation only. In-binary verification is parked
(spec §6); the binary's pull integrity is digest pinning over TLS.

**Files:**
- Modify in $PACKS: `.github/workflows/publish.yml` (permissions + attest
  steps replacing P2's placeholder), `CONTRACT.md` (verification section)
- Modify in $ROOT: `docs/pack-contract-v1.md` (same verification section —
  normative copy, GT12), `README.md` (one verification snippet)

**Interfaces:**
- Produces: every tag-published pack digest and each rebuilt index digest
  carry a GitHub provenance attestation (subject
  `ghcr.io/cube-idp/packs/<name>@sha256:…`), verifiable by anyone with:

```text
gh attestation verify oci://ghcr.io/cube-idp/packs/<name>:<version> --owner cube-idp
```

- Consumes: P2's `digests.env` (one `<name>=<sha256:…>` line per
  tag-triggered run) and `pack index push`'s printed digest; the
  `actions/attest-build-provenance` action (v3 major at plan time —
  VERIFY-API against the marketplace when executing; pin the major, never
  a branch).

- [ ] **Step 1: Wire attestation into `publish.yml`.** Extend the
  workflow's permissions block:

```yaml
permissions:
  contents: read
  packages: write
  id-token: write
  attestations: write
```

  Replace P2's `attest (P5 wires GitHub attestation here)` placeholder
  step with:

```yaml
      - name: export pack subject
        run: |
          IFS='=' read -r NAME DIGEST < digests.env
          echo "PACK_NAME=$NAME" >> "$GITHUB_ENV"
          echo "PACK_DIGEST=$DIGEST" >> "$GITHUB_ENV"
      - uses: actions/attest-build-provenance@v3
        with:
          subject-name: ghcr.io/cube-idp/packs/${{ env.PACK_NAME }}
          subject-digest: ${{ env.PACK_DIGEST }}
          push-to-registry: true
```

  and extend the `rebuild index` step to capture and attest the index
  digest (append to the same step's `run:` block, then add the second
  attest step):

```yaml
          cube-idp pack index push index.json --ref oci://ghcr.io/cube-idp/packs/index:latest | tee push.out
          echo "INDEX_DIGEST=$(grep -o 'sha256:[a-f0-9]*' push.out | head -1)" >> "$GITHUB_ENV"
```

```yaml
      - uses: actions/attest-build-provenance@v3
        with:
          subject-name: ghcr.io/cube-idp/packs/index
          subject-digest: ${{ env.INDEX_DIGEST }}
          push-to-registry: true
```

- [ ] **Step 2: Verify the workflow parses.** Run:
  `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/publish.yml'))" && echo YAML-OK`
  Expected: `YAML-OK`. (The attestation itself can only be proven on the
  first owner-tagged publish — the P4 Step 2 owner gate; state this in
  FINDINGS and HANDOFF. Do not fake a run.)

- [ ] **Step 3: Verification docs.** Add a "Verifying pack provenance"
  section to `$PACKS/CONTRACT.md` AND `$ROOT/docs/pack-contract-v1.md`
  (identical text, GT12), plus a two-line snippet in `$ROOT/README.md`
  next to the install instructions:

```markdown
## Verifying pack provenance

Every artifact under `ghcr.io/cube-idp/packs/` is published by the
`cube-idp/packs` GitHub workflow and carries a GitHub-native provenance
attestation. To verify one (requires `gh` ≥ 2.49, logged in):

    gh attestation verify oci://ghcr.io/cube-idp/packs/gitea:0.2.0 --owner cube-idp

Expected: `✓ Verification succeeded!` naming the cube-idp/packs workflow
as the builder. cube-idp itself pins digests (catalog index, e2e
packs.lock) and does not re-verify attestations at pull time.
```

- [ ] **Step 4: Gate + commits.** $PACKS:
  `git add .github/ CONTRACT.md && git commit -m "ci: keyless GitHub attestations for published packs + index"`
  $ROOT (docs only — full gate still runs and must stay green):
  `go build ./... && go vet ./... && go test ./...` then
  `git add docs/pack-contract-v1.md README.md && git commit -m "docs: pack provenance verification via gh attestation verify"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p5-pack-attest (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### P6: remote catalog — index-backed `pack list --available` / wizard / install  `[repo: $ROOT]`

**Branch:** `p5/p6-remote-catalog` · **Depends:** P2 (parallel with P4/P5 — touches only catalog surfaces)

**Files:**
- Create: `internal/pack/catalog.go`, `internal/pack/catalog_test.go`
- Modify: `cmd/pack.go` (packCatalog → index-backed with fallback,
  `pack list --available`, `pack search <term>`), `cmd/init.go` (wizard
  consumes the same), `internal/pack/cachedir.go` (reuse for index cache)

**Interfaces:**
- Produces:

```go
// Catalog is the parsed index artifact (P2 schema, schemaVersion 1).
type Catalog struct {
	SchemaVersion int            `json:"schemaVersion"`
	Packs         []CatalogEntry `json:"packs"`
}
type CatalogEntry struct {
	Name, Version, Description, Ref, Digest string
}
// FetchCatalog pulls oci://ghcr.io/cube-idp/packs/index:latest (override
// via CUBE_IDP_PACK_INDEX for tests/mirrors), caching 24h in the pack
// cache dir. Network failure → (nil, err); callers fall back to the
// built-in two-entry catalog and Note it.
func FetchCatalog(ctx context.Context) (*Catalog, error)
```

- Consumes: P2 index schema; `pullOCI` mechanics for a single-file
  artifact (`internal/pack/source.go:113`); the existing `packCatalog`
  shape (`cmd/pack.go:71`) as the FALLBACK, never deleted.

- [ ] **Step 1: Failing catalog tests** — fetch+parse against the ocitest
  fake (valid index → entries; corrupt JSON → error; cache hit within TTL
  skips the network — assert by killing the fake and re-fetching).
  Run: `go test ./internal/pack/ -run TestCatalog -v` — Expected: FAIL.

- [ ] **Step 2: Implement + pass.** `FetchCatalog` per the interface
  (pull index artifact to cache dir, mtime-based 24h TTL, env override).

- [ ] **Step 3: CLI wiring.** `cmd/pack.go`: `packCatalogOptions`/
  `packCatalogNames` gain a context-taking loader that tries
  `FetchCatalog` first, falls back to the built-in list with a single
  `ui` Note (`catalog: using built-in list (index unreachable: <err>)`).
  `pack list --available` prints name/version/description rows;
  `pack search <term>` filters name+description case-insensitively.
  `pack install`'s MultiSelect and `init`'s wizard consume the merged
  loader — no behavioral change when offline (fallback == today's list).
  Prompt doctrine untouched (menus already gated). Tests: list/search
  golden-ish assertions against the fake index; fallback path asserted
  with the env var pointing at a dead port.
  Run: `go test ./cmd/ -run 'TestPackList|TestPackSearch|TestPackInstall' -v`
  — Expected: PASS.

- [ ] **Step 4: Gate + fences + commit** — full gate + fences (wizard
  touched → prompt fence matters). Commit:
  `git add internal/pack/ cmd/ && git commit -m "feat(pack): remote catalog — index-backed list/search/install with built-in fallback"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p6-remote-catalog (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### P7: per-pack Gitea delivery (`delivery: repo`)  `[repo: $ROOT]`

**Branch:** `p5/p7-gitea-delivery` · **Depends:** P4

**Files:**
- Modify: `internal/config/types.go` (PackRef.Delivery) + `schema.cue`,
  `internal/up/up.go` (pack-loop branch + D11 record `delivery` field),
  `internal/pack/manifests/pack-crd.yaml` (DELIVERY printer column),
  `cmd/pack.go` (`pack install --via repo`), `internal/diag/codes.go` +
  `registry.go` (reuse 73xx repo codes where they fit; new code only if
  none fits — FINDINGS justifies)
- Test: `internal/config/load_test.go`, `internal/up/up_test.go` (unit
  seam), e2e leg

**Interfaces:**
- Produces: `PackRef.Delivery string` (yaml `delivery,omitempty`, CUE
  `delivery?: "oci" | "repo"`, empty == "oci" — byte-compatible).
  `pack install <name> --via repo` sets it. In the up pack loop,
  `delivery: repo` packs are: rendered exactly as today → written to a
  temp dir → `gitea.EnsureRepo(ctx, "cube-pack-<name>")` →
  `syncer.SyncOnce(ctx, deps, dir)` pushes the rendered manifests →
  `eng.DeliverGit(ctx, name, engine.GitSource{…in-cluster gitea URL,
  branch main…})` instead of `oci.PushRendered` + OCI deliver.
  Every pack's D11 record carries `delivery: oci|repo` (GT19; empty
  `PackRef.Delivery` records as `oci`), surfaced by a Pack CRD
  `additionalPrinterColumns` entry `DELIVERY`.
- Consumes: `gitea.EnsureRepo` (`internal/gitea/client.go:62`),
  `syncer.SyncOnce(ctx, deps Deps, dir string)`
  (`internal/syncer/syncer.go:88` — VERIFY-API its `Deps` fields; repo
  and sync commands construct it, copy their construction),
  `DeliverGit` (`internal/engine/{flux,argocd}/delivergit.go`),
  `repoCloneURL`/gitea URL derivation (`cmd/repo.go:179`).

- [ ] **Step 1: Failing config tests** — (a) `delivery: repo`
  round-trips; (b) `delivery: bogus` rejected by CUE; (c) GT-gitea
  guarantee: a cube with a `delivery: repo` pack but NO gitea pack in
  `spec.packs` fails load with a typed CUBE error naming the fix
  (`add the gitea pack or use delivery: oci`); (d) the gitea pack itself
  with `delivery: repo` fails load (self-reference). Gitea presence is
  matched by the same substring convention `filterSelectedPacks`
  (cmd/init.go) uses — reuse it, FINDINGS records the exact mechanism.
  Run + Expected: FAIL → implement field + CUE + validation → PASS.

- [ ] **Step 2: Up-loop branch.** Locate the pack delivery section in
  `internal/up/up.go` (the loop that calls `oci.PushRendered` — grep it;
  ~line 250-330). Extract the current per-pack delivery tail into
  `deliverPackOCI(...)` (pure move), add `deliverPackRepo(...)`
  implementing the Produces flow, and branch on `ref.Delivery == "repo"`.
  `deliverPackRepo` writes `pack.Rendered` objects as
  `manifests/NN-<kind>-<name>.yaml` files into a temp dir (stable
  ordering = stable git diffs; reuse the object-marshal helper the
  syncer/repo path already uses — VERIFY-API, FINDINGS records it), then
  EnsureRepo + SyncOnce + DeliverGit. Inventory: DeliverGit's returned
  objects get `RecordInventory` exactly like the OCI path's. Unit test
  via the up test seam with fakes for gitea/syncer (interfaces are
  narrow; if the existing test file lacks fakes for them, add minimal
  ones — the assertion is the branch: repo-delivery pack never touches
  the OCI pusher, OCI pack never touches gitea). Two more behaviors in
  this step (the gitea guarantee, owner 2026-07-18): **ordering** — when
  any `delivery: repo` pack exists, the pack loop delivers gitea
  immediately after the gateway pack (extend the existing gateway-first
  ordering seam; unit-test the sort); **readiness gate** —
  `deliverPackRepo` begins with a bounded poll of the gitea API (the
  same client `EnsureRepo` uses, `applyTimeout` cap) before touching it:
  ordering makes this wait short, the gate makes the flow correct
  (engine reconciliation is asynchronous — delivered ≠ ready).
- [ ] **Step 3: `pack install --via repo`** — flag sets
  `Delivery: "repo"` on the written PackRef; `--via oci` (default)
  writes nothing. Test asserts the yaml.
- [ ] **Step 4: DELIVERY surface (GT19).** Add to the Pack CRD
  (`internal/pack/manifests/pack-crd.yaml`) an `additionalPrinterColumns`
  entry `DELIVERY` (JSONPath onto the record field), and set
  `delivery: oci|repo` in the D11 record writer from `ref.Delivery`
  (empty maps to `oci` — every pack shows a value, repo-delivered packs
  stand out). Unit-test the record object's field for both modes; the
  visual `kubectl get packs` check rides the e2e leg below. CRD re-apply
  is idempotent (same note as U4 Step 5); U4 appends to the same two
  files from lane U — the append-only doctrine covers that merge.
- [ ] **Step 5: e2e leg (gated, GT14)** — extend the existing e2e with
  one repo-delivered pack: after `up`, assert the gitea repo
  `cube-pack-<name>` exists (via the gateway API the way repo tests do),
  the engine source object is a Git one (flux GitRepository /
  argocd Application spec.source.repoURL), and the pack's Pack record
  reports `delivery: repo`. Gated like the other e2e legs.
- [ ] **Step 6: Gate + fences + commit** —
  `git add internal/ cmd/ tests/ && git commit -m "feat(pack): per-pack delivery: repo — rendered packs as engine-watched Gitea repos"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p7-gitea-delivery (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### P8: engine self-management from zot (`engine.selfManage`)  `[repo: $ROOT]`

**Branch:** `p5/p8-engine-selfmanage` · **Depends:** P7 (lane order) + **U3**
(cross-lane: EngineTuning/ApplyTuning and the factory carrying the full
EngineSpec)

Implements GT16. The engine's own rendered manifests become a zot
artifact the engine watches — no gitea anywhere in this path. The four
config scenarios (tuning × selfManage) are this task's normative test
matrix:

| tuning | selfManage | rendered | applied by | drift corrected between `up`s |
|---|---|---|---|---|
| — | — | up-time (no-op patch) | cube-idp SSA | no |
| set | — | up-time | cube-idp SSA | no |
| — | true | up-time | SSA once, then the engine | yes |
| set | true | up-time | SSA first boot, then the engine | yes |

**Files:**
- Modify: `internal/config/types.go` (`SelfManage bool
  `yaml:"selfManage,omitempty" json:"selfManage,omitempty"`` on
  EngineSpec) + `internal/config/schema.cue` (`selfManage?: bool`),
  `internal/up/up.go` (self-source step after `waitHealthy` +
  unhealthy-at-start fallback + single-owner rule),
  `internal/engine/flux/deliver.go` + `internal/engine/argocd/deliver.go`
  (or sibling files: `DeliverSelf`), `internal/diag/codes.go` +
  `registry.go` (`CodeEngineSelfManage = "CUBE-3010"`)
- Test: `internal/engine/{flux,argocd}` unit tests for DeliverSelf
  object shapes, `internal/up/up_test.go`, e2e leg

**Interfaces:**
- Produces:

```go
// DeliverSelf returns the engine-native self-source objects watching the
// cube-engine artifact in the ENGINE's own namespace with pruning
// disabled (GT16): flux → OCIRepository + Kustomization (prune: false)
// in flux-system; argocd → Application over ns argocd (automated sync,
// prune false). VERIFY-API: mirror each engine's existing Deliver
// implementation for the artifact-ref/auth shape it expects from zot.
DeliverSelf(ctx context.Context, ref engine.ArtifactRef) ([]*unstructured.Unstructured, error)
```

  `up` flow (normative pseudocode — adapt names to the real file):

```go
	rendered := eng.InstallManifests()            // embedded + ApplyTuning (U3) — ALWAYS rendered first
	if firstInstall || engineUnhealthyAtStart {   // GT16 rules 1 + 3
		SSA(rendered)
	}
	// ... registry/gateway/packs exactly as today ...
	if cube.Spec.Engine.SelfManage {              // GT16 rule 2: engine owns itself from here
		ref := oci.PushRendered(ctx, asRendered("cube-engine", rendered), registryAddr)
		objs := eng.DeliverSelf(ctx, ref)
		hub.Apply(ctx, objs, true, applyTimeout); hub.RecordInventory(ctx, objs)
		waitHealthy(...)                          // instant when artifact == live state (no-flap)
	}
```

- Consumes: `oci.PushRendered` (`internal/oci/push.go:77`), the
  engine-health preflight (`eng.Health` — same call `waitHealthy` polls),
  `apply.Applier` + inventory, U3's `ApplyTuning` inside
  `InstallManifests`.

- [ ] **Step 1: Failing config + shape tests.** (a) config: `selfManage:
  true` round-trips, absent → false. (b) DeliverSelf shapes per engine
  (unit, no cluster): flux objects = OCIRepository named `cube-engine` +
  Kustomization with `spec.prune == false`, both ns `flux-system`;
  argocd = one Application, ns `argocd`, destination its own namespace,
  automated sync with `prune: false`. Run:
  `go test ./internal/config/ ./internal/engine/... -run 'SelfManage|DeliverSelf' -v`
  Expected: FAIL.
- [ ] **Step 2: Implement** config field + CUE + `DeliverSelf` in both
  engines (VERIFY-API: copy each engine's Deliver ref/auth handling —
  the zot pull path with the media-type constraints is already solved
  there; do NOT invent a second artifact shape). CUBE-3010 wraps every
  failure arm (push, apply, wait) with a fix line naming
  `cube-idp up` re-run as the retry. Re-run tests — Expected: PASS.
- [ ] **Step 3: `up` wiring** per the pseudocode: the unhealthy-preflight
  helper (one `eng.Health` call with a short timeout, tolerant of
  not-installed-yet), the single-owner skip (selfManage && healthy →
  no SSA), the post-packs self-source block. Unit-test with the up test
  seam/fakes: selfManage=false → pusher never called for cube-engine;
  selfManage=true first-run → SSA happened AND artifact pushed AND
  self-source applied; selfManage=true healthy-rerun → NO SSA, push +
  poke only. Run: `go test ./internal/up/ -run SelfManage -v` —
  Expected: PASS.
- [ ] **Step 4: e2e leg (gated, GT14).** cube.yaml with `selfManage:
  true` + a tuning replica bump: `up`, then flip the replica count and
  re-run `up`; assert (a) a NEW `cube-engine` digest exists in zot,
  (b) the component Deployment's replicas changed, (c) the
  `managedFields` owner of `spec.replicas` is the ENGINE's field manager
  (kustomize-controller / argocd), NOT cube-idp's applier — the proof
  the engine reconfigured itself. `down --yes` clean.
- [ ] **Step 5: Gate + fences + commit** — full gate + fences. Commit:
  `git add internal/ && git commit -m "feat(engine): opt-in self-management from zot — render, push, engine reconciles itself (GT16, CUBE-3010)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p8-engine-selfmanage (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### P9: plugins repo scaffold — per-platform artifacts, index, attestations  `[repo: $PLUGINS — creates it]`

**Branch:** `p5/p9-plugins-repo` · **Depends:** P8 (lane order; mirrors
P2's shape, no code dependency)

Implements GT17. Unlike the packs CI, this repo is self-contained Go —
no cube-idp checkout, no `CUBE_IDP_READ_TOKEN` needed.

**Files (all created in $PLUGINS):**
- `README.md`, `CONTRACT-PLUGINS.md` (folder layout, `plugin.yaml`
  schema, tag/artifact/index conventions, verification section — GT17
  verbatim), `plugins/hello/` (seed plugin: ~20-line Go `main` that
  prints its name+version — proves the whole pipeline),
  `hack/build-matrix.sh`, `hack/genindex.sh`,
  `.github/workflows/publish.yml`, `.github/workflows/ci.yml`

- [ ] **Step 1: ⚠ OWNER GATE — create the public repo.** STOP and report
  NEEDS_CONTEXT with exactly:
  `gh repo create cube-idp/plugins --public --description "cube-idp plugins — official exec plugins, published per-platform as attested OCI artifacts"`
  No secrets at all (attestations are keyless; the repo builds itself).
  If not pre-authorized: continue locally (git init, no remote).
- [ ] **Step 2: Scaffold + seed plugin.** `git init cube-idp-plugins` as
  $ROOT's sibling. `plugins/hello/main.go` (prints
  `cube-idp-hello <version>`; version injected via `-ldflags -X`),
  `plugins/hello/plugin.yaml`:

```yaml
name: hello
version: 0.1.0
description: seed plugin proving the cube-idp plugins pipeline
```

  `hack/build-matrix.sh <name> <version>`: for GOOS in linux darwin ×
  GOARCH in amd64 arm64 → `go build -ldflags "-X main.version=<ver>" -o
  dist/cube-idp-<name>-<os>-<arch> ./plugins/<name>`; smoke:
  `dist/cube-idp-<name>-$(go env GOOS)-$(go env GOARCH)` runs and prints
  the version. `hack/genindex.sh`: assemble the GT17 index.json from
  dist/ + digests (jq).
- [ ] **Step 3: `.github/workflows/publish.yml`** — on tags `*/v*`:
  parse `<name>/v<ver>` from the ref; build the matrix; for each
  platform binary `oras push
  ghcr.io/cube-idp/plugins/<name>:<ver>-<os>-<arch>` (single-layer,
  media type `application/vnd.cube-idp.plugin.v1`), capture digests;
  `actions/attest-build-provenance@v3` per digest (permissions:
  id-token + attestations + packages write); rebuild index.json via
  genindex + oras push `plugins/index:latest` + attest its digest.
  `ci.yml` — on PR: build matrix + run the native-platform smoke.
  Verify both workflows parse:
  `python3 -c "import yaml;[yaml.safe_load(open(f)) for f in ['.github/workflows/publish.yml','.github/workflows/ci.yml']]" && echo YAML-OK`
  Expected: `YAML-OK`. (Live attestation provable only on the first
  owner-tagged publish — state in FINDINGS/HANDOFF, do not fake.)
- [ ] **Step 4: Local proof + commit.** Run
  `bash hack/build-matrix.sh hello 0.1.0` — Expected: 4 binaries in
  dist/, native smoke prints `cube-idp-hello 0.1.0`; `bash
  hack/genindex.sh` emits index.json matching GT17's schema (digests
  computed with `shasum -a 256` locally as stand-ins, noted as such).
  Commit ($PLUGINS):
  `git add -A && git commit -m "chore: plugins repo scaffold — per-platform artifacts, index, attestations (GT17)"`
  Close the ledger in $ROOT; HANDOFF states whether the owner gate ran.

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p9-plugins-repo (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

### P10: `plugin install` from the official index  `[repo: $ROOT]`

**Branch:** `p5/p10-plugin-install` · **Depends:** P9

The existing trust doctrine does not move an inch: this task only adds a
RESOLUTION path (official index → platform entry → digest pull). The
sha256 consent flow, CUBE-7104 non-TTY refusal, and `plugin trust`
semantics stay byte-identical.

**Files:**
- Modify: `internal/plugin/index.go` (official-index resolver BESIDE the
  existing sha256-pinned git index — VERIFY-API the current
  `plugin install` mechanism there first and INTEGRATE; the git path
  keeps working unchanged), `internal/oci` (generic single-blob pull for
  the GT17 media type — mirror `pullOCI`'s auth/cache),
  `cmd/plugin.go` (`plugin install <name>[@version]` defaults to the
  official index; `plugin list --available`, `plugin search <term>`),
  `internal/diag/codes.go` + `registry.go` (one 71xx code for
  no-platform-match; reuse existing install-failure codes otherwise —
  FINDINGS justifies)
- Test: `internal/plugin/index_test.go`, `cmd/plugin_test.go` (ocitest
  fake serving index + artifact)

**Interfaces:**
- Produces: `plugin install hello` → fetch
  `oci://ghcr.io/cube-idp/plugins/index:latest` (override
  `CUBE_IDP_PLUGIN_INDEX`; 24h cache like P6's catalog) → select
  `platforms["<GOOS>-<GOARCH>"]` (absent → typed 71xx error naming
  available platforms) → **pull by digest, never by tag** → write
  executable to `plugin.InstallDir()` (0755) → hand off to the EXISTING
  trust-consent flow. `plugin list --available` / `plugin search` read
  the same index (built-in fallback: none — plugins have no hardcoded
  catalog; offline → clear typed error + Note pointing at the git-index
  path).
- Consumes: GT17 index schema + artifact shape, P6's cache/TTL pattern
  (`internal/pack/catalog.go` — copy the pattern, do NOT import
  pack from plugin), existing `plugin.InstallDir()`/trust store.

- [ ] **Step 1: Failing tests** — index resolve (name→platform→digest;
  missing platform → typed error), install against the ocitest fake
  writes an executable file and triggers the trust-consent seam (assert
  via the existing prompt-fence pattern: non-TTY without the trust flag
  refuses with CUBE-7104 — the fence test EXTENDS
  `TestPromptFenceNeverBlocksOnBufferStdin`'s table with the new path),
  `plugin list --available` renders index rows.
  Run: `go test ./internal/plugin/ ./cmd/ -run 'TestPlugin' -v` —
  Expected: FAIL on the new paths.
- [ ] **Step 2: Implement** per Interfaces (resolver + blob pull + cmd
  wiring). Re-run — Expected: PASS, including the extended prompt fence.
- [ ] **Step 3: Docs + gate + commit.** README plugin section: install
  from the official repo + `gh attestation verify
  oci://ghcr.io/cube-idp/plugins/hello:0.1.0-linux-amd64 --owner
  cube-idp` snippet. Full gate + fences. Commit:
  `git add internal/ cmd/ README.md && git commit -m "feat(plugin): install from the official attested index — digest pull + unchanged trust consent"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/p10-plugin-install (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

## Lane A — Wave A pack authoring  `[repo: $PACKS]`

Eleven tasks, one pack each, ALL depending only on P3 (and their `Depends`
column below). Fully parallel with each other ONCE P3 is DONE — but each A
agent works only under `$PACKS/packs/<name>/`, so conflicts are
structurally impossible; claim ANY unclaimed A task whose Depends are
DONE.

**Every A task follows the SAME steps** (the template below), varying only
by its parameter row. The template + row IS the task spec — treat every
`<param>` as literal substitution from the row.

### Parameter table

| Task | Pack `<name>` | Branch | Depends | Source `<kind>` | Upstream `<upstream>` (pin exactly) | Namespace `<ns>` | Health gate `<health>` | Expose |
|------|--------------|--------|---------|-----------------|--------------------------------------|------------------|------------------------|--------|
| A1 | `crossplane` | `p5/a1-crossplane` | P3 | helm | chart `crossplane` from `https://charts.crossplane.io/stable`, latest stable at execution (record exact version in pack.cue + FINDINGS) | `crossplane-system` | deploy `crossplane` + `crossplane-rbac-manager` Available | none |
| A2 | `kyverno` | `p5/a2-kyverno` | P3 | helm | chart `kyverno` from `https://kyverno.github.io/kyverno` | `kyverno` | deploys `kyverno-admission-controller`, `kyverno-background-controller`, `kyverno-cleanup-controller`, `kyverno-reports-controller` Available | none |
| A3 | `kyverno-policies` | `p5/a3-kyverno-policies` | P3 + A2 | manifests | Pod Security Standards *baseline* ClusterPolicies in `validationFailureAction: Audit` — author from kyverno/policies repo pinned commit | n/a (cluster-scoped) | policies report READY | none |
| A4 | `cloudnativepg` | `p5/a4-cloudnativepg` | P3 | manifests | upstream release manifest `cnpg-<ver>.yaml` (pin exact URL+version) | `cnpg-system` | deploy `cnpg-controller-manager` Available | none |
| A5 | `argo-rollouts` | `p5/a5-argo-rollouts` | P3 | manifests | upstream `install.yaml` from argo-rollouts release (pin) | `argo-rollouts` | deploy `argo-rollouts` Available | none |
| A6 | `argo-events` | `p5/a6-argo-events` | P3 | manifests | upstream `install.yaml` (pin) | `argo-events` | deploys `controller-manager`, `events-webhook` Available | none |
| A7 | `argo-workflows` | `p5/a7-argo-workflows` | P3 | manifests | upstream `install.yaml` (pin) | `argo` | deploys `workflow-controller`, `argo-server` Available | `https://workflows.${GATEWAY_HOST}` (server, `--auth-mode=server` for local IDP use) |
| A8 | `prometheus-stack` | `p5/a8-prometheus-stack` | P3 | helm | chart `kube-prometheus-stack` from `https://prometheus-community.github.io/helm-charts` (pin) | `monitoring` | deploy `prometheus-stack-grafana` + operator Available, statefulset prometheus Ready | `https://grafana.${GATEWAY_HOST}`, authSecretRef grafana admin secret + impliedFields username admin |
| A9 | `kargo` | `p5/a9-kargo` | P3 (cert-manager pack must be in the conformance cube — see template step 3 note) | helm | chart `kargo` from `oci://ghcr.io/akuity/kargo-charts/kargo` (pin) | `kargo` | deploys `kargo-api`, `kargo-controller` Available | `https://kargo.${GATEWAY_HOST}` |
| A10 | `floci` | `p5/a10-floci` | P3 | manifests (authored) | Docker-only upstream (github.com/floci-io/floci, AWS emulator) — author ns+Deployment+Service pinning image `floci/floci:1.5.33` (verify latest stable at execution; record tag+sha256). NO docker-socket mount: kind nodes run containerd, so container-backed services (Lambda/RDS/ECS…) are unavailable — core services (S3, DynamoDB, SQS, …) only; state this in the pack README | `floci` | deploy `floci` Available | `https://floci.${GATEWAY_HOST}` → Service port 4566 |
| A11 | `floci-ui` | `p5/a11-floci-ui` | P3 + A10 | manifests (authored) | Docker-only upstream (github.com/floci-io/floci-ui, web console) — author Deployment+Service pinning image `floci/floci-ui:0.2.0` (verify at execution: serving ports 4500 UI / 4501 API and env names); set env `FLOCI_ENDPOINT=http://floci.floci.svc.cluster.local:4566` | `floci` (shared with A10) | deploy `floci-ui` Available | `https://floci-ui.${GATEWAY_HOST}` → Service port 4500 |

### Template (every A task executes exactly these steps)

- [ ] **Step 1: Scaffold.** `mkdir packs/<name>`; write `pack.cue`:

```cue
name:        "<name>"
version:     "0.1.0"
description: "<one line — user-facing, shows in cube-idp pack list>"
// expose: {...}   // only if the parameter row's Expose column says so —
//                 // copy the shape from CONTRACT.md §2 / the argocd pack.
```

- [ ] **Step 2: Vendor the upstream at the pinned version.** `helm` kind:
  write `chart.yaml` following an existing helm pack in this repo
  (traefik's is the reference: repo/chart/version/values + the nodePort
  pinning pattern where relevant); values MUST pin every image tag the
  chart would otherwise float. `manifests` kind: download the pinned
  upstream YAML into `manifests/NN-*.yaml` files (numbered, namespace
  object first — copy argocd's layout), strip nothing, add nothing except
  the namespace if upstream omits it. `manifests (authored)` kind (the
  upstream is Docker-only, no YAML exists): write minimal
  namespace+Deployment+Service manifests yourself, image pinned by tag
  AND digest, resources set, no extras — the parameter row's notes are
  the source of truth. Record the exact upstream URL+version+sha256 as
  comments at the top of the vendored/authored file(s) AND in FINDINGS.
- [ ] **Step 3: Conformance.** `bash hack/conformance.sh <name>` —
  Expected: `CONFORMANT: <name>`, cluster torn down. A3 (needs kyverno),
  A9 (needs cert-manager) and A11 (needs floci) get their dependency added to the packs
  list of a COPY of the conformance template via a `EXTRA_PACKS`
  override the script already supports — if it does not, add
  `CUBE_IDP_CONFORMANCE_EXTRA_PACK_DIR` support to conformance.sh in
  YOUR branch (10 lines: second packs entry when set; FINDINGS notes it;
  later A tasks inherit it via merge order — APPEND-ONLY doctrine).
- [ ] **Step 4: Health gate = doctor contract.** The conformance run's
  `status --exit-status` green PROVES the `<health>` column: the engine
  reports the pack Ready only when those deployments are Available —
  verify by `kubectl get deploy -n <ns>` during the run and paste the
  output into FINDINGS (this is the doctor-coverage DoD for pack tasks;
  binary-side CUBE codes are not extended by A tasks).
- [ ] **Step 5: Commit ($PACKS)** —
  `git add packs/<name> && git commit -m "feat(pack): <name> 0.1.0 — <one-line description>"`
  Merge per protocol; ledger in $ROOT.
- [ ] **Step 6 (owner, later): tag `<name>/v0.1.0`** when the owner
  publishes — A tasks do NOT tag or push (⚠ OWNER GATE).

#### Outcomes (one block per task — agents fill ONLY theirs)

```
A1 STATUS: UNCLAIMED  BRANCH: p5/a1-crossplane        COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A2 STATUS: UNCLAIMED  BRANCH: p5/a2-kyverno           COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A3 STATUS: UNCLAIMED  BRANCH: p5/a3-kyverno-policies  COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A4 STATUS: UNCLAIMED  BRANCH: p5/a4-cloudnativepg     COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A5 STATUS: UNCLAIMED  BRANCH: p5/a5-argo-rollouts     COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A6 STATUS: UNCLAIMED  BRANCH: p5/a6-argo-events       COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A7 STATUS: UNCLAIMED  BRANCH: p5/a7-argo-workflows    COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A8 STATUS: UNCLAIMED  BRANCH: p5/a8-prometheus-stack  COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A9 STATUS: UNCLAIMED  BRANCH: p5/a9-kargo             COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A10 STATUS: UNCLAIMED BRANCH: p5/a10-floci            COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
A11 STATUS: UNCLAIMED BRANCH: p5/a11-floci-ui         COMMITS: -  FINDINGS: -  REVIEW: -  BLOCKERS: -  HANDOFF: -
```

---

## Lane F — final gate

### F1: CLI coherence — command-tree fence, conventions audit, docs sweep  `[repo: $ROOT]`

**Branch:** `p5/f1-cli-coherence` · **Depends:** ALL S, U, and P tasks
DONE (A tasks excluded — they live in $PACKS and add no CLI surface).
Claim this LAST; it is the phase's answer to "after our changes, the CLI
must be correct".

**Files:**
- Create: `cmd/clitree_test.go` + `cmd/testdata/clitree.golden`
- Modify: `README.md` (command reference), `docs/machine-readable-output.md`
  (final additive-fields sweep), plus whatever small drift the audit
  finds (each fix listed in FINDINGS; anything non-trivial → BLOCKED
  with a proposal, never silent scope creep)

**Interfaces:**
- Produces: `TestCommandTreeGolden` — a NEW permanent fence: walks the
  cobra tree (command path, Short, every flag name/default) into a
  stable text rendering, compared against `cmd/testdata/clitree.golden`.
  From this task on, ANY CLI-surface change must consciously regenerate
  the golden — the CLI is frozen the way the plain projection already
  is.
- Consumes: every command the phase added or touched: `spoke
  add/list/remove`, `pack publish`, `pack index build/push`, `pack list
  --available`, `pack search`, `pack install --via`, `plugin install`
  (index path), `plugin list --available`, `plugin search`, `config
  render-engine`, `status` (spokes), `doctor` (checklist), plus the
  cube.yaml fields `gateway.httpPort`, `engine.tuning`,
  `engine.selfManage`, `packs[].delivery`, `packs[].extraManifests`,
  `spec.spokes`.

- [ ] **Step 1: The fence.** Write `TestCommandTreeGolden` (walk
  `newRootCmd()`'s tree recursively; render one line per command:
  `path | Short | flag=default,flag=default…`; sorted, deterministic).
  First run with `-update` (the repo's golden-update convention — check
  how the TE goldens regenerate and reuse that flag; FINDINGS records
  it) writes the golden; second run passes clean.
  Run: `go test ./cmd/ -run TestCommandTreeGolden -v` — Expected: PASS.
- [ ] **Step 2: Conventions audit** — against the golden, verify and fix
  drift; every row of this table goes to FINDINGS with pass/fixed:
  (a) every config-reading command exposes `-f/--file` defaulting
  `cube.yaml`; (b) every destructive/mutating action has a
  non-interactive twin (`--yes`/`--confirm`) and refuses on non-TTY with
  CUBE-0010 (prompt doctrine, GT13); (c) Short strings share one style
  (verb-first, no trailing period — match the majority); (d) no command
  prints raw errors around the diag envelope; (e) new cube.yaml fields
  all appear in the README `cube.yaml` reference table with defaults and
  the cluster-shape caveat marks where applicable.
- [ ] **Step 3: Docs sweep.** README command table lists every command
  from Consumes; machine-readable-output.md documents the additive
  `status.spokes` and `doctor.checks` fields plus any other field the
  phase added; the "Terminal output & interactivity" contract section
  gains one line each for doctor's tri-state rows and spoke consent
  lines.
- [ ] **Step 4: The full gate, everything at once:**
  `go build ./... && go vet ./... && go test ./...` plus
  `go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence|TestCommandTreeGolden'`
  plus cross-compile smoke:
  `GOOS=linux GOARCH=amd64 go build -o /dev/null . && GOOS=darwin GOARCH=arm64 go build -o /dev/null .`
  Expected: ALL PASS.
- [ ] **Step 5: Commit** —
  `git add cmd/ README.md docs/ && git commit -m "test(cmd)+docs: CLI coherence gate — command-tree golden fence + conventions audit (F1)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH: p5/f1-cli-coherence (merged: -)
COMMITS: -
FINDINGS: -
REVIEW: -
BLOCKERS: -
HANDOFF: -
```

---

## Dispatch quick-reference

```text
Immediately dispatchable in parallel: S1, U1, P1     (three lanes, three agents)
Then:  S2→S3→S4   U2→U3→U4→U5   P2→{P3, P5, P6}   P3→P4→P7→P8→P9→P10
       (P8 also needs U3)   P3→A1..A11 (any order, parallel)
Last:  F1 (all S+U+P DONE) — the CLI coherence gate
Owner gates: P2 Step 4 (gh repo create packs + CUBE_IDP_READ_TOKEN),
             P4 Step 2 (publish 0.2.0), P9 Step 1 (gh repo create
             plugins), A Step 6 (tags).
```

## Plan-level completion

Phase 5 is DONE when every task above is DONE/DONE_WITH_CONCERNS, the
owner gates have run, and the two headline proofs hold:
1. A downloaded (or freshly built) cube-idp binary in an EMPTY directory:
   `cube-idp init --name t && cube-idp up && cube-idp status --exit-status`
   succeeds with no checkout present (F12 closed; packs digest-pinned,
   provenance attested in CI and verifiable via `gh attestation verify`).
2. A cube.yaml with one kind spoke: `up` registers it; the engine UI/CLI
   (argocd cluster list or flux kubeconfig secret) shows the spoke;
   `down --yes` removes everything including the spoke cluster.
3. F1 is DONE: `TestCommandTreeGolden` + the full fence matrix are green
   on main, `doctor` renders the tri-state checklist, and every command
   and cube.yaml field the phase added is in the README — the CLI
   surface is frozen and documented.

