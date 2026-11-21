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
	// NameElevation is 400 × 300 of signed 16-bit samples, metres of an
	// elevation model: hills up to about 1700, a basin down to about −500,
	// and a border of ElevationBorder pixels holding [ElevationInvalid],
	// declared invalid — the way a raster marks where it has no measurement
	// (ADR-0398 §SD2).
	NameElevation = "elevation"
	// NameDouble is 320 × 240 of 64-bit float samples: ripples of about
	// 10⁻⁴ on an offset of 10⁹, which 32-bit floats could not tell apart,
	// with a diagonal line of NaN.
	NameDouble = "double"
	// NameCounts is 320 × 240 of signed 32-bit samples, a background-
	// subtracted count: mostly a few hundred either side of zero, one source
	// reaching about 1.5 × 10⁹ and a corner of [CountsInvalid], declared
	// invalid.
	NameCounts = "counts"
)

const (
	// ElevationInvalid is the elevation sample's declared invalid value.
	ElevationInvalid = -32768
	// ElevationBorder is the width of the elevation sample's border of
	// invalid samples.
	ElevationBorder = 8
)

// CountsInvalid is the counts sample's declared invalid value, which fills
// its top left 16 × 16 pixels.
const CountsInvalid = math.MinInt32

// FloatInvalid is the float sample's block of invalid samples: columns
// [40, 120) and rows [300, 380).
var FloatInvalid = image.Rect(40, 300, 120, 380)

// ScaledTransform is the scaled sample's value transform.
var ScaledTransform = imaging.ValueTransform{Scale: 0.01, Offset: -40}

// Names lists the sample images in the order a menu shows them.
func Names() []string {
	return []string{NameField, NameColour, NameSmall, NameFloat, NameScaled, NameElevation, NameDouble, NameCounts}
}

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
	case NameElevation:
		return elevation()
	case NameDouble:
		return double()
	case NameCounts:
		return counts()
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

func elevation() (imaging.Image, error) {
	const w, h = 400, 300
	s := make([]int16, w*h)
	for y := range h {
		for x := range w {
			if x < ElevationBorder || y < ElevationBorder || x >= w-ElevationBorder || y >= h-ElevationBorder {
				s[y*w+x] = ElevationInvalid
				continue
			}
			hx, hy := float64(x-270), float64(y-110)
			hills := 1500*math.Exp(-(hx*hx+hy*hy)/(2*60*60)) + 300*math.Sin(float64(x)/31)*math.Cos(float64(y)/27)
			bx, by := float64(x-110), float64(y-200)
			basin := -650 * math.Exp(-(bx*bx+by*by)/(2*45*45))
			s[y*w+x] = int16(math.Round(250 + hills + basin))
		}
	}
	p, err := imaging.NewPlaneI16(w, h, s)
	if err != nil {
		return imaging.Image{}, err
	}
	if p, err = p.WithInvalid(ElevationInvalid); err != nil {
		return imaging.Image{}, err
	}
	return imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic elevation"})
}

func double() (imaging.Image, error) {
	const w, h = 320, 240
	s := make([]float64, w*h)
	for y := range h {
		for x := range w {
			v := 1e9 + 1e-4*math.Sin(float64(x)/9)*math.Cos(float64(y)/13)
			if x == y {
				v = math.NaN()
			}
			s[y*w+x] = v
		}
	}
	p, err := imaging.NewPlaneF64(w, h, s)
	if err != nil {
		return imaging.Image{}, err
	}
	return imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic double"})
}

func counts() (imaging.Image, error) {
	const w, h = 320, 240
	s := make([]int32, w*h)
	for y := range h {
		for x := range w {
			if x < 16 && y < 16 {
				s[y*w+x] = CountsInvalid
				continue
			}
			dx, dy := float64(x-200), float64(y-90)
			source := 1.5e9 * math.Exp(-(dx*dx+dy*dy)/(2*6*6))
			noise := 300 * math.Sin(float64(x*7+y*13))
			s[y*w+x] = int32(math.Round(source + noise))
		}
	}
	p, err := imaging.NewPlaneI32(w, h, s)
	if err != nil {
		return imaging.Image{}, err
	}
	if p, err = p.WithInvalid(CountsInvalid); err != nil {
		return imaging.Image{}, err
	}
	return imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "synthetic counts"})
}

// The names [StackByName] takes.
const (
	// StackBands is 160 × 120 of 16-bit samples along two axes: "time", a
	// selector of three acquisitions 16 days apart, and "band", a band axis
	// of four spectral bands labelled B2, B3, B4 and B8 with their centre
	// wavelengths in nanometres. 0 is declared invalid and fills a strip
	// that moves with the time, as a swath edge does; the placement is a
	// projected reference system at 10 m a pixel (ADR-0398 §SD3, §SD4).
	StackBands = "bands"
)

// BandsInvalid is the bands stack's declared invalid value.
const BandsInvalid = 0

// BandsPlacement is the bands stack's placement.
var BandsPlacement = imaging.Placement{
	Affine: [6]float64{500_000, 10, 0, 5_200_000, 0, -10},
	Frame: imaging.Frame{Kind: imaging.FrameKindReferenceSystem, Code: "EPSG:32632",
		AxisNames: [2]string{"E", "N"}, Units: [2]string{"m", "m"}},
	Source: "synthetic",
}

// StackNames lists the sample stacks in the order a menu shows them.
func StackNames() []string { return []string{StackBands} }

// StackByName makes the named sample stack. Its provenance names it as
// synthetic.
func StackByName(name string) (stack imaging.Stack, err error) {
	switch name {
	case StackBands:
		return bands()
	}
	return stack, eb.Build().Str("name", name).Strs("names", StackNames()).Errorf("no sample stack of that name")
}

func bands() (imaging.Stack, error) {
	const w, h = 160, 120
	axes := []imaging.Axis{
		{Name: "time", Role: imaging.AxisRoleSelector, Size: 3, Unit: "d", Coords: []float64{0, 16, 32},
			Labels: []string{"day 0", "day 16", "day 32"}},
		{Name: "band", Role: imaging.AxisRoleBand, Size: 4, Unit: "nm", Coords: []float64{490, 560, 665, 842},
			Labels: []string{"B2", "B3", "B4", "B8"}},
	}
	// Reflectance-like levels per band over two covers — water on the left,
	// vegetation on the right — so that B8 against B4 separates them.
	water := [4]float64{900, 700, 500, 300}
	green := [4]float64{500, 900, 400, 4000}
	planes := make([]imaging.Plane, 0, 12)
	for ti := range 3 {
		for bi := range 4 {
			s := make([]uint16, w*h)
			for y := range h {
				for x := range w {
					if x < 6+4*ti {
						s[y*w+x] = BandsInvalid
						continue
					}
					f := 0.5 + 0.5*math.Tanh(float64(x-70-6*ti)/8)
					v := water[bi]*(1-f) + green[bi]*f
					v *= 1 + 0.05*math.Sin(float64(y)/7+float64(ti))
					s[y*w+x] = uint16(max(1, math.Round(v)))
				}
			}
			p, err := imaging.NewPlaneU16(w, h, s)
			if err != nil {
				return imaging.Stack{}, err
			}
			if p, err = p.WithInvalid(BandsInvalid); err != nil {
				return imaging.Stack{}, err
			}
			planes = append(planes, p)
		}
	}
	return imaging.NewStack(axes, planes, 0, BandsPlacement, imaging.Provenance{SourceName: "synthetic bands"})
}
