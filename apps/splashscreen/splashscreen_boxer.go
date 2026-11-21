package splashscreen

import (
	"bytes"
	"image"
	_ "image/png" // register the PNG decoder for the embedded splash asset
	"sync"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
)

// boxer's own entry: the library's copyright, licence and NOTICE, and its
// artwork, all from this package's embedded assets.

const (
	boxerKey        = "boxer"
	boxerModule     = "github.com/stergiotis/boxer"
	boxerLicense    = "Licensed under the MIT License."
	splashAssetPath = "assets/splash.png"
	noticeAssetPath = "assets/NOTICE"
)

// boxerParty is boxer as a credited party.
func boxerParty() (p party) {
	loadAssets()
	p = party{
		key:       boxerKey,
		name:      "boxer",
		icon:      icons.PhSparkle,
		module:    boxerModule,
		copyright: vcs.CopyrightInfo(),
		license:   boxerLicense,
		notice:    boxerNotice,
		image:     boxerImage,
	}
	return
}

// boxerImage returns boxer's artwork, decoded once.
func boxerImage() (img decodedImage, err error) {
	loadAssets()
	img, err = boxerArt, boxerArtErr
	return
}

// The assets are loaded once, lazily, into package-level state shared
// read-only across every window: the data is immutable and each window's
// Image widget carries its own id, so sharing the upload source avoids
// re-decoding ~0.5 MB per window. Loading lazily (rather than in init)
// keeps the cost off the startup path when the window is never opened.
//
// A read or decode failure of the artwork is recorded in boxerArtErr and
// shown as a degraded pane rather than a hard error.
var (
	assetsOnce  sync.Once
	boxerArt    decodedImage
	boxerArtErr error
	boxerNotice string
)

func loadAssets() {
	assetsOnce.Do(func() {
		if data, err := assetsFS.ReadFile(noticeAssetPath); err == nil {
			boxerNotice = string(data)
		} else {
			boxerNotice = "NOTICE asset is missing from this build."
		}
		boxerArt, boxerArtErr = decodeBoxerArt()
	})
}

// decodeBoxerArt decodes the embedded artwork and maps its luminance
// through an IDS sequential palette via a 256-entry LUT. Default: the
// design-system grayscale ramp (Crameri grayC, which runs t=0→white …
// t=1→black, so luminance is inverted to keep the artwork's tonality —
// dark stays dark). When the KEELSON_EASTEREGG toggle is on, viridis
// instead (t runs dark→bright, no inversion). Alpha is carried through per
// pixel.
func decodeBoxerArt() (img decodedImage, err error) {
	data, err := assetsFS.ReadFile(splashAssetPath)
	if err != nil {
		err = eh.Errorf("read splash asset: %w", err)
		return
	}
	src, _, decErr := image.Decode(bytes.NewReader(data))
	if decErr != nil {
		err = eh.Errorf("decode splash png: %w", decErr)
		return
	}
	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= 0 || h <= 0 {
		err = eb.Build().Int("width", w).Int("height", h).Errorf("splash png has empty bounds")
		return
	}
	egg := app.EasterEgg.Get()
	var paletteLUT [256]uint32
	for n := range paletteLUT {
		var c8 styletokens.RGBA8
		if egg {
			c8 = styletokens.Sequential(styletokens.SequentialViridis, float32(n)/255.0)
		} else {
			c8 = styletokens.Sequential(styletokens.SequentialGrayC, 1.0-float32(n)/255.0)
		}
		paletteLUT[n] = uint32(c8.R)<<24 | uint32(c8.G)<<16 | uint32(c8.B)<<8
	}
	px := make([]uint32, w*h)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := src.At(x, y).RGBA()
			// Rec.601 luma (weights sum to 256). Equals the gray value for
			// the grayscale asset, and stays correct for a colour image.
			lum := (77*(r>>8) + 150*(g>>8) + 29*(bl>>8)) >> 8
			px[i] = paletteLUT[lum] | uint32(a>>8)
			i++
		}
	}
	img = decodedImage{pixels: px, w: uint32(w), h: uint32(h), version: 1}
	return
}
