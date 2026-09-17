package waveform

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stretchr/testify/require"
)

// A thumbnail renders without a client, costs a bounded number of messages
// whatever the recording's length, and turns a click into a seek fraction
// (ADR-0245 §SD6).
func TestRenderThumbnail(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	defer reset()
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()

	format := pcm.Format{SampleRate: 8000, Channels: 2}
	src, err := pcm.NewSynthSourceE(format, 80000, pcm.Sine(format, 440, 0.5))
	require.NoError(t, err)
	ov, err := peaks.OverviewE(context.Background(), src, 512)
	require.NoError(t, err)

	zero()
	res := RenderThumbnail(ThumbnailInput{Ids: ids, Key: "thumb", Overview: ov, W: 300, H: 120, Progress: -1})
	require.False(t, res.Clicked)
	idle := messages()
	require.LessOrEqual(t, idle, 8, "one rect batch per channel, a divider and the canvas")

	// An overview wider than the canvas is folded, not drawn sub-pixel: the
	// message count does not grow with it.
	zero()
	RenderThumbnail(ThumbnailInput{Ids: ids, Key: "thumb", Overview: ov, W: 40, H: 120, Progress: 0.5})
	require.LessOrEqual(t, messages(), idle+1, "plus the playhead")

	// Nothing to draw is a centre line, not a panic.
	RenderThumbnail(ThumbnailInput{Ids: ids, Key: "thumb", W: 300, H: 120, Progress: -1})
	RenderThumbnail(ThumbnailInput{Ids: ids, Key: "thumb", Overview: &peaks.Overview{}, W: 300, H: 120, Progress: -1})
	require.Equal(t, ThumbnailResult{}, RenderThumbnail(ThumbnailInput{Ids: ids, Key: "thumb", Overview: ov, W: 0, H: 120}))

	canvas := widgethandle.Make(ids.PrepareStr("thumb").Derive())
	sm.ScriptResponse(canvas, c.PrimaryClickedResponseFlags)
	sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: 75, PosY: 10})
	res = RenderThumbnail(ThumbnailInput{Ids: ids, Key: "thumb", Overview: ov, W: 300, H: 120, Progress: -1})
	require.True(t, res.Clicked)
	require.InDelta(t, 0.25, res.Fraction, 1e-6)
}
