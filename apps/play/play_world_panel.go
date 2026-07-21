package play

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// play_world_panel.go is the ADR-0114 World dock tab: a schematic world
// choropleth over the observed node's result. The panel is a plain PanelI
// observer (chMain), like Table — no panel-local lane, no query of its own.
// Column selection is data-driven (SD5): the country column is the first
// string column, name-hinted ones first, where at least half the sampled
// distinct values resolve against the widget's atlas; the value column
// defaults to the first numeric column, switchable via a combo. Duplicate
// country rows: last wins, counted in the status line — the pane never
// aggregates on the user's behalf (GROUP BY is the SQL author's tool).

const (
	// worldDetectMaxRows / worldDetectMaxDistinct bound the detection sample:
	// enough to judge a column, immune to huge results.
	worldDetectMaxRows     = 4096
	worldDetectMaxDistinct = 512
	// worldResolveThreshold is SD5's claim bar: the fraction of sampled
	// distinct values that must resolve to a country.
	worldResolveThreshold = 0.5
)

// signalSelectionCountry is the click companion to signalSelection: when a
// country is clicked the panel also publishes the clicked row's country cell
// as a string, so a query can cross-filter on {selection_country:String}. It
// mirrors the selection_node / selection_id companions the selection stamper
// attaches to every selection (play_bindings.go) — a typed, human-meaningful
// handle on the same click, here the country rather than the graph node or id.
const signalSelectionCountry SignalID = "selection_country"

// worldClaim is the panel's channel claim: the detected country column (-1 =
// none resolved — the pane renders its contract hint instead of a map).
type worldClaim struct {
	countryCol int
}

// WorldDriver owns the World tab state: the worldmap widget, the value-column
// choice, and the per-result extraction cache.
type WorldDriver struct {
	ids    *c.WidgetIdStack
	widget *worldmap.Widget

	// valueCol is the user's value-column pick (index into the schema);
	// worldValueAuto means "first numeric column". Persisted across runs;
	// falls back to auto when the column vanishes from the schema.
	valueCol int

	// Extraction cache key: the result identity (executed timestamp — the
	// same freshness token the pager uses) + the columns that fed it.
	forExecuted time.Time
	forCountry  int
	forValue    int
	forSchema   *arrow.Schema

	// Extraction outputs (render-thread only).
	rowOf     map[worldmap.CountryIdx]int64
	matched   int // countries with at least one resolved row
	unmatched int // rows whose country cell resolved to nothing
	dupes     int // resolved rows beyond the first per country (last wins)

	// pendingExecuted is stashed by renderWorldTab before dispatch — the
	// PanelI Render signature carries no result metadata (see noteExecuted).
	pendingExecuted time.Time

	// widthDraft backs the toolbar width slider (its own source of truth; the
	// widget quantizes + debounces in SetPixelWidth).
	widthDraft float64

	// detectFor caches detection per schema pointer (detection samples data,
	// but its outcome is stable for one result).
	detectFor *arrow.Schema
	detectCol int
}

const worldValueAuto = -1

func NewWorldDriver(ids *c.WidgetIdStack) *WorldDriver {
	d := &WorldDriver{
		ids:       ids,
		widget:    worldmap.New(ids, "world"),
		valueCol:  worldValueAuto,
		rowOf:     map[worldmap.CountryIdx]int64{},
		detectCol: -1,
	}
	d.widthDraft = d.widget.PixelWidth()
	return d
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; Render uses it to key the extraction cache.
func (inst *WorldDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// worldPanel is the PanelI face (ADR-0097 slice 4): schema-only acceptance —
// the data-driven column pick happens in Render, where the record is at hand.
type worldPanel struct {
	driver *WorldDriver
}

func (inst worldPanel) ID() PanelID { return "world" }

func (inst worldPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "countries"}}
}

// AcceptForChannel claims any result with a string-typed column — the
// cheapest schema-only precondition for "names countries". Whether the
// values actually resolve is data work, done (and cached) in Render; a
// result whose strings resolve nowhere renders the contract hint in-pane.
func (inst worldPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see the world map."
		return
	}
	for _, f := range schema.Fields() {
		if isWorldStringType(f.Type) {
			claim = worldClaim{countryCol: -1} // resolved in Render
			return
		}
	}
	reason = "The world map needs a text column carrying ISO country codes or country names."
	return
}

func (inst worldPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	inst.driver.render(main.Rec, main.Rec.Schema(), emit)
}

// render is the tab body on a claimed result: detect columns, extract (cached
// per result), draw the toolbar + widget, and emit a selection on click.
func (inst *WorldDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, emit SignalEmitterI) {
	atlas := inst.widget.Atlas()
	if atlas == nil {
		// Widget renders the load error itself.
		inst.widget.Render()
		return
	}
	countryCol := inst.detectCountryColumn(rec, schema, atlas)
	if countryCol < 0 {
		for rt := range c.RichTextLabel("No column resolves to countries (needs ISO 3166 alpha-2/alpha-3 codes or country names in at least half of its sampled distinct values).") {
			rt.Small().Weak()
		}
		return
	}
	numeric := numericColumns(schema)
	valueCol := inst.effectiveValueCol(numeric)
	inst.extract(rec, schema, countryCol, valueCol, atlas)

	for range c.Horizontal().KeepIter() {
		c.Label("country: " + schema.Field(countryCol).Name).Send()
		if len(numeric) > 0 {
			c.Separator().Vertical().Send()
			inst.renderValueCombo(schema, numeric)
		}
		c.Separator().Vertical().Send()
		// Raster width is an explicit control (like the Map panel's size):
		// the R18 available-size capture is a single global register already
		// owned by the editor pane. The widget quantizes + debounces.
		c.SliderF64(inst.ids.PrepareStr("world-width"), inst.widthDraft, 320, 2048).
			Text("width").SendRespVal(&inst.widthDraft)
		inst.widget.SetPixelWidth(inst.widthDraft)
	}
	// Status on its own row — sharing the toolbar row clips it against the
	// Detail split at common pane widths.
	c.Label(inst.statusLine(rec.NumRows(), valueCol, schema)).Send()

	if clicked, ok := inst.widget.Render(); ok {
		if row, found := inst.rowOf[clicked]; found {
			emit.Emit(signalSelection, row)
			// Companion string to the selection (cf. the selection_node /
			// selection_id stamper in play_bindings.go): publish the clicked
			// row's country cell so a query can cross-filter on
			// {selection_country:String} — the read side of a per-country
			// drill-down (SD7), no panel-run query. formatCell is the same
			// reader the country detector uses, so the value matches the data.
			emit.Emit(signalSelectionCountry, formatCell(rec, countryCol, row))
		}
	}
}

// effectiveValueCol maps the persisted pick onto the current schema: auto →
// first numeric; a stale explicit pick (column gone or no longer numeric)
// also falls back to auto rather than latching an arbitrary column.
func (inst *WorldDriver) effectiveValueCol(numeric []int) int {
	if len(numeric) == 0 {
		return -1
	}
	if inst.valueCol != worldValueAuto {
		for _, ci := range numeric {
			if ci == inst.valueCol {
				return ci
			}
		}
		inst.valueCol = worldValueAuto
	}
	return numeric[0]
}

// renderValueCombo is the value-column picker (the colorByFeature precedent):
// explicit stable ids per option, "auto" resets to first-numeric.
func (inst *WorldDriver) renderValueCombo(schema *arrow.Schema, numeric []int) {
	cur := "auto"
	if inst.valueCol != worldValueAuto {
		cur = schema.Field(inst.valueCol).Name
	}
	for range c.ComboBox(inst.ids.PrepareStr("world-value"),
		c.WidgetText().Text("value").Keep(),
		c.WidgetText().Text(cur).Keep()).
		KeepIter() {
		if c.Button(inst.ids.PrepareSeq(0x5000),
			c.Atoms().Text("auto (first numeric)").Keep()).
			Frame(false).
			Selected(inst.valueCol == worldValueAuto).
			SendResp().HasPrimaryClicked() {
			inst.valueCol = worldValueAuto
		}
		for i, ci := range numeric {
			if c.Button(inst.ids.PrepareSeq(uint64(0x5001+i)),
				c.Atoms().Text(schema.Field(ci).Name).Keep()).
				Frame(false).
				Selected(inst.valueCol == ci).
				SendResp().HasPrimaryClicked() {
				inst.valueCol = ci
			}
		}
	}
}

func (inst *WorldDriver) statusLine(numRows int64, valueCol int, schema *arrow.Schema) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d countries", inst.matched)
	if valueCol >= 0 {
		fmt.Fprintf(&b, " · %s", schema.Field(valueCol).Name)
	} else {
		b.WriteString(" · presence (no numeric column)")
	}
	if inst.unmatched > 0 {
		fmt.Fprintf(&b, " · %d rows unmatched", inst.unmatched)
	}
	if inst.dupes > 0 {
		fmt.Fprintf(&b, " · %d duplicate rows (last wins)", inst.dupes)
	}
	_ = numRows
	return b.String()
}

// detectCountryColumn picks the country column per SD5: string columns in
// hint-priority order (name contains country/iso/nation or equals cc), then
// left-to-right; the first whose sampled distinct values resolve at ≥ the
// threshold wins. Cached per schema pointer — one result, one detection.
func (inst *WorldDriver) detectCountryColumn(rec arrow.RecordBatch, schema *arrow.Schema, atlas *worldmap.Atlas) int {
	if schema == inst.detectFor {
		return inst.detectCol
	}
	var hinted, plain []int
	for ci, f := range schema.Fields() {
		if !isWorldStringType(f.Type) {
			continue
		}
		name := strings.ToLower(f.Name)
		if strings.Contains(name, "country") || strings.Contains(name, "iso") ||
			strings.Contains(name, "nation") || name == "cc" {
			hinted = append(hinted, ci)
		} else {
			plain = append(plain, ci)
		}
	}
	found := -1
	for _, ci := range append(hinted, plain...) {
		if countryColumnResolves(rec, ci, atlas) {
			found = ci
			break
		}
	}
	inst.detectFor = schema
	inst.detectCol = found
	return found
}

// countryColumnResolves samples the column's distinct values and applies the
// SD5 threshold. NULL/empty cells are excluded from the distinct set (they
// count as unmatched rows during extraction, but say nothing about whether
// the column is country-shaped).
func countryColumnResolves(rec arrow.RecordBatch, col int, atlas *worldmap.Atlas) bool {
	rows := min(rec.NumRows(), worldDetectMaxRows)
	distinct := make(map[string]struct{}, 64)
	for row := int64(0); row < rows && len(distinct) < worldDetectMaxDistinct; row++ {
		v := formatCell(rec, col, row)
		if v == "" {
			continue
		}
		distinct[v] = struct{}{}
	}
	if len(distinct) == 0 {
		return false
	}
	resolved := 0
	for v := range distinct {
		if _, ok := atlas.Resolve(v); ok {
			resolved++
		}
	}
	return float64(resolved) >= worldResolveThreshold*float64(len(distinct))
}

// extract builds the country→value map (and the country→row map for
// click-to-select) from the result, last row wins per country. Cached on
// (executed, columns, schema) — the executed timestamp is the same dataset-
// freshness token the pager keys on.
func (inst *WorldDriver) extract(rec arrow.RecordBatch, schema *arrow.Schema, countryCol, valueCol int, atlas *worldmap.Atlas) {
	if inst.pendingExecuted.Equal(inst.forExecuted) && countryCol == inst.forCountry &&
		valueCol == inst.forValue && schema == inst.forSchema {
		return
	}
	inst.forExecuted = inst.pendingExecuted
	inst.forCountry = countryCol
	inst.forValue = valueCol
	inst.forSchema = schema

	vals := make(map[worldmap.CountryIdx]float64, 64)
	present := make(map[worldmap.CountryIdx]bool, 64)
	clear(inst.rowOf)
	inst.matched, inst.unmatched, inst.dupes = 0, 0, 0

	numRows := rec.NumRows()
	valueArr := arrow.Array(nil)
	if valueCol >= 0 {
		valueArr = rec.Column(valueCol)
	}
	for row := range numRows {
		cell := formatCell(rec, countryCol, row)
		idx, ok := worldmap.CountryIdx(0), false
		if cell != "" {
			idx, ok = atlas.Resolve(cell)
		}
		if !ok {
			inst.unmatched++
			continue
		}
		if _, seen := inst.rowOf[idx]; seen {
			inst.dupes++
		}
		inst.rowOf[idx] = row
		present[idx] = true
		if valueArr != nil {
			if v, vok := numericCellValue(valueArr, row); vok {
				vals[idx] = v
			}
		}
	}
	inst.matched = len(present)
	if valueCol >= 0 {
		inst.widget.SetValues(vals)
	} else {
		inst.widget.SetPresence(present)
	}
}

// isWorldStringType reports column types the country detector considers:
// plain and large UTF-8, and dictionary-encoded strings (ClickHouse
// LowCardinality(String) arrives as a dictionary).
func isWorldStringType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.STRING, arrow.LARGE_STRING:
		return true
	case arrow.DICTIONARY:
		if d, ok := dt.(*arrow.DictionaryType); ok {
			return d.ValueType.ID() == arrow.STRING || d.ValueType.ID() == arrow.LARGE_STRING
		}
	}
	return false
}

// numericColumns lists columns usable as the choropleth value.
func numericColumns(schema *arrow.Schema) (out []int) {
	for ci, f := range schema.Fields() {
		switch f.Type.ID() {
		case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
			arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64,
			arrow.FLOAT32, arrow.FLOAT64:
			out = append(out, ci)
		}
	}
	return
}

// numericCellValue reads one numeric cell as float64 (ok=false on NULL or a
// non-numeric array — the row then contributes membership but no value).
func numericCellValue(arr arrow.Array, row int64) (v float64, ok bool) {
	if row < 0 || int(row) >= arr.Len() || arr.IsNull(int(row)) {
		return 0, false
	}
	i := int(row)
	switch a := arr.(type) {
	case *array.Int8:
		return float64(a.Value(i)), true
	case *array.Int16:
		return float64(a.Value(i)), true
	case *array.Int32:
		return float64(a.Value(i)), true
	case *array.Int64:
		return float64(a.Value(i)), true
	case *array.Uint8:
		return float64(a.Value(i)), true
	case *array.Uint16:
		return float64(a.Value(i)), true
	case *array.Uint32:
		return float64(a.Value(i)), true
	case *array.Uint64:
		return float64(a.Value(i)), true
	case *array.Float32:
		f := float64(a.Value(i))
		return f, !math.IsNaN(f)
	case *array.Float64:
		f := a.Value(i)
		return f, !math.IsNaN(f)
	}
	return 0, false
}
