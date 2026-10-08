package play

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/kanban"
)

// The Kanban pane as an agent reads it (ADR-0270, update of 2026-10-05):
// the lanes in board order with their card counts and dot tallies over the
// whole fold (up to the board's 2,000 cards, past sample_rows' 50), which
// lanes the `lanes` CTE declared, whether that CTE failed, and a lane's
// cards on request. The board has no options and is read-only, so there is
// no command; its selection is set_signal('selection').

const (
	opGetKanban          = "get_kanban"
	kanbanPaneId         = "kanban"
	kanbanReadMaxLanes   = 100
	kanbanReadCardsLimit = 50
)

// KanbanLaneReading is one lane of the board.
type KanbanLaneReading struct {
	Lane     string  `desc:"the lane's title, cut at 64 bytes; (none) for an empty lane value"`
	Declared bool    `json:",omitzero" desc:"the lanes CTE declared it; otherwise only the rows name it"`
	Cards    int32   `desc:"cards in the lane"`
	Dots     []int64 `json:",omitzero" desc:"the sum of each dot tally over the lane's cards, in the order of dot_labels"`
}

// KanbanCardReading is one card of a lane.
type KanbanCardReading struct {
	Row      int64   `desc:"the card's row in the result"`
	Title    string  `desc:"the title, cut at 64 bytes"`
	Subtitle string  `json:",omitzero" desc:"the subtitle, cut at 64 bytes"`
	Dots     []int64 `json:",omitzero" desc:"the card's dot tallies, in the order of dot_labels"`
}

// KanbanLaneCards is a page of one lane's cards.
type KanbanLaneCards struct {
	Lane  string              `desc:"the lane listed"`
	Cards []KanbanCardReading `json:",omitzero" desc:"the lane's cards from offset, in board order"`
	More  int32               `json:",omitzero" desc:"cards past the ones listed; read on with offset"`
}

// KanbanReading is get_kanban's result.
type KanbanReading struct {
	Drawn        PaneDraw            `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Cards        int64               `json:",omitzero" desc:"cards on the board"`
	RowsNotShown int64               `json:",omitzero" desc:"rows past the board's 2,000 cards, counted in no lane"`
	DotLabels    []string            `json:",omitzero" desc:"the dot tallies' labels, from the dot_<label> columns"`
	LanesQuery   string              `desc:"the lanes CTE: none (the rows name the lanes), pending, failed or declared"`
	LanesError   string              `json:",omitzero" desc:"why the lanes CTE failed; the board then takes its lanes from the rows"`
	Lanes        []KanbanLaneReading `json:",omitzero" desc:"the lanes in board order, at most 100"`
	LanesHidden  int32               `json:",omitzero" desc:"lanes past the 100 listed"`
	Lane         *KanbanLaneCards    `json:",omitzero" desc:"the cards of the lane asked for"`
}

// GetKanbanArgs is get_kanban's argument.
type GetKanbanArgs struct {
	Lane   string `json:",omitzero" desc:"a lane's title, to list its cards too"`
	Offset int32  `json:",omitzero" desc:"with lane, the first card to list"`
	Limit  int32  `json:",omitzero" desc:"with lane, cards to list, 50 by default and at most 50"`
}

// kanbanOpsView is what get_kanban reads: the model, shared (a rebuild
// replaces it and the read-only board never edits it), and the lanes
// query's state.
type kanbanOpsView struct {
	model     *kanban.Model
	declaredN int
	truncated int64
	lanesNode bool
	loading   bool
	lanesErr  string
}

func (inst *KanbanDriver) opsView() (v kanbanOpsView) {
	v = kanbanOpsView{model: inst.model, declaredN: inst.declaredN, truncated: inst.truncated,
		lanesNode: inst.lanesNode, loading: inst.lanesLoading}
	if inst.lanesErr != nil {
		v.lanesErr = truncateBytes(inst.lanesErr.Error(), opsStatusMaxBytes)
	}
	return
}

// kanbanView is the Kanban's view for the snapshot.
func (inst *PlayApp) kanbanView() kanbanOpsView { return inst.kanbanDriver.opsView() }

// kanbanDots spreads a card's tallies over the legend's order.
func kanbanDots(m *kanban.Model, card *kanban.Card) (dots []int64) {
	if len(m.DotLegend) == 0 {
		return nil
	}
	dots = make([]int64, len(m.DotLegend))
	for _, t := range card.Dots {
		for i, k := range m.DotLegend {
			if k.ID == t.ID {
				dots[i] += int64(t.Count)
			}
		}
	}
	return
}

// kanbanReading is get_kanban.
func kanbanReading(sn *opsSnap, in GetKanbanArgs) (out KanbanReading, err error) {
	d, readable, err := paneDrawOf(sn, kanbanPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.kanban
	out = KanbanReading{Drawn: d, LanesQuery: "none"}
	switch {
	case !v.lanesNode:
	case v.lanesErr != "":
		out.LanesQuery, out.LanesError = "failed", v.lanesErr
	case v.loading:
		out.LanesQuery = "pending"
	default:
		out.LanesQuery = "declared"
	}
	m := v.model
	if !readable || m == nil {
		return
	}
	out.Cards, out.RowsNotShown = int64(len(m.Cards)), v.truncated
	for _, k := range m.DotLegend {
		out.DotLabels = append(out.DotLabels, opsLabel(k.Label))
	}
	at := make(map[uint64]int, len(m.Columns))
	lanes := make([]KanbanLaneReading, len(m.Columns))
	for i, col := range m.Columns {
		at[col.ID] = i
		lanes[i] = KanbanLaneReading{Lane: opsLabel(col.Title), Declared: i < v.declaredN}
		if len(m.DotLegend) > 0 {
			lanes[i].Dots = make([]int64, len(m.DotLegend))
		}
	}
	laneIdx := -1
	if in.Lane != "" {
		for i, col := range m.Columns {
			if hierLabelNames(in.Lane, col.Title) || in.Lane == opsLabel(col.Title) {
				laneIdx = i
				break
			}
		}
		if laneIdx < 0 {
			return out, app.RefuseOperation("the board has no lane " + strconv.Quote(in.Lane))
		}
		out.Lane = &KanbanLaneCards{Lane: opsLabel(m.Columns[laneIdx].Title)}
	}
	limit := kanbanReadCardsLimit
	if in.Limit > 0 {
		limit = min(int(in.Limit), kanbanReadCardsLimit)
	}
	seen, used := 0, 0
	for ci := range m.Cards {
		card := &m.Cards[ci]
		i, ok := at[card.ColumnID]
		if !ok {
			continue
		}
		lanes[i].Cards++
		dots := kanbanDots(m, card)
		for j, n := range dots {
			lanes[i].Dots[j] += n
		}
		if i != laneIdx {
			continue
		}
		if seen++; seen <= int(in.Offset) {
			continue
		}
		r := KanbanCardReading{Row: int64(card.ID) - 1, Title: opsLabel(card.Title), Subtitle: opsLabel(card.Subtitle), Dots: dots}
		used += 48 + len(r.Title) + len(r.Subtitle)
		if len(out.Lane.Cards) >= limit || used > opsSampleMaxBytes {
			out.Lane.More++
			continue
		}
		out.Lane.Cards = append(out.Lane.Cards, r)
	}
	if len(lanes) > kanbanReadMaxLanes {
		out.LanesHidden = int32(len(lanes) - kanbanReadMaxLanes)
		lanes = lanes[:kanbanReadMaxLanes]
	}
	out.Lanes = lanes
	return
}

func addKanbanOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[KanbanReading, GetKanbanArgs, appops.None]{
		pane: kanbanPaneId,
		get:  opGetKanban,
		getSummary: "read the board the Kanban pane last drew: each lane's card count and dot sums over every card, " +
			"which lanes the lanes CTE declared and whether it failed, the rows past the board's cap, and a lane's cards",
		read: kanbanReading,
	})
}
