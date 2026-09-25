package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appcenter/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.AppCenterLaunch{AppId: "github.com/stergiotis/boxer/apps/appstate"}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.AppCenterLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.AppId, got.AppId)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
}
