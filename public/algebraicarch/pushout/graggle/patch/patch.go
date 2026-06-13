// Package patch provides patch construction, application, and dependency
// tracking for the pushout revision control system.
package patch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

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
// A patch's identity is the BLAKE3 hash of its canonicalized dependency
// set plus its serialized changes (see ComputeHash).
type Patch struct {
	Hash         t.PatchHash
	Author       string
	Description  string
	Dependencies []t.PatchHash // Patches that must be applied before this one
	Changes      []Change
}

// ComputeHash computes the patch hash from its dependencies and changes.
//
// The hashed payload is {canonicalized Dependencies, Changes}: the
// dependency set is part of patch identity, so an envelope whose
// dependency list was stripped or extended no longer validates against
// the stored hash. The list is canonicalized (sorted, deduplicated)
// before hashing — dependencies are semantically a set, and identity
// must not depend on declaration order. Author and description stay
// OUTSIDE the hash: they are provenance, carried at the envelope level,
// and two actors independently recording the same edit against the same
// state still converge on the same patch.
//
// Idempotence: NewPatch first hashes the changes with PlaceholderHash
// self-references, then rewrites those placeholders to the resulting
// patch hash. ComputeHash undoes that rewrite (changesForHash) before
// marshaling so repeated calls return the same value, regardless of
// fixup state.
//
// json.Marshal on the payload cannot fail in practice (all fields are
// types the encoder supports), so a marshal error indicates a programmer
// error in extending Change — panic rather than produce a silently bogus
// hash that breaks patch identity downstream.
func (inst *Patch) ComputeHash() (h t.PatchHash) {
	payload := struct {
		Dependencies []t.PatchHash
		Changes      []Change
	}{
		Dependencies: canonicalDeps(inst.Dependencies),
		Changes:      inst.changesForHash(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Errorf("patch.ComputeHash: marshal payload: %w", err))
	}
	h = t.HashBytes(data)
	return
}

// canonicalDeps returns a sorted, deduplicated copy of deps.
func canonicalDeps(deps []t.PatchHash) (out []t.PatchHash) {
	out = slices.Clone(deps)
	slices.SortFunc(out, func(a, b t.PatchHash) int { return bytes.Compare(a[:], b[:]) })
	out = slices.Compact(out)
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
//
// The changes (and their context slices) are deep-copied before the
// placeholder fixup below rewrites NodeIDs: callers keep ownership of
// their slice and can reuse it — building a second patch from the same
// changes must see the original placeholders, not this patch's hash.
// Content byte slices are shared and treated as immutable.
func NewPatch(author string, description string, deps []t.PatchHash, changes []Change) (inst *Patch) {
	owned := make([]Change, len(changes))
	for i, c := range changes {
		owned[i] = c
		owned[i].UpContext = slices.Clone(c.UpContext)
		owned[i].DownContext = slices.Clone(c.DownContext)
	}
	inst = &Patch{
		Author:       author,
		Description:  description,
		Dependencies: canonicalDeps(deps),
		Changes:      owned,
	}
	inst.Hash = inst.ComputeHash()

	// Fix up NodeIDs: replace placeholder hashes (0xFF...) with the real
	// hash — for every change kind, mirroring changesForHash's
	// unconditional de-fixup (a DeleteNode aimed at a node introduced by
	// this same patch must resolve too).
	for i := range inst.Changes {
		c := &inst.Changes[i]
		if c.NodeID.Patch.IsPlaceholder() {
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
		if c.Src.Patch.IsPlaceholder() {
			c.Src.Patch = inst.Hash
		}
		if c.Dest.Patch.IsPlaceholder() {
			c.Dest.Patch = inst.Hash
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
//
// Apply is all-or-nothing with respect to the failure modes it can
// detect: every change is validated against the store (pass 0) before
// the first mutation, so a patch with a missing context or dangling
// delete target leaves the store untouched instead of half-applied.
func (inst *Patch) Apply(g t.GraphStoreI) (err error) {
	err = inst.validateAgainst(g)
	if err != nil {
		err = eh.Errorf("apply %s: %w", inst.Hash, err)
		return
	}
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
		err = g.DeleteNode(c.NodeID, inst.Hash)
		if err != nil {
			err = eh.Errorf("apply DeleteNode %v: %w", c.NodeID, err)
			return
		}
	}
	g.ResolvePseudoEdges()
	return
}

// validateAgainst checks every change against the current graph state
// before any mutation, accounting for nodes this patch itself introduces
// earlier in the change list. Error wording mirrors the store's own
// messages so callers match on the same strings either way.
func (inst *Patch) validateAgainst(g t.GraphReaderI) (err error) {
	willExist := make(map[t.NodeID]struct{})
	exists := func(id t.NodeID) bool {
		if _, ok := willExist[id]; ok {
			return true
		}
		return g.HasNode(id)
	}
	for _, c := range inst.Changes {
		switch c.Kind {
		case ChangeKindNewNode:
			if exists(c.NodeID) {
				err = eh.Errorf("node %v: node already exists", c.NodeID)
				return
			}
			for _, up := range c.UpContext {
				if !exists(up) {
					err = eh.Errorf("up-context node %v does not exist", up)
					return
				}
			}
			for _, down := range c.DownContext {
				if !exists(down) {
					err = eh.Errorf("down-context node %v does not exist", down)
					return
				}
			}
			willExist[c.NodeID] = struct{}{}
		case ChangeKindNewEdge:
			if !exists(c.Src) {
				err = eh.Errorf("source node %v does not exist", c.Src)
				return
			}
			if !exists(c.Dest) {
				err = eh.Errorf("dest node %v does not exist", c.Dest)
				return
			}
		case ChangeKindDeleteNode:
			if c.NodeID == t.RootNodeID {
				err = eh.Errorf("cannot delete root node")
				return
			}
			if !exists(c.NodeID) {
				err = eh.Errorf("node %v does not exist", c.NodeID)
				return
			}
		}
	}
	return
}

// Unapply reverses the patch. Changes are reversed in reverse order:
// first undelete, then remove edges, then remove nodes.
//
// Returns an error if any node introduced by this patch still has
// incident edges that were introduced by a different patch — removing
// the node would leave those edges dangling — or is tombstoned by a
// still-applied patch (a delete-only dependent introduces no edges, so
// the edge check alone cannot see it). Callers must unapply dependents
// first.
//
// Also returns an error if any node this patch tombstoned — and would
// actually resurrect, being its last deleter — has had its content
// purged by SweepTombstones (or, in future, Forget): the resurrection
// would produce a node with no recoverable bytes, which breaks the
// system's content guarantees. Past the retention horizon, the patch is
// effectively permanent.
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

	// deleterCount reports how many patches currently hold a node
	// tombstoned, when the store can tell us (the concrete Graggle can).
	deleterCount := func(id t.NodeID) (n int, known bool) {
		if dc, ok := g.(interface{ NodeDeleterCount(id t.NodeID) int }); ok {
			n, known = dc.NodeDeleterCount(id), true
		}
		return
	}
	ownDeletes := make(map[t.NodeID]struct{})
	for _, c := range inst.Changes {
		if c.Kind == ChangeKindDeleteNode {
			ownDeletes[c.NodeID] = struct{}{}
		}
	}

	// Pre-flight: every node we are about to remove must not be tombstoned
	// by another still-applied patch. A DeleteNode-only dependent
	// introduces no edges, so the foreign-edge check above cannot see it —
	// and RemoveNode on a tombstone would rip the node out from under the
	// patch that deleted it.
	for _, c := range inst.Changes {
		if c.Kind != ChangeKindNewNode || !g.IsDeleted(c.NodeID) {
			continue
		}
		if _, own := ownDeletes[c.NodeID]; !own {
			err = eh.Errorf("unapply %s: node %v is tombstoned by another still-applied patch: %w", inst.Hash, c.NodeID, ErrHasDependents)
			return
		}
		if n, known := deleterCount(c.NodeID); known && n > 1 {
			err = eh.Errorf("unapply %s: node %v is tombstoned by %d patches: %w", inst.Hash, c.NodeID, n, ErrHasDependents)
			return
		}
	}

	// Pre-flight: every node whose undeletion would actually resurrect it
	// (we are its last deleter) must still have reconstructible content.
	// If the sweep dropped it, the patch can no longer be unapplied;
	// surface a clear error rather than producing an empty-content node.
	// While other deleters remain, no resurrection happens and the purge
	// is irrelevant to this unapply.
	for _, c := range inst.Changes {
		if c.Kind != ChangeKindDeleteNode {
			continue
		}
		if g.NodeContentStatus(c.NodeID) != t.NodeContentStatusPurged {
			continue
		}
		if n, known := deleterCount(c.NodeID); known && n > 1 {
			continue
		}
		err = eh.Errorf("unapply %s: node %v has been swept: %w", inst.Hash, c.NodeID, ErrRetentionPermanent)
		return
	}

	// Pass 1: undelete nodes.
	for i := len(inst.Changes) - 1; i >= 0; i-- {
		c := inst.Changes[i]
		if c.Kind != ChangeKindDeleteNode {
			continue
		}
		err = g.UndeleteNode(c.NodeID, inst.Hash)
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
			err = eh.Errorf("node %v has foreign forward edge from patch %s: %w", id, e.IntroducedBy, ErrHasDependents)
			return
		}
	}
	for e := range g.BackwardEdges(id) {
		if e.Kind == t.EdgeKindPseudo {
			continue
		}
		if e.IntroducedBy != own {
			err = eh.Errorf("node %v has foreign back edge from patch %s: %w", id, e.IntroducedBy, ErrHasDependents)
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
