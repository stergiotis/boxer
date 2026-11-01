package mdgraph

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// NodeKindE is what a node stands for.
type NodeKindE uint8

const (
	NodeKindNote NodeKindE = iota
	NodeKindTag
	// NodeKindUnresolved is a link target no note of the corpus answers.
	NodeKindUnresolved
)

var AllNodeKinds = []NodeKindE{NodeKindNote, NodeKindTag, NodeKindUnresolved}

func (inst NodeKindE) String() string {
	switch inst {
	case NodeKindNote:
		return "note"
	case NodeKindTag:
		return "tag"
	case NodeKindUnresolved:
		return "unresolved"
	}
	return "unknown"
}

// EdgeKindE is how a note reaches a node.
type EdgeKindE uint8

const (
	// EdgeKindLink is a wikilink or inline link.
	EdgeKindLink EdgeKindE = iota
	// EdgeKindEmbed is an embed or an image link.
	EdgeKindEmbed
	// EdgeKindTag joins a note to a tag it carries.
	EdgeKindTag
)

var AllEdgeKinds = []EdgeKindE{EdgeKindLink, EdgeKindEmbed, EdgeKindTag}

func (inst EdgeKindE) String() string {
	switch inst {
	case EdgeKindLink:
		return "link"
	case EdgeKindEmbed:
		return "embed"
	case EdgeKindTag:
		return "tag"
	}
	return "unknown"
}

// Options selects the optional node kinds.
type Options struct {
	// Tags adds a node per tag and an edge from each note carrying it.
	Tags bool
	// Unresolved adds a node per unresolved link target and the edges into
	// it.
	Unresolved bool
}

// Graph is a corpus's document graph as struct-of-arrays (ADR-0353 §SD2).
// Node i is (NodeKind[i], NodeKey[i]); the notes come first, node i being
// the i-th note given to [Build], then tags and unresolved targets in the
// order first met. Edge j runs from note EdgeSrc[j] to node EdgeDst[j];
// EdgeWeight[j] counts the occurrences collapsed into it.
type Graph struct {
	// NodeKey is a note's path, a tag's lowercased name without its "#",
	// or an unresolved target lowercased and without ".md".
	NodeKey  []string
	NodeKind []NodeKindE

	EdgeSrc    []int32
	EdgeDst    []int32
	EdgeKind   []EdgeKindE
	EdgeWeight []int32

	index map[nodeKey]int32
}

type nodeKey struct {
	kind NodeKindE
	key  string
}

type edgeKey struct {
	src, dst int32
	kind     EdgeKindE
}

// Build resolves every note's links and collects its tags into a graph.
func Build(notes []Note, opts Options) (g *Graph) {
	r := NewResolver(notes)
	g = &Graph{
		NodeKey:  make([]string, 0, len(notes)),
		NodeKind: make([]NodeKindE, 0, len(notes)),
		index:    make(map[nodeKey]int32, len(notes)),
	}
	for _, n := range notes {
		g.addNode(NodeKindNote, n.Path)
	}
	edges := make(map[edgeKey]int32, len(notes)*4)
	add := func(k edgeKey) {
		if e, ok := edges[k]; ok {
			g.EdgeWeight[e]++
			return
		}
		edges[k] = int32(len(g.EdgeSrc))
		g.EdgeSrc = append(g.EdgeSrc, k.src)
		g.EdgeDst = append(g.EdgeDst, k.dst)
		g.EdgeKind = append(g.EdgeKind, k.kind)
		g.EdgeWeight = append(g.EdgeWeight, 1)
	}
	for i, n := range notes {
		if n.Doc == nil {
			continue
		}
		src := int32(i)
		for _, l := range n.Doc.Links {
			kind := EdgeKindLink
			if l.Kind == mdextract.LinkKindEmbed || l.Kind == mdextract.LinkKindImage {
				kind = EdgeKindEmbed
			}
			dst, res, key := r.Resolve(src, l)
			switch res {
			case ResolutionNote:
				if dst != src {
					add(edgeKey{src: src, dst: dst, kind: kind})
				}
			case ResolutionUnresolved:
				if opts.Unresolved {
					add(edgeKey{src: src, dst: g.addNode(NodeKindUnresolved, key), kind: kind})
				}
			}
		}
		if !opts.Tags {
			continue
		}
		for _, t := range n.Doc.Tags {
			if t.Tag == "" {
				continue
			}
			add(edgeKey{src: src, dst: g.addNode(NodeKindTag, strings.ToLower(t.Tag)), kind: EdgeKindTag})
		}
	}
	return
}

// addNode returns the node for (kind, key), adding it when new.
func (inst *Graph) addNode(kind NodeKindE, key string) (id int32) {
	k := nodeKey{kind: kind, key: key}
	if id, ok := inst.index[k]; ok {
		return id
	}
	id = int32(len(inst.NodeKey))
	inst.index[k] = id
	inst.NodeKey = append(inst.NodeKey, key)
	inst.NodeKind = append(inst.NodeKind, kind)
	return
}

// GetNode returns the node for (kind, key).
func (inst *Graph) GetNode(kind NodeKindE, key string) (id int32, ok bool) {
	id, ok = inst.index[nodeKey{kind: kind, key: key}]
	return
}

// Degrees counts the edges touching each node, both directions.
func (inst *Graph) Degrees() (deg []int32) {
	deg = make([]int32, len(inst.NodeKey))
	for j := range inst.EdgeSrc {
		deg[inst.EdgeSrc[j]]++
		deg[inst.EdgeDst[j]]++
	}
	return
}
