package geodesy_test

import (
	"fmt"

	"github.com/stergiotis/boxer/public/science/geo/geodesy"
)

// A listener on the ground and an aircraft 3 km up and 4 km north of it:
// the slant range is the chord in ECEF, and the frame names the direction.
func ExampleFrame() {
	listener := geodesy.Geodetic{LatDeg: 47.4, LonDeg: 8.5, HeightM: 450}
	f := geodesy.NewFrame(listener)
	aircraft := f.EnuToEcef(geodesy.Vec3{X: 0, Y: 4000, Z: 3000})
	enu := f.EcefToEnu(aircraft)
	fmt.Printf("slant %.0f m, bearing north %.0f m, up %.0f m\n", enu.Norm(), enu.Y, enu.Z)
	g := geodesy.EcefToGeodetic(aircraft)
	fmt.Printf("aircraft at %.4f N %.4f E, %.0f m above the ellipsoid\n", g.LatDeg, g.LonDeg, g.HeightM)
	// Output:
	// slant 5000 m, bearing north 4000 m, up 3000 m
	// aircraft at 47.4360 N 8.5000 E, 3451 m above the ellipsoid
}

// A velocity report is east-north-up where it was made; rotate it there.
func ExampleEnuToEcefAt() {
	at := geodesy.Geodetic{LatDeg: 0, LonDeg: 0}
	v := geodesy.EnuToEcefAt(at, geodesy.GroundVelocityEnu(100, 0, 0))
	fmt.Printf("northbound at the equator is ECEF (%.0f, %.0f, %.0f) m/s\n", v.X, v.Y, v.Z)
	// Output:
	// northbound at the equator is ECEF (0, 0, 100) m/s
}
