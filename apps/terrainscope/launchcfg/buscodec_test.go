package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/terrainscope/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.TerrainscopeLaunch{ObserverLat: 46.9, ObserverLon: 8.2, TargetLat: 46.95, TargetLon: 8.3,
		ObserverHeight: 1.7, TargetHeight: 2, Samples: 8}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.TerrainscopeLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.ObserverLat, got.ObserverLat)
	assert.Equal(t, orig.ObserverLon, got.ObserverLon)
	assert.Equal(t, orig.TargetLat, got.TargetLat)
	assert.Equal(t, orig.TargetLon, got.TargetLon)
	assert.Equal(t, orig.ObserverHeight, got.ObserverHeight)
	assert.Equal(t, orig.TargetHeight, got.TargetHeight)
	assert.Equal(t, orig.Samples, got.Samples)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
	empty, err := buscodec.Encode(launchcfg.TerrainscopeLaunch{})
	require.NoError(t, err)
	got, err = buscodec.Decode[launchcfg.TerrainscopeLaunch](empty)
	require.NoError(t, err)
	assert.Zero(t, got.Samples)
}
