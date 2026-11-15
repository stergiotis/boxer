package samples

import (
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
)

// Names lists the sample images in the order a menu shows them.
func Names() []string { return []string{NameField, NameColour, NameSmall} }

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
