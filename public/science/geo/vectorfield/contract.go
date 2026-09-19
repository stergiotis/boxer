package vectorfield

import (
	"context"
	"errors"
	"time"
)

// ErrStepMissing reports a step the source lists and cannot serve — a late or
// failed file. It is distinct from a window of NaN, which says the step exists
// and has no data there.
var ErrStepMissing = errors.New("step is missing")

// ErrStepOutOfRange reports a step index the source does not list.
var ErrStepOutOfRange = errors.New("step index is out of range")

// StepKindE says whether a step's values hold at an instant or summarise an
// interval.
type StepKindE uint8

const (
	StepKindInstant StepKindE = iota
	StepKindInterval
)

// StepProcessE is what an interval step's values are of the interval.
type StepProcessE uint8

const (
	StepProcessNone StepProcessE = iota
	StepProcessAverage
	StepProcessMaximum
	StepProcessMinimum
	StepProcessAccumulation
)

// Step is one time step of a source. Steps are not assumed evenly spaced: a
// forecast is hourly and then three-hourly, and its horizon depends on the run.
type Step struct {
	// Valid is the instant the values hold at, or the END of the interval
	// they summarise, which is where GRIB stamps an interval too.
	Valid time.Time
	// Reference is the model run the step comes from; zero when there is
	// none. It may differ between steps of a series stitched across runs.
	Reference time.Time
	Kind      StepKindE
	// Start and Process describe an interval step and are zero for an instant.
	Start   time.Time
	Process StepProcessE
}

// SurfaceKindE names the kind of vertical surface a field lies on. The number
// alone does not identify a level: a 10 m wind and a 10 hPa wind share one.
type SurfaceKindE uint8

const (
	SurfaceKindUnspecified SurfaceKindE = iota
	SurfaceKindHeightAboveGround
	SurfaceKindPressure
	SurfaceKindAltitudeAboveSeaLevel
	SurfaceKindDepthBelowSeaSurface
	SurfaceKindModelLevel
	SurfaceKindNamed // tropopause, boundary layer, max-wind level: see Name
)

// Surface is the vertical surface of a field, by kind and value.
type Surface struct {
	Kind  SurfaceKindE
	Value float64
	Unit  string
	Name  string
}

// Meta is what a reader needs in order not to mistake one field for another.
type Meta struct {
	Name     string // what a legend shows
	Quantity string // wind, ocean current, gradient of …
	Unit     string // unit of both components and of Speed
	Surface  Surface
	// Provenance says what the source did to reach an earth-relative lat/lon
	// grid: the native grid, whether components were collocated and rotated,
	// the resampling method.
	Provenance string

	// West, East, South and North are the positions of the first and last
	// sample — node positions, not pixel edges. East may exceed 180.
	West, East, South, North float64
	// DLon and DLat are the native spacing, both positive.
	DLon, DLat float64
	// PeriodicLon says the field wraps in longitude. It is the source's
	// property, never the renderer's: a regional field is not periodic and
	// must not be drawn as if it were.
	PeriodicLon bool

	Steps []Step

	// SpeedMin and SpeedMax are the magnitude range a palette should span.
	SpeedMin, SpeedMax float32
	// ValidFraction is how much of what a coarse sample covers must be valid
	// for the sample to be valid, the policy at coasts and domain edges.
	ValidFraction float32
}

// Request asks for a window. Longitudes are unwrapped: West < East always,
// and a window across the antimeridian has East above 180 (or West below
// -180). South < North.
type Request struct {
	West, East, South, North float64
	Step                     int
	// MaxCols and MaxRows bound how many columns and rows may fall inside
	// the requested bounds. The gutter is extra.
	MaxCols, MaxRows int
}

// SourceI is one two-component field over the steps it lists.
//
// SampleE may be slow — a disk read, a regrid — and is called off the render
// thread; it must be safe for concurrent use and must honour ctx. What the
// returned [Window] promises is on that type.
type SourceI interface {
	Describe() (meta Meta)
	SampleE(ctx context.Context, req Request) (win Window, err error)
}
