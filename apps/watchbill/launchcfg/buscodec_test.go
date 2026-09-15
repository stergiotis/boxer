package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/watchbill/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.WatchbillLaunch{JobId: "j-1", Kind: "tender.download", State: "discarded"}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.WatchbillLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.JobId, got.JobId)
	assert.Equal(t, orig.Kind, got.Kind)
	assert.Equal(t, orig.State, got.State)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
	empty, err := buscodec.Encode(launchcfg.WatchbillLaunch{})
	require.NoError(t, err)
	got, err = buscodec.Decode[launchcfg.WatchbillLaunch](empty)
	require.NoError(t, err)
	assert.Empty(t, got.JobId)
}
