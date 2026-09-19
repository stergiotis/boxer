package vectorfield

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FieldFunc is a field given in closed form: the east and north components
// at a position and step. ok false is a missing sample.
type FieldFunc func(lon, lat float64, step int) (u, v float32, ok bool)

// AnalyticLoader is a [StepLoaderI] that samples a [FieldFunc] onto a grid —
// a field that needs no data file, for demos and for tests whose expected
// values are known exactly.
type AnalyticLoader struct {
	West, North float64
	DLon, DLat  float64
	Cols, Rows  int
	Fn          FieldFunc
}

var _ StepLoaderI = (*AnalyticLoader)(nil)

// NewGlobalAnalyticLoader covers the globe at the given spacing in degrees,
// from -180 without a repeated column, poles included.
func NewGlobalAnalyticLoader(spacing float64, fn FieldFunc) (inst *AnalyticLoader) {
	return &AnalyticLoader{
		West: -180, North: 90,
		DLon: spacing, DLat: spacing,
		Cols: int(math.Round(360 / spacing)),
		Rows: int(math.Round(180/spacing)) + 1,
		Fn:   fn,
	}
}

// LoadStepE implements [StepLoaderI].
func (inst *AnalyticLoader) LoadStepE(ctx context.Context, step int) (grid Grid, err error) {
	if inst.Fn == nil || inst.Cols < 2 || inst.Rows < 2 {
		err = eb.Build().Int("cols", inst.Cols).Int("rows", inst.Rows).Errorf("an analytic loader needs a field and a grid")
		return
	}
	n := inst.Cols * inst.Rows
	grid = Grid{
		West: inst.West, North: inst.North,
		DLon: inst.DLon, DLat: inst.DLat,
		Cols: inst.Cols, Rows: inst.Rows,
		U: make([]float32, n), V: make([]float32, n),
	}
	nan := float32(math.NaN())
	for r := range inst.Rows {
		if err = ctx.Err(); err != nil {
			return
		}
		lat := inst.North - float64(r)*inst.DLat
		for c := range inst.Cols {
			lon := inst.West + float64(c)*inst.DLon
			u, v, ok := inst.Fn(lon, lat, step)
			i := r*inst.Cols + c
			if !ok {
				u, v = nan, nan
			}
			grid.U[i], grid.V[i] = u, v
		}
	}
	return
}

// Uniform is the field that is (u, v) everywhere.
func Uniform(u, v float32) (fn FieldFunc) {
	return func(_, _ float64, _ int) (float32, float32, bool) { return u, v, true }
}

// Rotation is a rigid anticlockwise rotation about (lon0, lat0) in the plane
// of longitude and latitude, speed per degree of distance from the centre.
// Its streamlines are circles, which is what makes it the fixture for an
// integrator: a particle that leaves its circle was moved wrongly.
func Rotation(lon0, lat0 float64, speedPerDegree float32) (fn FieldFunc) {
	return func(lon, lat float64, _ int) (float32, float32, bool) {
		return -speedPerDegree * float32(lat-lat0), speedPerDegree * float32(lon-lon0), true
	}
}

// Swirl is a westerly jet that meanders with longitude plus a row of vortices
// that drift east by driftPerStep degrees each step: closed circulations,
// calm centres, a fast band and a change over time, for a demo to show.
func Swirl(driftPerStep float64) (fn FieldFunc) {
	return func(lon, lat float64, step int) (float32, float32, bool) {
		rad := math.Pi / 180
		// The jet: strongest at 45° either side of the equator.
		jet := 22 * math.Exp(-sq((math.Abs(lat)-45)/12))
		u := jet * (1 + 0.25*math.Cos(3*lon*rad))
		v := 6 * math.Sin(3*lon*rad) * math.Exp(-sq((math.Abs(lat)-45)/18))
		// Vortices every 60° of longitude at ±30°, turning opposite ways.
		shift := driftPerStep * float64(step)
		for k := range 6 {
			for _, side := range [2]float64{1, -1} {
				cx := wrap180(-150 + 60*float64(k) + shift)
				cy := 30 * side
				dx := wrap180(lon - cx)
				dy := lat - cy
				g := 14 * side * math.Exp(-(dx*dx+dy*dy)/(2*9*9)) / 9
				u += -g * dy
				v += g * dx
			}
		}
		return float32(u), float32(v), true
	}
}

// Masked wraps a field so that it is missing wherever land reports true — a
// stand-in for an ocean current's coastline.
func Masked(fn FieldFunc, land func(lon, lat float64) bool) (masked FieldFunc) {
	return func(lon, lat float64, step int) (float32, float32, bool) {
		if land(lon, lat) {
			return 0, 0, false
		}
		return fn(lon, lat, step)
	}
}

func sq(x float64) float64 { return x * x }

func wrap180(lon float64) float64 {
	lon = math.Mod(lon+180, 360)
	if lon < 0 {
		lon += 360
	}
	return lon - 180
}
