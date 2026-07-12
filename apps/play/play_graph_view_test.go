package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// nodeChannelEligibility infers the notable channels a node could fill from its
// SQL (4c): a _tl_time projection ⇒ events, _tl_band_from ⇒ bands, neither ⇒ none
// (the universal main channel is omitted).
func TestNodeChannelEligibility(t *testing.T) {
	require.Equal(t, []string{"Timeline·events"},
		nodeChannelEligibility(splitNode{SQL: "SELECT toDateTime64(t, 3) AS _tl_time FROM x"}))
	require.Equal(t, []string{"Timeline·bands"},
		nodeChannelEligibility(splitNode{SQL: "SELECT a AS _tl_band_from, b AS _tl_band_to, 'info.subtle' AS _tl_band_color"}))
	require.Empty(t,
		nodeChannelEligibility(splitNode{SQL: "SELECT n FROM t"}),
		"a plain result is only main-eligible (omitted)")
}

// channelInventory reads each registered panel's declared channels straight
// off Channels() — the channel model made visible (4c; registry-backed since
// 6a, which also brought the previously omitted Schema and World into the
// inventory).
func TestChannelInventoryListsPanelsAndChannels(t *testing.T) {
	inv := tabsTestApp().channelInventory()
	require.Contains(t, inv, "table: main")
	require.Contains(t, inv, "projection: main")
	require.Contains(t, inv, "detail: main")
	require.Contains(t, inv, "timeline: events+bands")
	require.Contains(t, inv, "world: main")
	require.Contains(t, inv, "schema: main")
}
