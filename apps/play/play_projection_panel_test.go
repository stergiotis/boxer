package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectionPanelAcceptClaimsSelectionRow(t *testing.T) {
	p := projectionPanel{}
	claim, reason := p.Accept(schemaWith(strField("c")), playSignals{selectedRow: 2})
	require.Empty(t, reason)
	row, ok := claim.(int64)
	require.True(t, ok)
	require.Equal(t, int64(2), row)
}

func TestProjectionPanelRejectsNilSchema(t *testing.T) {
	p := projectionPanel{}
	claim, reason := p.Accept(nil, playSignals{selectedRow: 0})
	require.Nil(t, claim)
	require.NotEmpty(t, reason)
}

func TestProjectionPanelBindsMainNode(t *testing.T) {
	var p PanelI = projectionPanel{}
	require.Equal(t, mainNodeID, p.BoundNode())
	require.Equal(t, PanelID("projection"), p.ID())
}
