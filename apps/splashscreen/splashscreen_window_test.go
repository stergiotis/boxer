package splashscreen

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func registeredHeight(t *testing.T) (h uint16) {
	m, ok := app.DefaultRegistry.LookupManifest(ManifestId)
	require.True(t, ok)
	h = m.SurfaceHints.PreferredHeight
	return
}

func TestWindowFitsTheProgramArtwork(t *testing.T) {
	t.Cleanup(ClearProgram)
	declared := manifest.SurfaceHints
	assert.Equal(t, declared.PreferredHeight, registeredHeight(t))

	p := testProgram(t)
	p.Png = pngOf(t, 960, 480, color.NRGBA{A: 0xff})
	require.NoError(t, SetProgram(p))
	want := uint16(windowChromeHeight + (float64(declared.PreferredWidth)-windowSideMargin)/2 + .5)
	assert.Equal(t, want, registeredHeight(t))
	assert.Equal(t, want, newApp().Manifest().SurfaceHints.PreferredHeight)
	assert.Equal(t, declared.PreferredWidth, newApp().Manifest().SurfaceHints.PreferredWidth)

	// a tall picture keeps the declared height, a very wide one the minimum
	p.Png = pngOf(t, 100, 1000, color.NRGBA{A: 0xff})
	require.NoError(t, SetProgram(p))
	assert.Equal(t, declared.PreferredHeight, registeredHeight(t))
	p.Png = pngOf(t, 1000, 10, color.NRGBA{A: 0xff})
	require.NoError(t, SetProgram(p))
	assert.Equal(t, uint16(windowMinHeight), registeredHeight(t))

	ClearProgram()
	assert.Equal(t, declared.PreferredHeight, registeredHeight(t))
}

func TestWindowWithoutArtworkKeepsTheDeclaredSize(t *testing.T) {
	t.Cleanup(ClearProgram)
	p := testProgram(t)
	p.Png = nil
	require.NoError(t, SetProgram(p))
	assert.Equal(t, manifest.SurfaceHints, windowHints())
}
