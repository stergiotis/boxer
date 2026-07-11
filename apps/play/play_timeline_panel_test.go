package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"
)

func schemaWith(fields ...arrow.Field) *arrow.Schema {
	return arrow.NewSchema(fields, nil)
}

func tsField(name string) arrow.Field {
	return arrow.Field{Name: name, Type: &arrow.TimestampType{Unit: arrow.Millisecond}}
}

func strField(name string) arrow.Field {
	return arrow.Field{Name: name, Type: arrow.BinaryTypes.String}
}

// Accept lifts the column contract: a renderable shape yields a claim carrying
// the resolved contract; no reason.
func TestTimelinePanelAcceptClaimsRenderableSchema(t *testing.T) {
	p := timelinePanel{}

	claim, reason := p.AcceptForChannel(chEvents, schemaWith(tsField(timelineSlotTime)), sigNone())
	require.Empty(t, reason)
	ct, ok := claim.(timelineContract)
	require.True(t, ok)
	require.Equal(t, timelineModePoints, ct.Mode)

	claim, reason = p.AcceptForChannel(chEvents, schemaWith(tsField(timelineSlotTime), tsField(timelineSlotTimeEnd)), sigNone())
	require.Empty(t, reason)
	ct, _ = claim.(timelineContract)
	require.Equal(t, timelineModeIntervals, ct.Mode)

	claim, reason = p.AcceptForChannel(chEvents, schemaWith(tsField(timelineSlotTime), strField(timelineSlotLabel)), sigNone())
	require.Empty(t, reason)
	ct, _ = claim.(timelineContract)
	require.Equal(t, timelineModeAnnotations, ct.Mode)
}

// Accept rejects with a reason (the empty-state text) for nil / incomplete /
// ambiguous schemas — the generalised Timeline Mode/Reject.
func TestTimelinePanelAcceptRejectsWithReason(t *testing.T) {
	p := timelinePanel{}

	for _, tc := range []struct {
		name   string
		schema *arrow.Schema
	}{
		{"nil schema", nil},
		{"end without time", schemaWith(tsField(timelineSlotTimeEnd))},
		{"label without time", schemaWith(strField(timelineSlotLabel))},
		{"ambiguous end+label", schemaWith(tsField(timelineSlotTime), tsField(timelineSlotTimeEnd), strField(timelineSlotLabel))},
		{"no contract columns", schemaWith(strField("whatever"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim, reason := p.AcceptForChannel(chEvents, tc.schema, sigNone())
			require.Nil(t, claim, "reject must carry no claim")
			require.NotEmpty(t, reason, "reject must carry an empty-state reason")
		})
	}
}

func TestTimelinePanelDeclaresEventsAndBandsChannels(t *testing.T) {
	var p PanelI = timelinePanel{}
	require.Equal(t, PanelID("timeline"), p.ID())
	require.Equal(t, []ChannelSpec{
		{ID: chEvents, Required: true, Label: "events"},
		{ID: chBands, Required: false, Label: "bands"},
	}, p.Channels())
}

// The bands channel accepts any non-nil result (the _tl_band_* contract is
// validated downstream in setBands); a nil schema leaves the optional channel
// unfilled.
func TestTimelinePanelBandsChannelAccepts(t *testing.T) {
	p := timelinePanel{}
	claim, reason := p.AcceptForChannel(chBands, schemaWith(tsField(timelineSlotBandFrom)), sigNone())
	require.Empty(t, reason)
	require.Equal(t, true, claim)

	_, reason = p.AcceptForChannel(chBands, nil, sigNone())
	require.NotEmpty(t, reason, "absent bands result → optional channel unfilled")
}

// The live emitter (slice 5b) writes any named signal into the store; a
// string value is encodable too (it is no longer a selection-only bridge).
// The write is visible from the next snapshot.
func TestGraphEmitterWritesStore(t *testing.T) {
	g := newQueryGraph(nil, nil)
	em := graphEmitter{graph: g}

	em.Emit(signalSelection, int64(7))
	got, ok := readSelection(g.signals())
	require.True(t, ok)
	require.Equal(t, int64(7), got)

	em.Emit("other", int64(9)) // any name is a signal now
	p, ok := g.signals().Get("other")
	require.True(t, ok)
	require.Equal(t, "9", p.Raw)

	em.Emit(signalSelection, struct{}{}) // unencodable ⇒ dropped with a warning
	got, ok = readSelection(g.signals())
	require.True(t, ok)
	require.Equal(t, int64(7), got)

	em.Emit(signalSelection, int64(3))
	got, _ = readSelection(g.signals())
	require.Equal(t, int64(3), got)
}

// A fresh store reports no signals and a zero revision.
func TestFreshStoreEnvIsEmpty(t *testing.T) {
	var sig SignalEnvI = sigNone()
	_, ok := sig.Get("anything")
	require.False(t, ok)
	require.Equal(t, uint64(0), sig.Revision())
}
