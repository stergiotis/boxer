//go:build llm_generated_opus47

package types

import "iter"

// GraphReader provides read-only access to the graph state.
// This is the minimal interface that graph algorithms need.
// Enumeration methods return iter.Seq for lazy evaluation.
//
// Precondition: callers must ensure ResolvePseudoEdges() has been called
// before invoking graph algorithms that depend on a consistent live subgraph.
type GraphReader interface {
	// Node queries.
	HasNode(id NodeID) bool
	IsLive(id NodeID) bool
	IsDeleted(id NodeID) bool
	NodeContent(id NodeID) []byte

	// Enumeration of the live subgraph.
	AllLiveNodes() iter.Seq[NodeID]
	LiveChildren(id NodeID) iter.Seq[NodeID]
	LiveParents(id NodeID) iter.Seq[NodeID]

	// Raw edge access (all kinds, including deleted/pseudo). Patch.Unapply
	// uses this to detect edges introduced by other patches.
	ForwardEdges(src NodeID) iter.Seq[Edge]
	BackwardEdges(dest NodeID) iter.Seq[Edge]
}

// GraphWriter provides mutation access to the graph.
// Used by Patch.Apply and Patch.Unapply.
type GraphWriter interface {
	AddNode(id NodeID, content []byte, patch PatchHash, upContext, downContext []NodeID) error
	DeleteNode(id NodeID) error
	UndeleteNode(id NodeID) error
	AddEdge(src, dest NodeID, patch PatchHash) error
	RemoveEdge(src, dest NodeID, kind EdgeKind, patch PatchHash)
	RemoveNode(id NodeID)
	ResolvePseudoEdges()
}

// GraphStore combines read and write access with cloning.
type GraphStore interface {
	GraphReader
	GraphWriter
	CloneStore() GraphStore
}

// Inspectable provides deep read access to internal state for invariant
// checking and quality control. This interface is deliberately broad —
// invariant checking needs to see everything. Only the QC subsystem
// should depend on it.
type Inspectable interface {
	GraphReader

	// Full node enumeration (including deleted).
	AllDeletedNodes() iter.Seq[NodeID]

	// Raw edge access not already exposed by GraphReader.
	ForwardEdgeSources() iter.Seq[NodeID]
	BackwardEdgeSources() iter.Seq[NodeID]
	HasLiveEdgeTo(src, dest NodeID) bool

	// Deleted partition inspection.
	DeletedPartitionContains(id NodeID) bool
	DeletedPartitionFind(id NodeID) NodeID
	DeletedPartitionRepresentatives() iter.Seq[NodeID]
	DeletedPartitionMembers(rep NodeID) iter.Seq[NodeID]

	// Pseudo-edge bookkeeping.
	DirtyRepCount() int
	PseudoEdgeReasonCount(src, dest NodeID) int
	ReasonPseudoEdgesForRep(rep NodeID) iter.Seq[[2]NodeID]
	AllTrackedPseudoEdges() iter.Seq[[2]NodeID]

	// Boundary computation (for completeness check).
	ExportFindBoundaryNodes(component []NodeID) (sources, dests []NodeID)
	ExportFindReachableBoundary(src NodeID, component, dests []NodeID) []NodeID

	// Resolution trigger.
	ResolvePseudoEdges()
}

// Visualizable provides read access for DOT/Graphviz rendering.
// Includes deleted nodes and all edge kinds for a complete picture.
type Visualizable interface {
	AllLiveNodes() iter.Seq[NodeID]
	AllDeletedNodes() iter.Seq[NodeID]
	NodeContent(id NodeID) []byte
	ForwardEdges(src NodeID) iter.Seq[Edge]
	ForwardEdgeSources() iter.Seq[NodeID]
}