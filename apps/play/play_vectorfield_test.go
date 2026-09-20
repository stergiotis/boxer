package play

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/sqlfield"
)

const vectorFieldBuffer = `SET param_level = 10;
WITH
    runs AS (SELECT max(run) AS latest FROM gfs.wind),
    vector_field AS (
        SELECT valid_time AS t, lat, lon, u, v
        FROM gfs.wind
        WHERE level = {level:UInt16} AND run = (SELECT latest FROM runs) AND member = {member:UInt8}
    ),
    vector_field_opts AS (SELECT 'GFS wind' AS name, 'm/s' AS unit, 40 AS speed_max)
SELECT count() FROM vector_field`

// The relation is the fused node in wrap form: the SET prelude, the upstream
// CTEs, the node last, and nothing downstream of it (ADR-0250 §SD5).
func TestVectorFieldRelationFromTheSplit(t *testing.T) {
	split, err := splitGraph(vectorFieldBuffer)
	require.NoError(t, err)

	rel, ok := vectorFieldRelation(split, vectorFieldNodeID)
	require.True(t, ok)
	assert.Equal(t, "vector_field", rel.From)
	assert.True(t, strings.HasPrefix(rel.Head, "SET param_level = 10;\nWITH runs AS ("), rel.Head)
	assert.True(t, strings.HasSuffix(rel.Head, ")"), "the WITH list is left open for the source's SELECT")
	assert.Contains(t, rel.Head, "vector_field AS (")
	assert.NotContains(t, rel.Head, "vector_field_opts", "a sibling is not upstream")
	assert.NotContains(t, rel.Head, "count()", "neither is the sink")

	node, _ := findSplitNode(split, vectorFieldNodeID)
	assert.ElementsMatch(t, []SignalID{"level", "member"}, node.Reads, "the relation's signals are the node's reads")

	_, ok = vectorFieldRelation(split, "no_such_node")
	assert.False(t, ok)
	_, ok = vectorFieldRelation(split, split.Sink)
	assert.False(t, ok, "the sink is a statement, not a relation")
}

// Every statement the source sends passes through the canonicaliser in front
// of play's executor (ADR-0108) with each parameter slot intact. The
// regression this guards is the Map's: a construct grammar1 does not model is
// rewritten into a syntax error and the pane goes quiet (ADR-0250 §SD4).
func TestVectorFieldStatementsSurviveCanonicalization(t *testing.T) {
	split, err := splitGraph(vectorFieldBuffer)
	require.NoError(t, err)
	rel, ok := vectorFieldRelation(split, vectorFieldNodeID)
	require.True(t, ok)

	for _, timeType := range []string{"", "DateTime", "DateTime64(3, 'UTC')"} {
		for _, statement := range sqlfield.Statements(rel, timeType) {
			out, cErr := passes.CanonicalizeFull(100).Run(statement)
			require.NoError(t, cErr, statement)
			slots, _, sErr := extractSlotsAndParams(statement)
			require.NoError(t, sErr, statement)
			for _, s := range slots {
				assert.Contains(t, out, "{"+s.Name+":"+s.Type+"}", "slot %q is carried through", s.Name)
			}
			assert.NotContains(t, out, `"DIV"`)
			assert.NotContains(t, out, `"nan"`)
		}
	}

	// The window statement names every parameter the source sends, and the
	// user's own beside them.
	window := sqlfield.Statements(rel, "DateTime")[3]
	slots, _, err := extractSlotsAndParams(window)
	require.NoError(t, err)
	names := make(map[string]string, len(slots))
	for _, s := range slots {
		names[s.Name] = s.Type
	}
	assert.Equal(t, "UInt8", names["member"])
	assert.Equal(t, "DateTime", names["ff_t"])
	assert.Equal(t, "Array(Int64)", names["ff_turns"])
	for name := range names {
		if name != "member" && name != "level" {
			assert.True(t, strings.HasPrefix(name, sqlfield.ParamPrefix), name)
		}
	}
}

func TestVectorFieldIdentityFollowsTextAndSignals(t *testing.T) {
	rel := sqlfield.Relation{Head: "WITH vector_field AS (SELECT 1)", From: "vector_field"}
	a := vectorFieldIdentity(rel, map[string]string{"param_level": "10", "param_member": "0"})
	assert.Equal(t, a, vectorFieldIdentity(rel, map[string]string{"param_member": "0", "param_level": "10"}), "order does not matter")
	assert.NotEqual(t, a, vectorFieldIdentity(rel, map[string]string{"param_level": "20", "param_member": "0"}))
	assert.NotEqual(t, a, vectorFieldIdentity(sqlfield.Relation{Head: rel.Head + " ", From: rel.From}, map[string]string{"param_level": "10", "param_member": "0"}))
}

// The channel carries the CTE's schema, and a rejection says which column.
func TestVectorFieldPanelClaims(t *testing.T) {
	p := vectorFieldPanel{}
	f := func(name string, dt arrow.DataType) arrow.Field { return arrow.Field{Name: name, Type: dt} }
	f64 := arrow.PrimitiveTypes.Float64

	_, reason := p.AcceptForChannel(chVectorField, nil, nil)
	assert.Contains(t, reason, "`vector_field` CTE")

	claim, reason := p.AcceptForChannel(chVectorField,
		arrow.NewSchema([]arrow.Field{f("t", arrow.PrimitiveTypes.Uint32), f("lat", f64), f("lon", f64), f("u", f64), f("v", f64)}, nil), nil)
	require.Empty(t, reason)
	assert.True(t, claim.(vectorFieldClaim).shape.HasTime)

	_, reason = p.AcceptForChannel(chVectorField,
		arrow.NewSchema([]arrow.Field{f("lat", f64), f("lon", f64), f("u", f64)}, nil), nil)
	assert.Contains(t, reason, "`v`")

	claim, reason = p.AcceptForChannel(chVectorFieldOpts,
		arrow.NewSchema([]arrow.Field{f("speed_max", f64), f("colour", arrow.BinaryTypes.String)}, nil), nil)
	require.Empty(t, reason)
	oc := claim.(vectorFieldOptsClaim)
	assert.Equal(t, 0, oc.speedMaxCol)
	assert.Equal(t, -1, oc.nameCol)

	node, fromSplit := splitFedChannel(chVectorField)
	assert.True(t, fromSplit)
	assert.Equal(t, vectorFieldNodeID, node)
}

// The pane's signals are declared, typed for their writer, and owned by it.
func TestVectorFieldSignalsAreDeclared(t *testing.T) {
	written := signalsWrittenBy("vectorfield")
	assert.Equal(t, []SignalID{signalVfT, signalVfMinLat, signalVfMaxLat, signalVfMinLon, signalVfMaxLon}, written)
	types := reservedSignalTypes()
	assert.Equal(t, "DateTime64(3, 'UTC')", types[string(signalVfT)])
	assert.Equal(t, "Float64", types[string(signalVfMinLat)])
	// Seeded, or a buffer whose sink reads vf_t could never run: the pane
	// learns of its CTE from the Run that the unfilled name would block.
	raw, seeded := signalSeedRaw(string(signalVfT))
	assert.True(t, seeded)
	assert.Equal(t, "1970-01-01 00:00:00.000", raw, "the epoch: no step is at it")
	raw, seeded = signalSeedRaw(string(signalVfMinLat))
	assert.True(t, seeded)
	assert.Equal(t, "0", raw)
}

type recordedEmits struct {
	names  []SignalID
	values []any
}

func (inst *recordedEmits) Emit(id SignalID, value any) {
	inst.names = append(inst.names, id)
	inst.values = append(inst.values, value)
}

// vf_t is the nearest step's valid time, written once the slider rests and
// never while it is dragged (ADR-0250 §SD6); playback counts as rest.
func TestVectorFieldTimeSignalIsWrittenAtRest(t *testing.T) {
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	meta := vectorfield.Meta{Steps: []vectorfield.Step{{Valid: t0}, {Valid: t0.Add(3 * time.Hour)}, {Valid: t0.Add(6 * time.Hour)}}}
	clock := t0
	d := &VectorFieldDriver{emittedStep: -1, now: func() time.Time { return clock }}
	emits := &recordedEmits{}

	d.emitWhenRested(meta, false, emits)
	assert.Empty(t, emits.names, "no field, no time")

	d.emitWhenRested(meta, true, emits)
	require.Equal(t, []SignalID{signalVfT}, emits.names, "the first step is published as soon as the field is there")
	assert.Equal(t, "2026-03-01 00:00:00.000", emits.values[0])

	// A drag: the position moves every frame, and nothing is written.
	for _, pos := range []float64{0.4, 0.9, 1.3, 1.6} {
		clock = clock.Add(50 * time.Millisecond)
		d.pos = pos
		d.emitWhenRested(meta, true, emits)
	}
	assert.Len(t, emits.names, 1)

	// It rests on 1.6: the nearest step is the third.
	clock = clock.Add(vectorFieldSettle)
	d.emitWhenRested(meta, true, emits)
	require.Len(t, emits.names, 2)
	assert.Equal(t, "2026-03-01 06:00:00.000", emits.values[1])

	// Resting on, and moving within the same step, writes nothing more.
	clock = clock.Add(time.Second)
	d.pos = 1.9
	d.emitWhenRested(meta, true, emits)
	clock = clock.Add(time.Second)
	d.emitWhenRested(meta, true, emits)
	assert.Len(t, emits.names, 2)

	// Playback ticks the signal once per step without waiting for rest.
	d.playing = true
	d.pos = 0.6
	d.emitWhenRested(meta, true, emits)
	require.Len(t, emits.names, 3)
	assert.Equal(t, "2026-03-01 03:00:00.000", emits.values[2])
}

// The pane's own write comes back a frame later and must not be mistaken for
// someone else's: only a value it did not publish moves the display time.
func TestVectorFieldFollowsATimeItDidNotWrite(t *testing.T) {
	g := newLiveQueryGraph(nil, nil, 1)
	d := &VectorFieldDriver{emittedStep: -1, now: time.Now, guest: newVectorFieldGuest(nil)}
	d.emittedT = "2026-03-01 03:00:00.000"

	g.setSignalRawFrom(signalVfT, d.emittedT, "vectorfield")
	_, moved := d.followTimeSignal(g.signals(), true)
	assert.False(t, moved, "its own value is not an instruction")

	g.setSignalRawFrom(signalVfT, "2026-03-01 06:00:00.000", signalWriterEditor)
	_, moved = d.followTimeSignal(g.signals(), true)
	assert.True(t, moved, "a written time is followed")
	assert.Equal(t, "2026-03-01 06:00:00.000", d.seenT)

	_, moved = d.followTimeSignal(g.signals(), true)
	assert.False(t, moved, "and followed once")
}

// A Live auto-run keeps the described field; a Run a person asked for, or one
// after a failed build, describes it again.
func TestVectorFieldForgetKeepsTheFieldAcrossAutoRuns(t *testing.T) {
	d := NewVectorFieldDriver(nil, nil, nil)
	defer d.close()
	d.guest.identity = "field"

	d.forgetLanes(true)
	assert.Equal(t, "field", d.guest.identity)

	d.guest.err = assert.AnError
	d.forgetLanes(true)
	assert.Empty(t, d.guest.identity, "a failed build is retried by any run")

	d.guest.identity, d.guest.err = "field", nil
	d.forgetLanes(false)
	assert.Empty(t, d.guest.identity)
}

// A tick belongs to the purpose that produced it, and is shown only while a
// statement of that purpose is on the wire: the fresh gate is what tells
// "running, no numbers yet" from "running, a third of the way".
func TestVectorFieldProgressIsPerPurposeAndGatedOnRunning(t *testing.T) {
	var p vectorFieldProgress

	_, fresh := p.view(sqlfield.PurposeDescribe)
	assert.False(t, fresh, "nothing has run")

	p.tick(sqlfield.PurposeDescribe, runstream.Progress{ReadRows: 7})
	_, fresh = p.view(sqlfield.PurposeDescribe)
	assert.False(t, fresh, "a tick with no statement behind it is dropped")

	p.reset(sqlfield.PurposeDescribe)
	p.begin(sqlfield.PurposeDescribe)
	p.tick(sqlfield.PurposeDescribe, runstream.Progress{ReadRows: 11})
	got, fresh := p.view(sqlfield.PurposeDescribe)
	require.True(t, fresh)
	assert.Equal(t, uint64(11), got.ReadRows)

	_, fresh = p.view(sqlfield.PurposeSummary)
	assert.False(t, fresh, "a summary runs beside a describe and counts its own rows")

	// A describe is three statements, and what it has read carries across
	// them: the counters belong to the phase, not to one statement of it.
	p.end(sqlfield.PurposeDescribe)
	p.begin(sqlfield.PurposeDescribe)
	got, fresh = p.view(sqlfield.PurposeDescribe)
	assert.True(t, fresh)
	assert.Equal(t, uint64(11), got.ReadRows, "the next statement of the phase does not blank the row")

	// The next PHASE starts from nothing.
	p.end(sqlfield.PurposeDescribe)
	p.reset(sqlfield.PurposeDescribe)
	got, fresh = p.view(sqlfield.PurposeDescribe)
	assert.False(t, fresh)
	assert.Zero(t, got.ReadRows)
}

// The phase the readout follows is the one the reader is waiting on: the
// describe first, because nothing is on screen for it, then a window, then
// the summary that runs beside them.
func TestVectorFieldGuestPhasePrefersTheDescribe(t *testing.T) {
	g := newVectorFieldGuest(nil)

	_, running, _, _ := g.Phase()
	assert.False(t, running, "an idle guest shows no row")

	g.summaryJob = &vectorFieldSummaryJob{cancel: func() {}}
	purpose, running, _, fresh := g.Phase()
	require.True(t, running)
	assert.Equal(t, sqlfield.PurposeSummary, purpose)
	assert.False(t, fresh, "no tick has landed")

	g.building = &vectorFieldBuild{cancel: func() {}}
	purpose, _, _, _ = g.Phase()
	assert.Equal(t, sqlfield.PurposeDescribe, purpose, "the describe outranks the summary beside it")

	g.progress.begin(sqlfield.PurposeDescribe)
	g.progress.tick(sqlfield.PurposeDescribe, runstream.Progress{ReadRows: 3, TotalRowsToRead: 9})
	_, _, p, fresh := g.Phase()
	require.True(t, fresh)
	assert.Equal(t, uint64(3), p.ReadRows)
}

// Cancel latches the phase it stops. The describe keeps its identity claimed
// and the summary its view key, so neither is asked for again by the next
// frame's demand — a cancel undone by its own disappearance is worse than
// none (the Map learned this live, 2026-08-05). A Run is what asks again.
func TestVectorFieldCancelLatchesTheWorkItStops(t *testing.T) {
	d := NewVectorFieldDriver(nil, nil, nil)
	defer d.close()
	g := d.guest
	cancels := 0

	g.identity = "field"
	g.building = &vectorFieldBuild{identity: "field", cancel: func() { cancels++ }}
	g.Cancel(sqlfield.PurposeDescribe)
	assert.Equal(t, 1, cancels)
	assert.Nil(t, g.building)
	assert.True(t, g.cancelled)
	assert.Equal(t, "field", g.identity, "the field stays claimed, so Ensure lets it be")

	g.summaryKey = "a view"
	g.summaryJob = &vectorFieldSummaryJob{key: "a view", cancel: func() { cancels++ }}
	g.Cancel(sqlfield.PurposeSummary)
	assert.Equal(t, 2, cancels)
	assert.Nil(t, g.summaryJob)
	assert.ErrorIs(t, g.summaryErr, errVectorFieldCancelled)
	assert.Equal(t, "a view", g.summaryKey, "the view stays keyed, so EnsureSummary lets it be")

	// A window has no latch to keep, and is not cancellable here.
	g.building = &vectorFieldBuild{identity: "field", cancel: func() { cancels++ }}
	g.Cancel(sqlfield.PurposeWindow)
	assert.Equal(t, 2, cancels)
	assert.NotNil(t, g.building)

	// A Run drops both, and the next Ensure describes the field again.
	g.building = nil
	d.forgetLanes(false)
	assert.Empty(t, g.identity)
	assert.False(t, g.cancelled)
}
