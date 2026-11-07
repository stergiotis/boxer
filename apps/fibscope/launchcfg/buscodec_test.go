package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/fibscope/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.FibscopeLaunch{Id: 12393906174523604993}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.FibscopeLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Id, got.Id)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
	empty, err := buscodec.Encode(launchcfg.FibscopeLaunch{})
	require.NoError(t, err)
	got, err = buscodec.Decode[launchcfg.FibscopeLaunch](empty)
	require.NoError(t, err)
	assert.Zero(t, got.Id)
}
