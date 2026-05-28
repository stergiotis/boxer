//go:build llm_generated_opus46

package swisstopo

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// LOSResult contains the result of a line-of-sight analysis.
type LOSResult struct {
	Visible          bool
	FromElev         float32  // terrain elevation at from point
	ToElev           float32  // terrain elevation at to point
	ObstructionDist  float64  // distance to first obstruction (0 if visible)
	ObstructionElev  float32  // terrain elevation at obstruction point
	ObstructionCoord LV95Coord
	ProfileDist      []float64  // distances along profile
	ProfileElev      []float32  // terrain elevations
	LOSElev          []float32  // LOS line elevations (for visualization)
}

// LineOfSight computes whether there is a clear line of sight between two points.
// fromHeight and toHeight are meters ABOVE terrain (e.g. 1.7m for eye height).
// The terrain profile is sampled at 2m intervals matching tile resolution.
func (inst *ElevationSampler) LineOfSight(from LV95Coord, fromHeight float64, to LV95Coord, toHeight float64) (result LOSResult, err error) {
	// sample terrain profile at 2m steps
	var distances []float64
	var elevations []float32
	distances, elevations, err = inst.SampleProfile(from, to, tileResolutionM)
	if err != nil {
		err = eh.Errorf("unable to sample terrain profile: %w", err)
		return
	}

	numPoints := int64(len(distances))
	if numPoints < 2 {
		// degenerate case: from and to are essentially the same point
		result.Visible = true
		if numPoints == 1 {
			result.FromElev = elevations[0]
			result.ToElev = elevations[0]
		}
		result.ProfileDist = distances
		result.ProfileElev = elevations
		result.LOSElev = make([]float32, numPoints)
		if numPoints == 1 {
			result.LOSElev[0] = elevations[0] + float32(fromHeight)
		}
		return
	}

	result.ProfileDist = distances
	result.ProfileElev = elevations
	result.FromElev = elevations[0]
	result.ToElev = elevations[numPoints-1]

	// compute LOS line elevations
	totalDist := distances[numPoints-1]
	fromLOSElev := float64(result.FromElev) + fromHeight
	toLOSElev := float64(result.ToElev) + toHeight

	result.LOSElev = make([]float32, numPoints)
	for i := int64(0); i < numPoints; i++ {
		t := distances[i] / totalDist
		losElev := fromLOSElev*(1.0-t) + toLOSElev*t
		result.LOSElev[i] = float32(losElev)
	}

	// check for obstructions (skip first and last points)
	result.Visible = true

	dE := to.E - from.E
	dN := to.N - from.N
	lineDist := math.Sqrt(dE*dE + dN*dN)
	var uE float64
	var uN float64
	if lineDist > 1e-6 {
		uE = dE / lineDist
		uN = dN / lineDist
	}

	for i := int64(1); i < numPoints-1; i++ {
		terrainElev := elevations[i]
		losElev := result.LOSElev[i]
		if terrainElev > losElev {
			result.Visible = false
			result.ObstructionDist = distances[i]
			result.ObstructionElev = terrainElev
			result.ObstructionCoord = LV95Coord{
				E: from.E + uE*distances[i],
				N: from.N + uN*distances[i],
			}
			break
		}
	}

	return
}
