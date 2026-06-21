package godep

import (
	"slices"
	"sort"
	"strings"
)

// This file is the architecture-altitude lens over a Manifest: it folds the
// per-package import graph into a *group* quotient (each apps/<name>, each
// public/<area>, each external module a node) and reports the coupling
// violations that quotient is meant to surface — e.g. the keelson rule that two
// apps must not depend on each other. Everything here is *derived* from the
// stored PackageNode fields (ImportPath/ModulePath/Class/Imports); nothing is
// added to the serialized seam (ADR-0064 SD6).

// GroupKey identifies a package group in the architecture (quotient) view.
// First-party packages key on their root-module-relative directory truncated to
// GroupingOpts depth (so apps/<name> and public/<area> become groups); external
// packages key on their owning module path; stdlib collapses to StdlibGroup.
// First-party keys never carry a domain segment and external module keys always
// do, so the two key spaces do not collide.
type GroupKey string

// StdlibGroup is the single group every stdlib package folds into — the stdlib
// is rarely the subject of an architecture question, so it collapses to one
// node (and is usually hidden entirely).
const StdlibGroup GroupKey = "stdlib"

// DefaultInternalDepth groups first-party packages at two leading path segments,
// which puts each apps/<name> and each public/<area> in its own node — the
// granularity at which the keelson "apps are independent" rule is legible.
const DefaultInternalDepth = 2

// GroupingOpts controls how packages fold into groups for the architecture view.
type GroupingOpts struct {
	// InternalDepth is how many leading segments of a first-party package's
	// module-relative path name its group. Values < 1 mean DefaultInternalDepth.
	// External packages always group by module regardless of this.
	InternalDepth int
}

func (o GroupingOpts) depth() (d int) {
	d = o.InternalDepth
	if d < 1 {
		d = DefaultInternalDepth
	}
	return
}

// GroupOf returns the group a package folds into. rootModule is the
// collection's RootModulePath; a package whose ImportPath is under it is
// first-party. GroupOf is a pure function of the package's stored fields, so the
// live and (future) facts paths produce identical groupings.
func GroupOf(p *PackageNode, rootModule string, opts GroupingOpts) (key GroupKey) {
	switch p.Class {
	case ClassStdlib:
		return StdlibGroup
	case ClassExternal:
		if p.ModulePath != "" {
			return GroupKey(p.ModulePath)
		}
		// An external package with no recorded module (rare): bucket by the
		// import path's first two segments — a stable, readable fallback.
		return GroupKey(firstSegments(p.ImportPath, 2))
	default: // internal / unknown
		return GroupKey(firstSegments(moduleRelative(p.ImportPath, rootModule), opts.depth()))
	}
}

// moduleRelative strips the root module prefix from a first-party import path,
// e.g. "<root>/apps/godepview" -> "apps/godepview". Paths not under rootModule
// (or an empty rootModule) are returned unchanged.
func moduleRelative(importPath string, rootModule string) (rel string) {
	if rootModule == "" {
		return importPath
	}
	if importPath == rootModule {
		return "." // the module-root package itself
	}
	if strings.HasPrefix(importPath, rootModule+"/") {
		return importPath[len(rootModule)+1:]
	}
	return importPath
}

// firstSegments returns the first n slash-separated segments of p (all of p when
// it has n or fewer).
func firstSegments(p string, n int) (s string) {
	if n < 1 {
		n = 1
	}
	segs := strings.Split(p, "/")
	if len(segs) <= n {
		return p
	}
	return strings.Join(segs[:n], "/")
}

// GroupNode is one node of the architecture (quotient) graph: a package group
// with its dominant class and member package ids (sorted).
type GroupNode struct {
	Key       GroupKey
	Class     string // dominant member class: internal | external | stdlib
	NumPkgs   int
	MemberIDs []uint64
}

// GroupEdge is a directed dependency between two groups, weighted by the number
// of distinct package import pairs that cross from From to To. Intra-group
// edges (From == To) are not emitted.
type GroupEdge struct {
	From, To GroupKey
	Weight   int
}

// GroupGraph is the quotient of the package import graph under a grouping: one
// node per group, one edge per ordered group pair some member import crosses.
// Unlike the package closure (thousands of nodes), the quotient is small (tens),
// so the architecture view draws it whole — no focus or cap needed. It is
// derived, never serialized; rebuild it from an Index.
type GroupGraph struct {
	Nodes []GroupNode
	Edges []GroupEdge
	byKey map[GroupKey]int
}

// Node returns the group with the given key, if present.
func (gg *GroupGraph) Node(key GroupKey) (n *GroupNode, ok bool) {
	i, ok := gg.byKey[key]
	if !ok {
		return nil, false
	}
	return &gg.Nodes[i], true
}

// BuildGroupGraph folds the whole package graph into groups per opts and returns
// the quotient. include, when non-nil, drops any package for which it returns
// false (and every edge touching it) — e.g. to keep stdlib or external modules
// out of the architecture view. rootModule is the collection's root module path.
func (idx *Index) BuildGroupGraph(rootModule string, opts GroupingOpts, include func(p *PackageNode) bool) (gg *GroupGraph) {
	gg = &GroupGraph{byKey: make(map[GroupKey]int)}
	groupOf := make(map[uint64]GroupKey, len(idx.byID))
	classVotes := make(map[GroupKey]map[string]int)

	ensure := func(key GroupKey) (i int) {
		if i, ok := gg.byKey[key]; ok {
			return i
		}
		i = len(gg.Nodes)
		gg.byKey[key] = i
		gg.Nodes = append(gg.Nodes, GroupNode{Key: key})
		return i
	}

	for id, p := range idx.byID {
		if include != nil && !include(p) {
			continue
		}
		key := GroupOf(p, rootModule, opts)
		groupOf[id] = key
		i := ensure(key)
		gg.Nodes[i].NumPkgs++
		gg.Nodes[i].MemberIDs = append(gg.Nodes[i].MemberIDs, id)
		if classVotes[key] == nil {
			classVotes[key] = make(map[string]int, 3)
		}
		classVotes[key][p.Class]++
	}

	type pair struct{ from, to GroupKey }
	weights := make(map[pair]int)
	for id, p := range idx.byID {
		fg, ok := groupOf[id]
		if !ok {
			continue
		}
		for _, to := range p.Imports {
			tg, ok := groupOf[to]
			if !ok || tg == fg {
				continue
			}
			weights[pair{fg, tg}]++
		}
	}
	for pr, w := range weights {
		gg.Edges = append(gg.Edges, GroupEdge{From: pr.from, To: pr.to, Weight: w})
	}

	for key, votes := range classVotes {
		gg.Nodes[gg.byKey[key]].Class = dominantClass(votes)
	}

	// Deterministic order; rebuild byKey after the node sort moves indices.
	sort.Slice(gg.Nodes, func(i, j int) bool { return gg.Nodes[i].Key < gg.Nodes[j].Key })
	gg.byKey = make(map[GroupKey]int, len(gg.Nodes))
	for i := range gg.Nodes {
		gg.byKey[gg.Nodes[i].Key] = i
		slices.Sort(gg.Nodes[i].MemberIDs)
	}
	sort.Slice(gg.Edges, func(i, j int) bool {
		if gg.Edges[i].From != gg.Edges[j].From {
			return gg.Edges[i].From < gg.Edges[j].From
		}
		return gg.Edges[i].To < gg.Edges[j].To
	})
	return gg
}

// dominantClass picks a group's representative class deterministically: the
// most-voted class, ties broken in a fixed precedence so map iteration order
// never leaks into the result.
func dominantClass(votes map[string]int) (class string) {
	best := -1
	for _, cl := range [...]string{ClassInternal, ClassExternal, ClassStdlib} {
		if n := votes[cl]; n > best {
			best, class = n, cl
		}
	}
	if best <= 0 {
		// Only non-standard classes present (e.g. ""): fall back to any vote.
		for cl, n := range votes {
			if n > best {
				best, class = n, cl
			}
		}
	}
	return class
}

// SiblingViolation is one forbidden import edge between two distinct siblings
// directly under a guarded prefix (e.g. two different apps/<name> trees). It is
// the package-level evidence behind a reddened architecture edge — From/To name
// the offending sibling groups and FromPkg/ToPkg the concrete packages, so the
// UI can list and click through to the exact import to remove.
type SiblingViolation struct {
	FromGroup GroupKey
	ToGroup   GroupKey
	FromPkg   uint64
	ToPkg     uint64
}

// SiblingViolations reports first-party import edges crossing between two
// distinct siblings directly under prefix (relative to rootModule). With prefix
// "apps/" this is the keelson rule "apps must not depend on each other": each
// returned edge is one app's package importing another app's package.
//
// The check keys on the immediate child of prefix, so it is independent of the
// architecture view's grouping depth — sliding the view coarser never hides a
// real violation from this list (it only stops colouring the collapsed edge).
func (idx *Index) SiblingViolations(rootModule string, prefix string) (out []SiblingViolation) {
	siblingOf := func(p *PackageNode) (key GroupKey, ok bool) {
		if p.Class != ClassInternal {
			return "", false
		}
		rel := moduleRelative(p.ImportPath, rootModule)
		if !strings.HasPrefix(rel, prefix) {
			return "", false
		}
		name := rel[len(prefix):]
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[:i]
		}
		if name == "" {
			return "", false
		}
		return GroupKey(prefix + name), true
	}
	for id, p := range idx.byID {
		fg, ok := siblingOf(p)
		if !ok {
			continue
		}
		for _, to := range p.Imports {
			tp, ok := idx.byID[to]
			if !ok {
				continue
			}
			tg, ok := siblingOf(tp)
			if !ok || tg == fg {
				continue
			}
			out = append(out, SiblingViolation{FromGroup: fg, ToGroup: tg, FromPkg: id, ToPkg: to})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.FromGroup != b.FromGroup:
			return a.FromGroup < b.FromGroup
		case a.ToGroup != b.ToGroup:
			return a.ToGroup < b.ToGroup
		case a.FromPkg != b.FromPkg:
			return a.FromPkg < b.FromPkg
		default:
			return a.ToPkg < b.ToPkg
		}
	})
	return out
}
