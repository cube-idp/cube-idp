package pack

import (
	"slices"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// InstanceID identifies one pack instance within a setup. It is the
// human-readable effective ID, never an opaque handle: it becomes the name of
// the delivery unit an operator reads in errors and in engine output.
type InstanceID string

// Instance is one entry of the setup paired with the pack it resolved to.
//
// Name is the pack's own name, from its pack.cue — the caller supplies it
// because resolving a packRef is I/O, and this package's graph is pure.
type Instance struct {
	Name string
	Spec v1alpha1.PackSpec
}

// ResolvedGraph is a setup's dependency graph: a deterministic install order
// and the edges it came from. It is data, not execution — sequencing it is the
// orchestrator's job.
type ResolvedGraph struct {
	// Order lists every instance, dependencies before dependents.
	Order []InstanceID
	// Dependencies maps each instance to the instances it depends on, with
	// duplicates removed and declaration order preserved.
	Dependencies map[InstanceID][]InstanceID
}

// ResolveOrder derives effective IDs, resolves every dependsOn entry, and
// topologically sorts the result.
//
// The graph contains exactly the declared edges. Nothing is inferred from a
// pack's category, its kinds, or anything else — an implicit edge is an
// ordering an operator never wrote and cannot find.
func ResolveOrder(instances []Instance) (ResolvedGraph, error) {
	ids, err := effectiveIDs(instances)
	if err != nil {
		return ResolvedGraph{}, err
	}
	deps, err := resolveDependencies(instances, ids)
	if err != nil {
		return ResolvedGraph{}, err
	}
	order, err := topoSort(ids, deps)
	if err != nil {
		return ResolvedGraph{}, err
	}
	return ResolvedGraph{Order: order, Dependencies: deps}, nil
}

// effectiveIDs derives each instance's identity, positionally.
//
// An explicit ID always wins. Otherwise the pack's own name serves, but only
// while it is unambiguous: once a name appears twice, neither copy can claim
// it, and the operator must say which is which.
func effectiveIDs(instances []Instance) ([]InstanceID, error) {
	byName := map[string]int{}
	for _, inst := range instances {
		byName[inst.Name]++
	}

	ids := make([]InstanceID, len(instances))
	seen := map[InstanceID]int{}

	for i, inst := range instances {
		id := InstanceID(inst.Spec.ID)
		if id == "" {
			if byName[inst.Name] > 1 {
				return nil, newInstanceIDRequiredError(inst.Name, byName[inst.Name])
			}
			id = InstanceID(inst.Name)
		}
		if first, dup := seen[id]; dup {
			return nil, newDuplicateInstanceIDError(string(id), first, i)
		}
		seen[id] = i
		ids[i] = id
	}
	return ids, nil
}

// resolveDependencies turns each declared dependsOn entry into an edge.
func resolveDependencies(instances []Instance, ids []InstanceID) (map[InstanceID][]InstanceID, error) {
	index := newInstanceIndex(instances, ids)
	deps := make(map[InstanceID][]InstanceID, len(instances))

	for i, inst := range instances {
		from := ids[i]
		var edges []InstanceID

		for _, target := range inst.Spec.DependsOn {
			to, err := index.lookup(target, from)
			if err != nil {
				return nil, err
			}
			// A target named twice is one edge; the graph records
			// relationships, not how often they were written down.
			if !slices.Contains(edges, to) {
				edges = append(edges, to)
			}
		}
		deps[from] = edges
	}
	return deps, nil
}

// instanceIndex resolves a dependsOn target, which may name either an
// effective ID or a pack name.
type instanceIndex struct {
	byID   map[InstanceID]bool
	byName map[string][]InstanceID
}

func newInstanceIndex(instances []Instance, ids []InstanceID) *instanceIndex {
	index := &instanceIndex{
		byID:   make(map[InstanceID]bool, len(instances)),
		byName: make(map[string][]InstanceID, len(instances)),
	}
	for i, inst := range instances {
		index.byID[ids[i]] = true
		index.byName[inst.Name] = append(index.byName[inst.Name], ids[i])
	}
	return index
}

// lookup resolves one target. IDs are tried first: an ID is unambiguous by
// construction, so a name that happens to equal one never shadows it.
func (x *instanceIndex) lookup(target string, from InstanceID) (InstanceID, error) {
	id := InstanceID(target)
	if !x.byID[id] {
		candidates := x.byName[target]
		switch len(candidates) {
		case 0:
			return "", newUnknownDependencyError(string(from), target)
		case 1:
			id = candidates[0]
		default:
			return "", newAmbiguousDependencyError(string(from), target, asStrings(candidates))
		}
	}
	if id == from {
		return "", newDependencyCycleError([]string{string(from)})
	}
	return id, nil
}

// topoSort orders dependencies before dependents.
//
// Kahn's algorithm, always taking the earliest-declared ready instance, so the
// order is a function of the document rather than of map iteration: the same
// setup yields the same order every run.
func topoSort(ids []InstanceID, deps map[InstanceID][]InstanceID) ([]InstanceID, error) {
	remaining := pendingCounts(ids, deps)

	order := make([]InstanceID, 0, len(ids))
	for len(order) < len(ids) {
		next, ok := readyInstance(ids, remaining)
		if !ok {
			return nil, newDependencyCycleError(asStrings(unresolved(ids, remaining)))
		}
		delete(remaining, next)
		order = append(order, next)

		// Everything that was waiting on next is one dependency closer.
		for _, id := range ids {
			if _, pending := remaining[id]; pending && slices.Contains(deps[id], next) {
				remaining[id]--
			}
		}
	}
	return order, nil
}

// pendingCounts gives each instance the number of dependencies that actually
// gate it.
//
// Edges to something outside the set are ignored rather than counted. Nothing
// builds such an edge today — lookup rejects unknown targets — but ResolveOrder
// is exported and ResolvedGraph.Dependencies is an ordinary map, so counting
// one would strand its dependent forever and report a cycle that does not
// exist. Being total here is cheaper than relying on the only current caller.
func pendingCounts(ids []InstanceID, deps map[InstanceID][]InstanceID) map[InstanceID]int {
	known := make(map[InstanceID]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}

	remaining := make(map[InstanceID]int, len(ids))
	for _, id := range ids {
		count := 0
		for _, dep := range deps[id] {
			if known[dep] {
				count++
			}
		}
		remaining[id] = count
	}
	return remaining
}

// readyInstance returns the earliest-declared instance whose dependencies are
// all placed.
func readyInstance(ids []InstanceID, remaining map[InstanceID]int) (InstanceID, bool) {
	for _, id := range ids {
		if count, pending := remaining[id]; pending && count == 0 {
			return id, true
		}
	}
	return "", false
}

// unresolved returns the instances still waiting, in declared order — the
// members of the cycle, which is what a user needs named.
func unresolved(ids []InstanceID, remaining map[InstanceID]int) []InstanceID {
	var stuck []InstanceID
	for _, id := range ids {
		if _, pending := remaining[id]; pending {
			stuck = append(stuck, id)
		}
	}
	return stuck
}

func asStrings(ids []InstanceID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}
