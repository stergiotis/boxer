package play

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/riskmatrix"
)

// play_riskmatrix_panel.go is the Risk matrix dock tab: a risk register drawn
// over widgets/riskmatrix (ADR-0122's 2026-11-01 update records the
// contract). The panel observes the active result on chMain, like Kanban.
//
// The contract is named columns rather than detection (ADR-0122 §SD1), one row
// per risk:
//
//	id                      required, any type — the mark's label
//	name                    optional, any type — the key and the tooltip
//	p_lo, p_hi              required, numeric — likelihood per year, as a range
//	impact_lo, impact_hi    required, numeric — impact if it happens, as a range
//	inherent_p_lo, inherent_p_hi, inherent_impact_lo, inherent_impact_hi
//	                        optional, numeric, all four or none — the same
//	                        before controls; a NULL row has no inherent rating
//
// Two numeric ranges cannot be told apart from a schema, so the pane asks for
// one `AS` per column instead of guessing which is likelihood.

const (
	riskIdCol       = "id"
	riskNameCol     = "name"
	riskPLoCol      = "p_lo"
	riskPHiCol      = "p_hi"
	riskImpactLoCol = "impact_lo"
	riskImpactHiCol = "impact_hi"
	riskInhPrefix   = "inherent_"
	// riskMaxRows bounds the fold. A matrix is a tens-of-risks picture; past
	// a few hundred the dots are one blot. The excess is dropped and counted
	// in the status line.
	riskMaxRows = 500
	// riskPaneProbeSalt keys the pane-size probe under the driver's stack.
	riskPaneProbeSalt uint64 = 0x7215c0de0a710035
)

// riskResidualCols are the required numeric columns, in Model order.
var riskResidualCols = [4]string{riskPLoCol, riskPHiCol, riskImpactLoCol, riskImpactHiCol}

// riskMatrixPaneFill is the canvas box — the shared pane rule
// (play_pane_box.go). The chrome is the widget's own control row above the
// canvas.
var riskMatrixPaneFill = paneFill{
	slack: 12, minW: 420, maxW: 1600, minH: 320,
	fallbackW: 900, fallbackH: 540,
	chrome: 40,
}

// riskMatrixClaim is the panel's channel claim: the resolved columns, -1 when
// an optional one is absent.
type riskMatrixClaim struct {
	idCol, nameCol int
	residual       [4]int
	inherent       [4]int // all -1 when the result carries none
	selRow         int64
}

// RiskMatrixDriver owns the Risk matrix tab state: the widget's state and the
// model folded from the last result.
type RiskMatrixDriver struct {
	ids   *c.WidgetIdStack
	state riskmatrix.State
	model riskmatrix.Model
	// rows maps a model index to its result row; rowOf is the inverse, for
	// following the shared selection.
	rows  []int64
	rowOf map[int64]int

	skipped, truncated int64

	paneW, paneH float32

	forExecuted     time.Time
	forSchema       *arrow.Schema
	pendingExecuted time.Time
	folded          bool
}

// NewRiskMatrixDriver builds the driver.
func NewRiskMatrixDriver(ids *c.WidgetIdStack) (inst *RiskMatrixDriver) {
	return &RiskMatrixDriver{ids: ids}
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; the fold keys its cache on it.
func (inst *RiskMatrixDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// riskMatrixPanel is the PanelI face; acceptance is schema-only.
type riskMatrixPanel struct {
	driver *RiskMatrixDriver
}

func (inst riskMatrixPanel) ID() PanelID { return "riskmatrix" }

func (inst riskMatrixPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "risks"}}
}

func (inst riskMatrixPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see the risk matrix."
		return
	}
	k, reason := resolveRiskMatrixColumns(schema)
	if reason != "" {
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst riskMatrixPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(riskMatrixClaim)
	if !ok {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), k, emit)
}

// resolveRiskMatrixColumns applies the contract to a schema. `id` and `name`
// are read through formatCell and carry no type requirement; the ranges are
// numbers on a log axis, and a textual one would be a meaningless matrix.
func resolveRiskMatrixColumns(schema *arrow.Schema) (k riskMatrixClaim, reason string) {
	k = riskMatrixClaim{
		idCol:   fieldIndexByLabel(schema, riskIdCol),
		nameCol: fieldIndexByLabel(schema, riskNameCol),
		selRow:  -1,
	}
	var missing []string
	if k.idCol < 0 {
		missing = append(missing, "`"+riskIdCol+"`")
	}
	for i, name := range riskResidualCols {
		k.residual[i] = fieldIndexByLabel(schema, name)
		if k.residual[i] < 0 {
			missing = append(missing, "`"+name+"`")
		}
	}
	if len(missing) > 0 {
		return k, "The risk matrix needs " + strings.Join(missing, ", ") + ". Name them in the query — " +
			"one row per risk: SELECT ref AS id, title AS name, p_low AS p_lo, p_high AS p_hi, loss_low AS impact_lo, loss_high AS impact_hi FROM register — " +
			"with likelihood per year and impact as ranges; inherent_p_lo, inherent_p_hi, inherent_impact_lo and inherent_impact_hi add the rating before controls."
	}
	var inhMissing []string
	present := 0
	for i, name := range riskResidualCols {
		k.inherent[i] = fieldIndexByLabel(schema, riskInhPrefix+name)
		if k.inherent[i] < 0 {
			inhMissing = append(inhMissing, "`"+riskInhPrefix+name+"`")
		} else {
			present++
		}
	}
	if present > 0 && present < len(riskResidualCols) {
		return k, "The inherent rating is all four columns or none; this result lacks " + strings.Join(inhMissing, ", ") + "."
	}
	cols := k.residual[:]
	if present > 0 {
		cols = append(cols, k.inherent[:]...)
	}
	for _, ci := range cols {
		if f := schema.Field(ci); !isRiskNumberType(f.Type) {
			return k, fmt.Sprintf("Column `%s` must be numeric; it is %s.", f.Name, f.Type)
		}
	}
	return k, ""
}

// isRiskNumberType reports the types a likelihood or an amount may carry.
func isRiskNumberType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64,
		arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64,
		arrow.DECIMAL32, arrow.DECIMAL64, arrow.DECIMAL128, arrow.DECIMAL256:
		return true
	}
	return false
}

// rebuild folds the result into a model, keyed on (executed, schema). A row
// whose residual range is not positive cannot sit on a log axis: it is
// skipped and counted, not drawn at an invented place. A range written high
// to low is still a range and is read as one.
func (inst *RiskMatrixDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k riskMatrixClaim) {
	if inst.folded && schema == inst.forSchema && inst.pendingExecuted.Equal(inst.forExecuted) {
		return
	}
	inst.folded, inst.forSchema, inst.forExecuted = true, schema, inst.pendingExecuted
	rows := rec.NumRows()
	inst.truncated, inst.skipped = 0, 0
	if rows > riskMaxRows {
		inst.truncated = rows - riskMaxRows
		rows = riskMaxRows
	}
	hasInh := k.inherent[0] >= 0
	m := riskmatrix.Model{}
	inst.rows = inst.rows[:0]
	inst.rowOf = make(map[int64]int, rows)
	pair := func(cols [4]int, row int64) (v [4]float64, ok bool) {
		ok = true
		for i, ci := range cols {
			x, has := numericCellValue(rec.Column(ci), row)
			if !has || !(x > 0) || math.IsInf(x, 1) {
				x, ok = math.NaN(), false
			}
			v[i] = x
		}
		v[0], v[1] = min(v[0], v[1]), max(v[0], v[1])
		v[2], v[3] = min(v[2], v[3]), max(v[2], v[3])
		return
	}
	for row := range rows {
		res, ok := pair(k.residual, row)
		if !ok {
			inst.skipped++
			continue
		}
		inst.rowOf[row] = len(inst.rows)
		inst.rows = append(inst.rows, row)
		id := formatCell(rec, k.idCol, row)
		m.Ids = append(m.Ids, id)
		if k.nameCol >= 0 {
			m.Names = append(m.Names, formatCell(rec, k.nameCol, row))
		}
		m.PLo, m.PHi = append(m.PLo, res[0]), append(m.PHi, res[1])
		m.ImpactLo, m.ImpactHi = append(m.ImpactLo, res[2]), append(m.ImpactHi, res[3])
		if hasInh {
			inh, whole := pair(k.inherent, row)
			if !whole {
				inh = [4]float64{math.NaN(), math.NaN(), math.NaN(), math.NaN()}
			}
			m.InherentPLo, m.InherentPHi = append(m.InherentPLo, inh[0]), append(m.InherentPHi, inh[1])
			m.InherentImpactLo, m.InherentImpactHi = append(m.InherentImpactLo, inh[2]), append(m.InherentImpactHi, inh[3])
		}
	}
	inst.model = m
	inst.state.SetSelected(-1)
}

// render folds the result, draws the matrix, and carries the selection both
// ways — the shared cursor in, a clicked risk out.
func (inst *RiskMatrixDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k riskMatrixClaim, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k)
	c.Label(inst.statusLine()).Send()
	if inst.model.Len() == 0 {
		for rt := range c.RichTextLabel("No row carries a positive likelihood and impact range, so there is nothing to place.") {
			rt.Small().Weak()
		}
		return
	}
	if idx, ok := inst.rowOf[k.selRow]; ok && k.selRow >= 0 {
		inst.state.SetSelected(idx)
	} else {
		inst.state.SetSelected(-1)
	}
	c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
	// The probe sits right above the widget: the rect is the room left for
	// the next widget, so one placed after it would size it against itself.
	if w, h, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(riskPaneProbeSalt).Derive()); ok {
		inst.paneW, inst.paneH = w, h
	}
	w, h := riskMatrixPaneFill.box(inst.paneW, inst.paneH)
	res := riskmatrix.Render(riskmatrix.Input{
		Ids: inst.ids, ScopeKey: "play-riskmatrix",
		Model: &inst.model, State: &inst.state,
		Width: w, Height: h,
	})
	if res.Clicked >= 0 && res.Clicked < len(inst.rows) {
		if row := inst.rows[res.Clicked]; row != k.selRow {
			emit.Emit(signalSelection, row)
		}
	}
}

func (inst *RiskMatrixDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d risks", inst.model.Len())
	if inst.model.HasInherent() {
		b.WriteString(" · with inherent ratings")
	}
	if inst.skipped > 0 {
		fmt.Fprintf(&b, " · %s rows skipped (a likelihood or impact that is not a positive number)", humanize.Comma(inst.skipped))
	}
	if inst.truncated > 0 {
		fmt.Fprintf(&b, " · %s more rows not shown (the matrix caps at %d)", humanize.Comma(inst.truncated), riskMaxRows)
	}
	return b.String()
}

// paneStatus is the matrix's last draw, for list_panes.
func (inst *RiskMatrixDriver) paneStatus() (line string, reject string) {
	switch {
	case !inst.folded:
	case inst.model.Len() == 0:
		reject = "No row carries a positive likelihood and impact range, so there is nothing to place."
	default:
		line = inst.statusLine()
	}
	return
}

// renderRiskMatrixTab is the Risk matrix dock tab body: a plain PanelI
// observer with the Kanban tab's guards, plus the executed timestamp handed to
// the driver as its fold-cache key.
func (inst *PlayApp) renderRiskMatrixTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query naming `id`, `p_lo`, `p_hi`, `impact_lo` and `impact_hi` columns to see a risk matrix.") {
			rt.Small().Weak()
		}
		return
	}
	inst.riskMatrixDriver.noteExecuted(executed)
	reject := dispatchPanel(riskMatrixPanel{driver: inst.riskMatrixDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("riskmatrix"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
