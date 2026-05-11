//go:build llm_generated_opus47

// Package types defines the core data types and interfaces for the pushout
// revision control system. This is the leaf package in the dependency graph —
// all other graggle sub-packages import it, and it imports nothing internal.
package types

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"lukechampine.com/blake3"
)

// PatchHas pushout uses the hash purely for content-addressed identity; collision
// resistance is the only requirement, and BLAKE3 already provides that
// at the same 32-byte size while matching the hash function used by the
// rest of pebble2impl (leeway/card schema fingerprint, IMAP client).
// Switching changes every patch hash — any persisted envelope files
// from a SHA-256 build will fail Decode's hash-validation guard.
type PatchHash [32]byte

func (inst PatchHash) String() (s string) {
	s = hex.EncodeToString(inst[:8]) // short form for display
	return
}

// MarshalText emits the patch hash as a 64-char lowercase hex string. This
// is what encoding/json uses for fields of this type, including NodeID.Patch.
func (inst PatchHash) MarshalText() (text []byte, err error) {
	text = make([]byte, hex.EncodedLen(len(inst)))
	hex.Encode(text, inst[:])
	return
}

// UnmarshalText parses a 64-char hex string back into a PatchHash.
func (inst *PatchHash) UnmarshalText(text []byte) (err error) {
	if len(text) != hex.EncodedLen(len(inst)) {
		err = eh.Errorf("PatchHash: expected %d hex chars, got %d", hex.EncodedLen(len(inst)), len(text))
		return
	}
	_, derr := hex.Decode(inst[:], text)
	if derr != nil {
		err = eh.Errorf("PatchHash: %w", derr)
	}
	return
}

// IsZero returns true for the genesis/root patch hash.
func (inst PatchHash) IsZero() (b bool) {
	b = inst == PatchHash{}
	return
}

// PlaceholderHash is a sentinel used in patch construction to mean
// "this patch" — distinct from the zero hash (which is the root/genesis).
var PlaceholderHash = func() (h PatchHash) {
	for i := range h {
		h[i] = 0xFF
	}
	return
}()

// IsPlaceholder returns true for the "current patch" placeholder hash.
func (inst PatchHash) IsPlaceholder() (b bool) {
	b = inst == PlaceholderHash
	return
}

// NodeID uniquely identifies a line/node in the graggle.
// It is the combination of the patch that introduced the node and the
// node's index within that patch.
type NodeID struct {
	Patch PatchHash
	Index uint64
}

func (inst NodeID) String() (s string) {
	s = fmt.Sprintf("%s/%d", inst.Patch, inst.Index)
	return
}

// RootNodeID is the sentinel root node present in every graggle.
// All top-level content is ordered after this node.
var RootNodeID = NodeID{}

// EdgeKindE classifies edges in the graggle.
type EdgeKindE uint8

const (
	EdgeKindLive    EdgeKindE = iota // Real ordering edge between live nodes
	EdgeKindDeleted                  // Edge to/from a tombstoned (ghost) node
	EdgeKindPseudo                   // Computed shortcut over deleted regions
)

func (inst EdgeKindE) String() (s string) {
	switch inst {
	case EdgeKindLive:
		s = "live"
	case EdgeKindDeleted:
		s = "deleted"
	case EdgeKindPseudo:
		s = "pseudo"
	default:
		s = "unknown"
	}
	return
}

// Edge represents a directed edge in the graggle.
type Edge struct {
	Dest         NodeID
	Kind         EdgeKindE
	IntroducedBy PatchHash // which patch created this edge
}

// NodeSet is an ordered set of NodeIDs.
type NodeSet struct {
	m map[NodeID]struct{}
}

func NewNodeSet() (inst *NodeSet) {
	inst = &NodeSet{m: make(map[NodeID]struct{})}
	return
}

func (inst *NodeSet) Add(id NodeID) {
	inst.m[id] = struct{}{}
}

func (inst *NodeSet) Remove(id NodeID) {
	delete(inst.m, id)
}

func (inst *NodeSet) Contains(id NodeID) (b bool) {
	_, b = inst.m[id]
	return
}

func (inst *NodeSet) Len() (n int) {
	n = len(inst.m)
	return
}

func (inst *NodeSet) Items() (out []NodeID) {
	out = make([]NodeID, 0, len(inst.m))
	for id := range inst.m {
		out = append(out, id)
	}
	slices.SortFunc(out, CompareNodeID)
	return
}

// MultiMap maps a NodeID to a set of Edges.
type MultiMap struct {
	m map[NodeID][]Edge
}

func NewMultiMap() (inst *MultiMap) {
	inst = &MultiMap{m: make(map[NodeID][]Edge)}
	return
}

func (inst *MultiMap) Add(src NodeID, e Edge) {
	inst.m[src] = append(inst.m[src], e)
}

func (inst *MultiMap) Remove(src NodeID, e Edge) {
	edges := inst.m[src]
	for i, existing := range edges {
		if existing == e {
			inst.m[src] = append(edges[:i], edges[i+1:]...)
			break
		}
	}
	if len(inst.m[src]) == 0 {
		delete(inst.m, src)
	}
}

func (inst *MultiMap) Get(src NodeID) (out []Edge) {
	out = inst.m[src]
	return
}

func (inst *MultiMap) Has(src NodeID, e Edge) (b bool) {
	for _, existing := range inst.m[src] {
		if existing == e {
			b = true
			return
		}
	}
	return
}

// HasEdgeTo checks if there is any edge from src to dest (regardless of kind).
func (inst *MultiMap) HasEdgeTo(src, dest NodeID) (b bool) {
	for _, e := range inst.m[src] {
		if e.Dest == dest {
			b = true
			return
		}
	}
	return
}

// HasLiveEdgeTo checks if there is a live edge from src to dest.
func (inst *MultiMap) HasLiveEdgeTo(src, dest NodeID) (b bool) {
	for _, e := range inst.m[src] {
		if e.Dest == dest && e.Kind == EdgeKindLive {
			b = true
			return
		}
	}
	return
}

func (inst *MultiMap) Sources() (out []NodeID) {
	out = make([]NodeID, 0, len(inst.m))
	for src := range inst.m {
		out = append(out, src)
	}
	slices.SortFunc(out, CompareNodeID)
	return
}

// CompareNodeID provides a deterministic ordering for NodeIDs: patches
// compare bytewise, ties broken by Index.
func CompareNodeID(a, b NodeID) (c int) {
	c = bytes.Compare(a.Patch[:], b.Patch[:])
	if c != 0 {
		return
	}
	c = cmp.Compare(a.Index, b.Index)
	return
}

// HashBytes computes a PatchHash from arbitrary data using BLAKE3.
func HashBytes(data []byte) (h PatchHash) {
	h = blake3.Sum256(data)
	return
}
