package play

import (
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// play_ops_result.go: describe_result and sample_rows read the result a pane
// is fed, or a node's (ADR-0270, update of 2026-10-05). The main result has
// always been safe to read off the render goroutine (MainSnapshot); a bound
// node's and an observed intermediate's results live on node lanes, which
// guard their result with a lock of their own. The snapshot therefore copies
// the lanes, not their records: each read peeks the lane and holds the
// record for the length of the call. Retaining records in the snapshot and
// releasing them at the next one would free a record under a query still
// reading it, since queries run on their own goroutines.

// opsResults is what the result reads resolve a pane or a node against,
// copied in snapshotPlay. The lanes are safe to peek from any goroutine.
type opsResults struct {
	graph *queryGraph
	// active is the node the unbound panels draw; observed is its lane when
	// it is an observed intermediate, nil when the panels draw the main
	// result.
	active   NodeID
	sink     NodeID
	observed *nodeLane
	// bound is one lane per node a pane is bound to.
	bound map[NodeID]*nodeLane
	// panes is the node each panel is fed this frame; frameless names the
	// panels that draw CTEs by name rather than a fed result.
	panes     map[string]NodeID
	frameless map[string]string
	// nodes are the buffer's split nodes, for refusals.
	nodes []string
}

// snapshotResults copies what the result reads resolve against.
func snapshotResults(p *PlayApp) (r opsResults) {
	split := p.currentSplit
	r = opsResults{graph: p.graph, active: p.activeNodeID(), sink: split.Sink,
		bound: make(map[NodeID]*nodeLane, len(p.boundLanes)), panes: make(map[string]NodeID, 16),
		frameless: make(map[string]string, 4)}
	if p.observedNode != "" && p.observedNode != split.Sink && p.intermediateLane != nil {
		if _, ok := findSplitNode(split, p.observedNode); ok {
			r.observed = p.intermediateLane
		}
	}
	for _, node := range p.tabBindings {
		if node == r.active {
			continue
		}
		if _, ok := findSplitNode(split, node); !ok {
			continue
		}
		if lane := p.boundLanes[node]; lane != nil {
			r.bound[node] = lane
		}
	}
	for _, spec := range p.tabs.all() {
		if spec.Panel == nil {
			continue
		}
		if spec.Frameless {
			r.frameless[spec.ID] = framelessRefusal(spec.ID, spec.Panel)
			continue
		}
		r.panes[spec.ID] = p.resolvedTabNode(spec.ID)
	}
	for _, n := range split.Nodes {
		r.nodes = append(r.nodes, string(n.ID))
	}
	return
}

// laneRead is one result as a read sees it. The caller releases rec
// (nil-safe).
type laneRead struct {
	node       NodeID
	rec        arrow.RecordBatch
	schema     *arrow.Schema
	numRows    int64
	loading    bool
	err        error
	id         ResultID
	truncation string
}

func (inst laneRead) release() {
	if inst.rec != nil {
		inst.rec.Release()
	}
}

func readOfView(node NodeID, v laneView) (lr laneRead) {
	lr = laneRead{node: node, rec: v.rec, schema: v.schema, loading: v.loading, err: v.err, id: v.id}
	if v.rec != nil {
		lr.numRows = v.rec.NumRows()
	}
	return
}

// isMain reports whether node names the main result.
func (inst *opsResults) isMain(node NodeID) bool {
	return node == mainNodeID || (inst.sink != "" && node == inst.sink)
}

// read resolves a pane or a node to its result: a pane to the node it is
// fed, no pane and no node to the result the unbound panels draw. A node is
// readable when something draws it — the active node, a bound one — or when
// it is the main result, which the window holds whatever is observed.
// Reading never starts a run.
func (inst *opsResults) read(pane string, node string) (lr laneRead, err error) {
	if inst.graph == nil {
		return lr, app.RefuseOperation("the window has not mounted")
	}
	if pane != "" && node != "" {
		return lr, app.RefuseOperation("name a pane or a node, not both")
	}
	want := NodeID(node)
	if pane != "" {
		if reason, frameless := inst.frameless[pane]; frameless {
			return lr, app.RefuseOperation(reason + "; to read one of those CTEs, bind a pane that draws rows (table) to it and read that pane")
		}
		fed, ok := inst.panes[pane]
		if !ok {
			return lr, app.RefuseOperation("no pane " + pane + " draws a result; list_panes names the panels")
		}
		want = fed
	}
	if want == "" {
		want = inst.active
	}
	switch {
	case want == inst.active && inst.observed != nil:
		lr = readOfView(want, inst.observed.peek())
	case want == inst.active || inst.isMain(want):
		rec, schema, numRows, loading, _, _, _, runErr, id := inst.graph.MainSnapshot()
		lr = laneRead{node: want, rec: rec, schema: schema, numRows: numRows, loading: loading, err: runErr, id: id,
			truncation: inst.graph.MainTruncation()}
	default:
		lane, ok := inst.bound[want]
		if !ok {
			if !slices.Contains(inst.nodes, string(want)) {
				return lr, app.RefuseOperation("no node " + string(want) + " in the buffer; it has " + strings.Join(inst.nodes, ", "))
			}
			return lr, app.RefuseOperation("node " + string(want) + " is not drawn by any pane, so it holds no result; " +
				"bind_pane a panel to it, then read that pane")
		}
		lr = readOfView(want, lane.peek())
	}
	return
}

// ResultArgs picks the result describe_result describes.
type ResultArgs struct {
	Pane string `json:",omitzero" desc:"the pane whose fed result to describe, as list_panes names it; left out, the result the unbound panels draw"`
	Node string `json:",omitzero" desc:"the split node whose result to describe, as list_panes names it; it must be drawn by a pane or be the main result"`
}

// describeResult is describe_result over one lane.
func describeResult(r *opsResults, glosses *glossColumnsOps, in ResultArgs) (out ResultDescription, err error) {
	lr, err := r.read(in.Pane, in.Node)
	if err != nil {
		return
	}
	defer lr.release()
	out = ResultDescription{Id: uint64(lr.id), Node: string(lr.node), Rows: lr.numRows, Loading: lr.loading}
	if lr.err != nil {
		out.Error = lr.err.Error()
	}
	if lr.truncation != "" {
		out.Truncated, out.TruncationReason = true, lr.truncation
	}
	if lr.schema == nil {
		return
	}
	names := make([]string, lr.schema.NumFields())
	for i, f := range lr.schema.Fields() {
		names[i] = f.Name
	}
	handles := lwsql.BuildLabels(names)
	glossed := glosses != nil && glosses.schema == lr.schema
	for i, f := range lr.schema.Fields() {
		col := Column{Name: f.Name, Type: f.Type.String(), Handle: handles[f.Name], Gloss: glosses.glossOf(lr.schema, i)}
		if label := pathColumnLabel(f.Name); label != f.Name {
			col.Label = label
		}
		out.Leeway = out.Leeway || col.Handle != ""
		out.Columns = append(out.Columns, col)
	}
	if out.Leeway {
		out.Reading = leewayReading
	}
	if !glossed {
		// The window resolves glosses for the schema a pane last drew
		// through them; another result has no resolution to report.
		out.GlossNote = "the window resolved glosses for another result (the one a pane last drew through them); show_pane table and describe again"
	}
	return
}

// sampleResult reads cells of one lane's result, bounded by rows and bytes.
func sampleResult(r *opsResults, in SampleArgs) (out SampleRows, err error) {
	lr, err := r.read(in.Pane, in.Node)
	if err != nil {
		return
	}
	defer lr.release()
	rec, schema, numRows, id := lr.rec, lr.schema, lr.numRows, lr.id
	if rec == nil || schema == nil {
		return out, app.RefuseOperation("node " + string(lr.node) + " holds no result")
	}
	if in.ResultId != nil && *in.ResultId != uint64(id) {
		return out, app.ConflictOperation("another result is held now: " + strconv.FormatUint(uint64(id), 10))
	}
	if len(in.Rows) > 0 && in.Offset != 0 {
		return out, app.RefuseOperation("name rows or an offset, not both")
	}
	out.ResultId, out.Node, out.ResultPrefix = uint64(id), string(lr.node), lr.truncation
	names := make([]string, schema.NumFields())
	for i, f := range schema.Fields() {
		names[i] = f.Name
	}
	// The handles the Table's header shows and describe_table teaches, so
	// a model can ask for and read back the names it writes.
	handles := lwsql.BuildLabels(names)
	var cols []int
	anyHandle, anyLabel := false, false
	for i, name := range names {
		handle := handles[name]
		label := pathColumnLabel(name)
		if label == name {
			label = ""
		}
		if len(in.Fields) == 0 || slices.Contains(in.Fields, name) || (handle != "" && slices.Contains(in.Fields, handle)) ||
			(label != "" && slices.Contains(in.Fields, label)) {
			cols = append(cols, i)
			out.Columns = append(out.Columns, name)
			out.Handles = append(out.Handles, handle)
			out.Labels = append(out.Labels, label)
			anyHandle = anyHandle || handle != ""
			anyLabel = anyLabel || label != ""
		}
	}
	if !anyHandle {
		out.Handles = nil
	}
	if !anyLabel {
		out.Labels = nil
	}
	if len(in.Fields) > 0 && len(cols) == 0 {
		return out, app.RefuseOperation("no column of the result is named " + strings.Join(in.Fields, ", ") + "; describe_result lists them")
	}
	limit := int64(in.Limit)
	if limit <= 0 || limit > opsSampleMaxRows {
		limit = opsSampleMaxRows
	}
	// The rows to read: the ones named, or a run from the offset.
	var rows []int64
	if len(in.Rows) > 0 {
		if int64(len(in.Rows)) > opsSampleMaxRows {
			return out, app.RefuseOperation("at most " + strconv.Itoa(opsSampleMaxRows) + " rows at a time")
		}
		for _, row := range in.Rows {
			if row < 0 || row >= numRows {
				return out, app.RefuseOperation("row " + strconv.FormatInt(row, 10) + " is not in the result; it has " +
					strconv.FormatInt(numRows, 10) + " rows, 0 for the first")
			}
		}
		rows = in.Rows
	} else {
		for row := int64(in.Offset); row < numRows && row < int64(in.Offset)+limit; row++ {
			rows = append(rows, row)
		}
	}
	bytes := 0
	anyNull := false
	for _, row := range rows {
		cells := make([]string, 0, len(cols))
		nulls := []int32{}
		for k, col := range cols {
			if rec.Column(col).IsNull(int(row)) {
				nulls = append(nulls, int32(k))
				cells = append(cells, "")
				continue
			}
			v := strings.Clone(formatCell(rec, col, row))
			bytes += len(v)
			cells = append(cells, v)
		}
		if bytes > opsSampleMaxBytes {
			out.Truncated = true
			out.TruncationReason = "the sample's byte bound (" + strconv.Itoa(opsSampleMaxBytes>>10) + " KiB of cell text)"
			break
		}
		out.Rows = append(out.Rows, cells)
		out.Nulls = append(out.Nulls, nulls)
		out.RowNumbers = append(out.RowNumbers, row)
		anyNull = anyNull || len(nulls) > 0
	}
	if !anyNull {
		out.Nulls = nil
	}
	if len(in.Rows) == 0 && !out.Truncated && int64(in.Offset)+int64(len(out.Rows)) < numRows && int64(len(out.Rows)) == limit {
		out.Truncated = true
		out.TruncationReason = "the row bound (" + strconv.FormatInt(limit, 10) + " rows); read on from offset " +
			strconv.FormatInt(int64(in.Offset)+limit, 10)
	}
	return
}

// windowConfined is the window's label (ADR-0270 §SD4, extended to lanes):
// true while the main result, the observed intermediate's or a bound
// node's is confined — every result a read or a capture of the window can
// show.
func (inst *PlayApp) windowConfined() (confined bool) {
	if inst.graph != nil && inst.graph.MainConfined() {
		return true
	}
	split := inst.currentSplit
	if inst.intermediateLane != nil && inst.observedNode != "" && inst.observedNode != split.Sink {
		if _, ok := findSplitNode(split, inst.observedNode); ok && inst.intermediateLane.servesConfined() {
			return true
		}
	}
	// gcBoundLanes keeps one lane per node a binding names, and no other.
	for _, lane := range inst.boundLanes {
		if lane.servesConfined() {
			return true
		}
	}
	return false
}
