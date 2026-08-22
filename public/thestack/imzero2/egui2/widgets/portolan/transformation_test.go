// Ports spec/suites/geometry/TransformationSpec.js from Leaflet at
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9. Top-level functions follow the
// upstream `describe` groups, subtests carry the upstream `it` titles.
//
// Not ported from upstream (JavaScript-specific, no Go analogue):
//
//	"#constructor" › "allows an array property for a" — the [a, b, c, d]
//	  constructor form.

package portolan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// transformationFixture is TransformationSpec's beforeEach.
func transformationFixture() (Transformation, Point) {
	return NewTransformation(1, 2, 3, 4), Pt(10, 20)
}

func TestTransformation_Transform(t *testing.T) {
	t.Run("performs a transformation", func(t *testing.T) {
		tr, p := transformationFixture()
		p2 := tr.Transform(p, 2)
		assert.Equal(t, Pt(24, 128), p2)
	})

	t.Run("assumes a scale of 1 if not specified", func(t *testing.T) {
		tr, p := transformationFixture()
		// Upstream omits the scale; here a zero scale reads as 1.
		p2 := tr.Transform(p, 0)
		assert.Equal(t, Pt(12, 64), p2)
	})
}

func TestTransformation_Untransform(t *testing.T) {
	t.Run("performs a reverse transformation", func(t *testing.T) {
		tr, p := transformationFixture()
		p2 := tr.Transform(p, 2)
		p3 := tr.Untransform(p2, 2)
		assert.Equal(t, p, p3)
	})

	t.Run("assumes a scale of 1 if not specified", func(t *testing.T) {
		tr, _ := transformationFixture()
		assert.Equal(t, Pt(10, 20), tr.Untransform(Pt(12, 64), 0))
	})
}
