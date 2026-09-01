package gloss

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The geographic coordinate gloss: one axis of a WGS-84 position, shown the
// way a position is read rather than as the number it is stored as.
//
// It is not a member of the quantity family even though a coordinate is an
// angle. A quantity's presentation is a matter of units; a coordinate's is a
// convention — degrees and decimal minutes with a hemisphere letter, which is
// what charts, almanacs and every GPS display use, and which no amount of unit
// conversion produces from a signed float.
const MediaTypeCoordinate = "gloss/coordinate"

// ParamAxis is the coordinate's required parameter: which axis the column
// holds. It is required because the hemisphere letters differ and nothing in
// the value distinguishes them — 7.5 is a valid latitude and a valid
// longitude, and only the column knows which.
const ParamAxis = "axis"

// The axes, as written after `axis=`.
const (
	AxisLatitude  = "lat"
	AxisLongitude = "lon"
)

// The coordinate presentations, as written after `as=`.
const (
	// CoordAsDM is degrees and decimal minutes — 47°04.943'N. The marine
	// convention, and the default.
	CoordAsDM = "dm"
	// CoordAsDMS is degrees, minutes and seconds — 47°04'56.6"N. The survey
	// and aviation convention.
	CoordAsDMS = "dms"
	// CoordAsDeg is signed decimal degrees with more precision than a raw
	// float64 rendering would show, and no hemisphere letter — the form to
	// copy into something that parses coordinates.
	CoordAsDeg = "deg"
)

var coordAxes = []string{AxisLatitude, AxisLongitude}
var coordPresentations = []string{CoordAsDM, CoordAsDMS, CoordAsDeg}

type coordinateGloss struct{}

var _ GlossI = coordinateGloss{}

func (coordinateGloss) MediaType() string { return MediaTypeCoordinate }
func (coordinateGloss) Doc() string {
	return "a latitude or longitude in decimal degrees, shown as degrees and decimal minutes with a hemisphere"
}
func (coordinateGloss) Params() []ParamSpec {
	return []ParamSpec{
		{Name: ParamAxis, Doc: "which axis the column holds", Values: coordAxes},
		{Name: ParamAs, Doc: "the presentation; defaults to dm", Values: coordPresentations},
	}
}
func (coordinateGloss) Affinities() []string { return nil }
func (inst coordinateGloss) Bind(params map[string]string) (InstanceI, error) {
	axis, ok := params[ParamAxis]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypeCoordinate).
			Str("accepted", strings.Join(coordAxes, "|")).
			Errorf(MediaTypeCoordinate + " requires " + ParamAxis +
				"=<axis>: the hemisphere letters differ and the value does not say which axis it is")
	}
	as := params[ParamAs]
	if as == "" {
		as = CoordAsDM
	}
	return &coordinateInstance{params: params, axis: axis, as: as}, nil
}

type coordinateInstance struct {
	params map[string]string
	axis   string
	as     string
}

var _ InstanceI = (*coordinateInstance)(nil)

func (inst *coordinateInstance) Gloss() GlossI             { return coordinateGloss{} }
func (inst *coordinateInstance) Params() map[string]string { return inst.params }
func (inst *coordinateInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypeCoordinate, numericOnly, kind)
}
func (inst *coordinateInstance) Inline(cell CellI) Inline {
	v, ok := cell.Float64()
	if !ok {
		return Inline{Text: cell.Text()}
	}
	return Inline{Text: FormatCoordinate(v, inst.axis, inst.as)}
}

// coordLimit is the valid magnitude per axis.
func coordLimit(axis string) float64 {
	if axis == AxisLatitude {
		return 90
	}
	return 180
}

// FormatCoordinate renders one axis of a position. A value outside the axis's
// range is returned as a plain number with a marker rather than dressed up: a
// latitude of 200 is a broken column, and formatting it as 200°00.000'N would
// present the breakage as a place.
func FormatCoordinate(deg float64, axis, as string) string {
	if math.IsNaN(deg) || math.IsInf(deg, 0) || math.Abs(deg) > coordLimit(axis) {
		return strconv.FormatFloat(deg, 'f', -1, 64) + " (out of range)"
	}
	if as == CoordAsDeg {
		return strconv.FormatFloat(deg, 'f', 6, 64) + "°"
	}
	hemi := hemisphere(deg, axis)
	abs := math.Abs(deg)
	d := math.Floor(abs)
	switch as {
	case CoordAsDMS:
		minutes := (abs - d) * 60
		m := math.Floor(minutes)
		sec := (minutes - m) * 60
		// Carry the rounding: 59.96" must not print as 60.0".
		if sec >= 59.95 {
			sec, m = 0, m+1
		}
		if m >= 60 {
			m, d = 0, d+1
		}
		return fmt.Sprintf("%.0f°%02.0f'%04.1f\"%s", d, m, sec, hemi)
	default: // CoordAsDM
		minutes := (abs - d) * 60
		if minutes >= 59.9995 {
			minutes, d = 0, d+1
		}
		return fmt.Sprintf("%.0f°%06.3f'%s", d, minutes, hemi)
	}
}

// hemisphere is the letter for a signed value on an axis. Zero takes the
// positive letter: the equator and the prime meridian have to be written
// somehow, and N and E are the conventional choices.
func hemisphere(deg float64, axis string) string {
	if axis == AxisLatitude {
		if deg < 0 {
			return "S"
		}
		return "N"
	}
	if deg < 0 {
		return "W"
	}
	return "E"
}
