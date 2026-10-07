package grib

import "math"

// gaussianLatitudes returns the 2N latitudes of a Gaussian grid with N
// circles between a pole and the equator, north to south, in degrees: the
// arcsines of the roots of the Legendre polynomial of degree 2N. The roots
// are found by Newton's method from the Chebyshev estimate, with the
// polynomial and its derivative from the three-term recurrence; twenty
// iterations at double precision converge for every N a producer uses
// (ecCodes' latitudes for N=32 agree to 1e-10 in the fixtures).
func gaussianLatitudes(n int) (lats []float64) {
	deg := 2 * n
	lats = make([]float64, deg)
	for i := 0; i < n; i++ {
		// Roots are symmetric; solve the northern half.
		x := math.Cos(math.Pi * (float64(i) + 0.75) / (float64(deg) + 0.5))
		for range 20 {
			// P_deg(x) and P'_deg(x) by recurrence.
			p0, p1 := 1.0, x
			for k := 2; k <= deg; k++ {
				p0, p1 = p1, ((2*float64(k)-1)*x*p1-(float64(k)-1)*p0)/float64(k)
			}
			dp := float64(deg) * (x*p1 - p0) / (x*x - 1)
			dx := p1 / dp
			x -= dx
			if math.Abs(dx) < 1e-15 {
				break
			}
		}
		lat := math.Asin(x) * 180 / math.Pi
		lats[i] = lat
		lats[deg-1-i] = -lat
	}
	return
}
