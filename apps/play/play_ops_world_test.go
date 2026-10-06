package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// The frameless panes and the World pane, each with its read, its command
// and, where the pane keeps a pin, its select (ADR-0270, update of
// 2026-10-05).
func TestFramelessAndWorldOpsCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, pane := range []struct{ id, get, set, sel string }{
		{worldPaneId, opGetWorld, opSetWorldOptions, ""},
		{networkPaneId, opGetNetwork, opSetNetworkOptions, opSelectNetworkNode},
		{graphviewPaneId, opGetGraphview, opSetGraphviewOptions, opSelectGraphviewNodes},
		{sankeyPaneId, opGetSankey, opSetSankeyOptions, opSelectSankeyNode},
	} {
		get, ok := m.Operations.Lookup(pane.get)
		require.True(t, ok, pane.get)
		assert.Equal(t, app.OperationClassQuery, get.Class, pane.get)
		assert.Equal(t, app.OperationEffectNone, get.Effect, pane.get)
		assert.True(t, get.Untrusted, pane.get)
		assert.True(t, get.Agents, pane.get)
		assert.Equal(t, []string{pane.id, opsResResult, opsResPanes}, get.Reads, pane.get)
		set, ok := m.Operations.Lookup(pane.set)
		require.True(t, ok, pane.set)
		assert.Equal(t, app.OperationEffectDocument, set.Effect, pane.set)
		assert.False(t, set.Untrusted, pane.set)
		assert.Equal(t, []string{pane.id}, set.Writes, pane.set)
		assert.NotEmpty(t, set.Gesture, pane.set)
		if pane.sel == "" {
			continue
		}
		sel, ok := m.Operations.Lookup(pane.sel)
		require.True(t, ok, pane.sel)
		assert.Equal(t, app.OperationClassCommand, sel.Class, pane.sel)
		assert.Equal(t, app.OperationEffectDocument, sel.Effect, pane.sel)
		assert.False(t, sel.Untrusted, pane.sel)
		assert.Equal(t, []string{opsResSignals, pane.id}, sel.Writes, pane.sel)
		assert.NotEmpty(t, sel.Gesture, pane.sel)
	}
	get, ok := m.Operations.Lookup(opGetVectorfield)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectNone, get.Effect)
	assert.True(t, get.Untrusted)
	assert.Equal(t, []string{opsResVectorfield, opsResResult, opsResPanes}, get.Reads)
	view, ok := m.Operations.Lookup(opSetVectorfieldView)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectView, view.Effect)
	assert.False(t, view.Untrusted)
	assert.Equal(t, []string{opsResVectorfield}, view.Writes)
}

// worldOpsRec is four rows naming two countries and one cell no atlas knows:
// France twice (the last wins), Japan once, "Atlantis" never.
func worldOpsRec(t *testing.T) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "country", Type: arrow.BinaryTypes.String},
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "gdp", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	return chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"FRA", "Japan", "Atlantis", "France"}, nil)
		b.Field(1).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{2.5, 4.2, 9, 3.1}, nil)
	})
}

func worldPane(t *testing.T, p *PlayApp) arrow.RecordBatch {
	t.Helper()
	if p.worldDriver.widget.Atlas() == nil {
		t.Skip("the world atlas did not load")
	}
	rec := worldOpsRec(t)
	p.worldDriver.noteExecuted(time.Unix(1, 0))
	_, _, _, ok := p.worldDriver.syncFold(rec, rec.Schema())
	require.True(t, ok)
	drawnPane(p, worldPaneId, 4, rec.Schema())
	return rec
}

func TestGetWorldReadsTheExtraction(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := worldPane(t, p)
	defer rec.Release()

	r := queryOp[WorldReading](t, h, opGetWorld, GetWorldArgs{})
	assert.Equal(t, uint64(4), r.Drawn.ResultId)
	assert.Equal(t, "country", r.CountryColumn)
	assert.Equal(t, worldValueAutoName, r.Value)
	assert.Equal(t, "id", r.ValueColumn, "auto takes the first numeric column")
	assert.Equal(t, []string{"id", "gdp"}, r.NumericColumns)
	assert.Equal(t, int32(2), r.Countries)
	assert.Equal(t, int64(1), r.UnmatchedRows)
	assert.Equal(t, int64(1), r.DuplicateRows)
	assert.Equal(t, []string{"Atlantis"}, r.Unmatched)
	assert.Contains(t, r.Projections, worldmap.ProjectionEqualEarth.String())
	require.Len(t, r.List, 2)
	assert.Equal(t, int64(3), r.List[0].Row, "France, set by its last row, has the highest id")
	assert.Equal(t, "France", r.List[0].Cell)
	assert.Equal(t, 4.0, *r.List[0].Value)
	assert.Equal(t, int64(1), r.List[1].Row)
	assert.Equal(t, 1.0, *r.Min)
	assert.Equal(t, 4.0, *r.Max)

	r = queryOp[WorldReading](t, h, opGetWorld, GetWorldArgs{Limit: 1})
	require.Len(t, r.List, 1)
	assert.Equal(t, int32(1), r.More)
	require.Error(t, queryErr(t, h, opGetWorld, GetWorldArgs{Limit: 301}))
}

// Options are set by name against the result the pane draws; a refused
// call applies nothing.
func TestSetWorldOptionsSetsAndRefuses(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := worldPane(t, p)
	defer rec.Release()
	before := h.ResourceValue(opsResWorld)

	gdp, equal := "gdp", worldmap.ProjectionEqualEarth.String()
	require.NoError(t, applyOp(t, h, opSetWorldOptions, SetWorldOptionsArgs{Value: &gdp, Projection: &equal}))
	assert.Equal(t, 2, p.worldDriver.valueCol)
	assert.Equal(t, worldmap.ProjectionEqualEarth, p.worldDriver.widget.Opts.Projection)
	assert.NotEqual(t, before, h.ResourceValue(opsResWorld))

	country, bad := "country", "Mercator"
	err := applyOp(t, h, opSetWorldOptions, SetWorldOptionsArgs{Value: &country})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id, gdp")
	require.Error(t, applyOp(t, h, opSetWorldOptions, SetWorldOptionsArgs{Value: new(worldValuePresenceName), Projection: &bad}))
	assert.Equal(t, 2, p.worldDriver.valueCol, "a refused call applies nothing")
	require.Error(t, applyOp(t, h, opSetWorldOptions, SetWorldOptionsArgs{}))

	require.NoError(t, applyOp(t, h, opSetWorldOptions, SetWorldOptionsArgs{Value: new(worldValuePresenceName)}))
	p.worldDriver.syncFold(rec, rec.Schema())
	r := queryOp[WorldReading](t, h, opGetWorld, GetWorldArgs{})
	assert.Equal(t, worldValuePresenceName, r.Value)
	assert.Empty(t, r.ValueColumn)
	assert.Nil(t, r.Min)
	assert.Nil(t, r.List[0].Value)
	assert.Equal(t, "France", r.List[0].Country, "by name under presence")
}

// The value and projection combos go through set_world_options as the
// person.
func TestWorldCombosGoThroughTheirCommand(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	rec := worldPane(t, p)
	defer rec.Release()
	p.worldDriver.requestOptions(SetWorldOptionsArgs{Value: new("gdp")})
	e := lastEntry(t, eng)
	assert.Equal(t, opSetWorldOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, 2, p.worldDriver.valueCol)
}
