package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.MdeditLaunch{Text: "# Title\n\nbody — with a fence:\n\n```sql\nSELECT 1\n```\n", Name: "chat 2026-10-03"}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.MdeditLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Text, got.Text)
	assert.Equal(t, orig.Name, got.Name)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
}
