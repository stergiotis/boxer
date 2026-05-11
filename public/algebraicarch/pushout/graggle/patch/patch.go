//go:build llm_generated_opus47

// Package patch provides patch construction, application, and dependency
// tracking for the pushout revision control system.
package patch

import (
	"encoding/json"
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// ChangeKindE identifies the type of atomic operation in a patch.
type ChangeKindE uint8

const (
	ChangeKindNewNode    ChangeKindE = iota // Add a new node with content
	ChangeKindDeleteNode                    // Tombstone an existing node
	ChangeKindNewEdge                       // Add an ordering edge between existing nodes
)

// Change is a single atomic operation in a patch.
type Change struct {
	Kind ChangeKindE

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

// ComputeHash now de-fixes-up self-references before
// hashing so it is idempotent across NewPatch's pre-/post-fixup states.
// The envelope codec relies on Decode(env).Patch.ComputeHash() reproducing
// the stored Hash; without this normalization that check would always fail
// for any patch that introduces nodes (the NodeIDs would carry inst.Hash
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
func (inst *Patch) ComputeHash() (h t.PatchHash) {
	data, err := json.Marshal(inst.changesForHash())
	if err != nil {
		panic(fmt.Errorf("patch.ComputeHash: marshal changes: %w", err))
	}
	h = t.HashBytes(data)
	return
}

// changesForHash returns inst.Changes with any post-fixup self-references
// (NodeID.Patch == inst.Hash) rewritten back to PlaceholderHash. When
// inst.Hash is zero (the first call from NewPatch, before fixup), the
// changes are returned unmodified.
func (inst *Patch) changesForHash() (out []Change) {
	if inst.Hash.IsZero() {
		out = inst.Changes
		return
	}
	out = make([]Change, len(inst.Changes))
	defixup := func(id t.NodeID) (rewritten t.NodeID) {
		rewritten = id
		if id.Patch == inst.Hash {
			rewritten.Patch = t.PlaceholderHash
		}
		return
	}
	for i, c := range inst.Changes {
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
	return
}

// NewPatch creates a patch from a list of changes, computing its hash.
func NewPatch(author string, description string, deps []t.PatchHash, changes []Change) (inst *Patch) {
	inst = &Patch{
		Author:       author,
		Description:  description,
		Dependencies: deps,
		Changes:      changes,
	}
	inst.Hash = inst.ComputeHash()

	// Fix up NodeIDs: replace placeholder hashes (0xFF...) with the real hash.
	for i := range inst.Changes {
		c := &inst.Changes[i]
		if c.Kind == ChangeKindNewNode && c.NodeID.Patch.IsPlaceholder() {
			c.NodeID.Patch = inst.Hash
		}
		for j := range c.UpContext {
			if c.UpContext[j].Patch.IsPlaceholder() {
				c.UpContext[j].Patch = inst.Hash
			}
		}
		for j := range c.DownContext {
			if c.DownContext[j].Patch.IsPlaceholder() {
				c.DownContext[j].Patch = inst.Hash
			}
		}
		if c.Kind == ChangeKindNewEdge {
			if c.Src.Patch.IsPlaceholder() {
				c.Src.Patch = inst.Hash
			}
			if c.Dest.Patch.IsPlaceholder() {
				c.Dest.Patch = inst.Hash
			}
		}
	}

	// Do NOT recompute hash after fixup. The hash is derived from the
	// original changes with placeholders, just like ojo/pijul. This ensures
	// the patch identity is stable and the fixed-up NodeIDs correctly
	// reference this patch's hash.
	return
}

// Apply applies the patch to a graph store.
// Changes are applied in order: NewNode and NewEdge first, then DeleteNode.
// After applying, ResolvePseudoEdges is called.
func (inst *Patch) Apply(g t.GraphStoreI) (err error) {
	// Pass 1: non-deletion changes.
	for _, c := range inst.Changes {
		switch c.Kind {
		case ChangeKindNewNode:
			err = g.AddNode(c.NodeID, c.Content, inst.Hash, c.UpContext, c.DownContext)
			if err != nil {
				err = eh.Errorf("apply NewNode %v: %w", c.NodeID, err)
				return
			}
		case ChangeKindNewEdge:
			err = g.AddEdge(c.Src, c.Dest, inst.Hash)
			if err != nil {
				err = eh.Errorf("apply NewEdge %v->%v: %w", c.Src, c.Dest, err)
				return
			}
		}
	}
	// Pass 2: deletions (after additions to handle ordering).
	for _, c := range inst.Changes {
		if c.Kind != ChangeKindDeleteNode {
			continue
		}
		err = g.DeleteNode(c.NodeID)
		if err != nil {
			err = eh.Errorf("apply DeleteNode %v: %w", c.NodeID, err)
			return
		}
	}
	g.ResolvePseudoEdges()
	return
}

// Unapply reverses the patch. Changes are reversed in reverse order:
// first undelete, then remove edges, then remove nodes.
//
// Returns an error if any node introduced by this patch still has
// incident edges that were introduced by a different patch — removing
// the node would leave those edges dangling. Callers must unapply
// dependents first.
//
// Also returns an error if any node this patch tombstoned has had its
// content purged by SweepTombstones (or, in future, Forget): the
// resurrection would produce a node with no recoverable bytes, which
// breaks the system's content guarantees. Past the retention horizon,
// the patch is effectively permanent.
func (inst *Patch) Unapply(g t.GraphStoreI) (err error) {
	// Pre-flight: every node we are about to remove must have no incident
	// edges from other patches.
	for _, c := range inst.Changes {
		if c.Kind != ChangeKindNewNode {
			continue
		}
		err = assertNoForeignEdges(g, c.NodeID, inst.Hash)
		if err != nil {
			err = eh.Errorf("unapply %s: %w", inst.Hash, err)
			return
		}
	}
	// Pre-flight: every node we are about to undelete must still have
	// reconstructible content. If the sweep dropped it, the patch can no
	// longer be unapplied; surface a clear error rather than producing
	// an empty-content node.
	for _, c := range inst.Changes {
		if c.Kind != ChangeKindDeleteNode {
			continue
		}
		if g.NodeContentStatus(c.NodeID) == t.NodeContentStatusPurged {
			err = eh.Errorf("unapply %s: node %v has been swept (content purged past retention horizon); patch is permanent past retention", inst.Hash, c.NodeID)
			return
		}
	}

	// Pass 1: undelete nodes.
	for i := len(inst.Changes) - 1; i >= 0; i-- {
		c := inst.Changes[i]
		if c.Kind != ChangeKindDeleteNode {
			continue
		}
		err = g.UndeleteNode(c.NodeID)
		if err != nil {
			err = eh.Errorf("unapply DeleteNode %v: %w", c.NodeID, err)
			return
		}
	}
	// Pass 2: remove edges then nodes.
	for i := len(inst.Changes) - 1; i >= 0; i-- {
		c := inst.Changes[i]
		switch c.Kind {
		case ChangeKindNewEdge:
			kind := t.EdgeKindLive
			if g.IsDeleted(c.Src) || g.IsDeleted(c.Dest) {
				kind = t.EdgeKindDeleted
			}
			g.RemoveEdge(c.Src, c.Dest, kind, inst.Hash)
		case ChangeKindNewNode:
			// Remove edges added during AddNode.
			for _, up := range c.UpContext {
				kind := t.EdgeKindLive
				if g.IsDeleted(up) || g.IsDeleted(c.NodeID) {
					kind = t.EdgeKindDeleted
				}
				g.RemoveEdge(up, c.NodeID, kind, inst.Hash)
			}
			for _, down := range c.DownContext {
				kind := t.EdgeKindLive
				if g.IsDeleted(c.NodeID) || g.IsDeleted(down) {
					kind = t.EdgeKindDeleted
				}
				g.RemoveEdge(c.NodeID, down, kind, inst.Hash)
			}
			// Remove the node itself.
			g.RemoveNode(c.NodeID)
		}
	}
	g.ResolvePseudoEdges()
	return
}

// assertNoForeignEdges errors if id has any incident edge whose IntroducedBy
// is not own. Such an edge belongs to a dependent patch and would dangle if
// we removed id.
func assertNoForeignEdges(g t.GraphReaderI, id t.NodeID, own t.PatchHash) (err error) {
	for e := range g.ForwardEdges(id) {
		if e.Kind == t.EdgeKindPseudo {
			continue // pseudo-edges are derived, not authored
		}
		if e.IntroducedBy != own {
			err = eh.Errorf("node %v has foreign forward edge from patch %s; unapply dependents first", id, e.IntroducedBy)
			return
		}
	}
	for e := range g.BackwardEdges(id) {
		if e.Kind == t.EdgeKindPseudo {
			continue
		}
		if e.IntroducedBy != own {
			err = eh.Errorf("node %v has foreign back edge from patch %s; unapply dependents first", id, e.IntroducedBy)
			return
		}
	}
	return
}

// ComputeDependencies computes the minimal set of patches that a set of
// changes depends on. A patch depends on any patch that introduced a node
// referenced by a DeleteNode or NewEdge change (or in up/down context).
//
// The zero hash (root/genesis) and the placeholder hash (self-references in
// pre-fixup changes) are intentionally excluded — neither identifies a
// real patch the changes could depend on.
func ComputeDependencies(changes []Change) (deps []t.PatchHash) {
	seen := make(map[t.PatchHash]struct{})
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
		case ChangeKindNewNode:
			for _, ctx := range c.UpContext {
				add(ctx.Patch)
			}
			for _, ctx := range c.DownContext {
				add(ctx.Patch)
			}
		case ChangeKindDeleteNode:
			add(c.NodeID.Patch)
		case ChangeKindNewEdge:
			add(c.Src.Patch)
			add(c.Dest.Patch)
		}
	}
	return
}
