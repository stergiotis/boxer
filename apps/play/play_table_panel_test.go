package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Accept claims any non-nil result; the claim is the selection row read from the
// signal env.
func TestTablePanelAcceptClaimsSelectionRow(t *testing.T) {
	p := tablePanel{}
	claim, reason := p.Accept(schemaWith(strField("c")), playSignals{selectedRow: 4})
	require.Empty(t, reason)
	row, ok := claim.(int64)
	require.True(t, ok)
	require.Equal(t, int64(4), row)
}

// Unlike Detail, the Table still renders with no selection (-1 ⇒ nothing
// highlighted), so Accept claims rather than rejecting.
func TestTablePanelClaimsWithoutSelection(t *testing.T) {
	p := tablePanel{}
	claim, reason := p.Accept(schemaWith(strField("c")), playSignals{selectedRow: -1})
	require.Empty(t, reason)
	row, _ := claim.(int64)
	require.Equal(t, int64(-1), row)
}

func TestTablePanelRejectsNilSchema(t *testing.T) {
	p := tablePanel{}
	claim, reason := p.Accept(nil, playSignals{selectedRow: 0})
	require.Nil(t, claim)
	require.NotEmpty(t, reason)
}

func TestTablePanelBindsMainNode(t *testing.T) {
	var p PanelI = tablePanel{}
	require.Equal(t, mainNodeID, p.BoundNode())
	require.Equal(t, PanelID("table"), p.ID())
}
