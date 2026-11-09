package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit/launchcfg"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.ExternalEditLaunch{Id: "0123abcd"}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.ExternalEditLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Id, got.Id)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
	empty, err := buscodec.Encode(launchcfg.ExternalEditLaunch{})
	require.NoError(t, err)
	got, err = buscodec.Decode[launchcfg.ExternalEditLaunch](empty)
	require.NoError(t, err)
	assert.Empty(t, got.Id)
}
