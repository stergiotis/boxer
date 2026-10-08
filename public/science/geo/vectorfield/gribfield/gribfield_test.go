package gribfield

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/grib"
)

var run = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

// wind is a 4 × 3 component on a 1° grid, north to south from 60°N 10°E,
// its values 0..11 in stored order, at a forecast hour.
func wind(number uint8, hours int32, scan uint8) grib.SynthField {
	x := make([]uint64, 12)
	for i := range x {
		x[i] = uint64(i)
	}
	lat1, lat2 := 60.0, 58.0
	if scan&0x40 != 0 {
		lat1, lat2 = 58, 60 // stored south to north
	}
	return grib.SynthField{
		Ni: 4, Nj: 3, Scan: scan,
		Lat1: lat1, Lon1: 10, Lat2: lat2, Lon2: 13,
		Bits: 8, X: x,
		Forecast: hours, TimeUnit: 1,
		Discipline: 0, Category: 2, Number: number,
		SurfaceType: 103, SurfaceValue: 10,
		RefTime: run,
	}
}

func file(fields ...grib.SynthField) []byte {
	var b bytes.Buffer
	for _, f := range fields {
		b.Write(f.Encode())
	}
	return b.Bytes()
}

func TestReadE_PairsComponentsByValidTime(t *testing.T) {
	buf := file(wind(numberV, 3, 0), wind(numberU, 0, 0), wind(numberU, 3, 0), wind(numberV, 0, 0))
	f, err := ReadE(buf, Options{Name: "w", Unit: "m/s"})
	require.NoError(t, err)
	require.Equal(t, []time.Time{run, run.Add(3 * time.Hour)}, f.Steps)
	g := f.Grids[0]
	assert.Equal(t, []any{10.0, 60.0, 1.0, 1.0, 4, 3}, []any{g.West, g.North, g.DLon, g.DLat, g.Cols, g.Rows})
	assert.Equal(t, float32(5), g.U[5])
	assert.Equal(t, float32(11), g.V[11])
	assert.Equal(t, "w", f.Name)
}

// A grid stored south to north comes out north to south: the raster view
// applies the scan flags once.
func TestReadE_AppliesScanFlags(t *testing.T) {
	f, err := ReadE(file(wind(numberU, 0, 0x40), wind(numberV, 0, 0x40)), Options{})
	require.NoError(t, err)
	g := f.Grids[0]
	assert.Equal(t, 60.0, g.North)
	assert.Equal(t, float32(8), g.U[0], "the first stored row is the southernmost")
}

func TestReadE_Refusals(t *testing.T) {
	other := wind(numberU, 0, 0)
	other.Number = 0 // temperature's number in category 0; here a non-wind
	other.Category = 0
	level := wind(numberV, 0, 0)
	level.SurfaceValue = 100
	run2 := wind(numberV, 0, 0)
	run2.RefTime = run.Add(6 * time.Hour)
	for name, buf := range map[string][]byte{
		"not wind":      file(other, wind(numberV, 0, 0)),
		"two levels":    file(wind(numberU, 0, 0), level),
		"one component": file(wind(numberU, 0, 0)),
		"two runs":      file(wind(numberU, 0, 0), run2),
		"twice":         file(wind(numberU, 0, 0), wind(numberU, 0, 0), wind(numberV, 0, 0)),
		"no fields":     nil,
	} {
		_, err := ReadE(buf, Options{})
		assert.Error(t, err, name)
	}
}
