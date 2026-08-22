package portolan

import (
	"math"
	"strconv"
)

// wrapNum maps x into [min, max) — or onto max itself when includeMax is set
// and x equals it — the way Leaflet's Util.wrapNum does; longitudes use it
// with [-180, 180]. JavaScript's % and Go's math.Mod agree on the sign of a
// negative remainder, so the double-mod form ports unchanged.
func wrapNum(x, min, max float64, includeMax bool) float64 {
	d := max - min
	if x == max && includeMax {
		return x
	}
	return math.Mod(math.Mod(x-min, d)+d, d) + min
}

// jsRound rounds half toward +∞, which is what JavaScript's Math.round does
// and Go's math.Round (half away from zero) does not: Math.round(-2.5) is -2.
// Every pixel rounding in the port goes through it so the numbers match
// Leaflet's bit for bit.
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// formatNum rounds to `precision` decimals — Leaflet's Util.formatNum; the
// String methods use six.
func formatNum(num float64, precision int) float64 {
	pow := math.Pow(10, float64(precision))
	return jsRound(num*pow) / pow
}

// fmtNum renders formatNum's result the way JavaScript's template literal
// would: no trailing zeros, no ".0" on an integer.
func fmtNum(num float64, precision int) string {
	return strconv.FormatFloat(formatNum(num, precision), 'f', -1, 64)
}
