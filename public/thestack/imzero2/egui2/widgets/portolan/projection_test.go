// Port of Leaflet's spec/suites/geo/projection/ProjectionSpec.js at upstream
// commit c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9. Each upstream `describe`
// is a Test function, each `it` a subtest named by its upstream title; the
// `near` helper is assertNearPt in crs_test.go, default delta 1.
//
// Every it is ported. Upstream's "unprojects other points" (in both
// projections) has no matcher — `expect(pr(...))` stands on its own — so what
// it evidently means, the project∘unproject round trip landing within
// `near`'s default delta of the input, is what is asserted here.

package portolan

import (
	"math"
	"testing"
)

// describe('Projection.Mercator')

func TestProjectionMercator_Project(t *testing.T) {
	p := Mercator

	t.Run("projects a center point", func(t *testing.T) {
		// edge cases
		assertNearPt(t, Pt(0, 0), p.Project(LL(0, 0)), 1)
	})

	t.Run("projects the northeast corner of the world", func(t *testing.T) {
		assertNearPt(t, Pt(20037508, 20037508), p.Project(LL(85.0840591556, 180)), 1)
	})

	t.Run("projects the southwest corner of the world", func(t *testing.T) {
		assertNearPt(t, Pt(-20037508, -20037508), p.Project(LL(-85.0840591556, -180)), 1)
	})

	t.Run("projects other points", func(t *testing.T) {
		assertNearPt(t, Pt(3339584, 6413524), p.Project(LL(50, 30)), 1)

		// from https://github.com/Leaflet/Leaflet/issues/1578
		assertNearPt(t, Pt(8918060.964088084, 6755099.410887127),
			p.Project(LL(51.9371170300465, 80.11230468750001)), 1)
	})
}

func TestProjectionMercator_Unproject(t *testing.T) {
	p := Mercator
	pr := func(point Point) Point { return p.Project(p.Unproject(point)) }

	t.Run("unprojects a center point", func(t *testing.T) {
		assertNearPt(t, Pt(0, 0), pr(Pt(0, 0)), 1)
	})

	t.Run("unprojects pi points", func(t *testing.T) {
		assertNearPt(t, Pt(-math.Pi, math.Pi), pr(Pt(-math.Pi, math.Pi)), 1)
		assertNearPt(t, Pt(-math.Pi, -math.Pi), pr(Pt(-math.Pi, -math.Pi)), 1)

		assertNearPt(t, Pt(0.523598775598, 1.010683188683), pr(Pt(0.523598775598, 1.010683188683)), 1)
	})

	t.Run("unprojects other points", func(t *testing.T) {
		// from https://github.com/Leaflet/Leaflet/issues/1578
		assertNearPt(t, Pt(8918060.964088084, 6755099.410887127), pr(Pt(8918060.964088084, 6755099.410887127)), 1)
	})
}

// describe('Projection.SphericalMercator')

func TestProjectionSphericalMercator_Project(t *testing.T) {
	p := SphericalMercator

	t.Run("projects a center point", func(t *testing.T) {
		// edge cases
		assertNearPt(t, Pt(0, 0), p.Project(LL(0, 0)), 1)
	})

	t.Run("projects the northeast corner of the world", func(t *testing.T) {
		assertNearPt(t, Pt(20037508, 20037508), p.Project(LL(85.0511287798, 180)), 1)
	})

	t.Run("projects the southwest corner of the world", func(t *testing.T) {
		assertNearPt(t, Pt(-20037508, -20037508), p.Project(LL(-85.0511287798, -180)), 1)
	})

	t.Run("projects other points", func(t *testing.T) {
		assertNearPt(t, Pt(3339584, 6446275), p.Project(LL(50, 30)), 1)

		// from https://github.com/Leaflet/Leaflet/issues/1578
		assertNearPt(t, Pt(8918060.96409, 6788763.38325),
			p.Project(LL(51.9371170300465, 80.11230468750001)), 1)
	})
}

func TestProjectionSphericalMercator_Unproject(t *testing.T) {
	p := SphericalMercator
	pr := func(point Point) Point { return p.Project(p.Unproject(point)) }

	t.Run("unprojects a center point", func(t *testing.T) {
		assertNearPt(t, Pt(0, 0), pr(Pt(0, 0)), 1)
	})

	t.Run("unprojects pi points", func(t *testing.T) {
		assertNearPt(t, Pt(-math.Pi, math.Pi), pr(Pt(-math.Pi, math.Pi)), 1)
		assertNearPt(t, Pt(-math.Pi, -math.Pi), pr(Pt(-math.Pi, -math.Pi)), 1)

		assertNearPt(t, Pt(0.523598775598, 1.010683188683), pr(Pt(0.523598775598, 1.010683188683)), 1)
	})

	t.Run("unprojects other points", func(t *testing.T) {
		// from https://github.com/Leaflet/Leaflet/issues/1578
		assertNearPt(t, Pt(8918060.964088084, 6755099.410887127), pr(Pt(8918060.964088084, 6755099.410887127)), 1)
	})
}
