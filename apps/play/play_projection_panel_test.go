package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectionPanelAcceptClaimsSelectionRow(t *testing.T) {
	p := projectionPanel{}
	claim, reason := p.AcceptForChannel(chMain, schemaWith(strField("c")), playSignals{selectedRow: 2})
	require.Empty(t, reason)
	row, ok := claim.(int64)
	require.True(t, ok)
	require.Equal(t, int64(2), row)
}

func TestProjectionPanelRejectsNilSchema(t *testing.T) {
	p := projectionPanel{}
	claim, reason := p.AcceptForChannel(chMain, nil, playSignals{selectedRow: 0})
	require.Nil(t, claim)
	require.NotEmpty(t, reason)
}

func TestProjectionPanelDeclaresMainChannel(t *testing.T) {
	var p PanelI = projectionPanel{}
	require.Equal(t, PanelID("projection"), p.ID())
	require.Equal(t, []ChannelSpec{{ID: chMain, Required: true, Label: "points"}}, p.Channels())
}
