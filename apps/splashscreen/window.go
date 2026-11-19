package splashscreen

import (
	"bytes"
	"image"
	_ "image/png" // register the PNG decoder for the program's artwork

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// The window's chrome around the artwork, in egui points: the host's title
// bar and toolbar, the party and page rows, and the margins. The artwork
// fills the rest of the window's width and as much height as its aspect
// asks for.
const (
	windowChromeHeight = 176
	windowSideMargin   = 24
	windowMinHeight    = 320
)

// windowHints is the splash window's initial size: the declared width and a
// height that fits the program's artwork under the tab rows, so the window
// opens without space left under a landscape picture or a tall one cut short.
// The height stays between windowMinHeight and the declared height. Without
// a program, or with artwork whose size cannot be read, the declared size.
func windowHints() (h app.SurfaceHints) {
	h = manifest.SurfaceHints
	program.mu.Lock()
	var src []byte
	if program.p != nil {
		src = program.p.Png
	}
	program.mu.Unlock()
	if len(src) == 0 {
		return
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return
	}
	artW := float64(h.PreferredWidth) - windowSideMargin
	height := windowChromeHeight + artW*float64(cfg.Height)/float64(cfg.Width)
	height = min(max(height, windowMinHeight), float64(manifest.SurfaceHints.PreferredHeight))
	h.PreferredHeight = uint16(height + .5)
	return
}

// applyWindowHints restates the registered manifest's window size, so the
// next window opens sized to the program's artwork.
func applyWindowHints() {
	h := windowHints()
	err := app.DefaultRegistry.OverrideManifests(map[app.AppIdT]app.ManifestOverride{ManifestId: {SurfaceHints: &h}})
	if err != nil {
		log.Warn().Err(err).Msg("splash window keeps its declared size")
	}
}
