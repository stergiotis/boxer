package wasmsurvey

import (
	"strings"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
)

// classifier holds the per-target static analysis state: each package's own
// seed verdict (from leafSeed) and the memoized transitive verdict (the worst
// tier over the package and everything it imports).
type classifier struct {
	idx       *godep.Index
	nodes     []godep.PackageNode
	target    TargetID
	seedTier  map[uint64]Tier
	seedKind  map[uint64]ReasonKind
	tier      map[uint64]Tier // memoized transitive verdict
	cleanSink map[uint64]bool // counterfactual: treat as a Green leaf (no out-propagation)
}

// newClassifier seeds every node in the closure with its on-its-own-account
// verdict (leafSeed) — internal packages seed Green and earn their verdict
// purely through propagation. cleanPrefixes models a counterfactual: any
// package whose import path matches a prefix is treated as if it were
// wasm-clean — a Green sink whose own imports no longer count against anything
// downstream (answers "what would go green/yellow if X were fixed?").
func newClassifier(tc targetClosure, cleanPrefixes []string) (c *classifier) {
	c = &classifier{
		idx:       tc.index,
		nodes:     tc.manifest.Packages,
		target:    tc.target,
		seedTier:  make(map[uint64]Tier, len(tc.manifest.Packages)),
		seedKind:  make(map[uint64]ReasonKind, len(tc.manifest.Packages)),
		tier:      make(map[uint64]Tier, len(tc.manifest.Packages)),
		cleanSink: make(map[uint64]bool),
	}
	for i := range tc.manifest.Packages {
		p := &tc.manifest.Packages[i]
		if matchesAnyPrefix(p.ImportPath, cleanPrefixes) {
			c.cleanSink[p.Id] = true
		}
		t, k := leafSeed(p.ImportPath, p.Class, tc.target)
		c.seedTier[p.Id] = t
		c.seedKind[p.Id] = k
	}
	return
}

// matchesAnyPrefix reports whether s is prefixed by any non-empty entry.
func matchesAnyPrefix(s string, prefixes []string) (b bool) {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// resolve computes the transitive verdict of id: the worst of its own seed
// and the verdicts of everything it imports. Results are memoized. The import
// graph is acyclic (Go forbids import cycles; godep collects only production
// edges — ADR-0064 SD10), but an explicit on-stack guard keeps a malformed
// graph from recursing forever.
func (c *classifier) resolve(id uint64, onStack map[uint64]bool) (t Tier) {
	if v, ok := c.tier[id]; ok {
		return v
	}
	if c.cleanSink[id] {
		c.tier[id] = TierGreen // counterfactual: assumed fixed, contributes nothing
		return TierGreen
	}
	if onStack[id] {
		return c.seedTier[id] // cycle: fall back to the seed, don't loop
	}
	onStack[id] = true
	t = c.seedTier[id]
	if node, ok := c.idx.Node(id); ok {
		for _, imp := range node.Imports {
			t = worstTier(t, c.resolve(imp, onStack))
		}
	}
	delete(onStack, id)
	c.tier[id] = t
	return
}

// resolveAll memoizes the transitive verdict for every node.
func (c *classifier) resolveAll() {
	onStack := make(map[uint64]bool, 16)
	for i := range c.nodes {
		c.resolve(c.nodes[i].Id, onStack)
	}
}

// blame explains a non-Green verdict: a breadth-first walk from root over
// import edges, recording the nearest seed package of the worst tier for each
// distinct reason kind. BFS order makes every recorded Path a shortest import
// path root→…→leaf. Returns nil for a Green (or unknown) verdict.
func (c *classifier) blame(rootID uint64, worst Tier) (reasons []Reason) {
	if worst == TierGreen || worst == TierUnknown {
		return nil
	}
	const maxReasons = 8
	visited := map[uint64]bool{rootID: true}
	parent := make(map[uint64]uint64, 32)
	queue := []uint64{rootID}
	seen := make(map[ReasonKind]bool, 8)
	for len(queue) > 0 && len(reasons) < maxReasons {
		id := queue[0]
		queue = queue[1:]
		if c.cleanSink[id] {
			continue // assumed fixed: not a blame source, and don't blame through it
		}
		if c.seedTier[id] == worst {
			if k := c.seedKind[id]; !seen[k] {
				seen[k] = true
				leaf := ""
				if node, ok := c.idx.Node(id); ok {
					leaf = node.ImportPath
				}
				reasons = append(reasons, Reason{
					Kind: k,
					Leaf: leaf,
					Path: c.pathTo(parent, rootID, id),
				})
			}
		}
		node, ok := c.idx.Node(id)
		if !ok {
			continue
		}
		for _, imp := range node.Imports {
			if !visited[imp] {
				visited[imp] = true
				parent[imp] = id
				queue = append(queue, imp)
			}
		}
	}
	return
}

// pathTo reconstructs the import-path chain root→…→target from a BFS parent
// map. For target == root it returns the single-element path.
func (c *classifier) pathTo(parent map[uint64]uint64, root uint64, target uint64) (path []string) {
	var ids []uint64
	for id := target; ; {
		ids = append(ids, id)
		if id == root {
			break
		}
		p, ok := parent[id]
		if !ok {
			break
		}
		id = p
	}
	path = make([]string, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		if node, ok := c.idx.Node(ids[i]); ok {
			path = append(path, node.ImportPath)
		}
	}
	return
}

// classifyStatic runs the graph-only triage for one target and returns a
// per-package TargetVerdict keyed by package id. want, when non-nil, restricts
// which packages get a verdict (e.g. internal-only) — the whole closure is
// still resolved so propagation sees every leaf, but blame is computed only
// for wanted packages.
func classifyStatic(tc targetClosure, want func(p *godep.PackageNode) bool, cleanPrefixes []string) (verdicts map[uint64]TargetVerdict) {
	c := newClassifier(tc, cleanPrefixes)
	c.resolveAll()
	verdicts = make(map[uint64]TargetVerdict, len(tc.manifest.Packages))
	for i := range tc.manifest.Packages {
		p := &tc.manifest.Packages[i]
		if want != nil && !want(p) {
			continue
		}
		t := c.tier[p.Id]
		verdicts[p.Id] = TargetVerdict{
			Target:  tc.target,
			Static:  t,
			Reasons: c.blame(p.Id, t),
		}
	}
	return
}
