// Package gfsdemo is one real wind forecast for demos and tests (ADR-0292
// SD5): NOAA GFS 10 m wind at 1.0°, 17 steps over 48 hours, committed so
// that a build that shows it stays offline. SOURCES.txt says where it comes
// from and how to fetch another run.
package gfsdemo

import (
	_ "embed"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield/gribfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/keelsonfield"
)

// GRIB is the forecast's 34 GRIB2 messages, u and v per step.
//
//go:embed gfs-10m-wind-2026100700.grib2
var GRIB []byte

// Name is what a legend shows for it.
const Name = "GFS 10 m wind, run 2026-10-07 00 UTC"

// FieldE decodes the forecast into a field.
func FieldE() (f keelsonfield.Field, err error) {
	return gribfield.ReadE(GRIB, gribfield.Options{Name: Name, Unit: "m/s", SpeedMax: 25})
}
