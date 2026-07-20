// depgraph resolves the pack dependency graph (spec 2026-07-19 §3.2):
// explicit deps (pack.cue dependsOn ∪ cube.yaml packs[].dependsOn) plus
// two implicit, never-declared edges — (a) any pack whose render contains
// a gateway.networking.k8s.io object depends on the gateway pack (DD3a,
// ratified: render-derived, NOT blanket), killing the A10 CRD-ordering
// race class; (b) any delivery:"repo" pack depends on gitea — gitea is an
// optional pack, so repo delivery validates its presence at config load and
// must be ordered behind it; that hoist is now expressed as a real edge
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
				// unreachable in up (config load already rejects a repo-delivered
				// pack in a cube with no gitea pack) but
				// ResolveOrder is a public seam — fail typed, not with a panic.
				return nil, nil, diag.New(diag.CodePackDepUnknown,
					fmt.Sprintf("pack %q is delivery: repo but no pack named \"gitea\" is in this cube", packs[i].Name),
					"repo delivery needs the gitea pack in spec.packs")
			}
			if j != i {
				edges[i][j] = true // implicit edge (b): repo delivery needs gitea up first
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
