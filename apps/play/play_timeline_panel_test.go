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

	claim, reason := p.AcceptForChannel(chEvents, schemaWith(tsField(timelineSlotTime)), emptySignals{})
	require.Empty(t, reason)
	ct, ok := claim.(timelineContract)
	require.True(t, ok)
	require.Equal(t, timelineModePoints, ct.Mode)

	claim, reason = p.AcceptForChannel(chEvents, schemaWith(tsField(timelineSlotTime), tsField(timelineSlotTimeEnd)), emptySignals{})
	require.Empty(t, reason)
	ct, _ = claim.(timelineContract)
	require.Equal(t, timelineModeIntervals, ct.Mode)

	claim, reason = p.AcceptForChannel(chEvents, schemaWith(tsField(timelineSlotTime), strField(timelineSlotLabel)), emptySignals{})
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
			claim, reason := p.AcceptForChannel(chEvents, tc.schema, emptySignals{})
			require.Nil(t, claim, "reject must carry no claim")
			require.NotEmpty(t, reason, "reject must carry an empty-state reason")
		})
	}
}

func TestTimelinePanelDeclaresEventsChannel(t *testing.T) {
	var p PanelI = timelinePanel{}
	require.Equal(t, PanelID("timeline"), p.ID())
	require.Equal(t, []ChannelSpec{{ID: chEvents, Required: true, Label: "events"}}, p.Channels())
}

// The selection emitter bridges signalSelection writes to the legacy
// selectedRow sink, and ignores everything else.
func TestSelectedRowEmitterBridgesSelection(t *testing.T) {
	row := int64(-1)
	em := selectedRowEmitter{target: &row}

	em.Emit(signalSelection, int64(7))
	require.Equal(t, int64(7), row)

	em.Emit("other", int64(9)) // wrong signal id
	require.Equal(t, int64(7), row)

	em.Emit(signalSelection, "nope") // wrong value type
	require.Equal(t, int64(7), row)

	em.Emit(signalSelection, int64(3))
	require.Equal(t, int64(3), row)
}

// emptySignals reports no bound signals and a zero revision.
func TestEmptySignals(t *testing.T) {
	var sig SignalEnvI = emptySignals{}
	_, ok := sig.Get("anything")
	require.False(t, ok)
	require.Equal(t, uint64(0), sig.Revision())
}
