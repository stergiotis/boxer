package launchcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/notebook/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTripAndKindCheck(t *testing.T) {
	orig := launchcfg.NotebookLaunch{Text: "# Report\n\n```sql\nSELECT 1 AS one\n```\n^one\n", Name: "report.md",
		Mount: 0x2a, SnapNanos: 1731500000000000000, Path: "reports/report.md"}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.NotebookLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Text, got.Text)
	assert.Equal(t, orig.Name, got.Name)
	assert.Equal(t, orig.Mount, got.Mount)
	assert.Equal(t, orig.SnapNanos, got.SnapNanos)
	assert.Equal(t, orig.Path, got.Path)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))

	bare, err := buscodec.Encode(launchcfg.NotebookLaunch{Text: "x", Name: "n"})
	require.NoError(t, err)
	got, err = buscodec.Decode[launchcfg.NotebookLaunch](bare)
	require.NoError(t, err)
	assert.Zero(t, got.Mount)
	assert.Empty(t, got.Path)
}
