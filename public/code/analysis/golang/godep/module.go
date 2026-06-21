package godep

import (
	"slices"
	"sort"
)

// This file is the third-party lens over a Manifest: it rolls external packages
// up by their owning go.mod module and answers the dependency-surface questions
// godepview's per-package table cannot — how many first-party packages lean on a
// module (fan-in), whether the module is a *direct* first-party dependency or
// only pulled in transitively, and the blast radius (the first-party packages a
// change to the module would reach). All derived from the stored graph (ADR-0064
// SD6); nothing is added to the serialized seam.

// ModuleStat aggregates one external module's footprint in the collection.
type ModuleStat struct {
	ModulePath  string   // owning go.mod module path; the rollup key
	NumPkgs     int      // external packages belonging to this module
	Direct      bool     // some first-party package imports a package in this module
	FanIn       int      // distinct first-party packages importing into this module
	BlastRadius int      // first-party packages that (transitively) depend on this module
	PkgIDs      []uint64 // the module's package ids, sorted
}

// unknownModule labels external packages whose owning module the collector
// could not record, so they still roll up into a single visible bucket rather
// than vanishing.
const unknownModule = "(unknown module)"

// ModuleStats rolls the external packages up by owning module and computes, per
// module, the footprint fields of ModuleStat. rootModule is unused today
// (Class already marks first-party packages) but is taken for symmetry with the
// other derived builders and to stay stable if classification moves. The result
// is sorted by module path.
func (idx *Index) ModuleStats(rootModule string) (out []ModuleStat) {
	_ = rootModule
	byModule := make(map[string][]uint64)
	for id, p := range idx.byID {
		if p.Class != ClassExternal {
			continue
		}
		mp := p.ModulePath
		if mp == "" {
			mp = unknownModule
		}
		byModule[mp] = append(byModule[mp], id)
	}

	out = make([]ModuleStat, 0, len(byModule))
	for mp, ids := range byModule {
		slices.Sort(ids)
		st := ModuleStat{ModulePath: mp, NumPkgs: len(ids), PkgIDs: ids}

		// Direct + fan-in: distinct first-party packages with an edge into the
		// module (i.e. first-party importers of any of the module's packages).
		fanIn := make(map[uint64]struct{})
		for _, mid := range ids {
			for _, from := range idx.importers[mid] {
				if fp, ok := idx.byID[from]; ok && fp.Class == ClassInternal {
					fanIn[from] = struct{}{}
				}
			}
		}
		st.FanIn = len(fanIn)
		st.Direct = st.FanIn > 0

		st.BlastRadius = len(idx.ReverseReachInternal(ids))
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModulePath < out[j].ModulePath })
	return out
}

// ReverseReachInternal returns the first-party (internal) packages that
// transitively import any package in seeds — the set that would be affected if
// the seed packages changed (a module's blast radius when seeds are that
// module's packages). It walks importers (reverse edges) and, since the seeds
// are typically external, the seeds themselves are excluded unless reached as an
// importer of another seed. The returned ids are sorted.
func (idx *Index) ReverseReachInternal(seeds []uint64) (internal []uint64) {
	visited := make(map[uint64]struct{}, len(seeds))
	stack := make([]uint64, 0, len(seeds))
	for _, s := range seeds {
		if _, ok := visited[s]; !ok {
			visited[s] = struct{}{}
			stack = append(stack, s)
		}
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, from := range idx.importers[id] {
			if _, ok := visited[from]; ok {
				continue
			}
			visited[from] = struct{}{}
			stack = append(stack, from)
			if p, ok := idx.byID[from]; ok && p.Class == ClassInternal {
				internal = append(internal, from)
			}
		}
	}
	slices.Sort(internal)
	return internal
}

// ShortestImportPathTo returns the shortest forward-import chain from `from` to
// the nearest package satisfying isTarget, as package ids [from, …, target]
// (inclusive). It is the minimum-hop witness for "why does `from` depend on
// (something satisfying isTarget)" — e.g. why a first-party package pulls in a
// given external module. It is a breadth-first walk of the forward Imports
// edges; it returns [from] when `from` itself satisfies isTarget, and nil when
// no target is reachable.
func (idx *Index) ShortestImportPathTo(from uint64, isTarget func(id uint64) bool) (path []uint64) {
	if isTarget(from) {
		return []uint64{from}
	}
	prev := map[uint64]uint64{from: from} // child → parent; from is its own parent (the stop sentinel)
	queue := []uint64{from}
	var hit uint64
	found := false
	for len(queue) > 0 && !found {
		id := queue[0]
		queue = queue[1:]
		p, ok := idx.byID[id]
		if !ok {
			continue
		}
		for _, to := range p.Imports {
			if _, seen := prev[to]; seen {
				continue
			}
			prev[to] = id
			if isTarget(to) {
				hit, found = to, true
				break
			}
			queue = append(queue, to)
		}
	}
	if !found {
		return nil
	}
	for cur := hit; ; cur = prev[cur] {
		path = append(path, cur)
		if cur == from {
			break
		}
	}
	slices.Reverse(path)
	return path
}

// ModuleImporters returns the first-party packages that import directly into the
// given module's packages (the fan-in set, not the transitive blast radius),
// sorted. seeds are the module's package ids (ModuleStat.PkgIDs).
func (idx *Index) ModuleImporters(seeds []uint64) (importers []uint64) {
	seen := make(map[uint64]struct{})
	for _, mid := range seeds {
		for _, from := range idx.importers[mid] {
			if _, ok := seen[from]; ok {
				continue
			}
			if p, ok := idx.byID[from]; ok && p.Class == ClassInternal {
				seen[from] = struct{}{}
				importers = append(importers, from)
			}
		}
	}
	slices.Sort(importers)
	return importers
}
