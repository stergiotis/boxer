package chat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/launchcfg"
)

// Open in trail viewer names the conversation case, the conversation and a span
// ending now, in the viewer's launch kind.
func TestOpenTrailConfigEncodesTheConversation(t *testing.T) {
	wire, err := trailViewerLaunchConfig("conv-42")
	require.NoError(t, err)
	require.NoError(t, kindcheck.Check(launchcfg.Kind, wire))
	cfg, err := buscodec.Decode[launchcfg.TrailViewerLaunch](wire)
	require.NoError(t, err)
	assert.Equal(t, "conversation", cfg.Case)
	assert.Equal(t, "conv-42", cfg.Value)
	assert.Equal(t, int64(7*24*time.Hour), cfg.LastNanos)
	assert.Empty(t, cfg.Tab)
}
