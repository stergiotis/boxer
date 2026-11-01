//go:build !(js || wasip1)

package goccyengine

import (
	"slices"
	"strconv"

	"github.com/goccy/go-graphviz/cgraph"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// rankSubgraphPrefix names the subgraph that holds one Rank's nodes. Subgraph
// names live in their own namespace, so it cannot collide with a node ID.
const rankSubgraphPrefix = "layeredgraph-rank-"

// pinRanks expresses Node.Rank in DOT: every set of nodes sharing a Rank goes
// into a rank=same subgraph, which puts them on one layer. It returns the
// Rank of every pinned node, by ID, for pinEdge to read; nil when no node is
// pinned, which leaves the DOT exactly as it was before Rank existed.
//
// The layers' order and spacing come from the edges, not from here: pinEdge
// gives an edge between two pinned nodes a minimum length of their Rank
// difference. Graphviz has no absolute rank, and the alternative — a chain
// of invisible anchor nodes, one per Rank — would take room in every layer
// and in the bounding box.
func pinRanks(graph *cgraph.Graph, m layeredgraph.GraphModel) (rankOf map[string]int, err error) {
	byRank := make(map[int][]string, 8)
	for _, n := range m.Nodes {
		if n.Rank == 0 {
			continue
		}
		if rankOf == nil {
			rankOf = make(map[string]int, len(m.Nodes))
		}
		if _, dup := rankOf[n.ID]; dup {
			// Same ID, same node: the first occurrence wins, as for the label.
			continue
		}
		rankOf[n.ID] = n.Rank
		byRank[n.Rank] = append(byRank[n.Rank], n.ID)
	}
	ranks := make([]int, 0, len(byRank))
	for r := range byRank {
		ranks = append(ranks, r)
	}
	slices.Sort(ranks) // deterministic DOT, whatever the map order
	for _, r := range ranks {
		sg, serr := graph.CreateSubGraphByName(rankSubgraphPrefix + strconv.Itoa(r))
		if serr != nil {
			return nil, eb.Build().Int("rank", r).Errorf("create rank subgraph: %w", serr)
		}
		_ = sg.SafeSet("rank", "same", "")
		for _, id := range byRank[r] {
			// On a subgraph, creating a node the root already holds inserts
			// that node rather than making a second one.
			if _, nerr := sg.CreateNodeByName(id); nerr != nil {
				return nil, eb.Build().Str("node", id).Int("rank", r).Errorf("pin node to rank: %w", nerr)
			}
		}
	}
	return rankOf, nil
}

// pinEdge gives an edge between two pinned nodes the length their Ranks ask
// for: a forward edge at least its Rank difference, a backward one no say in
// the ranking at all (constraint=false), so it cannot fight the pins. An edge
// within one Rank is left alone — rank=same already put both ends on the
// layer — as is any edge with an unpinned end.
func pinEdge(ge *cgraph.Edge, rankOf map[string]int, ed layeredgraph.Edge) {
	if rankOf == nil {
		return
	}
	from, okFrom := rankOf[ed.From]
	to, okTo := rankOf[ed.To]
	if !okFrom || !okTo {
		return
	}
	switch d := to - from; {
	case d > 0:
		ge.SetMinLen(d)
	case d < 0:
		ge.SetConstraint(false)
	}
}
