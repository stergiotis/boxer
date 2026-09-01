package gloss

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One golden per inline face (ADR-0186 §Verification), for the quantity
// members a marine or geographic column needs.

func TestVelocityFace(t *testing.T) {
	assert.Equal(t, Inline{Text: "6.7 kn"}, instFor(t, "sog@gloss/velocity;unit=kn").Inline(num("6.73")))
	assert.Equal(t, Inline{Text: "12.3 m/s"}, instFor(t, "w@gloss/velocity;unit=mps").Inline(num("12.3")),
		"the spelling is a MIME token; the symbol shown is not")

	// show converts: the same column read in the unit the reader wants. A knot
	// is exactly one nautical mile per hour, so 10 kn is exactly 18.52 km/h.
	assert.Equal(t, Inline{Text: "18.5 km/h"}, instFor(t, "s@gloss/velocity;unit=kn;show=kmh").Inline(num("10")))
	assert.Equal(t, Inline{Text: "19.4 kn"}, instFor(t, "s@gloss/velocity;unit=mps;show=kn").Inline(num("10")))

	// A speed is never auto-scaled: the same column reads in one unit whatever
	// the magnitude, so two rows stay comparable.
	assert.Equal(t, Inline{Text: "0.0 kn"}, instFor(t, "s@gloss/velocity;unit=kn").Inline(num("0.02")))
	assert.Equal(t, Inline{Text: "600.0 kn"}, instFor(t, "s@gloss/velocity;unit=kn").Inline(num("600")))

	// The stored unit is required; a non-numeric cell falls through to its text.
	d, declared := Default().ParseColumn("s@gloss/velocity")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "requires unit=")
	assert.Equal(t, Inline{Text: "n/a"}, instFor(t, "s@gloss/velocity;unit=kn").Inline(txt("n/a")))
}

func TestPlaneAngleFace(t *testing.T) {
	assert.Equal(t, Inline{Text: "42.5°"}, instFor(t, "a@gloss/planeangle;unit=deg").Inline(num("42.5")))
	assert.Equal(t, Inline{Text: "180.0°"}, instFor(t, "a@gloss/planeangle;unit=rad").Inline(num("3.141592653589793")))

	// A bearing is three digits so a column of them lines up, and wraps.
	assert.Equal(t, Inline{Text: "007.0°"}, instFor(t, "h@gloss/planeangle;unit=deg;as=bearing").Inline(num("7")))
	assert.Equal(t, Inline{Text: "359.0°"}, instFor(t, "h@gloss/planeangle;unit=deg;as=bearing").Inline(num("-1")))
	assert.Equal(t, Inline{Text: "000.0°"}, instFor(t, "h@gloss/planeangle;unit=deg;as=bearing").Inline(num("360")))

	// A relative angle keeps its side: 40 degrees off to port is -40, not 320.
	assert.Equal(t, Inline{Text: "-40.0°"}, instFor(t, "awa@gloss/planeangle;unit=deg;as=signed").Inline(num("320")))
	assert.Equal(t, Inline{Text: "180.0°"}, instFor(t, "awa@gloss/planeangle;unit=deg;as=signed").Inline(num("180")))
	assert.Equal(t, Inline{Text: "-179.0°"}, instFor(t, "awa@gloss/planeangle;unit=deg;as=signed").Inline(num("181")))
}

func TestLengthShowsNonSIUnits(t *testing.T) {
	// The default is unchanged: every declaration written before `show` still
	// auto-scales in SI.
	assert.Equal(t, Inline{Text: "1.500 km"}, instFor(t, "d@gloss/length;unit=m").Inline(num("1500")))

	// A sounder reads in the unit it is calibrated in, not in whatever SI step
	// the depth happens to fall in.
	assert.Equal(t, Inline{Text: "30.20 ft"}, instFor(t, "d@gloss/length;unit=ft;show=ft").Inline(num("30.2")))
	assert.Equal(t, Inline{Text: "5.03 fathom"}, instFor(t, "d@gloss/length;unit=ft;show=fathom").Inline(num("30.2")))

	// A distance run is quoted in nautical miles or not at all. The nautical
	// mile is exactly 1852 m.
	assert.Equal(t, Inline{Text: "1.00 NM"}, instFor(t, "l@gloss/length;unit=m;show=NM").Inline(num("1852")))
	assert.Equal(t, Inline{Text: "1852.00 m"}, instFor(t, "l@gloss/length;unit=NM;show=m").Inline(num("1")))
	assert.Equal(t, Inline{Text: "1.852 km"}, instFor(t, "l@gloss/length;unit=NM").Inline(num("1")), "NM still auto-scales under the default")

	// si asks for the default back, for a column a rule set otherwise.
	assert.Equal(t, Inline{Text: "1.852 km"}, instFor(t, "l@gloss/length;unit=NM;show=si").Inline(num("1")))
}

func TestCoordinateFace(t *testing.T) {
	// Degrees and decimal minutes with a hemisphere — what a chart, an almanac
	// and every GPS display use.
	assert.Equal(t, Inline{Text: "47°04.943'N"}, instFor(t, "lat@gloss/coordinate;axis=lat").Inline(num("47.08238333")))
	assert.Equal(t, Inline{Text: "7°10.113'E"}, instFor(t, "lon@gloss/coordinate;axis=lon").Inline(num("7.16855")))

	// The sign is the hemisphere, and it differs by axis — which is why the
	// axis is required rather than inferred.
	assert.Equal(t, Inline{Text: "33°51.900'S"}, instFor(t, "lat@gloss/coordinate;axis=lat").Inline(num("-33.865")))
	assert.Equal(t, Inline{Text: "5°22.200'W"}, instFor(t, "lon@gloss/coordinate;axis=lon").Inline(num("-5.37")))

	// Zero is on the equator and the prime meridian; it has to be written
	// somehow, and N/E are the conventional choices.
	assert.Equal(t, Inline{Text: "0°00.000'N"}, instFor(t, "lat@gloss/coordinate;axis=lat").Inline(num("0")))

	assert.Equal(t, Inline{Text: `47°04'56.6"N`}, instFor(t, "lat@gloss/coordinate;axis=lat;as=dms").Inline(num("47.08238333")))
	assert.Equal(t, Inline{Text: "47.082383°"}, instFor(t, "lat@gloss/coordinate;axis=lat;as=deg").Inline(num("47.08238333")))

	// The axis is required: 7.5 is a valid latitude and a valid longitude, and
	// only the column knows which.
	d, declared := Default().ParseColumn("lat@gloss/coordinate")
	require.True(t, declared)
	assert.Nil(t, d.Instance)
	assert.Contains(t, d.Reason, "requires axis=")
}

// TestCoordinateRefusesOutOfRange pins that a broken column is shown as broken.
// Formatting a latitude of 200 as 200°00.000'N would present the breakage as a
// place, which is worse than the raw number.
func TestCoordinateRefusesOutOfRange(t *testing.T) {
	lat := instFor(t, "lat@gloss/coordinate;axis=lat")
	assert.Equal(t, Inline{Text: "200 (out of range)"}, lat.Inline(num("200")))
	assert.Equal(t, Inline{Text: "-90.5 (out of range)"}, lat.Inline(num("-90.5")))
	assert.Equal(t, Inline{Text: "90°00.000'N"}, lat.Inline(num("90")), "the pole itself is in range")

	// A longitude reaches 180 where a latitude does not.
	lon := instFor(t, "lon@gloss/coordinate;axis=lon")
	assert.Equal(t, Inline{Text: "180°00.000'E"}, lon.Inline(num("179.99999999")),
		"the carry runs rather than printing sixty minutes")
	assert.Equal(t, Inline{Text: "180.5 (out of range)"}, lon.Inline(num("180.5")))
}

// TestCoordinateCarriesTheRounding pins that a value just under a boundary
// does not print sixty minutes or sixty seconds.
func TestCoordinateCarriesTheRounding(t *testing.T) {
	assert.Equal(t, Inline{Text: "48°00.000'N"},
		instFor(t, "lat@gloss/coordinate;axis=lat").Inline(num("47.99999999")))
	assert.Equal(t, Inline{Text: `48°00'00.0"N`},
		instFor(t, "lat@gloss/coordinate;axis=lat;as=dms").Inline(num("47.99999999")))
}
