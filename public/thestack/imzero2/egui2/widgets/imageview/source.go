package imageview

import (
	"context"
	"image"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// tileSize is the side of a tile in image pixels (ADR-0373 §SD6).
const tileSize = 512

// checkerSize is the side of one square of the checkerboard under a
// transparent pixel, in image pixels.
const checkerSize = 8

// shown is everything a worker needs to map a tile, fixed at the moment the
// viewer published it: the image, which plane or the image as stored, and
// the mappers compiled from the recipe. It is never modified once stored.
type shown struct {
	img     imaging.Image
	plane   imaging.Plane // the single plane shown, or the zero plane for a colour composite
	alpha   imaging.Plane // the alpha drawn with a single plane, or the zero plane
	planes  *display.PlaneMapper
	colours *display.ColorMapper
	checker [2]uint32
}

// tileSource answers portolan's loader with tiles of the image as currently
// shown. Workers read the current state through an atomic pointer; the
// viewer replaces it and then invalidates the map's tiles.
type tileSource struct {
	cur atomic.Pointer[shown]
}

var _ portolan.TilePixelsI = (*tileSource)(nil)

// TilePixels maps the tile at (coords.X, coords.Y) of the full-resolution
// level. Pixels outside the image are transparent; pixels inside it are
// opaque, a transparent sample having been laid over the checkerboard.
func (inst *tileSource) TilePixels(_ context.Context, coords portolan.TileCoords) (px []uint32, w, h int, err error) {
	if coords.Z != 0 {
		return nil, 0, 0, eb.Build().Int("level", coords.Z).Errorf("image has no tiles at this level")
	}
	px = make([]uint32, tileSize*tileSize)
	sh := inst.cur.Load()
	if sh == nil || sh.img.IsZero() {
		return px, tileSize, tileSize, nil
	}
	x0, y0 := coords.X*tileSize, coords.Y*tileSize
	rect := image.Rect(x0, y0, x0+tileSize, y0+tileSize).Intersect(image.Rect(0, 0, sh.img.Width(), sh.img.Height()))
	if rect.Empty() {
		return px, tileSize, tileSize, nil
	}
	dst := px
	whole := rect.Dx() == tileSize && rect.Dy() == tileSize
	if !whole {
		dst = make([]uint32, rect.Dx()*rect.Dy())
	}
	if sh.colours != nil {
		err = sh.colours.Map(dst, sh.img, rect)
	} else {
		err = sh.planes.Map(dst, sh.plane, sh.alpha, rect)
	}
	if err != nil {
		return nil, 0, 0, err
	}
	overChecker(dst, rect, sh.checker)
	if !whole {
		for y := range rect.Dy() {
			copy(px[y*tileSize:y*tileSize+rect.Dx()], dst[y*rect.Dx():(y+1)*rect.Dx()])
		}
	}
	return px, tileSize, tileSize, nil
}

// overChecker lays every pixel that is not opaque over a checkerboard fixed
// to the image's pixel grid and makes it opaque.
func overChecker(px []uint32, rect image.Rectangle, checker [2]uint32) {
	rw := rect.Dx()
	for i, p := range px {
		a := p & 0xff
		if a == 0xff {
			continue
		}
		col, row := rect.Min.X+i%rw, rect.Min.Y+i/rw
		k := checker[(col/checkerSize+row/checkerSize)&1]
		mix := func(shift uint) uint32 {
			return ((p>>shift&0xff)*a + (k>>shift&0xff)*(0xff-a) + 127) / 0xff
		}
		px[i] = mix(24)<<24 | mix(16)<<16 | mix(8)<<8 | 0xff
	}
}

// checkerColours are the two tones of the checkerboard, taken from the
// theme's neutral surfaces so it reads as "nothing here" in either theme.
func checkerColours() [2]uint32 {
	return [2]uint32{styletokens.NeutralBgSurface.AsHex(), styletokens.NeutralSubtle.AsHex()}
}
