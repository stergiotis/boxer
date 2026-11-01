package launchcfg_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/launchcfg"
)

// Every field survives the codec, and the kind check accepts the bytes.
func TestRoundTripAndKindCheck(t *testing.T) {
	from := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	orig := launchcfg.TrailViewerLaunch{Case: "conversation", Value: "conv-1", LastNanos: int64(7 * 24 * time.Hour),
		FromNanos: from.UnixNano(), ToNanos: from.Add(time.Hour).UnixNano(), Tab: "tree", View: "my view"}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[launchcfg.TrailViewerLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Case, got.Case)
	assert.Equal(t, orig.Value, got.Value)
	assert.Equal(t, orig.LastNanos, got.LastNanos)
	assert.Equal(t, orig.FromNanos, got.FromNanos)
	assert.Equal(t, orig.ToNanos, got.ToNanos)
	assert.Equal(t, orig.Tab, got.Tab)
	assert.Equal(t, orig.View, got.View)
	assert.NoError(t, kindcheck.Check(launchcfg.Kind, wire))

	// The optional fields may be absent.
	wire, err = buscodec.Encode(launchcfg.TrailViewerLaunch{Case: "all"})
	require.NoError(t, err)
	got, err = buscodec.Decode[launchcfg.TrailViewerLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, "all", got.Case)
	assert.Zero(t, got.LastNanos)
	assert.Empty(t, got.View)
}
