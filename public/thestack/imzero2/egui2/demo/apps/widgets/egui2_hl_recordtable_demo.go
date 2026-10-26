package widgets

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "recordtable",
		Category: "Tables",
		Title:    icons.IconTable + " recordtable (grouped)",
		Stage:    [2]float32{1024, 700},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "widgets/recordtable over an exploded record — one row per reading of a vessel, with the plain section it sits in and the " +
			"membership path that names it — grouped two levels deep (ADR-0336): by the section column and then by the path's segments, or by " +
			"the vessel's source row and then the path. A band per group carries the chevron, the key and a leaf count; the aggregating " +
			"column shows its mean or sum under its header on the band's line. Expansion is Go state the host owns — expand-all, " +
			"collapse-all and reveal-a-row are calls — and the grid applies no gesture itself: a band click folds, or selects when the key " +
			"is a record row, and a grouped column's header reverses its level.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newRecordTableDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoRecordTable(ids, state.(*recordTableDemoState))
		},
		SourceFunc: demoRecordTable,
	})
}

// =============================================================================
// recordtable (grouped) — widgets/recordtable showcase + DX example
// (ADR-0336 M2).
//
// The fixture is the shape play's per-attribute view will hand the grid
// (ADR-0336 §SD12): a leeway entity exploded into one record row per
// attribute, each carrying its source row, the plain section it belongs to
// and the membership path that names it. Three vessels and their readings
// are enough to show both level kinds — a column level over the section, a
// path level over the JSON Pointer — a null group at each level, a path
// whose leaves sit beside a subgroup, and an aggregate merged up two levels.
//
// What the demo is trying to show, in the order the sections appear:
//
//   - the grouping is a spec the host sets once, and a mode switch is a
//     different spec, not a different grid;
//   - expansion lives in a caller-owned GroupState, so "expand all",
//     "collapse all" and "reveal this row" are calls;
//   - the grid reports its gestures and applies none: the readout shows what
//     came back before Apply folded it.
// =============================================================================

// reading is one exploded attribute: the nested source the widget does NOT
// take. The record below is built from it once; play's view builds the same
// record from a leeway result.
type reading struct {
	Vessel  string
	Src     int64 // the vessel's row in the record it was exploded from
	Section string
	Path    string // JSON Pointer; "" is a null path
	Value   float64
	HasVal  bool
	Unit    string
}

const (
	rtColVessel  = 0
	rtColSrc     = 1
	rtColSection = 2
	rtColPath    = 3
	rtColValue   = 4
	rtColUnit    = 5
)

var (
	readingsFixture = []reading{
		{"Aurora", 0, "hull", "/draught/fore", 6.2, true, "m"},
		{"Aurora", 0, "hull", "/draught/aft", 6.8, true, "m"},
		{"Aurora", 0, "hull", "/length", 142, true, "m"},
		{"Aurora", 0, "engine", "/engines/0/rpm", 1180, true, "rpm"},
		{"Aurora", 0, "engine", "/engines/0/temp", 84, true, "°C"},
		{"Aurora", 0, "engine", "/engines/1/rpm", 1175, true, "rpm"},
		{"Aurora", 0, "engine", "/engines/1/temp", 86, true, "°C"},
		{"Aurora", 0, "cargo", "/holds/0/mass", 1200, true, "t"},
		{"Aurora", 0, "cargo", "/holds/1/mass", 980, true, "t"},
		{"Boreas", 1, "hull", "/draught/fore", 4.1, true, "m"},
		{"Boreas", 1, "hull", "/draught/aft", 4.4, true, "m"},
		{"Boreas", 1, "hull", "/length", 98, true, "m"},
		{"Boreas", 1, "engine", "/engines/0/rpm", 1420, true, "rpm"},
		{"Boreas", 1, "engine", "/engines/0/temp", 91, true, "°C"},
		{"Boreas", 1, "cargo", "/holds/0/mass", 610, true, "t"},
		{"Boreas", 1, "cargo", "/holds/0/hazard", 1, true, ""},
		{"Boreas", 1, "note", "", 0, false, ""},
		{"Calypso", 2, "hull", "/draught/fore", 2.9, true, "m"},
		{"Calypso", 2, "hull", "/length", 61, true, "m"},
		{"Calypso", 2, "engine", "/engines/0/rpm", 2200, true, "rpm"},
		{"Calypso", 2, "engine", "/engines/0/temp", 79, true, "°C"},
		{"Calypso", 2, "cargo", "/holds/0/mass", 140, true, "t"},
		{"Calypso", 2, "", "/remarks/0", 0, false, ""},
	}

	// The record the grid takes, built once: the fixture is immutable, and a
	// demo that rebuilt its record every frame would be showing the wrong
	// lesson. Never released — it lives as long as the process.
	readingsRec arrow.RecordBatch
)

func init() {
	readingsRec = buildReadingsRecord(readingsFixture)
}

// buildReadingsRecord is the whole of "how do I get my data in": one builder
// per column, a null where the fixture has none, one record at the end.
func buildReadingsRecord(rows []reading) arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "vessel", Type: arrow.BinaryTypes.String},
		{Name: "src", Type: arrow.PrimitiveTypes.Int64},
		{Name: "section", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "path", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "unit", Type: arrow.BinaryTypes.String},
	}, nil)
	vb := array.NewStringBuilder(mem)
	sb := array.NewInt64Builder(mem)
	secb := array.NewStringBuilder(mem)
	pb := array.NewStringBuilder(mem)
	valb := array.NewFloat64Builder(mem)
	ub := array.NewStringBuilder(mem)
	for _, r := range rows {
		vb.Append(r.Vessel)
		sb.Append(r.Src)
		if r.Section == "" {
			secb.AppendNull()
		} else {
			secb.Append(r.Section)
		}
		if r.Path == "" {
			pb.AppendNull()
		} else {
			pb.Append(r.Path)
		}
		if r.HasVal {
			valb.Append(r.Value)
		} else {
			valb.AppendNull()
		}
		ub.Append(r.Unit)
	}
	arrs := []arrow.Array{vb.NewArray(), sb.NewArray(), secb.NewArray(), pb.NewArray(), valb.NewArray(), ub.NewArray()}
	rec := array.NewRecordBatch(schema, arrs, int64(len(rows)))
	for _, b := range []interface{ Release() }{vb, sb, secb, pb, valb, ub} {
		b.Release()
	}
	for _, a := range arrs {
		a.Release()
	}
	return rec
}

// readingRowOf resolves a vessel's reading by path to its record row, or -1.
// Only used to pin the reveal target at construction.
func readingRowOf(vessel, path string) int64 {
	for i, r := range readingsFixture {
		if r.Vessel == vessel && r.Path == path {
			return int64(i)
		}
	}
	return -1
}

// readingsSegments is the path level's segmenter: the JSON Pointer's steps,
// each a group key, the last the leaf's own name. A null path yields none,
// which is the level's null group. play's view will hand the grid its
// jsonshred segmenter here; the grid never parses a path itself.
func readingsSegments(row int64) (segs []recordtable.Key) {
	col := readingsRec.Column(rtColPath).(*array.String)
	if col.IsNull(int(row)) {
		return
	}
	for _, s := range strings.Split(strings.TrimPrefix(col.Value(int(row)), "/"), "/") {
		segs = append(segs, recordtable.Key{Value: s})
	}
	return
}

// readingsVesselKey keys the vessel level on the source row, so the band
// stands for a record of the other table (ADR-0336 §SD6): its click selects
// the vessel rather than folding. The label is what the band shows.
func readingsVesselKey(row int64) recordtable.Key {
	src := readingsRec.Column(rtColSrc).(*array.Int64).Value(int(row))
	name := readingsRec.Column(rtColVessel).(*array.String).Value(int(row))
	return recordtable.Key{Value: strconv.FormatInt(src, 10), Label: name}
}

// rtGroupModeE is which spec the GroupState holds.
type rtGroupModeE uint8

const (
	rtUngrouped rtGroupModeE = iota
	rtBySectionThenPath
	rtByVesselThenPath
)

// recordTableDemoState is the per-window view state: the three things the
// grid remembers through the host, the mode and aggregate the controls
// switch, and the selections the readout prints.
type recordTableDemoState struct {
	grid   recordtable.State
	sort   recordtable.SortState
	groups recordtable.GroupState
	mode   rtGroupModeE
	// sum swaps the value column's aggregate from the mean to the sum — a
	// changed declaration, which the layout recomputes on its own.
	sum bool
	// refit asks the grid to fit its columns again, once: a changed spec or
	// aggregate is a new answer in the sense of the how-to, and the band's
	// aggregates are wider than the leaves they summarise.
	refit bool
	// selected is a record row of the readings; selectedVessel a source row
	// reported by a band click in vessel mode. Both -1 for none.
	selected       int64
	selectedVessel int64
	revealTarget   int64
	lastAction     string
}

func newRecordTableDemoState() (st *recordTableDemoState) {
	st = &recordTableDemoState{
		selected:       -1,
		selectedVessel: -1,
		revealTarget:   readingRowOf("Calypso", "/engines/0/temp"),
		lastAction:     "(nothing yet)",
	}
	st.setMode(rtBySectionThenPath)
	return
}

// setMode is the one place the spec is set. The expansion survives a
// changed spec only where the key paths coincide, so switching modes starts
// the groups open.
func (st *recordTableDemoState) setMode(mode rtGroupModeE) {
	st.mode = mode
	st.refit = true
	switch mode {
	case rtUngrouped:
		st.groups.SetLevels(nil)
	case rtBySectionThenPath:
		st.groups.SetLevels([]recordtable.GroupLevel{
			{Arrow: rtColSection},
			{Segments: readingsSegments},
		})
	case rtByVesselThenPath:
		st.groups.SetLevels([]recordtable.GroupLevel{
			{Arrow: rtColSrc, Key: readingsVesselKey, KeyIsRow: true},
			{Segments: readingsSegments},
		})
	}
}

// -----------------------------------------------------------------------------
// Top-level demo
// -----------------------------------------------------------------------------

func demoRecordTable(ids *c.WidgetIdStack, st *recordTableDemoState) {
	recordTableControlsSection(ids, st)
	recordTableGridSection(ids, st)
	recordTableReadoutSection(st)
}

// -----------------------------------------------------------------------------
// Controls — the point of host-owned state
// -----------------------------------------------------------------------------

func recordTableControlsSection(ids *c.WidgetIdStack, st *recordTableDemoState) {
	stdSection("driving the grouping from Go",
		"the spec, the expansion and the reveal request are all calls on a caller-owned recordtable.GroupState")

	for range c.Horizontal().KeepIter() {
		for _, m := range []struct {
			mode  rtGroupModeE
			key   string
			label string
		}{
			{rtUngrouped, "rt-mode-flat", "ungrouped"},
			{rtBySectionThenPath, "rt-mode-section", "section › path"},
			{rtByVesselThenPath, "rt-mode-vessel", "vessel › path"},
		} {
			if c.Button(ids.PrepareStr(m.key), c.Atoms().Text(m.label).Keep()).Selected(st.mode == m.mode).
				SendResp().HasPrimaryClicked() && st.mode != m.mode {
				st.setMode(m.mode)
				st.lastAction = "mode " + m.label
			}
			c.AddSpace(gapInline())
		}
		label := "aggregate: mean"
		if st.sum {
			label = "aggregate: sum"
		}
		if c.Button(ids.PrepareStr("rt-agg"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() {
			st.sum = !st.sum
			st.refit = true
			st.lastAction = "aggregate toggled"
		}
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("rt-expand"), c.Atoms().Text("expand all").Keep()).
			SendResp().HasPrimaryClicked() {
			st.groups.ExpandAll()
			st.lastAction = "expand all"
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("rt-collapse"), c.Atoms().Text("collapse all").Keep()).
			SendResp().HasPrimaryClicked() {
			st.groups.CollapseAll()
			st.lastAction = "collapse all"
		}
		c.AddSpace(gapInline())
		// Reveal needs the layout's help: the ancestors open in the next
		// flatten, which also assigns the line the grid then scrolls to, so
		// it is a request the render consumes rather than two calls here.
		if c.Button(ids.PrepareStr("rt-reveal"), c.Atoms().Text("reveal Calypso's engine temperature").Keep()).
			SendResp().HasPrimaryClicked() {
			st.groups.Reveal(st.revealTarget)
			st.selected = st.revealTarget
			st.lastAction = "reveal"
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("rt-clear"), c.Atoms().Text("clear selection").Keep()).
			SendResp().HasPrimaryClicked() {
			st.selected, st.selectedVessel = -1, -1
			st.lastAction = "clear selection"
		}
	}
	c.AddSpace(padInner())
}

// -----------------------------------------------------------------------------
// The grid
// -----------------------------------------------------------------------------

func recordTableGridSection(ids *c.WidgetIdStack, st *recordTableDemoState) {
	stdSection("readings — three vessels, exploded one row per attribute",
		"a band per group; the value column's aggregate under its header on the band's line; a band click folds, or selects the vessel")

	agg := recordtable.AggMean
	if st.sum {
		agg = recordtable.AggSum
	}
	// The aggregating column sits right of everything the band's label may
	// need: the label is clipped at the first column that aggregates.
	cols := []recordtable.Column{
		{Arrow: rtColVessel},
		{Arrow: rtColSection},
		{Arrow: rtColPath},
		{Arrow: rtColValue, Agg: agg},
		{Arrow: rtColUnit},
	}
	rec := readingsRec
	order := st.sort.OrderFor(rec, rec.NumRows())
	res := recordtable.RenderGrid(recordtable.Input{
		Ids:               ids,
		ScopeKey:          "readings",
		Record:            rec,
		Columns:           cols,
		Sort:              &st.sort,
		Order:             order,
		Selected:          st.selected,
		HasSelection:      st.selected >= 0,
		Groups:            &st.groups,
		SelectedKeyRow:    st.selectedVessel,
		HasSelectedKeyRow: st.selectedVessel >= 0,
		MaxHeight:         320,
		Refit:             st.refit,
		State:             &st.grid,
	})
	st.refit = false
	if res.Err != nil {
		return
	}
	// The readout names the gesture before Apply consumes it: the group's
	// label is read from the layout the gesture was made against.
	if g := res.GroupToggled; g >= 0 {
		st.lastAction = "toggled " + rtGroupLabel(st.groups.Layout(), g)
	}
	if res.GroupOrderClicked >= 0 {
		st.lastAction = "reversed level " + strconv.Itoa(res.GroupOrderClicked)
	}
	st.sort.Apply(res)
	st.groups.Apply(res)
	if res.Clicked >= 0 {
		st.selected = res.Clicked
		st.lastAction = "clicked row " + strconv.FormatInt(res.Clicked, 10)
	}
	if res.GroupClicked >= 0 {
		st.selectedVessel = res.GroupClicked
		st.lastAction = "clicked vessel " + readingsFixture[rtFirstReadingOf(res.GroupClicked)].Vessel
	}
}

// rtGroupLabel is a group's shown name, for the readout.
func rtGroupLabel(lay *recordtable.Layout, g int32) string {
	if lay == nil || int(g) >= len(lay.Groups) {
		return "?"
	}
	k := lay.Groups[g].Key
	switch {
	case k.Null:
		return "(null)"
	case k.Label != "":
		return k.Label
	}
	return k.Value
}

// rtFirstReadingOf is the first fixture row of a source vessel row.
func rtFirstReadingOf(src int64) int {
	for i, r := range readingsFixture {
		if r.Src == src {
			return i
		}
	}
	return 0
}

// -----------------------------------------------------------------------------
// Readout
// -----------------------------------------------------------------------------

// recordTableReadoutSection prints what the host state holds. It is what the
// ADR-0336 M2 headless scene asserts against: a driver reads a cell's text,
// not a band's layout, so it watches these lines change instead.
func recordTableReadoutSection(st *recordTableDemoState) {
	stdSection("readout", "the line count is the layout's; the selections are the host's")

	lines := readingsRec.NumRows()
	if st.groups.Active() {
		lines = st.groups.Layout().Len()
	}
	c.Label(fmt.Sprintf("lines: %d", lines)).Send()

	sel := "(nothing)"
	if st.selected >= 0 && st.selected < int64(len(readingsFixture)) {
		r := readingsFixture[st.selected]
		path := r.Path
		if path == "" {
			path = "(null)"
		}
		section := r.Section
		if section == "" {
			section = "(null)"
		}
		sel = r.Vessel + " " + section + " " + path
	}
	c.Label("selected: " + sel).Send()

	vessel := "(nothing)"
	if st.selectedVessel >= 0 {
		vessel = readingsFixture[rtFirstReadingOf(st.selectedVessel)].Vessel
	}
	c.Label("selected vessel: " + vessel).Send()
	c.Label("last action: " + st.lastAction).Send()
}
