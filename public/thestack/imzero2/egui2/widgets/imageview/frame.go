package imageview

import (
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// FramePosition is a point of an image in the frame its placement names
// (ADR-0398 §SD4): where it is in continuous pixel coordinates — pixel
// (col, row) covers [col, col+1) × [row, row+1), row 0 at the top — and its
// two coordinates in the frame there. Its text writes each coordinate to the
// decimals that resolve a tenth of a pixel, and never fewer than two.
type FramePosition struct {
	// X and Y are the continuous pixel coordinates.
	X, Y float64
	// A and B are the frame coordinates: easting and northing, or x and y,
	// in the units of a reference system or a physical scale; longitude in
	// [0, 360) and latitude, in decimal degrees, on the sky.
	A, B  float64
	Frame imaging.Frame
	// Step is what one pixel spans in each coordinate, in its unit: what the
	// text takes its decimals from.
	Step [2]float64
}

// FramePositionAt is the position of continuous pixel coordinates (x, y)
// under a placement, through [imaging.Placement.World]. The zero placement,
// and a point the frame has no finite coordinates for, report ok false.
func FramePositionAt(p imaging.Placement, x, y float64) (pos FramePosition, ok bool) {
	a, b, ok := p.World(x, y)
	if !ok {
		return pos, false
	}
	sx, sy := p.PixelSize()
	step := min(sx, sy)
	pos = FramePosition{X: x, Y: y, A: a, B: b, Frame: p.Frame, Step: [2]float64{step, step}}
	if p.Frame.Kind == imaging.FrameKindCelestialGnomonic {
		// A degree of longitude is cos(latitude) degrees of arc on the sky.
		if c := math.Cos(b * math.Pi / 180); c > 1e-6 {
			pos.Step[0] = step / c
		}
	}
	return pos, true
}

// AxisName is what coordinate i (0 or 1) is called: the frame's name for
// it, or x and y when it gives none.
func (inst FramePosition) AxisName(i int) string {
	if n := inst.Frame.AxisNames[i]; n != "" {
		return n
	}
	return [2]string{"x", "y"}[i]
}

// Coordinate writes coordinate i (0 or 1) with its name and unit:
// "E 2600000.25 m", "Dec 2.198761°".
func (inst FramePosition) Coordinate(i int) string {
	v := [2]float64{inst.A, inst.B}[i]
	unit := inst.Frame.Units[i]
	if inst.Frame.Kind == imaging.FrameKindCelestialGnomonic {
		unit = "deg"
	}
	return inst.AxisName(i) + " " + formatCoordinate(v, inst.Step[i], unit)
}

// Text is the position in one line: both coordinates, then the frame's code
// in parentheses when it has one — "E 2600000.25 m  N 1199999.88 m
// (EPSG:2056)".
func (inst FramePosition) Text() string {
	s := inst.Coordinate(0) + "  " + inst.Coordinate(1)
	if inst.Frame.Code != "" {
		s += "  (" + inst.Frame.Code + ")"
	}
	return s
}

// isDegrees reports whether a unit names degrees.
func isDegrees(unit string) bool {
	switch strings.ToLower(unit) {
	case "deg", "degree", "degrees", "°":
		return true
	}
	return false
}

// formatCoordinate writes a coordinate to the decimals that resolve a tenth
// of step, at least two and at most twelve, with its unit: degrees as °,
// any other unit after a space, none when it is unknown.
func formatCoordinate(v, step float64, unit string) string {
	decimals := 2
	if step > 0 && !math.IsInf(step, 0) {
		decimals = min(max(int(math.Ceil(-math.Log10(step)))+1, 2), 12)
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	switch {
	case isDegrees(unit):
		return s + "°"
	case unit != "":
		return s + " " + unit
	}
	return s
}

// DescribePlacement says in one line what a placement places the pixels in,
// for a person: the frame, the size of a pixel in it, and the statement of
// the file it was derived from. What the file declared and the placement
// does not apply ([imaging.Placement.Unapplied]) is not in it; a host lists
// that beside it. The zero placement is "not placed".
func DescribePlacement(p imaging.Placement) string {
	f := p.Frame
	sx, sy := p.PixelSize()
	names := func() string {
		pos := FramePosition{Frame: f}
		s := pos.AxisName(0) + ", " + pos.AxisName(1)
		if f.Units[0] != "" && f.Units[0] == f.Units[1] {
			s += " in " + f.Units[0]
		}
		return s
	}
	var b strings.Builder
	switch f.Kind {
	case imaging.FrameKindReferenceSystem:
		code := f.Code
		if code == "" {
			code = "an unnamed reference system"
		}
		b.WriteString("placed in " + code + " (" + names() + "; coordinates as the system defines them, not converted)")
		b.WriteString(" · pixel " + formatSize(sx, sy, f.Units[0]))
	case imaging.FrameKindPhysical:
		b.WriteString("placed at a physical scale (" + names() + "; the origin means nothing)")
		b.WriteString(" · pixel " + formatSize(sx, sy, f.Units[0]))
	case imaging.FrameKindCelestialGnomonic:
		b.WriteString("placed on the sky by a gnomonic (TAN) projection about ")
		ref := FramePosition{A: f.RefLon, B: f.RefLat, Frame: f, Step: [2]float64{1e-5, 1e-5}}
		b.WriteString(ref.Coordinate(0) + ", " + ref.Coordinate(1))
		if f.Code != "" {
			b.WriteString(" (" + f.Code + ")")
		}
		b.WriteString("; coordinates in decimal degrees")
		b.WriteString(" · pixel " + formatArc(sx, sy))
	default:
		return "not placed"
	}
	if p.Source != "" {
		b.WriteString(" · from " + p.Source)
	}
	return b.String()
}

// formatSize writes a pixel's two sides in a unit, to four significant
// digits: "10 × 10 m".
func formatSize(sx, sy float64, unit string) string {
	s := strconv.FormatFloat(sx, 'g', 4, 64) + " × " + strconv.FormatFloat(sy, 'g', 4, 64)
	switch {
	case isDegrees(unit):
		return s + "°"
	case unit != "":
		return s + " " + unit
	}
	return s
}

// formatArc writes a pixel's two sides on the sky, in degrees, as
// arcseconds when they are under a minute of arc.
func formatArc(sx, sy float64) string {
	if max(sx, sy) < 1.0/60 {
		return strconv.FormatFloat(sx*3600, 'g', 4, 64) + " × " + strconv.FormatFloat(sy*3600, 'g', 4, 64) + "″"
	}
	return strconv.FormatFloat(sx, 'g', 4, 64) + " × " + strconv.FormatFloat(sy, 'g', 4, 64) + "°"
}
