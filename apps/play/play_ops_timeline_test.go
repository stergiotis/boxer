package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// signalOf is a signal's value and writer as the store holds them.
func signalOf(t *testing.T, p *PlayApp, name SignalID) (raw, writer string) {
	t.Helper()
	for _, r := range p.graph.signalRows() {
		if r.Name == name {
			return r.Raw, r.Writer
		}
	}
	return "", ""
}

// intervalsRec is three intervals over two lanes, one ending before it
// begins.
func intervalsRec(t *testing.T) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	ts := &arrow.TimestampType{Unit: arrow.Millisecond}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: timelineSlotTime, Type: ts},
		{Name: timelineSlotTimeEnd, Type: ts},
		{Name: timelineSlotLane, Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	b.Field(0).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{1000, 5000, 9000}, nil)
	b.Field(1).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{2000, 6000, 8000}, nil)
	b.Field(2).(*array.StringBuilder).AppendValues([]string{"api", "db", "api"}, nil)
	return b.NewRecordBatch()
}

func syncTimeline(t *testing.T, p *PlayApp, rec arrow.RecordBatch) {
	t.Helper()
	ct := resolveContract(rec.Schema())
	require.Equal(t, timelineModeIntervals, ct.Mode)
	p.timeline.syncEvents(rec, ct)
}

func TestTimelineOpsCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	get, ok := m.Operations.Lookup(opGetTimeline)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectNone, get.Effect)
	assert.True(t, get.Untrusted)
	assert.Equal(t, []string{opsResTimeline, opsResResult, opsResPanes}, get.Reads)
	set, ok := m.Operations.Lookup(opSetTimelineOptions)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectDocument, set.Effect)
	assert.Equal(t, []string{opsResTimeline}, set.Writes)
	win, ok := m.Operations.Lookup(opSetTimelineWindow)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassCommand, win.Class)
	assert.Equal(t, app.OperationEffectDocument, win.Effect)
	assert.False(t, win.Untrusted)
	assert.True(t, win.Agents)
	assert.Equal(t, []string{opsResSignals, opsResTimeline}, win.Writes)
	assert.NotEmpty(t, win.Gesture)
}

func TestGetTimelineReadsEventsLanesAndTheWindow(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	rec := intervalsRec(t)
	defer rec.Release()
	syncTimeline(t, p, rec)
	drawnPane(p, timelinePaneId, 4, rec.Schema())

	r := queryOp[TimelineReading](t, h, opGetTimeline, nil)
	assert.Equal(t, uint64(4), r.Drawn.ResultId)
	assert.Empty(t, r.Drawn.CannotDraw)
	assert.Contains(t, r.Drawn.Status, "1 row(s) skipped")
	assert.Equal(t, "intervals", r.Mode)
	assert.Equal(t, int64(2), r.Events)
	assert.Equal(t, int64(1), r.Skipped)
	assert.Equal(t, []TimelineLaneReading{{Lane: "api", Events: 1}, {Lane: "db", Events: 1}}, r.Lanes)
	require.NotNil(t, r.Extent)
	assert.Equal(t, "1970-01-01 00:00:01.000", r.Extent.From)
	assert.Equal(t, "1970-01-01 00:00:06.000", r.Extent.To)
	assert.Nil(t, r.Window, "nothing brushed")
	assert.False(t, r.Bands.SqlSet)

	require.NoError(t, applyOp(t, h, opSetTimelineWindow, SetTimelineWindowArgs{From: "1970-01-01 00:00:01.500", To: "1970-01-01T00:00:05Z"}))
	r = queryOp[TimelineReading](t, h, opGetTimeline, nil)
	require.NotNil(t, r.Window)
	assert.Equal(t, TimelineRange{From: "1970-01-01 00:00:01.500", To: "1970-01-01 00:00:05.000"}, *r.Window)

	// Every row skipped is the pane drawing nothing.
	mem := memory.NewGoAllocator()
	b := array.NewRecordBuilder(mem, rec.Schema())
	defer b.Release()
	b.Field(0).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{5000}, nil)
	b.Field(1).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{1000}, nil)
	b.Field(2).(*array.StringBuilder).AppendValues([]string{"x"}, nil)
	bad := b.NewRecordBatch()
	defer bad.Release()
	syncTimeline(t, p, bad)
	r = queryOp[TimelineReading](t, h, opGetTimeline, nil)
	assert.Contains(t, r.Drawn.CannotDraw, "Every row was skipped")
	assert.Empty(t, r.Mode, "a reading the pane did not draw leaves its fields empty")
	assert.NotNil(t, r.Window, "the window is the pane's setting and stays readable")
}

// set_timeline_window moves the brush and the two signals together, with the
// task as writer; the pane's own publish then finds them in place. A refused
// call applies nothing.
func TestSetTimelineWindowRefusesAndClears(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	for _, bad := range []SetTimelineWindowArgs{
		{From: "2026-10-05 14:00:00"},
		{From: "yesterday", To: "2026-10-05"},
		{From: "2026-10-05 15:00", To: "2026-10-05 14:00"},
		{From: "2026-10-05", To: "2026-10-05"},
		{Clear: true, From: "2026-10-05"},
		{},
	} {
		require.Error(t, applyOp(t, h, opSetTimelineWindow, bad), "%+v", bad)
	}
	_, ok := p.timeline.tl.Brush()
	assert.False(t, ok, "a refused call applies nothing")

	require.NoError(t, applyOp(t, h, opSetTimelineWindow, SetTimelineWindowArgs{From: "2026-10-05 14:00", To: "2026-10-05 16:30:00.250"}))
	br, ok := p.timeline.tl.Brush()
	require.True(t, ok)
	assert.Equal(t, int64(250), br.ToMS-br.FromMS-150*60*1000)
	from, writer := signalOf(t, p, signalTimelineFrom)
	assert.Equal(t, "2026-10-05 14:00:00.000", from)
	assert.Equal(t, "task:t", writer)
	to, _ := signalOf(t, p, signalTimelineTo)
	assert.Equal(t, "2026-10-05 16:30:00.250", to)

	// The pane's publish the same frame writes the same values: the store
	// keeps the task as writer.
	p.timeline.publishWindow(graphEmitter{graph: p.graph, writer: timelinePaneId})
	_, writer = signalOf(t, p, signalTimelineFrom)
	assert.Equal(t, "task:t", writer)

	require.NoError(t, applyOp(t, h, opSetTimelineWindow, SetTimelineWindowArgs{Clear: true}))
	_, ok = p.timeline.tl.Brush()
	assert.False(t, ok)
	from, _ = signalOf(t, p, signalTimelineFrom)
	to, _ = signalOf(t, p, signalTimelineTo)
	assert.Equal(t, timelineWindowFloor, from)
	assert.Equal(t, timelineWindowCeil, to)
}

func TestSetTimelineOptionsSetsTheNowLine(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	before := h.ResourceValue(opsResTimeline)
	on := !*p.timeline.nowLinePtr
	require.NoError(t, applyOp(t, h, opSetTimelineOptions, SetTimelineOptionsArgs{NowLine: &on}))
	assert.Equal(t, on, *p.timeline.nowLinePtr)
	assert.NotEqual(t, before, h.ResourceValue(opsResTimeline))
	require.Error(t, applyOp(t, h, opSetTimelineOptions, SetTimelineOptionsArgs{}))
}

// The person's brush goes through set_timeline_window and keeps the pane as
// the signals' writer, as the brush always wrote them.
func TestTheBrushGoesThroughSetTimelineWindow(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	p.timeline.requestWindow(SetTimelineWindowArgs{From: "2026-10-05 14:00", To: "2026-10-05 15:00"})
	e := lastEntry(t, eng)
	assert.Equal(t, opSetTimelineWindow, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	_, ok := p.timeline.tl.Brush()
	assert.True(t, ok)
	_, writer := signalOf(t, p, signalTimelineTo)
	assert.Equal(t, timelinePaneId, writer)

	bare, _ := opsLauncher(t)
	bare.inner.timeline.requestWindow(SetTimelineWindowArgs{From: "2026-10-05 14:00", To: "2026-10-05 15:00"})
	_, ok = bare.inner.timeline.tl.Brush()
	assert.True(t, ok, "without a host the brush applies directly")
}
