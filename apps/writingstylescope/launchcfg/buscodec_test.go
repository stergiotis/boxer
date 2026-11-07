package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/writingstylescope/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.WritingstylescopeLaunch{DocA: "# A\n\ntext", DocB: "# B\n\nmore", Floor: 120}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.WritingstylescopeLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.DocA, got.DocA)
	assert.Equal(t, orig.DocB, got.DocB)
	assert.Equal(t, orig.Floor, got.Floor)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
	empty, err := buscodec.Encode(launchcfg.WritingstylescopeLaunch{})
	require.NoError(t, err)
	got, err = buscodec.Decode[launchcfg.WritingstylescopeLaunch](empty)
	require.NoError(t, err)
	assert.Empty(t, got.DocA)
}
