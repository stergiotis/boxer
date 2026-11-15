package samples

import (
	"image"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// The names [ByName] takes.
const (
	// NameField is a 1400 × 1000 plane of 16-bit samples: a slow wave
	// between about 1500 and 4500, one bright blob reaching about 40000 and
	// a column of saturated pixels — the shape of a detector frame, where
	// the nominal range shows almost nothing and a stretch shows the rest.
	NameField = "field"
	// NameColour is 640 × 480 of 8-bit RGBA: red rises with the column,
	// green with the row, blue in blocks, and the right third fades to
	// transparent.
	NameColour = "colour"
	// NameSmall is 48 × 32 of 8-bit samples: few enough pixels that a
	// fitted view shows each as a square.
	NameSmall = "small"
	// NameFloat is 640 × 480 of 32-bit float samples spanning six decades —
	// a background near 1, one peak near 10⁵ and a trough down to −50 — with
	// a block of invalid (NaN) samples: what a log or asinh stretch and the
	// handling of invalid samples are for.
	NameFloat = "float"
	// NameScaled is 480 × 320 of 16-bit samples under a value transform,
	// value = 0.01 × stored − 40: a sample stands for a quantity the file
	// does not store directly, as a detector's counts stand for a
	// temperature. Limits, the histogram and statistics are in values.
	NameScaled = "scaled"
)

// FloatInvalid is the float sample's block of invalid samples: columns
// [40, 120) and rows [300, 380).
var FloatInvalid = image.Rect(40, 300, 120, 380)

// ScaledTransform is the scaled sample's value transform.
var ScaledTransform = imaging.ValueTransform{Scale: 0.01, Offset: -40}

// Names lists the sample images in the order a menu shows them.
func Names() []string { return []string{NameField, NameColour, NameSmall, NameFloat, NameScaled} }

// ByName makes the named sample image. Its provenance names it as
// synthetic.
func ByName(name string) (img imaging.Image, err error) {
	switch name {
	case NameField:
		return field()
	case NameColour:
		return colour()
	case NameSmall:
		return small()
	case NameFloat:
		return float()
	case NameScaled:
		return scaled()
	}
	return img, eb.Build().Str("name", name).Strs("names", slices.Clone(Names())).Errorf("no sample image of that name")
}

func field() (imaging.Image, error) {
	const w, h = 1400, 1000
	s := make([]uint16, w*h)
	for y := range h {
		for x := range w {
			wave := (0.5 + 0.5*math.Sin(float64(x)/40)) * (0.5 + 0.5*math.Cos(float64(y)/55))
			dx, dy := float64(x-980), float64(y-330)
			blob := 36000 * math.Exp(-(dx*dx+dy*dy)/(2*45*45))
			v := 1500 + 3000*wave + blob
			if x == 200 && y%7 == 0 {
				v = 65535
			}
			s[y*w+x] = uint16(min(v, 65535))
		}
	}
	p, err := imaging.NewPlaneU16(w, h, s)
	if err != nil {
		return imaging.Image{}, err
	}
	return imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic field"})
}

func colour() (imaging.Image, error) {
	const w, h = 640, 480
	var s [4][]uint8
	for i := range s {
		s[i] = make([]uint8, w*h)
	}
	for y := range h {
		for x := range w {
			i := y*w + x
			s[0][i] = uint8(x * 255 / (w - 1))
			s[1][i] = uint8(y * 255 / (h - 1))
			s[2][i] = uint8(((x/40 + y/40) % 2) * 200)
			s[3][i] = 255
			if x > 2*w/3 {
				s[3][i] = uint8(255 - (x-2*w/3)*255/(w/3))
			}
		}
	}
	planes := make([]imaging.Plane, 4)
	for i := range planes {
		var err error
		if planes[i], err = imaging.NewPlaneU8(w, h, s[i]); err != nil {
			return imaging.Image{}, err
		}
	}
	return imaging.NewImage(imaging.ColorModelRGBA, planes, imaging.Provenance{SourceName: "synthetic colour"})
}

func small() (imaging.Image, error) {
	const w, h = 48, 32
	s := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			s[y*w+x] = uint8((x*5 + y*3) % 256)
		}
	}
	p, err := imaging.NewPlaneU8(w, h, s)
	if err != nil {
		return imaging.Image{}, err
	}
	return imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic small"})
}

func float() (imaging.Image, error) {
	const w, h = 640, 480
	s := make([]float32, w*h)
	for y := range h {
		for x := range w {
			dx, dy := float64(x-430), float64(y-170)
			peak := 1e5 * math.Exp(-(dx*dx+dy*dy)/(2*30*30))
			tx, ty := float64(x-160), float64(y-140)
			trough := -51 * math.Exp(-(tx*tx+ty*ty)/(2*40*40))
			ripple := 0.5 + 0.4*math.Sin(float64(x)/23)*math.Cos(float64(y)/31)
			v := float32(ripple + peak + trough)
			if image.Pt(x, y).In(FloatInvalid) {
				v = float32(math.NaN())
			}
			s[y*w+x] = v
		}
	}
	p, err := imaging.NewPlaneF32(w, h, s)
	if err != nil {
		return imaging.Image{}, err
	}
	return imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic float"})
}

func scaled() (imaging.Image, error) {
	const w, h = 480, 320
	s := make([]uint16, w*h)
	for y := range h {
		for x := range w {
			// Stored 4000 … 9000 across the image, a warm spot reaching
			// 30000: values −0 … 50 and up to 260.
			dx, dy := float64(x-330), float64(y-120)
			spot := 21000 * math.Exp(-(dx*dx+dy*dy)/(2*25*25))
			s[y*w+x] = uint16(4000 + 5000*float64(x)/float64(w-1) + spot)
		}
	}
	p, err := imaging.NewPlaneU16(w, h, s)
	if err != nil {
		return imaging.Image{}, err
	}
	if p, err = p.WithTransform(ScaledTransform); err != nil {
		return imaging.Image{}, err
	}
	return imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic scaled"})
}
