package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func paneOf(t *testing.T, panes PanesState, id string) PaneState {
	t.Helper()
	for _, p := range panes.Panes {
		if p.Pane == id {
			return p
		}
	}
	t.Fatalf("no pane %s", id)
	return PaneState{}
}

// ADR-0270, update of 2026-10-05: the World accepts any text column by
// schema, and only its draw finds that none resolves to countries. Once it
// has drawn that, list_panes says it cannot draw — but not on a draw older
// than the last frame.
func TestListPanesReportsTheWorldsDataReject(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	p.frameSchema = arrow.NewSchema([]arrow.Field{{Name: "colour", Type: arrow.BinaryTypes.String}}, nil)
	p.frame = 5
	p.worldDriver.drawnReject = worldNoCountryReason
	p.paneDrawn = map[string]paneDrawnMark{"world": {frame: 4, result: 9, fed: true}}

	world := paneOf(t, queryOp[PanesState](t, h, opListPanes, nil), "world")
	assert.Equal(t, PaneDrawNo, world.Draws)
	assert.Equal(t, worldNoCountryReason, world.Reason)
	assert.Empty(t, world.Status)
	assert.Equal(t, uint64(9), world.StatusOf)
	assert.True(t, world.Lazy)
	assert.Equal(t, "body", world.Zone)
	assert.False(t, world.Visible, "a lazy pane whose gate never went live is not visible")

	p.paneDrawn["world"] = paneDrawnMark{frame: 2, result: 9, fed: true}
	world = paneOf(t, queryOp[PanesState](t, h, opListPanes, nil), "world")
	assert.Equal(t, PaneDrawYes, world.Draws, "a draw older than the last frame does not judge the pane")
}

// The Chart's fold rejects a repeated heatmap cell; list_panes carries the
// reject, and for a chart it can draw, its status line and the result it is
// of.
func TestListPanesReportsTheChartsFoldRejectAndStatus(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	schema := arrow.NewSchema([]arrow.Field{
		{Name: chartColX, Type: arrow.BinaryTypes.String},
		{Name: chartColY, Type: arrow.BinaryTypes.String},
		{Name: chartColZ, Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	rec := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"mon", "mon"}, nil)
		b.Field(1).(*array.StringBuilder).AppendValues([]string{"am", "am"}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
	})
	defer rec.Release()
	k, reason := resolveChartColumns(schema)
	require.Empty(t, reason)
	p.chartDriver.noteExecuted(time.Unix(1, 0))
	p.chartDriver.rebuild(rec, schema, k)
	p.frameSchema = schema
	p.frame = 3
	p.paneDrawn = map[string]paneDrawnMark{"chart": {frame: 2, result: 7, fed: true}}

	chart := paneOf(t, queryOp[PanesState](t, h, opListPanes, nil), "chart")
	assert.Equal(t, PaneDrawNo, chart.Draws)
	assert.Contains(t, chart.Reason, "Row 1")
	assert.Empty(t, chart.Status)

	good := chartRecord(t, schema, func(b *array.RecordBuilder) {
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"mon", "tue"}, nil)
		b.Field(1).(*array.StringBuilder).AppendValues([]string{"am", "am"}, nil)
		b.Field(2).(*array.Float64Builder).AppendValues([]float64{1, 2}, nil)
	})
	defer good.Release()
	p.chartDriver.noteExecuted(time.Unix(2, 0))
	p.chartDriver.rebuild(good, schema, k)
	chart = paneOf(t, queryOp[PanesState](t, h, opListPanes, nil), "chart")
	assert.Equal(t, PaneDrawYes, chart.Draws)
	assert.Contains(t, chart.Status, "cells")
	assert.Equal(t, uint64(7), chart.StatusOf)

	// A pane that has never drawn reports no status.
	assert.Empty(t, paneOf(t, queryOp[PanesState](t, h, opListPanes, nil), "dist").Status)
}

func TestListPanesCatalogEntry(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opListPanes)
	require.True(t, ok)
	assert.Equal(t, 2, int(spec.Version))
	assert.True(t, spec.Untrusted, "a status line quotes the data")
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.Equal(t, []string{opsResPanes, opsResResult, opsResSql}, spec.Reads)
}
