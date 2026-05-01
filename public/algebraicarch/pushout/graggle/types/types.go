//go:build llm_generated_opus47

// Package types defines the core data types and interfaces for the pushout
// revision control system. This is the leaf package in the dependency graph —
// all other graggle sub-packages import it, and it imports nothing internal.
package types

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
)

// PatchHash identifies a patch by the SHA-256 hash of its contents.
// The zero value represents the "genesis" patch that introduces the root node.
type PatchHash [32]byte

func (h PatchHash) String() string {
	return hex.EncodeToString(h[:8]) // short form for display
}

// VENDOR DEVIATION: hex MarshalText/UnmarshalText below is added downstream
// so JSON envelopes can carry patch hashes as readable hex strings. The
// upstream pushout types serialise [32]byte as a 32-element JSON array.

// MarshalText emits the patch hash as a 64-char lowercase hex string. This
// is what encoding/json uses for fields of this type, including NodeID.Patch.
func (h PatchHash) MarshalText() (text []byte, err error) {
	text = make([]byte, hex.EncodedLen(len(h)))
	hex.Encode(text, h[:])
	return
}

// UnmarshalText parses a 64-char hex string back into a PatchHash.
func (h *PatchHash) UnmarshalText(text []byte) (err error) {
	if len(text) != hex.EncodedLen(len(h)) {
		err = fmt.Errorf("PatchHash: expected %d hex chars, got %d", hex.EncodedLen(len(h)), len(text))
		return
	}
	if _, err = hex.Decode(h[:], text); err != nil {
		err = fmt.Errorf("PatchHash: %w", err)
	}
	return
}

// IsZero returns true for the genesis/root patch hash.
func (h PatchHash) IsZero() bool {
	return h == PatchHash{}
}

// PlaceholderHash is a sentinel used in patch construction to mean
// "this patch" — distinct from the zero hash (which is the root/genesis).
var PlaceholderHash = func() PatchHash {
	var h PatchHash
	for i := range h {
		h[i] = 0xFF
	}
	return h
}()

// IsPlaceholder returns true for the "current patch" placeholder hash.
func (h PatchHash) IsPlaceholder() bool {
	return h == PlaceholderHash
}

// NodeID uniquely identifies a line/node in the graggle.
// It is the combination of the patch that introduced the node and the
// node's index within that patch.
type NodeID struct {
	Patch PatchHash
	Index uint64
}

func (n NodeID) String() string {
	return fmt.Sprintf("%s/%d", n.Patch, n.Index)
}

// RootNodeID is the sentinel root node present in every graggle.
// All top-level content is ordered after this node.
var RootNodeID = NodeID{}

// EdgeKind classifies edges in the graggle.
type EdgeKind uint8

const (
	EdgeLive    EdgeKind = iota // Real ordering edge between live nodes
	EdgeDeleted                 // Edge to/from a tombstoned (ghost) node
	EdgePseudo                  // Computed shortcut over deleted regions
)

func (k EdgeKind) String() string {
	switch k {
	case EdgeLive:
		return "live"
	case EdgeDeleted:
		return "deleted"
	case EdgePseudo:
		return "pseudo"
	default:
		return "unknown"
	}
}

// Edge represents a directed edge in the graggle.
type Edge struct {
	Dest         NodeID
	Kind         EdgeKind
	IntroducedBy PatchHash // which patch created this edge
}

// NodeSet is an ordered set of NodeIDs.
type NodeSet struct {
	m map[NodeID]struct{}
}

func NewNodeSet() *NodeSet {
	return &NodeSet{m: make(map[NodeID]struct{})}
}

func (s *NodeSet) Add(id NodeID) {
	s.m[id] = struct{}{}
}

func (s *NodeSet) Remove(id NodeID) {
	delete(s.m, id)
}

func (s *NodeSet) Contains(id NodeID) bool {
	_, ok := s.m[id]
	return ok
}

func (s *NodeSet) Len() int {
	return len(s.m)
}

func (s *NodeSet) Items() []NodeID {
	out := make([]NodeID, 0, len(s.m))
	for id := range s.m {
		out = append(out, id)
	}
	slices.SortFunc(out, CompareNodeID)
	return out
}

// MultiMap maps a NodeID to a set of Edges.
type MultiMap struct {
	m map[NodeID][]Edge
}

func NewMultiMap() *MultiMap {
	return &MultiMap{m: make(map[NodeID][]Edge)}
}

func (mm *MultiMap) Add(src NodeID, e Edge) {
	mm.m[src] = append(mm.m[src], e)
}

func (mm *MultiMap) Remove(src NodeID, e Edge) {
	edges := mm.m[src]
	for i, existing := range edges {
		if existing == e {
			mm.m[src] = append(edges[:i], edges[i+1:]...)
			break
		}
	}
	if len(mm.m[src]) == 0 {
		delete(mm.m, src)
	}
}

func (mm *MultiMap) Get(src NodeID) []Edge {
	return mm.m[src]
}

func (mm *MultiMap) Has(src NodeID, e Edge) bool {
	for _, existing := range mm.m[src] {
		if existing == e {
			return true
		}
	}
	return false
}

// HasEdgeTo checks if there is any edge from src to dest (regardless of kind).
func (mm *MultiMap) HasEdgeTo(src, dest NodeID) bool {
	for _, e := range mm.m[src] {
		if e.Dest == dest {
			return true
		}
	}
	return false
}

// HasLiveEdgeTo checks if there is a live edge from src to dest.
func (mm *MultiMap) HasLiveEdgeTo(src, dest NodeID) bool {
	for _, e := range mm.m[src] {
		if e.Dest == dest && e.Kind == EdgeLive {
			return true
		}
	}
	return false
}

func (mm *MultiMap) Sources() []NodeID {
	out := make([]NodeID, 0, len(mm.m))
	for src := range mm.m {
		out = append(out, src)
	}
	slices.SortFunc(out, CompareNodeID)
	return out
}

// CompareNodeID provides a deterministic ordering for NodeIDs: patches
// compare bytewise, ties broken by Index.
func CompareNodeID(a, b NodeID) int {
	if c := bytes.Compare(a.Patch[:], b.Patch[:]); c != 0 {
		return c
	}
	return cmp.Compare(a.Index, b.Index)
}

// HashBytes computes a PatchHash from arbitrary data.
func HashBytes(data []byte) PatchHash {
	return sha256.Sum256(data)
}