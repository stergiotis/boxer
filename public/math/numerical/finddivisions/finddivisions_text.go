package finddivisions

import (
	"fmt"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

type TextMeasurerI interface {
	MeasureSingleLine(s string, fontSizePt float64, dpi float64) (xAdvancePx float64)
}

type CachingTextMeasurer struct {
	cache    map[string]float64
	measurer TextMeasurerI
	size     int
	Hits     uint64
	Misses   uint64
}
func (inst *CachingTextMeasurer) Reset() {
	clear(inst.cache)
	inst.Hits = 0
	inst.Misses = 0
}

var _ TextMeasurerI = (*CachingTextMeasurer)(nil)

func NewCachingTextMeasurer(m TextMeasurerI, sz int) *CachingTextMeasurer {
	return &CachingTextMeasurer{
		cache:    make(map[string]float64, sz),
		measurer: m,
		size:     sz,
		Hits:     0,
		Misses:   0,
	}
}
func (inst *CachingTextMeasurer) MeasureSingleLine(s string, fontSizePt float64, dpi float64) (xAdvancePx float64) {
	c := inst.cache
	k := fmt.Sprintf("%s:%g:%g", s, fontSizePt, dpi)
	var has bool
	xAdvancePx, has = c[k]
	if has {
		inst.Hits++
		return
	}
	if len(c) == inst.size {
		// randomly delete an item (range over map is randomized in go)
		for k, _ := range c {
			delete(c, k)
			break
		}
	}
	xAdvancePx = inst.measurer.MeasureSingleLine(s, fontSizePt, dpi)
	c[k] = xAdvancePx
	inst.Misses++
	return
}

type TextMeasurerGoHarfbuzz struct {
	face   *font.Face
	shaper shaping.HarfbuzzShaper
}

var _ TextMeasurerI = (*TextMeasurerGoHarfbuzz)(nil)

func (inst *TextMeasurerGoHarfbuzz) MeasureSingleLine(s string, fontSizePt float64, dpi float64) (xAdvancePx float64) {
	runes := []rune(s)

	// 1 unit = 1/64th of a point.
	fixedSize := fixed.Int26_6(fontSizePt * 64)

	input := shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: di.DirectionLTR,
		Face:      inst.face,
		Size:      fixedSize,
		Script:    language.Latin,
	}

	output := inst.shaper.Shape(input)

	var totalAdvance fixed.Int26_6
	for _, glyph := range output.Glyphs {
		totalAdvance += glyph.Advance
	}

	// Convert 26.6 fixed point back to float pixels
	// (Value / 64) gives points, then scale by DPI
	widthPts := float64(totalAdvance) / 64.0
	widthPx := widthPts * dpi / 72.0

	return widthPx
}

func NewTextMeasurerGoHarfbuzz(face *font.Face) *TextMeasurerGoHarfbuzz {
	return &TextMeasurerGoHarfbuzz{
		face:   face,
		shaper: shaping.HarfbuzzShaper{},
	}
}
