package play

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bowtie"
)

// play_bowtie_panel.go is the Bow-tie dock tab: a result set drawn as a
// bow-tie diagram over widgets/bowtie — threats, prevention barriers in
// series, one top event, mitigation barriers in series, consequences
// (ADR-0122's 2026-11-01 update records the contract). The panel observes
// the active result on chMain, like Kanban.
//
// The contract is named columns rather than detection (ADR-0122 §SD1), one
// row per element of a path:
//
//	top_event  the loss the diagram is about — one value across the result
//	side       'prevention' (a threat's path) or 'mitigation' (a consequence's)
//	path       the threat or consequence
//	step       the barrier's place along its path, an integer; NULL for a row
//	           that names a path with no barrier
//	barrier    the barrier's name; NULL with the step
//	status     effective | degraded | failed | untested; empty is untested
//	evidence   the test behind the status, shown on hover (optional)
//
// One row per element rather than a nodes/edges pair: a bow-tie's edges are
// implied by side and step, so asking for them would only add a second place
// to get the order wrong.

const (
	bowtieTopEventCol = "top_event"
	bowtieSideCol     = "side"
	bowtiePathCol     = "path"
	bowtieStepCol     = "step"
	bowtieBarrierCol  = "barrier"
	bowtieStatusCol   = "status"
	bowtieEvidenceCol = "evidence"
	// bowtieMaxRows bounds the fold. A bow-tie is a tens-of-elements picture;
	// past a few hundred it is unreadable and its layout slow. The excess is
	// dropped and counted in the status line.
	bowtieMaxRows = 400
)

// bowtieClaim is the panel's channel claim: the resolved columns, -1 when an
// optional one is absent.
type bowtieClaim struct {
	topCol, sideCol, pathCol int
	stepCol, barrierCol      int
	statusCol, evidenceCol   int
	selRow                   int64
}

// BowtieDriver owns the Bow-tie tab state: the diagram widget and the model
// folded from the last result.
type BowtieDriver struct {
	diagram *bowtie.Diagram
	model   bowtie.Model
	// foldErr is the data-level reject of the last fold — a row the contract
	// cannot read, or rows that are not one bow-tie.
	foldErr   string
	truncated int64

	forExecuted     time.Time
	forSchema       *arrow.Schema
	pendingExecuted time.Time
	folded          bool
}

// NewBowtieDriver builds the driver.
func NewBowtieDriver(ids *c.WidgetIdStack) (inst *BowtieDriver) {
	inst = &BowtieDriver{diagram: bowtie.New(ids, "play-bowtie", bowtie.Options{})}
	return
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; the fold keys its cache on it.
func (inst *BowtieDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// bowtiePanel is the PanelI face; acceptance is schema-only.
type bowtiePanel struct {
	driver *BowtieDriver
}

func (inst bowtiePanel) ID() PanelID { return "bowtie" }

func (inst bowtiePanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "elements"}}
}

func (inst bowtiePanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see the bow-tie."
		return
	}
	k, reason := resolveBowtieColumns(schema)
	if reason != "" {
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst bowtiePanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(bowtieClaim)
	if !ok {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), k, emit)
}

// resolveBowtieColumns applies the contract to a schema. The text columns
// carry no type requirement (formatCell is total over Arrow types); `step` is
// an order and must be an integer.
func resolveBowtieColumns(schema *arrow.Schema) (k bowtieClaim, reason string) {
	k = bowtieClaim{
		topCol:      fieldIndexByLabel(schema, bowtieTopEventCol),
		sideCol:     fieldIndexByLabel(schema, bowtieSideCol),
		pathCol:     fieldIndexByLabel(schema, bowtiePathCol),
		stepCol:     fieldIndexByLabel(schema, bowtieStepCol),
		barrierCol:  fieldIndexByLabel(schema, bowtieBarrierCol),
		statusCol:   fieldIndexByLabel(schema, bowtieStatusCol),
		evidenceCol: fieldIndexByLabel(schema, bowtieEvidenceCol),
		selRow:      -1,
	}
	var missing []string
	for _, req := range []struct {
		col  int
		name string
	}{{k.topCol, bowtieTopEventCol}, {k.sideCol, bowtieSideCol}, {k.pathCol, bowtiePathCol}} {
		if req.col < 0 {
			missing = append(missing, "`"+req.name+"`")
		}
	}
	if len(missing) > 0 {
		reason = fmt.Sprintf("The bow-tie needs %s. One row per element of a path: "+
			"SELECT 'Data breach' AS top_event, 'prevention' AS side, 'Phishing' AS path, "+
			"1 AS step, 'MFA' AS barrier, 'effective' AS status — see the Snippets library's \"Bow-tie\".",
			strings.Join(missing, ", "))
		return
	}
	if k.barrierCol >= 0 && k.stepCol < 0 {
		reason = "A `barrier` column needs a `step` column beside it: the barrier's place along its path, 1 nearest the threat or the top event."
		return
	}
	if k.barrierCol >= 0 && k.statusCol < 0 {
		reason = "A `barrier` column needs a `status` column beside it — " + strings.Join(bowtie.StatusTokens(), ", ") +
			". Leaving it out would draw every barrier as untested."
		return
	}
	if k.stepCol >= 0 && !isBowtieStepType(schema.Field(k.stepCol).Type) {
		reason = fmt.Sprintf("The `step` column must be an integer (NULL for a path with no barrier); it is %s.", schema.Field(k.stepCol).Type)
		return
	}
	return
}

func isBowtieStepType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		return true
	}
	return false
}

// render folds the result (cached), draws it, and publishes a click as the
// row cursor: a barrier's own row, a path's first row. The top event is no
// one row, so clicking it publishes nothing.
func (inst *BowtieDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k bowtieClaim, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k)
	if inst.foldErr != "" {
		for rt := range c.RichTextLabel(inst.foldErr) {
			rt.Small().Weak()
		}
		return
	}
	if inst.model.TopEvent == "" {
		for rt := range c.RichTextLabel("The query returned no rows, so there is no bow-tie to draw.") {
			rt.Small().Weak()
		}
		return
	}
	c.Label(inst.statusLine()).Send()
	ev := inst.diagram.RenderFill(&inst.model, 900, 460)
	if row := bowtieRowOf(&inst.model, ev.Clicked); row >= 0 && row != k.selRow {
		emit.Emit(signalSelection, row)
	}
}

// bowtieRowOf is the result row an element was folded from, -1 for none.
// The fold reads rows in order and drops only a tail, so Source is the row.
func bowtieRowOf(m *bowtie.Model, el bowtie.Element) int64 {
	switch el.Kind {
	case bowtie.ElementPath, bowtie.ElementBarrier:
	default:
		return -1
	}
	paths := m.Paths(el.Side)
	if el.Path < 0 || el.Path >= len(paths) {
		return -1
	}
	p := paths[el.Path]
	if el.Kind == bowtie.ElementPath {
		return int64(p.Source)
	}
	if el.Barrier < 0 || el.Barrier >= len(p.Barriers) {
		return -1
	}
	return int64(p.Barriers[el.Barrier].Source)
}

// rebuild folds the result into a model, keyed on (executed, schema).
func (inst *BowtieDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k bowtieClaim) {
	if inst.folded && schema == inst.forSchema && inst.pendingExecuted.Equal(inst.forExecuted) {
		return
	}
	inst.folded = true
	inst.forSchema = schema
	inst.forExecuted = inst.pendingExecuted
	inst.model, inst.foldErr, inst.truncated = foldBowtie(rec, k)
}

// foldBowtie reads the claimed columns row by row into bowtie.Rows and hands
// them to bowtie.BuildModel. A cell the vocabulary does not know is refused
// with its row number rather than guessed at.
func foldBowtie(rec arrow.RecordBatch, k bowtieClaim) (m bowtie.Model, reject string, truncated int64) {
	rows := rec.NumRows()
	if rows > bowtieMaxRows {
		truncated = rows - bowtieMaxRows
		rows = bowtieMaxRows
	}
	cell := func(col int, row int64) string {
		if col < 0 {
			return ""
		}
		return formatCell(rec, col, row)
	}
	in := make([]bowtie.Row, 0, rows)
	for row := range rows {
		side, ok := bowtie.ParseSide(cell(k.sideCol, row))
		if !ok {
			reject = fmt.Sprintf("Row %d: `side` is %q — it must be 'prevention' (a threat's path) or 'mitigation' (a consequence's).",
				row+1, cell(k.sideCol, row))
			return
		}
		st, ok := bowtie.ParseStatus(cell(k.statusCol, row))
		if !ok {
			reject = fmt.Sprintf("Row %d: `status` is %q — it must be one of %s, or empty for untested.",
				row+1, cell(k.statusCol, row), strings.Join(bowtie.StatusTokens(), ", "))
			return
		}
		r := bowtie.Row{
			TopEvent: cell(k.topCol, row),
			Side:     side,
			Path:     cell(k.pathCol, row),
			Barrier:  cell(k.barrierCol, row),
			Status:   st,
			Evidence: cell(k.evidenceCol, row),
		}
		if k.stepCol >= 0 {
			if v, has := numericCellValue(rec.Column(k.stepCol), row); has && !math.IsInf(v, 0) {
				r.Step, r.HasStep = int64(v), true
			}
		}
		in = append(in, r)
	}
	m, err := bowtie.BuildModel(in)
	if err != nil {
		reject = "These rows are not one bow-tie: " + firstLineOf(err.Error())
	}
	return
}

func (inst *BowtieDriver) statusLine() string {
	m := &inst.model
	var counts [4]int
	barriers := 0
	for _, p := range append(append([]bowtie.Path(nil), m.Threats...), m.Consequences...) {
		for _, b := range p.Barriers {
			counts[b.Status]++
			barriers++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s · %d %s · %d %s",
		len(m.Threats), plural(len(m.Threats), "threat", "threats"),
		len(m.Consequences), plural(len(m.Consequences), "consequence", "consequences"),
		barriers, plural(barriers, "barrier", "barriers"))
	for _, s := range [...]bowtie.StatusE{bowtie.StatusFailed, bowtie.StatusDegraded, bowtie.StatusUntested} {
		if n := counts[s]; n > 0 {
			fmt.Fprintf(&b, " · %d %s", n, s.Token())
		}
	}
	if inst.truncated > 0 {
		fmt.Fprintf(&b, " · %s more rows not shown (the pane reads at most %d)", humanize.Comma(inst.truncated), bowtieMaxRows)
	}
	return b.String()
}

// paneStatus is the Bow-tie's last draw.
func (inst *BowtieDriver) paneStatus() (line string, reject string) {
	switch {
	case !inst.folded:
	case inst.foldErr != "":
		reject = inst.foldErr
	case inst.model.TopEvent == "":
		reject = "The query returned no rows, so there is no bow-tie to draw."
	default:
		line = inst.statusLine()
	}
	return
}

// renderBowtieTab is the Bow-tie dock tab body: the active result as a
// bow-tie, with the guards every active-result pane has.
func (inst *PlayApp) renderBowtieTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query with `top_event`, `side`, `path`, `step`, `barrier` and `status` columns to see a bow-tie.") {
			rt.Small().Weak()
		}
		return
	}
	inst.bowtieDriver.noteExecuted(executed)
	reject := dispatchPanel(bowtiePanel{driver: inst.bowtieDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("bowtie"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
