//go:build llm_generated_opus47

// Package patch provides patch construction, application, and dependency
// tracking for the pushout revision control system.
package patch

import (
	"encoding/json"
	"fmt"

	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

// ChangeKind identifies the type of atomic operation in a patch.
type ChangeKind uint8

const (
	ChangeNewNode    ChangeKind = iota // Add a new node with content
	ChangeDeleteNode                   // Tombstone an existing node
	ChangeNewEdge                      // Add an ordering edge between existing nodes
)

// Change is a single atomic operation in a patch.
type Change struct {
	Kind ChangeKind

	// For NewNode:
	NodeID      t.NodeID   // ID of the new node
	Content     []byte     // Content of the new node
	UpContext   []t.NodeID // Nodes that should precede this one
	DownContext []t.NodeID // Nodes that should follow this one

	// For DeleteNode:
	// NodeID is reused

	// For NewEdge:
	Src  t.NodeID // Edge source
	Dest t.NodeID // Edge destination
}

// Patch is a set of changes with metadata and dependency tracking.
// A patch's identity is the SHA-256 hash of its serialized changes.
type Patch struct {
	Hash         t.PatchHash
	Author       string
	Description  string
	Dependencies []t.PatchHash // Patches that must be applied before this one
	Changes      []Change
}

// VENDOR DEVIATION: ComputeHash now de-fixes-up self-references before
// hashing so it is idempotent across NewPatch's pre-/post-fixup states.
// The envelope codec relies on Decode(env).Patch.ComputeHash() reproducing
// the stored Hash; without this normalization that check would always fail
// for any patch that introduces nodes (the NodeIDs would carry p.Hash
// post-fixup but PlaceholderHash pre-fixup).

// ComputeHash computes the patch hash from its changes.
// Dependencies, author, and description are NOT part of the hash — only
// the actual graph operations matter for identity.
//
// Idempotence: NewPatch first hashes the changes with PlaceholderHash
// self-references, then rewrites those placeholders to the resulting
// patch hash. ComputeHash undoes that rewrite before marshaling so
// repeated calls return the same value, regardless of fixup state.
//
// json.Marshal on []Change cannot fail in practice (all fields are types
// that the encoder supports), so a marshal error indicates a programmer
// error in extending Change — panic rather than produce a silently bogus
// hash that breaks patch identity downstream.
func (p *Patch) ComputeHash() t.PatchHash {
	data, err := json.Marshal(p.changesForHash())
	if err != nil {
		panic(fmt.Errorf("patch.ComputeHash: marshal changes: %w", err))
	}
	return t.HashBytes(data)
}

// changesForHash returns p.Changes with any post-fixup self-references
// (NodeID.Patch == p.Hash) rewritten back to PlaceholderHash. When p.Hash
// is zero (the first call from NewPatch, before fixup), the changes are
// returned unmodified.
func (p *Patch) changesForHash() []Change {
	if p.Hash.IsZero() {
		return p.Changes
	}
	out := make([]Change, len(p.Changes))
	defixup := func(id t.NodeID) t.NodeID {
		if id.Patch == p.Hash {
			id.Patch = t.PlaceholderHash
		}
		return id
	}
	for i, c := range p.Changes {
		out[i] = c
		out[i].NodeID = defixup(c.NodeID)
		if len(c.UpContext) > 0 {
			up := make([]t.NodeID, len(c.UpContext))
			for j, n := range c.UpContext {
				up[j] = defixup(n)
			}
			out[i].UpContext = up
		}
		if len(c.DownContext) > 0 {
			down := make([]t.NodeID, len(c.DownContext))
			for j, n := range c.DownContext {
				down[j] = defixup(n)
			}
			out[i].DownContext = down
		}
		out[i].Src = defixup(c.Src)
		out[i].Dest = defixup(c.Dest)
	}
	return out
}

// NewPatch creates a patch from a list of changes, computing its hash.
func NewPatch(author, description string, deps []t.PatchHash, changes []Change) *Patch {
	p := &Patch{
		Author:       author,
		Description:  description,
		Dependencies: deps,
		Changes:      changes,
	}
	p.Hash = p.ComputeHash()

	// Fix up NodeIDs: replace placeholder hashes (0xFF...) with the real hash.
	for i := range p.Changes {
		c := &p.Changes[i]
		if c.Kind == ChangeNewNode && c.NodeID.Patch.IsPlaceholder() {
			c.NodeID.Patch = p.Hash
		}
		for j := range c.UpContext {
			if c.UpContext[j].Patch.IsPlaceholder() {
				c.UpContext[j].Patch = p.Hash
			}
		}
		for j := range c.DownContext {
			if c.DownContext[j].Patch.IsPlaceholder() {
				c.DownContext[j].Patch = p.Hash
			}
		}
		if c.Kind == ChangeNewEdge {
			if c.Src.Patch.IsPlaceholder() {
				c.Src.Patch = p.Hash
			}
			if c.Dest.Patch.IsPlaceholder() {
				c.Dest.Patch = p.Hash
			}
		}
	}

	// Do NOT recompute hash after fixup. The hash is derived from the
	// original changes with placeholders, just like ojo/pijul. This ensures
	// the patch identity is stable and the fixed-up NodeIDs correctly
	// reference this patch's hash.
	return p
}

// Apply applies the patch to a graph store.
// Changes are applied in order: NewNode and NewEdge first, then DeleteNode.
// After applying, ResolvePseudoEdges is called.
func (p *Patch) Apply(g t.GraphStore) error {
	// Pass 1: non-deletion changes.
	for _, c := range p.Changes {
		switch c.Kind {
		case ChangeNewNode:
			if err := g.AddNode(c.NodeID, c.Content, p.Hash, c.UpContext, c.DownContext); err != nil {
				return fmt.Errorf("apply NewNode %v: %w", c.NodeID, err)
			}
		case ChangeNewEdge:
			if err := g.AddEdge(c.Src, c.Dest, p.Hash); err != nil {
				return fmt.Errorf("apply NewEdge %v->%v: %w", c.Src, c.Dest, err)
			}
		}
	}
	// Pass 2: deletions (after additions to handle ordering).
	for _, c := range p.Changes {
		if c.Kind == ChangeDeleteNode {
			if err := g.DeleteNode(c.NodeID); err != nil {
				return fmt.Errorf("apply DeleteNode %v: %w", c.NodeID, err)
			}
		}
	}
	g.ResolvePseudoEdges()
	return nil
}

// Unapply reverses the patch. Changes are reversed in reverse order:
// first undelete, then remove edges, then remove nodes.
//
// Returns an error if any node introduced by this patch still has incident
// edges that were introduced by a different patch — removing the node would
// leave those edges dangling. Callers must unapply dependents first.
func (p *Patch) Unapply(g t.GraphStore) error {
	// Pre-flight: every node we are about to remove must have no incident
	// edges from other patches.
	for _, c := range p.Changes {
		if c.Kind != ChangeNewNode {
			continue
		}
		if err := assertNoForeignEdges(g, c.NodeID, p.Hash); err != nil {
			return fmt.Errorf("unapply %s: %w", p.Hash, err)
		}
	}

	// Pass 1: undelete nodes.
	for i := len(p.Changes) - 1; i >= 0; i-- {
		c := p.Changes[i]
		if c.Kind == ChangeDeleteNode {
			if err := g.UndeleteNode(c.NodeID); err != nil {
				return fmt.Errorf("unapply DeleteNode %v: %w", c.NodeID, err)
			}
		}
	}
	// Pass 2: remove edges then nodes.
	for i := len(p.Changes) - 1; i >= 0; i-- {
		c := p.Changes[i]
		switch c.Kind {
		case ChangeNewEdge:
			kind := t.EdgeLive
			if g.IsDeleted(c.Src) || g.IsDeleted(c.Dest) {
				kind = t.EdgeDeleted
			}
			g.RemoveEdge(c.Src, c.Dest, kind, p.Hash)
		case ChangeNewNode:
			// Remove edges added during AddNode.
			for _, up := range c.UpContext {
				kind := t.EdgeLive
				if g.IsDeleted(up) || g.IsDeleted(c.NodeID) {
					kind = t.EdgeDeleted
				}
				g.RemoveEdge(up, c.NodeID, kind, p.Hash)
			}
			for _, down := range c.DownContext {
				kind := t.EdgeLive
				if g.IsDeleted(c.NodeID) || g.IsDeleted(down) {
					kind = t.EdgeDeleted
				}
				g.RemoveEdge(c.NodeID, down, kind, p.Hash)
			}
			// Remove the node itself.
			g.RemoveNode(c.NodeID)
		}
	}
	g.ResolvePseudoEdges()
	return nil
}

// assertNoForeignEdges errors if id has any incident edge whose IntroducedBy
// is not own. Such an edge belongs to a dependent patch and would dangle if
// we removed id.
func assertNoForeignEdges(g t.GraphReader, id t.NodeID, own t.PatchHash) error {
	for e := range g.ForwardEdges(id) {
		if e.Kind == t.EdgePseudo {
			continue // pseudo-edges are derived, not authored
		}
		if e.IntroducedBy != own {
			return fmt.Errorf("node %v has foreign forward edge from patch %s; unapply dependents first", id, e.IntroducedBy)
		}
	}
	for e := range g.BackwardEdges(id) {
		if e.Kind == t.EdgePseudo {
			continue
		}
		if e.IntroducedBy != own {
			return fmt.Errorf("node %v has foreign back edge from patch %s; unapply dependents first", id, e.IntroducedBy)
		}
	}
	return nil
}

// ComputeDependencies computes the minimal set of patches that a set of
// changes depends on. A patch depends on any patch that introduced a node
// referenced by a DeleteNode or NewEdge change (or in up/down context).
//
// The zero hash (root/genesis) and the placeholder hash (self-references in
// pre-fixup changes) are intentionally excluded — neither identifies a
// real patch the changes could depend on.
func ComputeDependencies(changes []Change) []t.PatchHash {
	seen := make(map[t.PatchHash]struct{})
	var deps []t.PatchHash
	add := func(h t.PatchHash) {
		if h.IsZero() || h.IsPlaceholder() {
			return
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		deps = append(deps, h)
	}
	for _, c := range changes {
		switch c.Kind {
		case ChangeNewNode:
			for _, ctx := range c.UpContext {
				add(ctx.Patch)
			}
			for _, ctx := range c.DownContext {
				add(ctx.Patch)
			}
		case ChangeDeleteNode:
			add(c.NodeID.Patch)
		case ChangeNewEdge:
			add(c.Src.Patch)
			add(c.Dest.Patch)
		}
	}
	return deps
}