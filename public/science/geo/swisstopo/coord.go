package swisstopo

import "fmt"

// WGS84Coord is a geographic coordinate in the WGS84 datum (EPSG:4326).
// Lat and Lon are in decimal degrees.
type WGS84Coord struct {
	Lat float64
	Lon float64
}

func (inst WGS84Coord) String() string {
	return fmt.Sprintf("WGS84(%.10f, %.10f)", inst.Lat, inst.Lon)
}

// LV95Coord is a planimetric coordinate in the Swiss LV95 / CH1903+ frame (EPSG:2056).
// E is easting (meters), N is northing (meters).
// The origin is at the old observatory in Bern: E=2'600'000, N=1'200'000.
type LV95Coord struct {
	E float64
	N float64
}

func (inst LV95Coord) String() string {
	return fmt.Sprintf("LV95(%.3f, %.3f)", inst.E, inst.N)
}
