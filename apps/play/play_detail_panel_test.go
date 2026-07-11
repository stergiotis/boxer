package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Accept claims a renderable result + a valid selection, carrying the schema and
// the row read from the selection signal.
func TestDetailPanelAcceptClaimsWithSelection(t *testing.T) {
	p := detailPanel{}
	schema := schemaWith(strField("id:naturalKey:x"))

	claim, reason := p.AcceptForChannel(chMain, schema, sigWith(3))
	require.Empty(t, reason)
	dc, ok := claim.(detailClaim)
	require.True(t, ok)
	require.Equal(t, int64(3), dc.row)
	require.Equal(t, schema, dc.schema)
}

// Accept rejects with the right empty-state reason for no result and no selection.
func TestDetailPanelAcceptRejects(t *testing.T) {
	p := detailPanel{}

	claim, reason := p.AcceptForChannel(chMain, nil, sigWith(0))
	require.Nil(t, claim)
	require.NotEmpty(t, reason, "no schema → run-a-query empty state")

	claim, reason = p.AcceptForChannel(chMain, schemaWith(strField("c")), sigWith(-1))
	require.Nil(t, claim)
	require.NotEmpty(t, reason, "no selection → select-a-row empty state")
}

func TestDetailPanelDeclaresMainChannel(t *testing.T) {
	var p PanelI = detailPanel{}
	require.Equal(t, PanelID("detail"), p.ID())
	require.Equal(t, []ChannelSpec{{ID: chMain, Required: true, Label: "row detail"}}, p.Channels())
}

// The selection signal round-trips through the LIVE store (slice 5b): a
// panel's emitter writes the row, the next snapshot exposes it, and Detail's
// readSelection decodes it — the producer→consumer loop (SD8) with no bridge.
func TestSelectionSignalRoundTrip(t *testing.T) {
	g := newQueryGraph(nil, nil)
	em := graphEmitter{graph: g}
	em.Emit(signalSelection, int64(5))

	got, ok := readSelection(g.signals())
	require.True(t, ok)
	require.Equal(t, int64(5), got)
}

// The store env exposes exactly the signals written to it; an unwritten name
// reports absent (and a nil env reads as no selection).
func TestStoreEnvExposesWrittenSignalsOnly(t *testing.T) {
	var sig SignalEnvI = sigWith(2)

	p, ok := sig.Get(signalSelection)
	require.True(t, ok)
	require.Equal(t, "2", p.Raw)

	_, ok = sig.Get("other")
	require.False(t, ok)

	_, ok = readSelection(nil)
	require.False(t, ok)
}
