package chat

import (
	"context"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

const (
	surfaceGraphH float32 = 340
	// surfaceLiveEvery is how often the surface is read again while a turn
	// runs: the model may be widening the grant and calling.
	surfaceLiveEvery = 3 * time.Second

	tipSurface        = "Everything the model could reach from this conversation — each open window's operations, opening an app, arranging the desktop — with what the settings and the running task allow, and the calls the model made."
	tipSurfaceRefresh = "Read the grants and calls again."
	tipSurfaceView    = "The same cells as a list, or as a graph around this conversation: colour is the status, a ring the calls by outcome."
)

var atomsSurfaceRefresh = c.Atoms().Text(icons.PhArrowClockwise).Keep()

// surfaceView is the Analytics panel's surface section: the snapshot, the
// job that reads it, and the graph built from it.
type surfaceView struct {
	job   bgjob.Runner[surfaceModel]
	model *surfaceModel
	conv  string
	turns int
	err   string
	// graph switches the section between the list and the graph.
	graph bool
	gv    *graphview.View
	nodes []graphview.NodeSpec
	edges []graphview.EdgeSpec
	// cellOf is a node's cell, -1 for a hub.
	cellOf map[uint64]int
}

func statusTone(s agent.CellStatusE) (tone styletokens.RGBA8) {
	switch s {
	case agent.CellStatusAboveCeiling:
		return styletokens.ErrorDefault
	case agent.CellStatusObserveOnly:
		return styletokens.WarningDefault
	case agent.CellStatusGranted:
		return styletokens.SuccessDefault
	}
	return styletokens.NeutralTextDisabled
}

var useTones = [useCount]styletokens.RGBA8{styletokens.SuccessDefault, styletokens.InfoDefault, styletokens.WarningDefault,
	styletokens.ErrorDefault, styletokens.NeutralStrong}

// statusLabel is the status as the person reads it.
func statusLabel(s agent.CellStatusE) (l string) {
	switch s {
	case agent.CellStatusAboveCeiling:
		return "above the ceiling"
	case agent.CellStatusNotGranted:
		return "not granted"
	case agent.CellStatusObserveOnly:
		return "observe only"
	case agent.CellStatusGranted:
		return "granted"
	}
	return "unknown"
}

// usesText is a cell's calls, "3 calls: 2 done · 1 refused".
func usesText(cell *surfaceCell) (s string) {
	n := cell.calls()
	if n == 0 {
		if cell.status == agent.CellStatusGranted {
			return "unused"
		}
		return ""
	}
	parts := make([]string, 0, useCount)
	for u := range useCount {
		if cell.uses[u] > 0 {
			parts = append(parts, strconv.Itoa(cell.uses[u])+" "+useNames[u])
		}
	}
	s = strconv.Itoa(n) + " call"
	if n != 1 {
		s += "s"
	}
	return s + ": " + strings.Join(parts, " · ")
}

// refreshSurface starts a read when the snapshot is missing, describes
// another conversation, predates the last landed turn, or is old while a
// turn runs.
func (inst *App) refreshSurface(force bool) {
	sv := &inst.surface
	if res, _, ok := sv.job.TakeResult(); ok {
		sv.model, sv.err = res, ""
		sv.buildGraph()
	} else if snap := sv.job.Snapshot(); snap.State == bgjob.StateFailed {
		sv.job.Invalidate()
		sv.err = "could not read the surface"
		if snap.Err != nil {
			sv.err += ": " + snap.Err.Error()
		}
	}
	if inst.coord == nil || sv.job.Running() {
		return
	}
	stale := force || sv.conv != inst.conv.id || sv.turns != len(inst.stats.turns)
	if sv.err == "" && sv.model != nil && inst.pending != nil && time.Since(sv.model.at) > surfaceLiveEvery {
		stale = true
	}
	if sv.model == nil && sv.err == "" {
		stale = true
	}
	if !stale {
		return
	}
	if sv.conv != inst.conv.id {
		sv.model, sv.err = nil, ""
	}
	sv.conv, sv.turns = inst.conv.id, len(inst.stats.turns)
	cli, kq, ceiling, conv, tasks := inst.agentCli, inst.kq, inst.coord.ceilingNow(), inst.conv.id, inst.coord.heldTasks()
	sv.job.Start(nil, bgjob.Spec{Kind: "chat-surface", Title: "the agent surface"},
		func(ctx context.Context) (m *surfaceModel, err error) {
			return fetchSurface(ctx, cli, kq, ceiling, conv, tasks)
		})
}

// renderSurface is the Analytics panel's surface section.
func (inst *App) renderSurface(w float32) {
	if inst.coord == nil {
		return
	}
	section("Agent surface")
	sv := &inst.surface
	refresh := false
	for range c.HorizontalTop().KeepIter() {
		for range c.HoverText(tipSurfaceView).KeepIter() {
			selector.Segmented(inst.ids, "surface-view", &sv.graph).Option(false, "As list").Option(true, "As graph").Send()
		}
		for range c.HoverText(tipSurfaceRefresh).KeepIter() {
			refresh = c.Button(inst.ids.PrepareStr("surface-refresh"), atomsSurfaceRefresh).SendResp().HasPrimaryClicked()
		}
		if sv.job.Running() {
			c.Spinner().Send()
		}
	}
	inst.refreshSurface(refresh)
	if sv.err != "" {
		for rt := range c.RichTextLabelColored(color.Hex(styletokens.ErrorDefault.AsHex()), color.Transparent, sv.err) {
			rt.Small()
		}
	}
	m := sv.model
	if m == nil {
		weak("reading the surface…")
		return
	}
	for range c.HoverText(tipSurface).KeepIter() {
		inst.renderSurfaceSummary(m)
	}
	if sv.graph {
		inst.renderSurfaceGraph(w)
		return
	}
	inst.renderSurfaceList(m)
}

// renderSurfaceSummary is the task line and the cells per status.
func (inst *App) renderSurfaceSummary(m *surfaceModel) {
	switch {
	case m.task != "":
		c.Label("task " + strings.TrimPrefix(m.task, "task-") + " is running").Selectable(false).Send()
	case m.ended > 0:
		c.Label("no running task; " + strconv.Itoa(m.ended) + " ended").Selectable(false).Send()
	default:
		c.Label("no task yet: nothing is granted").Selectable(false).Send()
	}
	n, used, calls := m.statusCounts()
	// Two rows of two: a wrapped row breaks a trailing label letter by
	// letter.
	for row := range 2 {
		for range c.HorizontalTop().KeepIter() {
			for _, s := range agent.AllCellStatuses[2*row : 2*row+2] {
				for rt := range c.RichTextLabelColored(color.Hex(statusTone(s).AsHex()), color.Transparent, "● "+strconv.Itoa(n[s])+" "+statusLabel(s)) {
					rt.Small()
				}
			}
		}
	}
	weak(strconv.Itoa(used) + " of " + strconv.Itoa(n[agent.CellStatusGranted]) + " granted used · " + strconv.Itoa(calls) + " calls in this conversation")
}

// renderSurfaceList draws the cells under one heading per window, then the
// launches and the desktop.
func (inst *App) renderSurfaceList(m *surfaceModel) {
	head := ""
	for i := range m.cells {
		cell := &m.cells[i]
		if h := cellHeading(cell); h != head {
			head = h
			c.AddSpace(4)
			for rt := range c.RichTextLabel(h) {
				rt.Strong()
			}
		}
		for range c.IdScope(inst.ids.PrepareStr("surface-row-" + strconv.Itoa(i))) {
			for range c.HorizontalTop().KeepIter() {
				for rt := range c.RichTextLabelColored(color.Hex(statusTone(cell.status).AsHex()), color.Transparent, "● "+statusLabel(cell.status)) {
					rt.Small()
				}
				name := cellName(cell)
				c.Label(name).Selectable(false).Send()
				if cell.effect != "" && cell.effect != "none" {
					weak(cell.effect)
				}
			}
			if u := usesText(cell); u != "" {
				weak("    " + u)
			}
		}
	}
}

// cellName is an operation's name, or the app a launch opens by its
// display name: the id is an import path, too long for the panel.
func cellName(cell *surfaceCell) (name string) {
	if cell.group != groupLaunch {
		return cell.op
	}
	if cell.window != "" {
		return cell.window
	}
	return cell.app
}

func cellHeading(cell *surfaceCell) (h string) {
	switch cell.group {
	case groupWindow:
		h = "window " + strconv.FormatUint(cell.instance, 10) + " · " + cell.window
		if !cell.open {
			h += " (closed)"
		}
	case groupLaunch:
		h = "open a window"
	default:
		h = "desktop"
	}
	return
}

// renderSurfaceGraph draws the graph and, under it, the hovered cell.
func (inst *App) renderSurfaceGraph(w float32) {
	sv := &inst.surface
	if sv.gv == nil {
		sv.gv = graphview.New(inst.ids, "chat-surface", graphview.Options{Layout: graphview.LayoutRadial,
			Radial: graphview.RadialParams{Centers: []uint64{nodeRoot}, RingDist: 70}, FitToScreen: true, Undirected: true,
			Auras: graphview.AuraParams{Enabled: true, Overlap: true}})
	}
	_ = sv.gv.Render(sv.nodes, sv.edges, w, surfaceGraphH)
	id, ok := sv.gv.HoveredNode()
	if !ok {
		weak("colour: status · ring: calls by outcome · faded: not granted or above the ceiling")
		return
	}
	i, isCell := sv.cellOf[id]
	if !isCell || i < 0 || sv.model == nil {
		weak("hover an operation for its detail")
		return
	}
	cell := &sv.model.cells[i]
	line := cellHeading(cell) + " · " + cellName(cell) + " · " + statusLabel(cell.status)
	if u := usesText(cell); u != "" {
		line += " · " + u
	}
	c.Label(line).Selectable(false).Send()
}

const nodeRoot uint64 = 1

func nodeId(s string) (id uint64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64() | 2 // never the root's id
}

// buildGraph declares the snapshot as a tree around the conversation: a hub
// per window, one for opening windows and one for the desktop, a leaf per
// cell. It runs when a snapshot lands, not per frame.
func (inst *surfaceView) buildGraph() {
	inst.nodes, inst.edges = inst.nodes[:0], inst.edges[:0]
	inst.cellOf = map[uint64]int{}
	m := inst.model
	if m == nil {
		return
	}
	inst.nodes = append(inst.nodes, graphview.NodeSpec{Id: nodeRoot, Label: "this conversation", Radius: 9,
		Color: color.Hex(styletokens.AccentDefault.AsHex()), LabelAlways: true})
	inst.cellOf[nodeRoot] = -1
	hubs := map[string]uint64{}
	for i := range m.cells {
		cell := &m.cells[i]
		hubKey := cellHeading(cell)
		hub, seen := hubs[hubKey]
		if !seen {
			hub = nodeId("hub:" + hubKey)
			hubs[hubKey] = hub
			label := hubKey
			if cell.group == groupWindow {
				label = cell.window
			}
			inst.nodes = append(inst.nodes, graphview.NodeSpec{Id: hub, Label: label, Radius: 6, LabelAlways: true,
				Color: color.Hex(styletokens.NeutralStrong.AsHex()), Auras: []string{hubKey}})
			inst.edges = append(inst.edges, graphview.EdgeSpec{From: nodeRoot, To: hub, Color: color.Hex(styletokens.NeutralBorderDefault.AsHex()), Width: 1})
			inst.cellOf[hub] = -1
		}
		id := nodeId(cellKey(cell.group, cell.instance, cell.app, cell.op))
		label := cellName(cell)
		n := cell.calls()
		node := graphview.NodeSpec{Id: id, Label: label, Radius: 4 + 2*float32(math.Sqrt(float64(n))),
			Color: color.Hex(statusTone(cell.status).AsHex()), Auras: []string{hubKey}}
		if n > 0 {
			node.Donut = graphview.Donut{Values: make([]float32, useCount), Colors: color.NewColors(int(useCount))}
			for u := range useCount {
				node.Donut.Values[u] = float32(cell.uses[u])
				node.Donut.Colors[u] = useTones[u].AsHex()
			}
		}
		if cell.status == agent.CellStatusNotGranted || cell.status == agent.CellStatusAboveCeiling {
			node.Opacity = 0.4
		}
		inst.nodes = append(inst.nodes, node)
		inst.edges = append(inst.edges, graphview.EdgeSpec{From: hub, To: id, Width: 1 + float32(min(n, 6)),
			Color: color.Hex(styletokens.NeutralBorderDefault.AsHex())})
		inst.cellOf[id] = i
	}
}
