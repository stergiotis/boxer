package math32

import "math"

func Min(a float32, b float32) float32 {
	if a <= b {
		return a
	}
	return b
}
func Max(a float32, b float32) float32 {
	if a >= b {
		return a
	}
	return b
}
func Clamp(a float32, low float32, high float32) float32 {
	if a < low {
		return low
	}
	if a > high {
		return high
	}
	return a
}
func Trunc(a float32) float32 {
	return float32(math.Trunc(float64(a)))
}
