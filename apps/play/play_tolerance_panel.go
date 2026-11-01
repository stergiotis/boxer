package play

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/impacttolerance"
)

// play_tolerance_panel.go is the Impact tolerance dock tab: important services
// against their impact tolerances under named scenarios, drawn over
// widgets/impacttolerance (ADR-0122's 2026-11-01 update records the contract).
// The panel observes the active result on chMain, like Kanban.
//
// The contract is named columns rather than detection (ADR-0122 §SD1), one row
// per (service, scenario) estimate — the long form a scenario exercise
// produces:
//
//	service          required, any type — the row; first-seen order is row order
//	tolerance        required, numeric — the service's impact tolerance, repeated
//	                 on each of its rows
//	scenario         required, any type — first-seen order is legend order
//	recovery         required, numeric — the estimated time to recover; NULL for
//	                 a pair with no estimate
//
// A service with no estimate yet is one row with a NULL scenario.
//
// `tolerance` and `recovery` carry their unit in a gloss/duration declaration,
// each its own: spans in different units are the same quantity and are
// converted, to hours, which is what the widget compares in. `tolerance_hours`
// and `recovery_hours` are the shorthand for a column in hours. A bare number
// named `tolerance` has no unit, and is refused rather than read as hours.

const (
	tolServiceCol   = "service"
	tolToleranceCol = "tolerance"
	tolScenarioCol  = "scenario"
	tolRecoveryCol  = "recovery"
	// tolHoursSuffix makes a time column's shorthand, whose name is its unit.
	tolHoursSuffix = "_hours"
	// tolMaxServices bounds the fold: a row per service, so past a few dozen
	// the chart is a scroll, not a picture. The excess is counted.
	tolMaxServices = 60
	// tolMaxRows bounds the rows read at all.
	tolMaxRows = 5000
	// tolPaneProbeSalt keys the pane-size probe under the driver's stack.
	tolPaneProbeSalt uint64 = 0x7015c0de0a710036
	// tolNanosPerHour is the widget's internal unit in the gloss's.
	tolNanosPerHour = 3600e9
)

// tolerancePaneFill sizes the chart's width; its height follows the rows.
var tolerancePaneFill = paneFill{
	slack: 12, minW: 420, maxW: 1600, minH: 200,
	fallbackW: 900, fallbackH: 400,
}

// toleranceClaim is the panel's channel claim.
type toleranceClaim struct {
	serviceCol, toleranceCol, scenarioCol, recoveryCol int
	// toleranceToHours and recoveryToHours convert each time column to the
	// widget's hours.
	toleranceToHours, recoveryToHours float64
	selRow                            int64
}

// ToleranceDriver owns the Impact tolerance tab state: the widget's selection
// and the model folded from the last result.
type ToleranceDriver struct {
	ids   *c.WidgetIdStack
	state impacttolerance.State
	model impacttolerance.Model
	// rowAt maps a (service, scenario) point to its result row; pointAt is
	// the inverse, for following the shared selection.
	rowAt   map[impacttolerance.Point]int64
	pointAt map[int64]impacttolerance.Point

	// conflicts counts services whose rows disagree on the tolerance (the
	// first wins), duplicates repeated (service, scenario) pairs (the first
	// wins), skipped rows the contract cannot read, dropped services past
	// the cap.
	conflicts, duplicates, skipped, dropped int

	paneW float32

	forExecuted     time.Time
	forSchema       *arrow.Schema
	pendingExecuted time.Time
	folded          bool
}

// NewToleranceDriver builds the driver.
func NewToleranceDriver(ids *c.WidgetIdStack) (inst *ToleranceDriver) {
	return &ToleranceDriver{ids: ids}
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; the fold keys its cache on it.
func (inst *ToleranceDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// tolerancePanel is the PanelI face; acceptance is schema-only.
type tolerancePanel struct {
	driver *ToleranceDriver
}

func (inst tolerancePanel) ID() PanelID { return "tolerance" }

func (inst tolerancePanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "estimates"}}
}

func (inst tolerancePanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see impact tolerances."
		return
	}
	k, reason := resolveToleranceColumns(schema)
	if reason != "" {
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst tolerancePanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(toleranceClaim)
	if !ok {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), k, emit)
}

// resolveToleranceColumns applies the contract to a schema.
func resolveToleranceColumns(schema *arrow.Schema) (k toleranceClaim, reason string) {
	k = toleranceClaim{
		serviceCol:  fieldIndexByLabel(schema, tolServiceCol),
		scenarioCol: fieldIndexByLabel(schema, tolScenarioCol),
		selRow:      -1,
	}
	k.toleranceCol, k.toleranceToHours, reason = resolveToleranceTime(schema, tolToleranceCol)
	if reason != "" {
		return k, reason
	}
	k.recoveryCol, k.recoveryToHours, reason = resolveToleranceTime(schema, tolRecoveryCol)
	if reason != "" {
		return k, reason
	}
	var missing []string
	for _, req := range []struct {
		col  int
		name string
	}{{k.serviceCol, tolServiceCol}, {k.toleranceCol, tolToleranceCol + tolHoursSuffix}, {k.scenarioCol, tolScenarioCol}, {k.recoveryCol, tolRecoveryCol + tolHoursSuffix}} {
		if req.col < 0 {
			missing = append(missing, "`"+req.name+"`")
		}
	}
	if len(missing) > 0 {
		return k, "Impact tolerance needs " + strings.Join(missing, ", ") + ". Name them in the query — one row per service and scenario: " +
			"SELECT svc AS service, mtd_h AS tolerance_hours, exercise AS scenario, rto_h AS recovery_hours FROM estimates — " +
			"or say the unit with a gloss: rto_m AS `recovery@gloss/duration;unit=min`."
	}
	return k, ""
}

// resolveToleranceTime finds one time column of the contract, under its plain
// name or its hours shorthand, and the factor from its stored unit to hours;
// col is -1 when the result carries neither.
func resolveToleranceTime(schema *arrow.Schema, name string) (col int, toHours float64, reason string) {
	shorthand := name + tolHoursSuffix
	plain, inHours := fieldIndexByLabel(schema, name), fieldIndexByLabel(schema, shorthand)
	switch {
	case plain >= 0 && inHours >= 0:
		return -1, 0, "Both `" + name + "` and `" + shorthand + "` are present; keep one, since the pane cannot tell which to draw."
	case plain < 0 && inHours < 0:
		return -1, 0, ""
	}
	col = plain
	if plain < 0 {
		col = inHours
	}
	f := schema.Field(col)
	if !isRiskNumberType(f.Type) {
		if plain < 0 {
			return col, 0, fmt.Sprintf("Column `%s` must be a number of hours; it is %s.", f.Name, f.Type)
		}
		return col, 0, fmt.Sprintf("Column `%s` must be numeric, a span of time; it is %s.", f.Name, f.Type)
	}
	u, why := columnUnitOf(f)
	if why != "" {
		return col, 0, why
	}
	if u.declared() && u.mediaType != gloss.MediaTypeDuration {
		return col, 0, fmt.Sprintf("Column `%s` must be a span of time; it declares %s.", pathColumnLabel(f.Name), u)
	}
	if plain < 0 {
		if u.declared() && u.unit != gloss.UnitHour {
			return col, 0, fmt.Sprintf("Column `%s` says hours in its name and %s in its gloss. Name it `%s`, or declare unit=h.", shorthand, u, name)
		}
		return col, 1, ""
	}
	if !u.declared() {
		return col, 0, fmt.Sprintf("Column `%[1]s` has no unit. Declare it, `%[1]s@gloss/duration;unit=h` — or ns, us, ms, s, min — or name it `%[2]s`.", name, shorthand)
	}
	ns, ok := gloss.TimeUnitNanos(u.unit)
	if !ok {
		return col, 0, fmt.Sprintf("Column `%s` declares %s, a unit this pane cannot convert.", name, u)
	}
	return col, ns / tolNanosPerHour, ""
}

// rebuild folds the long-form rows into the service × scenario matrix, keyed
// on (executed, schema). Services and scenarios keep their first-seen order,
// so the query's ORDER BY decides the rows and the legend.
func (inst *ToleranceDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k toleranceClaim) {
	if inst.folded && schema == inst.forSchema && inst.pendingExecuted.Equal(inst.forExecuted) {
		return
	}
	inst.folded, inst.forSchema, inst.forExecuted = true, schema, inst.pendingExecuted
	inst.conflicts, inst.duplicates, inst.skipped, inst.dropped = 0, 0, 0, 0
	rows := min(rec.NumRows(), tolMaxRows)
	inst.skipped = int(rec.NumRows() - rows)

	type est struct {
		v   float64
		row int64
	}
	var services, scenarios []string
	svcIdx, scnIdx := map[string]int{}, map[string]int{}
	var tol []float64
	conflicted := map[int]bool{}
	ests := map[impacttolerance.Point]est{}
	for row := range rows {
		svc := formatCell(rec, k.serviceCol, row)
		s, seen := svcIdx[svc]
		if !seen {
			if len(services) >= tolMaxServices {
				inst.dropped++
				continue
			}
			s = len(services)
			svcIdx[svc] = s
			services = append(services, svc)
			tol = append(tol, math.NaN())
		}
		if t, ok := numericCellValue(rec.Column(k.toleranceCol), row); ok && t > 0 {
			t *= k.toleranceToHours
			switch {
			case math.IsNaN(tol[s]):
				tol[s] = t
			case tol[s] != t && !conflicted[s]:
				conflicted[s] = true
				inst.conflicts++
			}
		}
		if rec.Column(k.scenarioCol).IsNull(int(row)) {
			continue
		}
		scn := formatCell(rec, k.scenarioCol, row)
		sc, seenScn := scnIdx[scn]
		if !seenScn {
			sc = len(scenarios)
			scnIdx[scn] = sc
			scenarios = append(scenarios, scn)
		}
		v, ok := numericCellValue(rec.Column(k.recoveryCol), row)
		if !ok {
			continue // a pair with no estimate yet
		}
		if !(v > 0) {
			inst.skipped++
			continue
		}
		p := impacttolerance.Point{Service: s, Scenario: sc}
		if _, dup := ests[p]; dup {
			inst.duplicates++
			continue
		}
		ests[p] = est{v: v * k.recoveryToHours, row: row}
	}
	// A service with no usable tolerance cannot be drawn against one; it is
	// dropped and counted rather than drawn at an invented line.
	keep := make([]int, 0, len(services))
	for s := range services {
		if math.IsNaN(tol[s]) {
			inst.skipped++
			continue
		}
		keep = append(keep, s)
	}
	m := impacttolerance.Model{Scenarios: scenarios}
	inst.rowAt = make(map[impacttolerance.Point]int64, len(ests))
	inst.pointAt = make(map[int64]impacttolerance.Point, len(ests))
	for ns, s := range keep {
		m.Services = append(m.Services, services[s])
		m.ToleranceHours = append(m.ToleranceHours, tol[s])
		for sc := range scenarios {
			e, ok := ests[impacttolerance.Point{Service: s, Scenario: sc}]
			if !ok {
				m.RecoveryHours = append(m.RecoveryHours, math.NaN())
				continue
			}
			m.RecoveryHours = append(m.RecoveryHours, e.v)
			p := impacttolerance.Point{Service: ns, Scenario: sc}
			inst.rowAt[p], inst.pointAt[e.row] = e.row, p
		}
	}
	inst.model = m
	inst.state.SetSelected(impacttolerance.NoPoint)
}

// render folds the result, draws the chart, and carries the selection both
// ways — the shared cursor in, a clicked estimate out.
func (inst *ToleranceDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k toleranceClaim, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k)
	c.Label(inst.statusLine()).Send()
	if len(inst.model.Services) == 0 {
		for rt := range c.RichTextLabel("No service carries a positive tolerance, so there is nothing to draw.") {
			rt.Small().Weak()
		}
		return
	}
	if p, ok := inst.pointAt[k.selRow]; ok && k.selRow >= 0 {
		inst.state.SetSelected(p)
	} else {
		inst.state.SetSelected(impacttolerance.NoPoint)
	}
	c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
	if w, _, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(tolPaneProbeSalt).Derive()); ok {
		inst.paneW = w
	}
	w, _ := tolerancePaneFill.box(inst.paneW, 0)
	res := impacttolerance.Render(impacttolerance.Input{
		Ids: inst.ids, ScopeKey: "play-tolerance",
		Model: &inst.model, State: &inst.state,
		Width: w,
	})
	if res.Clicked.Valid() {
		if row, ok := inst.rowAt[res.Clicked]; ok && row != k.selRow {
			emit.Emit(signalSelection, row)
		}
	}
}

func (inst *ToleranceDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d services · %d scenarios", len(inst.model.Services), len(inst.model.Scenarios))
	breaching := 0
	for s := range inst.model.Services {
		for sc := range inst.model.Scenarios {
			if v := inst.model.Recovery(s, sc); v > inst.model.ToleranceHours[s] {
				breaching++
				break
			}
		}
	}
	fmt.Fprintf(&b, " · %d breach under at least one scenario", breaching)
	if inst.conflicts > 0 {
		fmt.Fprintf(&b, " · %d services carry more than one tolerance (the first row's is drawn)", inst.conflicts)
	}
	if inst.duplicates > 0 {
		fmt.Fprintf(&b, " · %d repeated service/scenario rows ignored", inst.duplicates)
	}
	if inst.skipped > 0 {
		fmt.Fprintf(&b, " · %s rows skipped (no positive tolerance or recovery)", humanize.Comma(int64(inst.skipped)))
	}
	if inst.dropped > 0 {
		fmt.Fprintf(&b, " · %s rows past the %d-service cap not shown", humanize.Comma(int64(inst.dropped)), tolMaxServices)
	}
	return b.String()
}

// paneStatus is the chart's last draw, for list_panes.
func (inst *ToleranceDriver) paneStatus() (line string, reject string) {
	switch {
	case !inst.folded:
	case len(inst.model.Services) == 0:
		reject = "No service carries a positive tolerance, so there is nothing to draw."
	default:
		line = inst.statusLine()
	}
	return
}

// renderToleranceTab is the Impact tolerance dock tab body: a plain PanelI
// observer with the Kanban tab's guards.
func (inst *PlayApp) renderToleranceTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query naming `service`, `tolerance_hours`, `scenario` and `recovery_hours` columns, or `tolerance` and `recovery` carrying gloss/duration, to see impact tolerances.") {
			rt.Small().Weak()
		}
		return
	}
	inst.toleranceDriver.noteExecuted(executed)
	reject := dispatchPanel(tolerancePanel{driver: inst.toleranceDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("tolerance"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
