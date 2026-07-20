# cube-idp pack `dependsOn` + CubeLock object — Implementation Plan (p6)

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking. **This file is the persistent
> ledger.** The Phase 5 dispatch prompt
> [2026-07-18-phase5-agent-prompt-v2.md](2026-07-18-phase5-agent-prompt-v2.md)
> applies verbatim with two substitutions: the plan/ledger file is THIS file,
> and branches are `p6/<task-id>-<slug>`.

**Goal:** Implement the ratified spec
[2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md](../specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md):
declared + implicit pack dependencies with cycle detection and per-engine
translation (flux `spec.dependsOn`, argocd wave-gated delivery), and
CubeLock as a proper KRM object with an in-cluster inert CRD record.

**Architecture:** One new pure module (`internal/pack/depgraph.go`) feeds a
two-pass `up.Run` (fetch+render → graph → deliver) and `diff.desiredState`;
engines translate resolved deps behind the D2 seam (flux natively, argocd
via a bounded per-pack health gate in `up`); `internal/lock` gains the KRM
shape with a transparent legacy lift plus a second inert CRD following the
`internal/pack` D11 pattern exactly. No new subsystem, no new dependency.

**Tech Stack:** Go (stdlib + existing pinned deps ONLY — `go.mod` gains no
new module in any task), CUE for the two schema surfaces, envtest for the
CRD legs, the Phase 3 conformance harness in `$PACKS` for DEP5.

## Global Constraints

- **Ratified decisions bind:** spec DD1–DD10 and §7 (render-derived gateway
  edge; argocd wave-gate + document; NO in-cluster Cube record; post-F1).
- **`go.mod` gains no new module** in any task, either repo.
- **Frozen fences stay green in every task:**
  `go test ./internal/ui/... ./cmd/... -run 'TE|TestModeMatrixFence|TestPromptFence|TestCommandTreeGolden'`.
  This phase adds NO command, flag, or Short — `cmd/testdata/clitree.golden`
  must be byte-identical at every merge (regenerating it is a BLOCKED
  condition, not a fix).
- **CUBE codes reserved here:** CUBE-4018, CUBE-4019, CUBE-4020 (pack),
  CUBE-3011 (engine). RECONCILE at DEP1/DEP3 claim: `grep -n "4018\|4019\|4020\|3011" internal/diag/codes.go`
  must come back empty; if not, take the next free numbers and record in
  FINDINGS (the constant NAMES below are what other tasks reference).
- **Append-only shared surfaces** (same doctrine as Phase 5):
  `internal/diag/codes.go` plus `registry.go`, `internal/config/types.go`
  plus `schema.cue`,
  `internal/pack/manifests/pack-crd.yaml` printer columns (append after
  DELIVERY), the `PackObject` parameter list.
- **Delivery-name convention** `cube-idp-<pack name>` is load-bearing in
  both engines and the wave gate — never derive it any other way than the
  engines' `deliveryName()`.
- **Commit trailer** on every commit:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Local e2e recipe (testowy cluster squats 8443): prefix every e2e run with
  `CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443`.

## Lanes and ordering

Two lanes, independently claimable; serial within a lane:

- **DEP lane:** DEP1 → DEP2 → DEP3 → DEP4 → DEP5 (`DEP5 [repo: $PACKS]`)
- **LOCK lane:** LOCK1 → LOCK2 — **LOCK2 additionally Depends: DEP2**
  (shared `internal/up/up.go` seams; merge order DEP2 first).

Deliberate shared files (append-only doctrine applies): `internal/up/up.go`
(DEP2/DEP3/LOCK2), `internal/diff/diff.go` (DEP2/DEP3/LOCK2),
`internal/diag/codes.go`+`registry.go` (DEP1/DEP3). On conflict: take both
sides, run the task gate, note in FINDINGS.

## Task Index

| ID | Slug | Repo | Depends |
| --- | --- | --- | --- |
| DEP1 | depgraph | $ROOT | — |
| DEP2 | up-two-pass | $ROOT | DEP1 |
| DEP3 | engine-translation | $ROOT | DEP2 |
| DEP4 | record-docs-e2e | $ROOT | DEP3 |
| DEP5 | packs-declare-deps | $PACKS | DEP4 |
| LOCK1 | lock-krm-shape | $ROOT | — |
| LOCK2 | cubelock-crd | $ROOT | LOCK1, DEP2 |

```text
Immediately dispatchable in parallel: DEP1, LOCK1   (two lanes, two agents)
Then:  DEP1→DEP2→DEP3→DEP4→DEP5        LOCK1→(after DEP2)→LOCK2
Owner gates: DEP5 Step 5 (per-pack tags in $PACKS, one tag per push).
```

---

### DEP1: dependency graph — loaders, `ResolveOrder`, cycle detection  `[repo: $ROOT]`

**Branch:** `p6/dep1-depgraph` · **Depends:** —

**Files:**
- Create: `internal/pack/depgraph.go`, `internal/pack/depgraph_test.go`
- Modify: `internal/pack/pack.go` (Pack.DependsOn + loadMeta),
  `internal/config/types.go` (PackRef.DependsOn),
  `internal/config/schema.cue` (packs entry),
  `internal/diag/codes.go` + `internal/diag/registry.go` (3 codes)

**Interfaces:**
- Produces: `pack.ResolveOrder(packs []*Pack, refs []config.PackRef, rendered []*Rendered) (order []int, deps map[string][]string, err error)`
  — order = delivery order as indices into the index-aligned slices
  (index 0 is always the gateway pack); deps = resolved per-pack dep
  names (explicit ∪ implicit, sorted, absent key when none). Also:
  `Pack.DependsOn []string`, `config.PackRef.DependsOn []string`,
  `diag.CodePackDepUnknown/CodePackDepCycle/CodePackDepGateway`.
- Consumes: existing `Pack`, `Rendered`, `config.PackRef`, `diag`.

- [ ] **Step 1: Codes first** (registry fence makes tests fail without
  them). Append inside the existing GT15 pack const block in
  `internal/diag/codes.go`, directly after CUBE-4017:

```go
	// Pack dependencies (p6 DEP1, spec 2026-07-19 §3).
	CodePackDepUnknown Code = "CUBE-4018" // dependsOn names a pack not in this cube
	CodePackDepCycle   Code = "CUBE-4019" // pack dependency cycle (the message shows the path)
	CodePackDepGateway Code = "CUBE-4020" // gateway pack cannot carry a dependsOn of its own
```

  And in `internal/diag/registry.go`, in the 4xxx section after
  `CodePackExtraManifests`:

```go
	CodePackDepUnknown: {Summary: "dependsOn names a pack not in this cube"},
	CodePackDepCycle:   {Summary: "pack dependency cycle (the message shows the path)"},
	CodePackDepGateway: {Summary: "gateway pack cannot carry a dependsOn of its own"},
```

  Run: `go test ./internal/diag/ -run TestRegistryCoversEveryDeclaredCode -v`
  Expected: PASS.
- [ ] **Step 2: Declaration surfaces.** `internal/config/types.go`, append
  to `PackRef` after `Delivery`:

```go
	// DependsOn lists pack NAMES (pack.cue name — never refs; DD1) that
	// must be delivered, and per engine semantics healthy, before this
	// pack (spec 2026-07-19 §3.1). Unioned with the pack's own pack.cue
	// dependsOn at graph time (pack.ResolveOrder). omitempty: absent must
	// round-trip as an absent key, not an explicit YAML null — same
	// discipline as Values above.
	DependsOn []string `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
```

  `internal/config/schema.cue`: the packs line becomes (one added field):

```cue
		packs?: [...{ref: string & !="", values?: {...}, extraManifests?: string & !="", delivery?: "oci" | "repo", dependsOn?: [...string & !=""]}]
```

  `internal/pack/pack.go`: add `DependsOn []string` to `Pack` (comment:
  "optional pack.cue dependsOn: pack names this pack needs first — nil
  when undeclared, packs predating the field load exactly as before;
  spec 2026-07-19 §3.1") and in `loadMeta`, after the `images` block:

```go
	if dv := v.LookupPath(cue.ParsePath("dependsOn")); dv.Exists() {
		if err := dv.Decode(&p.DependsOn); err != nil {
			return nil, diag.Wrap(err, diag.CodePackCueInvalid,
				fmt.Sprintf("pack.cue dependsOn: in %s is invalid", dir),
				`dependsOn: must be a list of pack names, e.g. dependsOn: ["floci"]`)
		}
	}
```

  Tests (add to the existing loader-test file style,
  `internal/pack/pack_test.go`): a pack.cue with
  `dependsOn: ["floci"]` loads it; a pack.cue WITHOUT the field yields
  nil (the packs-predating-the-field fence); `dependsOn: "floci"`
  (non-list) is CUBE-4003. Config side (`internal/config/load_test.go`
  style): a cube.yaml packs entry with `dependsOn: ["gitea"]`
  round-trips; `dependsOn: [""]` fails CUBE-0002.
  Run: `go test ./internal/pack/ ./internal/config/ -run 'Depends' -v`
  Expected: FAIL first (write tests before code within this step), then
  PASS.
- [ ] **Step 3: The graph.** Create `internal/pack/depgraph.go`:

```go
// depgraph resolves the pack dependency graph (spec 2026-07-19 §3.2):
// explicit deps (pack.cue dependsOn ∪ cube.yaml packs[].dependsOn) plus
// two implicit, never-declared edges — (a) any pack whose render contains
// a gateway.networking.k8s.io object depends on the gateway pack (DD3a,
// ratified: render-derived, NOT blanket), killing the A10 CRD-ordering
// race class; (b) any delivery:"repo" pack depends on gitea (decision 13
// formalized). Kahn-sorted with a deterministic declared-order tie-break
// (DD6). Shared by up.Run and diff.desiredState so both walk one order.
package pack

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// gatewayAPIGroup is the object group that triggers implicit edge (a).
const gatewayAPIGroup = "gateway.networking.k8s.io"

// ResolveOrder validates the dependency graph over the fetched packs and
// returns the delivery order (indices into the index-aligned inputs;
// index 0 is the gateway pack, always first) plus each pack's resolved
// dependency names (sorted; key absent when a pack has none). Errors are
// typed: CUBE-4018 unknown name, CUBE-4019 cycle, CUBE-4020 a dependsOn
// on the gateway pack itself.
func ResolveOrder(packs []*Pack, refs []config.PackRef, rendered []*Rendered) ([]int, map[string][]string, error) {
	n := len(packs)
	idxByName := make(map[string]int, n)
	for i, p := range packs {
		if prev, dup := idxByName[p.Name]; dup {
			return nil, nil, diag.New(diag.CodePackDepUnknown,
				fmt.Sprintf("packs %q and %q both name themselves %q — delivery names would collide", refs[prev].Ref, refs[i].Ref, p.Name),
				"every pack in a cube needs a unique pack.cue name")
		}
		idxByName[p.Name] = i
	}
	if len(packs[0].DependsOn) > 0 || len(refs[0].DependsOn) > 0 {
		return nil, nil, diag.New(diag.CodePackDepGateway,
			fmt.Sprintf("gateway pack %q declares dependsOn %v", packs[0].Name,
				append(append([]string{}, packs[0].DependsOn...), refs[0].DependsOn...)),
			"the gateway pack is delivered first unconditionally and cannot depend on other packs")
	}

	// edges[i][j] = true: pack i depends on pack j.
	edges := make([]map[int]bool, n)
	for i := range edges {
		edges[i] = map[int]bool{}
	}
	for i := 1; i < n; i++ {
		for _, name := range append(append([]string{}, packs[i].DependsOn...), refs[i].DependsOn...) {
			j, ok := idxByName[name]
			if !ok {
				return nil, nil, diag.New(diag.CodePackDepUnknown,
					fmt.Sprintf("pack %q dependsOn %q, which is not in this cube (installed: %s)",
						packs[i].Name, name, strings.Join(installedNames(packs), ", ")),
					"add the missing pack to spec.packs or fix the name")
			}
			if j == i {
				return nil, nil, cycleError([]string{packs[i].Name, packs[i].Name})
			}
			edges[i][j] = true
		}
		for _, o := range rendered[i].Objects {
			if o.GroupVersionKind().Group == gatewayAPIGroup {
				edges[i][0] = true // implicit edge (a): needs the gateway pack's CRDs
				break
			}
		}
		if refs[i].Delivery == "repo" {
			j, ok := idxByName["gitea"]
			if !ok {
				// unreachable in up (config load guards it, decision 13) but
				// ResolveOrder is a public seam — fail typed, not with a panic.
				return nil, nil, diag.New(diag.CodePackDepUnknown,
					fmt.Sprintf("pack %q is delivery: repo but no pack named \"gitea\" is in this cube", packs[i].Name),
					"repo delivery needs the gitea pack in spec.packs")
			}
			if j != i {
				edges[i][j] = true // implicit edge (b): decision 13
			}
		}
	}

	deps := make(map[string][]string, n)
	for i, es := range edges {
		if len(es) == 0 {
			continue
		}
		names := make([]string, 0, len(es))
		for j := range es {
			names = append(names, packs[j].Name)
		}
		sort.Strings(names)
		deps[packs[i].Name] = names
	}

	// Kahn, O(n²) — a cube holds tens of packs, not thousands. Each round
	// emits the LOWEST-index ready pack: dependency-free cubes therefore
	// come out in declared order byte-for-byte (DD6 regression fence).
	order := make([]int, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		picked := -1
		for i := 0; i < n; i++ {
			if done[i] {
				continue
			}
			ready := true
			for j := range edges[i] {
				if !done[j] {
					ready = false
					break
				}
			}
			if ready {
				picked = i
				break
			}
		}
		if picked == -1 {
			return nil, nil, cycleError(cyclePath(packs, edges, done))
		}
		done[picked] = true
		order = append(order, picked)
	}
	return order, deps, nil
}

func installedNames(packs []*Pack) []string {
	names := make([]string, len(packs))
	for i, p := range packs {
		names[i] = p.Name
	}
	sort.Strings(names)
	return names
}

func cycleError(path []string) error {
	return diag.New(diag.CodePackDepCycle,
		fmt.Sprintf("pack dependency cycle: %s", strings.Join(path, " → ")),
		"break the cycle — remove one dependsOn edge (kubectl get packs shows resolved deps once up succeeds)")
}

// cyclePath walks the residual (not-done) graph from its lowest surviving
// node until a node repeats, rendering "a → b → a" for CUBE-4019.
func cyclePath(packs []*Pack, edges []map[int]bool, done []bool) []string {
	start := -1
	for i := range packs {
		if !done[i] {
			start = i
			break
		}
	}
	seen := map[int]int{} // node -> position in path
	var path []string
	for at := start; ; {
		if pos, ok := seen[at]; ok {
			return append(path[pos:], packs[at].Name)
		}
		seen[at] = len(path)
		path = append(path, packs[at].Name)
		next := -1
		for j := range edges[at] {
			if !done[j] {
				next = j
				break
			}
		}
		if next == -1 { // residual node with no live edge cannot start the cycle
			return path
		}
		at = next
	}
}
```

- [ ] **Step 4: Graph tests** (`internal/pack/depgraph_test.go`). Helper
  builds index-aligned fixtures: `mk(name string, deps []string, gvks ...schema.GroupVersionKind)`
  producing a `*Pack{Name, DependsOn}`, a `config.PackRef{Ref: "packs/" + name}`,
  and a `*Rendered{Name, Objects: <one unstructured per gvk>}`. Cases
  (table-driven where natural; each asserts either the exact order or the
  exact diag code via `diag.CodeOf(err)` — check the existing helper name
  with `go doc ./internal/diag` first, VERIFY-API):
  1. no deps, no repo → order = declared order (DD6 fence, byte-for-byte).
  2. diamond (d→b, d→c, b→a, c→a over declared a,b,c,d after gw) →
     order respects edges AND ties break by declared index.
  3. unknown name → CUBE-4018, message contains the installed list.
  4. self-dep → CUBE-4019, path `x → x`.
  5. 2-cycle and 3-cycle → CUBE-4019, path contains each member once plus
     the repeated head (`a → b → a`).
  6. gateway pack (index 0) with pack.cue deps → CUBE-4020; same for
     refs[0].DependsOn.
  7. implicit (a): pack rendering an
     `{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}`
     object sorts after the gateway even declared first; gateway's own
     Gateway-API objects add no self-edge; deps map shows the gateway name.
  8. implicit (b): `Delivery: "repo"` pack sorts after gitea, deps map
     shows gitea; repo pack with NO gitea in the cube → CUBE-4018.
  9. repo-delivery guarantee: declared `[argocd, gitea, mypack(repo)]` →
     gitea before mypack (DD6's guarantee half; argocd may precede gitea).
  10. duplicate pack names → CUBE-4018 (collision message).
  Run: `go test ./internal/pack/ -run TestResolveOrder -v`
  Expected: FAIL before Step 3 lands, PASS after.
- [ ] **Step 5: Task gate.**
  `go build ./... && go vet ./... && go test ./...` plus the frozen-fence
  run from Global Constraints. Expected: ALL PASS —
  `cmd/testdata/clitree.golden` untouched by `git status`.
- [ ] **Step 6: Commit** —
  `git add internal/pack/ internal/config/ internal/diag/ && git commit -m "feat(pack): dependency graph — dependsOn loaders, ResolveOrder, cycle detection (p6 DEP1)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH:
COMMITS:
FINDINGS:
REVIEW:
BLOCKERS:
HANDOFF:
```

---

### DEP2: `up` two-pass restructure + `diff` mirror  `[repo: $ROOT]`

**Branch:** `p6/dep2-up-two-pass` · **Depends:** DEP1

**Files:**
- Modify: `internal/up/up.go` (pack loop split, `orderPackRefs` retirement),
  `internal/diff/diff.go` (`desiredState` graph call), affected unit tests
  and any output golden asserting the old single step line

**Interfaces:**
- Consumes: `pack.ResolveOrder` (DEP1, exact signature above).
- Produces: in `up.Run`, index-aligned `refs []config.PackRef`,
  `packs []*pack.Pack`, `renders []*pack.Rendered` in DECLARED order plus
  `order []int`, `deps map[string][]string` in scope at the deliver loop
  — DEP3 and LOCK2 build on exactly these names. `orderPackRefs` reduced
  to gateway-prepend only.

- [ ] **Step 1: Failing behavior test first.** In the up branch unit tests
  (the P7 fake-collaborator style around `deliverDeps` —
  `internal/up/up_test.go`), add `TestDeliverOrderRespectsDependsOn`: a
  faked cube with gateway + packs `[b (dependsOn a), a]` records the
  ORDER of `deliverPack` invocations; assert `gateway, a, b`. And
  `TestUpFailsFastOnDepCycle`: packs `[a (dependsOn b), b (dependsOn a)]`
  → `up.Run` returns CUBE-4019 BEFORE any deliver invocation (assert the
  fake recorded zero deliveries — the fail-position improvement of §3.3).
  Run: `go test ./internal/up/ -run 'TestDeliverOrder|TestUpFailsFast' -v`
  Expected: FAIL (order is declared-order today; cycle undetected).
- [ ] **Step 2: Split the loop.** In `up.Run`, replace the single
  fetch+render+deliver loop with:
  1. *Fetch+render pass* (declared order, `i, pref := range refs`): keeps
     `stepFetchSource`, `pack.Fetch`, the `i == 0` F11
     `verifyGatewayPackRef` guard, `RenderWith`, `lock.RenderedHash`, and
     the `entries`/`packs` appends verbatim — plus `renders = append(renders, rendered)`.
     Step: `pr := con.ProgressN("pack-fetch", "fetching "+pref.Ref, i+1, len(refs))`,
     closed `pr.Done("%s@%s rendered", rendered.Name, rendered.Version)`.
  2. *Graph pass*: `order, deps, err := pack.ResolveOrder(packs, refs, renders)`;
     error → return (no cluster mutation yet for any pack).
  3. *Deliver pass*: `for pos, i := range order` with
     `pr := con.ProgressN("pack", "delivering "+refs[i].Ref, pos+1, len(refs))`,
     the P7 `deliverPack(ctx, deps, refs[i], renders[i])` tail and
     `pr.Done("%s@%s delivered", …)` moved verbatim. (`deps` the
     graph map vs. `deps` the P7 `deliverDeps` value collide — rename the
     graph result `packDeps` throughout; FINDINGS records it.)
  `orderPackRefs` shrinks to:

```go
// orderPackRefs prepends the gateway pack ref. Ordering beyond that —
// including decision 13's gitea-before-repo-packs guarantee — moved to
// pack.ResolveOrder (p6 DEP2): the implicit repo→gitea edge plus the
// declared-order tie-break keep the guarantee; the giteaSession bounded
// gate still backstops the wait either way.
func orderPackRefs(gatewayRef string, packs []config.PackRef) []config.PackRef {
	return append([]config.PackRef{{Ref: gatewayRef}}, packs...)
}
```

  Update `orderPackRefs`' existing gitea-hoist unit tests: the hoist
  assertions move to DEP1's case 9; the remaining tests assert
  gateway-prepend only.
- [ ] **Step 3: Mirror in `diff`.** `diff.desiredState` already
  fetches+renders everything in one pass — accumulate `dPacks []*pack.Pack`
  and `dRenders []*pack.Rendered` alongside its existing loop and call
  `ResolveOrder` after it, discarding `order` (diff does not deliver) but
  keeping `packDeps` in a variable DEP3 will thread into the Deliver
  calls; a graph error surfaces from `diff` exactly as CUBE-4016/4017 do
  today. Unit test: cycle cube → `diff` returns CUBE-4019.
- [ ] **Step 4: Step-line fences.** The `pack-fetch` step is a NEW plain
  line and the delivery line's enumeration now counts the deliver pass.
  RECONCILE (mandatory, in-worktree): `grep -rn "delivering " --include='*_test.go' --include='*.golden' internal/ cmd/ tests/`
  and update every hit consciously; list each touched golden/test in
  FINDINGS. The TE/mode-matrix fences themselves must NOT be
  regenerated — only per-test expectations that literally assert the
  up step sequence.
- [ ] **Step 5: Verify + gate.**
  `go test ./internal/up/ -run 'TestDeliverOrder|TestUpFailsFast' -v` →
  PASS; then the full Global-Constraints gate. Expected: ALL PASS.
- [ ] **Step 6: Commit** —
  `git add internal/up/ internal/diff/ && git commit -m "feat(up): two-pass pack delivery — fetch/render, resolve graph, deliver in topo order (p6 DEP2)"`
  (plus any golden files the RECONCILE touched, staged explicitly).

#### Outcome

```
STATUS: UNCLAIMED
BRANCH:
COMMITS:
FINDINGS:
REVIEW:
BLOCKERS:
HANDOFF:
```

---

### DEP3: engine translation — flux `dependsOn`, argocd annotation + wave gate  `[repo: $ROOT]`

**Branch:** `p6/dep3-engine-translation` · **Depends:** DEP2

**Files:**
- Modify: `internal/pack/pack.go` (Rendered.DependsOn),
  `internal/engine/engine.go` (OrdersDeliveries + DeliverGit widening),
  `internal/engine/flux/deliver.go` + `delivergit.go` + `flux.go`,
  `internal/engine/argocd/deliver.go` + `delivergit.go` + `argocd.go`,
  `internal/engine/contract/contract.go` (exercise the new surface),
  `internal/up/up.go` (thread deps; wave gate), `internal/diff/diff.go`
  (thread deps), `internal/diag/codes.go` + `registry.go` (CUBE-3011),
  every `engine.Engine` fake (compiler-led)
- Test: `internal/engine/flux/deliver_test.go` (or the existing flux test
  file), argocd equivalents, `internal/up/up_test.go`

**Interfaces:**
- Consumes: DEP2's in-scope `order`, `packDeps`, `renders` in `up.Run`.
- Produces: `pack.Rendered.DependsOn []string`;
  `Engine.OrdersDeliveries() bool` (flux true, argocd false);
  `Engine.DeliverGit(ctx context.Context, name string, src GitSource, dependsOn []string)`;
  `diag.CodeEngineDepWait` = CUBE-3011; up-internal
  `waitDepsHealthy(ctx, eng, a, packName string, deps []string, timeout, poll time.Duration) error`.

- [ ] **Step 1: Failing engine tests.** Flux: `Deliver` of a `Rendered`
  with `DependsOn: []string{"floci", "gitea"}` must produce a
  Kustomization whose `spec.dependsOn` is
  `[{"name": "cube-idp-floci"}, {"name": "cube-idp-gitea"}]`, and one
  with nil DependsOn must have NO `dependsOn` key (byte-compat fence for
  dep-free cubes); same pair for `DeliverGit`. Argocd: same inputs must
  yield the Application annotation
  `cube-idp.dev/depends-on: "floci,gitea"`, and no annotations key when
  nil. Run: `go test ./internal/engine/... -run Depends -v` — Expected:
  FAIL (compile errors count — the fields/params don't exist yet).
- [ ] **Step 2: Widen the seam.** `pack.Rendered` gains:

```go
	// DependsOn is the pack's resolved dependency list (pack names, sorted
	// — pack.ResolveOrder's deps entry), set by the ORCHESTRATOR after
	// graph resolution, never by RenderWith: deps are cube-composition
	// intent, not render output. Engines translate it (flux: Kustomization
	// spec.dependsOn; argocd: the cube-idp.dev/depends-on annotation plus
	// up's wave gate — argocd has no cross-Application ordering).
	DependsOn []string
```

  `engine.Engine` gains (doc comments as in the spec §3.4):
  `OrdersDeliveries() bool`, and `DeliverGit` widens to
  `DeliverGit(ctx context.Context, name string, src GitSource, dependsOn []string) ([]*unstructured.Unstructured, error)`.
  Flux implementation — in `deliver.go` after building `kust`:

```go
	if len(r.DependsOn) > 0 {
		kust.Object["spec"].(map[string]any)["dependsOn"] = dependsOnRefs(r.DependsOn)
	}
```

  with the shared helper (one place, used by DeliverGit too):

```go
// dependsOnRefs renders resolved dep names as flux dependency references
// (name-only: every cube-idp Kustomization lives in flux-system). Input
// is sorted (ResolveOrder), so the render is deterministic.
func dependsOnRefs(deps []string) []any {
	out := make([]any, len(deps))
	for i, d := range deps {
		out[i] = map[string]any{"name": deliveryName(d)}
	}
	return out
}
```

  `func (f *Flux) OrdersDeliveries() bool { return true }`. Argocd:
  `application(name, source)` widens to
  `application(name string, source map[string]any, dependsOn []string)`;
  when non-empty it sets
  `metadata.annotations = map[string]any{"cube-idp.dev/depends-on": strings.Join(dependsOn, ",")}`;
  `func (g *ArgoCD) OrdersDeliveries() bool { return false }`. Chase the
  compiler through every fake (`grep -rln "DeliverGit" --include='*_test.go' internal/ cmd/`)
  — each fake adds the param and a trivial `OrdersDeliveries() bool`.
  `engine/contract/contract.go` (`Run`): add one assertion per impl —
  Deliver with `DependsOn: ["x"]` yields engine-native ordering intent
  (flux: `spec.dependsOn` present; argocd: the annotation) so any future
  engine must answer the question consciously.
- [ ] **Step 3: CUBE-3011 + wave gate.** `internal/diag/codes.go`, engine
  3xxx block (after CUBE-3010):

```go
	CodeEngineDepWait Code = "CUBE-3011" // a pack's dependency did not become healthy before its wave-gated delivery (argocd)
```

  registry.go: `CodeEngineDepWait: {Summary: "a pack's dependency did not become healthy before its wave-gated delivery (argocd)"},`.
  In `internal/up/up.go` (near `giteaSession`, whose bounded-gate shape
  this mirrors):

```go
// waitDepsHealthy is the wave gate for engines that cannot order
// deliveries natively (OrdersDeliveries false — argocd; spec 2026-07-19
// DD5, ratified): before applying a dependent pack's delivery, poll
// Health until every dependency's component (cube-idp-<dep>) is Ready,
// bounded by timeout (the no-infinite-spinner rule). Flux never enters
// here — its Kustomization dependsOn orders reconciliation in-cluster.
func waitDepsHealthy(ctx context.Context, eng engine.Engine, a *apply.Applier, packName string, deps []string, timeout, poll time.Duration) error {
	if len(deps) == 0 {
		return nil
	}
	want := make(map[string]bool, len(deps))
	for _, d := range deps {
		want["cube-idp-"+d] = true
	}
	deadline := time.Now().Add(timeout)
	for {
		health, err := eng.Health(ctx, a)
		if err != nil {
			return err
		}
		ready := 0
		for _, h := range health {
			if want[h.Name] && h.Ready {
				ready++
			}
		}
		if ready == len(want) {
			return nil
		}
		if time.Now().After(deadline) {
			return diag.New(diag.CodeEngineDepWait,
				fmt.Sprintf("pack %s waits on %s — dependency not healthy within %s", packName, strings.Join(deps, ", "), timeout),
				"re-run `cube-idp up` (idempotent), or check the dependency with `cube-idp status`")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}
```

  Deliver-pass wiring (DEP2's loop): before `deliverPack` for pack i,
  `renders[i].DependsOn = packDeps[packs[i].Name]`, then
  `if !eng.OrdersDeliveries() { waitDepsHealthy(ctx, eng, a, packs[i].Name, renders[i].DependsOn, applyTimeout, giteaReadyPoll) }`
  (reuse the existing poll constant; FINDINGS records the actual name if
  it differs — VERIFY-API). `diff.desiredState` sets the same
  `DependsOn` before ITS Deliver calls so diff previews byte-identical
  flux objects. Unit tests (faked engine): OrdersDeliveries-false engine
  → deliveries interleave with health polls and a never-ready dep times
  out as CUBE-3011; OrdersDeliveries-true engine → zero wave-gate Health
  calls beyond today's. Spec §3.6 rider: find where the health-gate
  timeout error is raised (`waitHealthy`'s deadline path — RECONCILE the
  code it carries, expected CUBE-3004) and append to its remediation
  text: "deep dependsOn chains serialize startup — deps reconcile before
  dependents".
- [ ] **Step 4: Verify + gate.**
  `go test ./internal/engine/... ./internal/up/ ./internal/diff/ -v -run 'Depends|Contract|DepWait'`
  → PASS; the diag registry fence; then the full Global-Constraints gate.
  Expected: ALL PASS.
- [ ] **Step 5: Commit** —
  `git add internal/pack/ internal/engine/ internal/up/ internal/diff/ internal/diag/ && git commit -m "feat(engine): translate pack deps — flux Kustomization dependsOn, argocd annotation + wave gate (p6 DEP3)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH:
COMMITS:
FINDINGS:
REVIEW:
BLOCKERS:
HANDOFF:
```

---

### DEP4: DEPENDS-ON record column, docs, e2e leg  `[repo: $ROOT]`

**Branch:** `p6/dep4-record-docs-e2e` · **Depends:** DEP3

**Files:**
- Modify: `internal/pack/expose.go` (PackObject), `internal/pack/manifests/pack-crd.yaml`,
  `internal/up/up.go` (record-writer call site), `README.md` (cube.yaml
  table + a Pack-dependencies paragraph), `docs/pack-contract-v1.md` (§2
  row + conformance note)
- Test: `internal/pack/expose_test.go`, `tests/e2e/e2e_test.go`

**Interfaces:**
- Consumes: DEP2/DEP3's `packDeps` map at the D11 record-writer loop.
- Produces: `PackObject(p *Pack, gw config.GatewaySpec, ready, customized bool, delivery string, dependsOn []string)`
  — the new final param is the sanctioned append-only widening (P8
  HANDOFF: "future record fields widen the same way"). Record fields:
  `spec.dependsOnList` (array, machine truth) + `spec.dependsOn`
  (comma-joined string for the printer column — the authSecretRef/
  authSecret flattened-twin precedent, shorter name to the column-facing
  field). Printer column `DEPENDS-ON` appended after `DELIVERY`.

- [ ] **Step 1: Failing record test.** `internal/pack/expose_test.go`:
  `PackObject(..., []string{"floci", "gitea"})` yields
  `spec.dependsOnList == []any{"floci", "gitea"}` and
  `spec.dependsOn == "floci,gitea"`; nil deps yields NEITHER key (stock
  records byte-identical to pre-p6 — unlike customized/delivery, absence
  means "no deps", which the blank column cell already communicates).
  Run: `go test ./internal/pack/ -run TestPackObject -v` — Expected:
  FAIL (compile: param count).
- [ ] **Step 2: Implement.** Widen `PackObject` (doc comment gains: "
  dependsOn is the pack's RESOLVED dependency list — explicit ∪ implicit,
  what actually gated delivery (p6 DEP4)"); body after the delivery
  default:

```go
	if len(dependsOn) > 0 {
		list := make([]any, len(dependsOn))
		for i, d := range dependsOn {
			list[i] = d
		}
		spec["dependsOnList"] = list
		spec["dependsOn"] = strings.Join(dependsOn, ",")
	}
```

  Call site in `up.Run`'s record loop:
  `pack.PackObject(pk, cube.Spec.Gateway, healthByName["cube-idp-"+pk.Name], customized, refs[i].Delivery, packDeps[pk.Name])`
  — NOTE the record loop iterates the DECLARED-order `packs` slice;
  `packDeps` is keyed by name, so no index remap is needed.
  `pack-crd.yaml`: schema properties gain
  `dependsOn: {type: string}` and
  `dependsOnList: {type: array, items: {type: string}}`; columns gain
  `- {name: DEPENDS-ON, type: string, jsonPath: .spec.dependsOn}`
  appended after DELIVERY. Run the Step 1 test → PASS.
- [ ] **Step 3: Docs.** README cube.yaml reference table (the F1-audited
  table): add row `packs[].dependsOn` — "list of pack *names* this pack
  needs delivered/healthy first; unioned with the pack's own pack.cue
  dependsOn; cycles are CUBE-4019 at up/diff time; flux orders
  reconciliation natively, argocd orders delivery only (annotation +
  wave gate)". Add a short "Pack dependencies" paragraph in the packs
  section covering the two implicit edges (Gateway-API render → gateway
  pack; delivery: repo → gitea) and `kubectl get packs` DEPENDS-ON.
  `docs/pack-contract-v1.md`: §2 table row
  `dependsOn | no | Pack names (not refs) this pack requires; NEW
  (additive, §6) — pre-p6 binaries ignore the field and simply don't
  order the delivery`; conformance section: the harness delivers a
  pack's declared dep closure from the monorepo by name (supersedes the
  A11 EXTRA_PACK workaround for in-repo deps; DEP5 implements).
  `docs/machine-readable-output.md` needs NO change (kubectl columns are
  not cube-idp JSON output) — verify and state in FINDINGS. Spec §3.6
  rider (RECONCILE): read `cmd/pack.go`'s install path — it edits
  cube.yaml and delivers nothing itself, so graph validation lands at
  the next `up`/`diff` (CUBE-4018/4019 there). If, and only if, the
  install path already resolves the pack (it fetches for tag
  defaulting), add a warn-only line when a declared dep is absent from
  spec.packs — never a block; record the choice in FINDINGS.
- [ ] **Step 4: e2e leg.** `tests/e2e/e2e_test.go` `TestPackDependsOn`
  (same env-gating pattern as `TestPublishedPacksByDigest`): cube.yaml
  with the default gateway + `packs: [gitea, argocd]` where the argocd
  entry carries `dependsOn: ["gitea"]` (cube.yaml surface — published
  packs declare nothing until DEP5); run `up`; assert (a)
  `kubectl get kustomization cube-idp-argocd -n flux-system -o jsonpath={.spec.dependsOn[0].name}`
  = `cube-idp-gitea`, (b) `kubectl get packs argocd -o jsonpath={.spec.dependsOn}`
  = `gitea`, (c) status/health converge as usual; `down` tears down.
  Run: `CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestPackDependsOn -v -timeout 25m`
  Expected: PASS (record actual wall time in FINDINGS).
- [ ] **Step 5: Gate + commit.** Full Global-Constraints gate → ALL PASS.
  `git add internal/pack/ internal/up/ README.md docs/ tests/e2e/ && git commit -m "feat(pack): DEPENDS-ON record column + docs + e2e dep-chain leg (p6 DEP4)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH:
COMMITS:
FINDINGS:
REVIEW:
BLOCKERS:
HANDOFF:
```

---

### DEP5: packs declare their deps; conformance dep closure  `[repo: $PACKS]`

**Branch:** `p6/dep5-packs-declare-deps` (in `$PACKS`) · **Depends:** DEP4

**Files (in `$PACKS`):**
- Modify: `packs/floci-ui/pack.cue` (+ README), `packs/kyverno-policies/pack.cue`
  (+ README), the conformance harness (`hack/conformance.sh` and/or its
  helper — RECONCILE below)

**Interfaces:**
- Consumes: a cube-idp binary built from `$ROOT` main (DEP4 merged) that
  honors `dependsOn`.
- Produces: `floci-ui` `dependsOn: ["floci"]`, `kyverno-policies`
  `dependsOn: ["kyverno"]`, each with a PATCH version bump; a conformance
  harness that reads `dependsOn` from the pack-under-test's pack.cue and
  adds each named in-repo pack to the delivered set (topo-handled by
  cube-idp itself — the harness only needs to include them in cube.yaml).

- [ ] **Step 1: RECONCILE the harness shape.** Read `hack/conformance.sh`
  as it exists NOW in `$PACKS` (A10/A11 evolved it): locate where the
  pack-under-test and `CUBE_IDP_CONFORMANCE_EXTRA_PACK_DIR` are turned
  into cube.yaml packs entries. Record in FINDINGS the exact mechanism
  before changing it.
- [ ] **Step 2: Dep closure in the harness.** Parse `dependsOn` from the
  pack-under-test's pack.cue (`cue eval` is available in the harness
  toolchain — RECONCILE: confirm; otherwise `grep`-parse the single
  line) and, for each named pack that exists under `packs/`, add it to
  the generated cube.yaml BEFORE the pack under test. Keep
  `EXTRA_PACK_DIR` working for out-of-repo deps; in-repo deps no longer
  need it. Conformance of a dep-free pack is byte-identical.
- [ ] **Step 3: floci-ui.** `packs/floci-ui/pack.cue`: add
  `dependsOn: ["floci"]`, bump `version` PATCH (RECONCILE the current
  value in-tree — A11 shipped 0.1.0, so expect 0.1.1). README: replace
  the EXTRA_PACK conformance instruction with "declared via dependsOn;
  the harness delivers floci automatically". Heed the A11 HANDOFF
  cautions if the conformance run flags them (enableServiceLinks,
  HTTPRoute CRD race — the race is now structurally dead via the
  implicit gateway edge; observe and record).
  Run: `bash hack/conformance.sh floci-ui <cube-idp built from $ROOT main>`
  Expected: CONFORMANT, floci delivered first (flux `dependsOn` visible
  on `cube-idp-floci-ui`).
- [ ] **Step 4: kyverno-policies.** Same shape: `dependsOn: ["kyverno"]`,
  PATCH bump, README note.
  Run: `bash hack/conformance.sh kyverno-policies <same binary>` —
  Expected: CONFORMANT.
- [ ] **Step 5: OWNER GATE — tags.** One tag per push, per the standing
  pre-authorization: `floci-ui/v<bumped>` then `kyverno-policies/v<bumped>`,
  each watched to publish-workflow SUCCESS +
  `gh attestation verify oci://ghcr.io/cube-idp/packs/<name>:<ver> --owner cube-idp`
  exit 0. Record run URLs in FINDINGS.
- [ ] **Step 6: Commit + ledger.** Commits in `$PACKS`
  (`feat(floci-ui): declare dependsOn floci (p6 DEP5)` etc.); the ledger
  tick for this task is committed in `$ROOT` on `main` as always.

#### Outcome

```
STATUS: UNCLAIMED
BRANCH:
COMMITS:
FINDINGS:
REVIEW:
BLOCKERS:
HANDOFF:
```

---

### LOCK1: cube.lock KRM shape + legacy lift  `[repo: $ROOT]`

**Branch:** `p6/lock1-lock-krm-shape` · **Depends:** — (parallel with DEP lane)

**Files:**
- Modify: `internal/lock/lock.go`, `internal/lock/lock_test.go`, and the
  compiler-led consumer sweep: `internal/up/up.go`, `internal/up/bundle.go`,
  `internal/upgrade/plan.go`, `internal/diff/diff.go`,
  `internal/bundle/vendor.go`, `internal/bundle/bundle.go` (+ their tests)

**Interfaces:**
- Produces: `lock.File{APIVersion, Kind, Metadata Metadata, Spec Spec}`,
  `lock.Metadata{Name string}`, `lock.Spec{Engine EngineLock, Packs []Entry}`
  — `Entry` and `EngineLock` unchanged. Every consumer reads
  `f.Spec.Packs` / `f.Spec.Engine`; `up` sets `Metadata.Name` from
  `cube.Metadata.Name`. `Read` guarantees: legacy top-level-shape files
  parse forever (lifted into Spec); wrong kind / wrong apiVersion group →
  CUBE-0003.
- Consumes: nothing new.

- [ ] **Step 1: Failing lock tests.** In `internal/lock/lock_test.go`:
  (a) `TestWriteReadRoundTripKRM` — write
  `&File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock", Metadata: Metadata{Name: "dev"}, Spec: Spec{Engine: EngineLock{Type: "flux"}, Packs: []Entry{…}}}`,
  read it back, assert `Spec.Packs` and `Metadata.Name` survive AND the
  serialized bytes contain a `spec:` key with `engine:`/`packs:` nested
  under it (shape assertion, not just struct equality);
  (b) `TestReadLiftsLegacyShape` — a raw fixture string in the OLD shape
  (top-level `engine:`/`packs:`, no metadata/spec — copy the exact shape
  from a pre-p6 `cube.lock`, e.g. the bundle testdata) reads into
  `Spec.Engine.Type == "flux"` and the packs entries;
  (c) `TestReadRejectsWrongKind` — `kind: NotCubeLock` → CUBE-0003;
  `apiVersion: something.else/v1` → CUBE-0003; missing kind on an
  otherwise-legacy file still lifts (old files DID carry kind CubeLock —
  the tolerance is for hand-truncated files, which the corrupt path
  already owns).
  Run: `go test ./internal/lock/ -v` — Expected: FAIL (compile).
- [ ] **Step 2: Reshape.** `internal/lock/lock.go`:

```go
// File is the top-level shape of cube.lock — since p6 LOCK1 a proper
// KRM-style object (apiVersion/kind/metadata/spec), the same shape
// discipline as cube.yaml's Cube (spec 2026-07-19 §4.1, DD7). Read lifts
// the pre-p6 legacy shape (engine/packs at top level) transparently;
// Write always emits the KRM shape.
type File struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

// Metadata names the cube this lock was written for (cube.yaml
// metadata.name) — the in-cluster CubeLock record (LOCK2) uses the same
// name.
type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

// Spec is the body of a CubeLock document.
type Spec struct {
	Engine EngineLock `yaml:"engine" json:"engine"`
	Packs  []Entry    `yaml:"packs" json:"packs"`
}
```

  `Read` after the existing unmarshal:

```go
	if f.Kind != "" && f.Kind != "CubeLock" {
		return nil, diag.New(diag.CodeLockCorrupt,
			fmt.Sprintf("%s has kind %q, not CubeLock", path, f.Kind),
			"delete it and re-run `cube-idp up` to regenerate")
	}
	if f.APIVersion != "" && !strings.HasPrefix(f.APIVersion, "cube-idp.dev/") {
		return nil, diag.New(diag.CodeLockCorrupt,
			fmt.Sprintf("%s has apiVersion %q, not cube-idp.dev/*", path, f.APIVersion),
			"delete it and re-run `cube-idp up` to regenerate")
	}
	if f.Spec.Engine.Type == "" && len(f.Spec.Packs) == 0 {
		// Legacy lift (DD7): pre-p6 locks carried engine/packs at top level.
		// cube.lock is derived state, so the lift is read-only tolerance —
		// the next Write emits the KRM shape. up always locks ≥1 pack (the
		// gateway), so a genuinely empty new-shape lock cannot reach here.
		var legacy struct {
			Engine EngineLock `yaml:"engine" json:"engine"`
			Packs  []Entry    `yaml:"packs" json:"packs"`
		}
		if err := yaml.Unmarshal(raw, &legacy); err == nil {
			f.Spec.Engine, f.Spec.Packs = legacy.Engine, legacy.Packs
		}
	}
```

- [ ] **Step 3: Consumer sweep, compiler-led.**
  `go build ./... 2>&1 | head -40` and fix every `.Packs`/`.Engine`
  reference to `.Spec.Packs`/`.Spec.Engine` — expected sites:
  `internal/up/up.go` (the `lf :=` assembly becomes
  `&lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock", Metadata: lock.Metadata{Name: cube.Metadata.Name}, Spec: lock.Spec{Engine: lock.EngineLock{Type: cube.Spec.Engine.Type}, Packs: entries}}`),
  `internal/up/bundle.go` (`resolveBundleRefs`, `bundlePackName`),
  `internal/upgrade/plan.go` (`lockEntryByRef`), `internal/diff/diff.go`
  (`lockEntryFor`), `internal/bundle/vendor.go` (`vendorPacks`,
  `vendorImages`), `internal/bundle/bundle.go` (open/verify). List every
  touched site in FINDINGS; NO behavior change beyond field paths. Fix
  test fixtures the same way EXCEPT deliberately-legacy ones from
  Step 1b.
- [ ] **Step 4: Verify + gate.** `go test ./internal/lock/ -v` → PASS;
  `go test ./internal/bundle/ ./internal/upgrade/ ./internal/diff/ ./internal/up/`
  → PASS (vendor/bundle round-trips prove old embedded locks still
  open); full Global-Constraints gate → ALL PASS.
- [ ] **Step 5: Commit** —
  `git add internal/ && git commit -m "feat(lock): cube.lock is a proper KRM object — metadata/spec shape, legacy lift, identity checks (p6 LOCK1)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH:
COMMITS:
FINDINGS:
REVIEW:
BLOCKERS:
HANDOFF:
```

---

### LOCK2: CubeLock CRD + in-cluster record  `[repo: $ROOT]`

**Branch:** `p6/lock2-cubelock-crd` · **Depends:** LOCK1, DEP2 (merge after both)

**Files:**
- Create: `internal/lock/manifests/cubelock-crd.yaml`,
  `internal/lock/record.go`, `internal/lock/record_test.go`
- Modify: `internal/up/up.go` (records-crd step + lock step),
  `internal/diff/diff.go` (`desiredState`), `internal/up` envtest

**Interfaces:**
- Consumes: LOCK1's `lock.File` shape; `pack.CRD()`'s embed/accessor
  pattern (`internal/pack/discovery.go`) copied, not shared;
  `apply.Applier` + `identityStub` in diff.
- Produces: `lock.CRD() (*unstructured.Unstructured, error)`;
  `lock.RecordObject(f *File) *unstructured.Unstructured` (named from
  `f.Metadata.Name` — LOCK1 guarantees `up` fills it).

- [ ] **Step 1: The CRD manifest.** Create
  `internal/lock/manifests/cubelock-crd.yaml`:

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: cubelocks.cube-idp.dev
  labels:
    app.kubernetes.io/part-of: cube-idp
spec:
  group: cube-idp.dev
  scope: Cluster
  names:
    kind: CubeLock
    listKind: CubeLockList
    plural: cubelocks
    singular: cubelock
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                engine:
                  type: object
                  properties:
                    type: {type: string}
                # packCount is written explicitly by the record writer —
                # JSONPath printer columns cannot compute array lengths.
                packCount: {type: integer}
                packs:
                  type: array
                  items:
                    type: object
                    properties:
                      ref: {type: string}
                      name: {type: string}
                      version: {type: string}
                      resolved: {type: string}
                      renderedHash: {type: string}
                      images: {type: array, items: {type: string}}
      additionalPrinterColumns:
        - {name: ENGINE, type: string, jsonPath: .spec.engine.type}
        - {name: PACKS, type: integer, jsonPath: .spec.packCount}
```

- [ ] **Step 2: Failing accessor tests.** `internal/lock/record_test.go`:
  `TestCRDParses` (CRD() returns kind CustomResourceDefinition named
  `cubelocks.cube-idp.dev`); `TestRecordObject` — a `File` with 2 packs
  yields kind CubeLock, `metadata.name` = the file's `Metadata.Name`,
  the part-of label, `spec.packCount == int64(2)`, and pack entries with
  all five scalar fields + images. Run:
  `go test ./internal/lock/ -run 'TestCRD|TestRecord' -v` — Expected:
  FAIL (compile).
- [ ] **Step 3: Implement `internal/lock/record.go`** (mirrors
  `internal/pack/discovery.go`):

```go
package lock

import (
	_ "embed"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/diag"
)

//go:embed manifests/cubelock-crd.yaml
var cubelockCRD []byte

// CRD returns the inert cubelocks.cube-idp.dev CustomResourceDefinition
// (spec 2026-07-19 §4.2, the D11 pattern): no controller — the record is
// written by `up` and pruned by `down` via the inventory, exactly like
// the Pack CRD.
func CRD() (*unstructured.Unstructured, error) {
	var obj map[string]any
	if err := yaml.Unmarshal(cubelockCRD, &obj); err != nil {
		return nil, diag.Wrap(err, diag.CodeLockCorrupt,
			"embedded cubelock CRD manifest is invalid", "this is a cube-idp bug — please report it")
	}
	return &unstructured.Unstructured{Object: obj}, nil
}

// RecordObject projects a written cube.lock into its cluster-scoped
// CubeLock record: the file stays the source of truth (vendor/bundle
// read it offline); the record's one job is `kubectl get cubelocks` —
// a running cluster answers "what was delivered here, from which pins"
// with no checkout and no local lock file.
func RecordObject(f *File) *unstructured.Unstructured {
	packs := make([]any, len(f.Spec.Packs))
	for i, e := range f.Spec.Packs {
		images := make([]any, len(e.Images))
		for j, im := range e.Images {
			images[j] = im
		}
		packs[i] = map[string]any{
			"ref": e.Ref, "name": e.Name, "version": e.Version,
			"resolved": e.Resolved, "renderedHash": e.RenderedHash,
			"images": images,
		}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cube-idp.dev/v1alpha1",
		"kind":       "CubeLock",
		"metadata": map[string]any{
			"name":   f.Metadata.Name,
			"labels": map[string]any{"app.kubernetes.io/part-of": "cube-idp"},
		},
		"spec": map[string]any{
			"engine":    map[string]any{"type": f.Spec.Engine.Type},
			"packCount": int64(len(f.Spec.Packs)), // int64: unstructured SSA (DeepCopyJSONValue) accepts int64, not int
			"packs":     packs,
		},
	}}
}
```

  Run the Step 2 tests → PASS.
- [ ] **Step 4: `up` + `diff` wiring.** In `up.Run`'s D11 CRD step: build
  `crdObjs` from BOTH `pack.CRD()` and `lock.CRD()`; step Done text
  becomes `"record CRDs established"` (was "Pack CRD established" —
  grep-sweep any test/golden asserting the old text, list in FINDINGS).
  At the lock step, after `lock.Write` succeeds:

```go
	rec := lock.RecordObject(lf)
	if err := a.Apply(ctx, []*unstructured.Unstructured{rec}, false, applyTimeout); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, []*unstructured.Unstructured{rec}); err != nil {
		return err
	}
	con.Step("lock", "cube.lock written (%d packs) — try `kubectl get cubelocks`", len(entries))
```

  (replacing the existing `con.Step("lock", …)` line — deliberately
  BEFORE the health gate: the lock records what was delivered, not what
  is healthy; an `up` aborted at the health gate leaves file and record
  agreeing.) `diff.desiredState`: `lock.CRD()` joins the desired block
  beside `pack.CRD()`; the record joins `orphanOnly` via
  `identityStub(schema.GroupVersionKind{Group: "cube-idp.dev", Version: "v1alpha1", Kind: "CubeLock"}, "", cube.Metadata.Name)`
  (identity-only — the record spec embeds fetch-resolved digests, so
  re-deriving it would fabricate perpetual drift; check `identityStub`'s
  exact signature in diff.go first, VERIFY-API). `down` needs no change:
  record + CRD ride the inventory cascade — prove it in the envtest.
- [ ] **Step 5: envtest.** In `internal/up`'s envtest file (pattern:
  `crd_wait_envtest_test.go`): establish both CRDs via the applier, apply
  a `RecordObject` for a 2-pack File, read it back
  (`kubectl`-equivalent client Get on cubelocks/`dev`), assert
  packCount==2; delete via the inventory path and assert gone.
  Run: `go test ./internal/up/ -run Envtest -v` (match the file's actual
  run-gate convention — VERIFY-API). Expected: PASS.
- [ ] **Step 6: Gate + commit.** Full Global-Constraints gate → ALL PASS.
  `git add internal/lock/ internal/up/ internal/diff/ && git commit -m "feat(lock): CubeLock in-cluster — inert cubelocks CRD + up-written record, kubectl get cubelocks (p6 LOCK2)"`

#### Outcome

```
STATUS: UNCLAIMED
BRANCH:
COMMITS:
FINDINGS:
REVIEW:
BLOCKERS:
HANDOFF:
```

---

## Plan-level completion

This plan is DONE when every task above is DONE/DONE_WITH_CONCERNS, the
DEP5 owner gate has run, and the two headline proofs hold on a fresh kind
cube (local recipe, port 18443):

1. **Dependencies:** a cube with `packs: [gitea, argocd(dependsOn: gitea)]`
   on flux shows `spec.dependsOn: [{name: cube-idp-gitea}]` on the argocd
   Kustomization and `DEPENDS-ON: gitea` in `kubectl get packs`; a cycle
   in cube.yaml fails `up` AND `diff` with CUBE-4019 naming the path
   before any delivery.
2. **CubeLock:** `kubectl get cubelocks` shows the cube's record (ENGINE,
   PACKS columns) matching `cube.lock` on disk, which now carries the
   apiVersion/kind/metadata/spec shape; a pre-p6 cube.lock still opens
   (`cube-idp vendor` over an old bundle), and `down` removes the record.
