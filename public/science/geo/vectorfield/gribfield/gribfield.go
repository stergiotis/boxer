// Package gribfield reads GRIB wind into the planes a vector field is made
// of (ADR-0292 SD4): pairs of eastward and northward components on a regular
// latitude/longitude grid, one pair per valid time, as a keelsonfield.Field.
//
// It reads one level of one run and refuses, by name, what it cannot place:
// another parameter or level beside the first, a second run, a grid that is
// not regular and unrotated, components relative to the grid (which would
// need a rotation), a component without its partner, or grids that differ.
package gribfield

import (
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/geo/grib"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/keelsonfield"
)

// The parameters read: discipline 0 (meteorological), category 2 (momentum).
const (
	disciplineMeteorological = 0
	categoryMomentum         = 2
	numberU                  = 2 // u-component of wind, eastward
	numberV                  = 3 // v-component of wind, northward
	// gridTemplateLatLon is template 3.0, regular latitude/longitude; 3.40,
	// the Gaussian grid, is not regular in latitude.
	gridTemplateLatLon = 0
)

// Options names the field; Name, Unit and SpeedMax go to the legend.
type Options struct {
	Name     string
	Unit     string
	SpeedMax float32
}

type step struct {
	valid time.Time
	u, v  []float32
}

type geometry struct {
	west, north, di, dj float64
	ni, nj              int
}

// ReadE reads every u and v field of buf into a field, its steps in time
// order.
func ReadE(buf []byte, opts Options) (f keelsonfield.Field, err error) {
	var (
		run     time.Time
		level   grib.Surface
		geo     geometry
		started bool
		steps   = map[time.Time]*step{}
	)
	for msg, scanErr := range grib.ScanBytes(buf) {
		if scanErr != nil {
			return f, eh.Errorf("gribfield: %w", scanErr)
		}
		for _, fld := range msg.Fields {
			p := fld.Product
			if msg.Discipline != disciplineMeteorological || p.Category != categoryMomentum || (p.Number != numberU && p.Number != numberV) {
				return f, eb.Build().Uint8("discipline", msg.Discipline).Uint8("category", p.Category).Uint8("number", p.Number).
					Errorf("gribfield: a field that is not a wind component")
			}
			if !started {
				run, level = msg.Ident.RefTime, p.Surface1
			}
			if !msg.Ident.RefTime.Equal(run) {
				return f, eb.Build().Time("run", run).Time("other", msg.Ident.RefTime).Errorf("gribfield: fields of more than one run")
			}
			if p.Surface1.Type != level.Type || p.Surface1.Value != level.Value {
				return f, eb.Build().Uint8("type", p.Surface1.Type).Float64("value", p.Surface1.Value).
					Errorf("gribfield: fields of more than one level")
			}
			valid, ok := p.ValidTime(run)
			if !ok {
				return f, eh.Errorf("gribfield: a field whose valid time has no duration unit")
			}
			if ll := fld.Grid.LatLon; ll != nil && ll.UVRelativeToGrid {
				return f, eh.Errorf("gribfield: components relative to the grid need a rotation this reader does not do")
			}
			r, gErr := fld.Grid.LatLonRasterE()
			if gErr != nil {
				return f, eh.Errorf("gribfield: %w", gErr)
			}
			if fld.Grid.Template != gridTemplateLatLon {
				return f, eb.Build().Uint16("template", fld.Grid.Template).Errorf("gribfield: only a regular latitude/longitude grid (template 3.0) is regular in latitude")
			}
			g := geometry{west: r.West, north: r.North, di: r.Di, dj: r.Dj, ni: r.Ni, nj: r.Nj}
			if !started {
				geo, started = g, true
			} else if g != geo {
				return f, eh.Errorf("gribfield: fields on different grids")
			}
			values, _, _, vErr := fld.RasterE(nil)
			if vErr != nil {
				return f, eh.Errorf("gribfield: %w", vErr)
			}
			plane := make([]float32, len(values))
			for i, x := range values {
				plane[i] = float32(x) // NaN, missing, stays NaN
			}
			s := steps[valid]
			if s == nil {
				s = &step{valid: valid}
				steps[valid] = s
			}
			dst := &s.u
			if p.Number == numberV {
				dst = &s.v
			}
			if *dst != nil {
				return f, eb.Build().Time("valid", valid).Errorf("gribfield: a component appears twice for one valid time")
			}
			*dst = plane
		}
	}
	if len(steps) == 0 {
		return f, eh.Errorf("gribfield: no wind fields")
	}
	times := make([]time.Time, 0, len(steps))
	for t := range steps {
		times = append(times, t)
	}
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	f = keelsonfield.Field{Name: opts.Name, Unit: opts.Unit, SpeedMax: opts.SpeedMax}
	for _, t := range times {
		s := steps[t]
		if s.u == nil || s.v == nil {
			return keelsonfield.Field{}, eb.Build().Time("valid", t).Errorf("gribfield: a valid time with one component only")
		}
		f.Steps = append(f.Steps, t)
		f.Grids = append(f.Grids, vectorfield.Grid{
			West: geo.west, North: geo.north,
			DLon: geo.di, DLat: geo.dj,
			Cols: geo.ni, Rows: geo.nj,
			U: s.u, V: s.v,
		})
	}
	return
}
