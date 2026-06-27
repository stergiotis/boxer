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

	claim, reason := p.Accept(schema, playSignals{selectedRow: 3})
	require.Empty(t, reason)
	dc, ok := claim.(detailClaim)
	require.True(t, ok)
	require.Equal(t, int64(3), dc.row)
	require.Equal(t, schema, dc.schema)
}

// Accept rejects with the right empty-state reason for no result and no selection.
func TestDetailPanelAcceptRejects(t *testing.T) {
	p := detailPanel{}

	claim, reason := p.Accept(nil, playSignals{selectedRow: 0})
	require.Nil(t, claim)
	require.NotEmpty(t, reason, "no schema → run-a-query empty state")

	claim, reason = p.Accept(schemaWith(strField("c")), playSignals{selectedRow: -1})
	require.Nil(t, claim)
	require.NotEmpty(t, reason, "no selection → select-a-row empty state")
}

func TestDetailPanelBindsMainNode(t *testing.T) {
	var p PanelI = detailPanel{}
	require.Equal(t, mainNodeID, p.BoundNode())
	require.Equal(t, PanelID("detail"), p.ID())
}

// The selection signal round-trips through the legacy selectedRow store: the
// Timeline's emitter writes a row, playSignals exposes it as the signal, and
// Detail's readSelection decodes it — the producer→consumer loop (SD8).
func TestSelectionSignalRoundTrip(t *testing.T) {
	row := int64(-1)
	em := selectedRowEmitter{target: &row}
	em.Emit(signalSelection, int64(5))

	got, ok := readSelection(playSignals{selectedRow: row})
	require.True(t, ok)
	require.Equal(t, int64(5), got)
}

func TestPlaySignalsExposesOnlySelection(t *testing.T) {
	var sig SignalEnvI = playSignals{selectedRow: 2}

	p, ok := sig.Get(signalSelection)
	require.True(t, ok)
	require.Equal(t, "2", p.Raw)

	_, ok = sig.Get("other")
	require.False(t, ok)
}
