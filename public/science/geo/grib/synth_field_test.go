package grib

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func decodeOne(t *testing.T, msg []byte) (f *Field) {
	t.Helper()
	for m, err := range ScanBytes(msg) {
		require.NoError(t, err)
		require.Len(t, m.Fields, 1)
		f = m.Fields[0]
	}
	require.NotNil(t, f)
	return
}

// TestScanFlagsPlaceEveryStoredValue builds a 3×2 field for each of the 16
// scan-flag combinations with X = 10·i + j coded at the point's (i, j), so
// the raster and the point iterator can be checked against the flags by
// construction: the raster must come back west-to-east, north-to-south, and
// the k-th stored point's coordinates must be those of the (i, j) whose
// value was stored k-th (ADR-0292 §R2, §R5).
func TestScanFlagsPlaceEveryStoredValue(t *testing.T) {
	const ni, nj = 3, 2
	for flags := 0; flags < 16; flags++ {
		scan := uint8(flags << 4)
		sc := parseScanFlags(scan)
		// Stored order per the flags: the coded (i, j) of stored index k.
		var order [][2]int
		if sc.JConsecutive {
			for i := 0; i < ni; i++ {
				for j := 0; j < nj; j++ {
					jj := j
					if sc.Alternating && i%2 == 1 {
						jj = nj - 1 - j
					}
					order = append(order, [2]int{i, jj})
				}
			}
		} else {
			for j := 0; j < nj; j++ {
				for i := 0; i < ni; i++ {
					ii := i
					if sc.Alternating && j%2 == 1 {
						ii = ni - 1 - i
					}
					order = append(order, [2]int{ii, j})
				}
			}
		}
		x := make([]uint64, 0, ni*nj)
		for _, ij := range order {
			x = append(x, uint64(10*ij[0]+ij[1]))
		}
		// Corners follow the flags: i runs east unless INegative, j runs
		// south unless JPositive.
		lat1, lat2 := 50.0, 40.0
		if sc.JPositive {
			lat1, lat2 = 40.0, 50.0
		}
		lon1, lon2 := 10.0, 30.0
		if sc.INegative {
			lon1, lon2 = 30.0, 10.0
		}
		s := SynthField{Ni: ni, Nj: nj, Scan: scan, Lat1: lat1, Lon1: lon1, Lat2: lat2, Lon2: lon2, Ref: 0, Bits: 8, X: x, TimeUnit: 1}
		f := decodeOne(t, s.Encode())
		require.Equal(t, sc, f.Grid.Scan)
		values, err := f.Values(nil)
		require.NoError(t, err)
		for k, ij := range order {
			require.Equal(t, float64(10*ij[0]+ij[1]), values[k], "flags %#x stored index %d", scan, k)
		}
		// Points: coded i counts from lon1 in the coded direction, so the
		// geographic column is i when east-running and ni−1−i otherwise.
		points, err := f.Grid.Points()
		require.NoError(t, err)
		k := 0
		for lat, lon := range points {
			ij := order[k]
			wantLat := 50.0 - 10.0*float64(ij[1])
			if sc.JPositive {
				wantLat = 40.0 + 10.0*float64(ij[1])
			}
			wantLon := 10.0 + 10.0*float64(ij[0])
			if sc.INegative {
				wantLon = 30.0 - 10.0*float64(ij[0])
			}
			require.InDelta(t, wantLat, lat, 1e-9, "flags %#x point %d", scan, k)
			require.InDelta(t, wantLon, lon, 1e-9, "flags %#x point %d", scan, k)
			k++
		}
		require.Equal(t, ni*nj, k)
		// Raster: row 0 is the northern row, column 0 the western one.
		raster, rni, rnj, err := f.Raster(nil)
		require.NoError(t, err)
		require.Equal(t, ni, rni)
		require.Equal(t, nj, rnj)
		for row := 0; row < nj; row++ {
			for col := 0; col < ni; col++ {
				// Geographic (col, row) ↔ coded (i, j).
				i, j := col, row
				if sc.INegative {
					i = ni - 1 - col
				}
				if sc.JPositive {
					j = nj - 1 - row
				}
				require.Equal(t, float64(10*i+j), raster[row*ni+col], "flags %#x raster (%d,%d)", scan, col, row)
			}
		}
	}
}

// TestConstantFieldHonoursBothScales is the trap a reference library still
// emulates: bits-per-value 0 with a non-zero decimal scale, with and without
// a bitmap, and a negative decimal scale that multiplies.
func TestConstantFieldHonoursBothScales(t *testing.T) {
	base := SynthField{Ni: 2, Nj: 2, Lat1: 1, Lon1: 1, Lat2: 0, Lon2: 2, Ref: 1234.5, Bits: 0, TimeUnit: 1}
	for _, tc := range []struct {
		dec  int16
		bin  int16
		want float64
	}{{0, 0, 1234.5}, {1, 0, 123.45}, {-2, 0, 123450}, {2, 3, 12.345}} {
		s := base
		s.DecScale, s.BinScale = tc.dec, tc.bin
		values, err := decodeOne(t, s.Encode()).Values(nil)
		require.NoError(t, err)
		for _, v := range values {
			require.InDelta(t, tc.want, v, 1e-9*tc.want, "D=%d", tc.dec)
		}
	}
	s := base
	s.DecScale = 1
	s.Bitmap = []bool{true, false, false, true}
	values, err := decodeOne(t, s.Encode()).Values(nil)
	require.NoError(t, err)
	require.InDelta(t, 123.45, values[0], 1e-9)
	require.True(t, math.IsNaN(values[1]))
	require.True(t, math.IsNaN(values[2]))
	require.InDelta(t, 123.45, values[3], 1e-9)
}

// TestNegativeDecimalScaleMultiplies holds the JMA case: D = −2 divides by
// 10⁻², so a coded 949.22 hPa becomes 94 922 Pa.
func TestNegativeDecimalScaleMultiplies(t *testing.T) {
	s := SynthField{Ni: 1, Nj: 1, Lat1: 0, Lon1: 0, Lat2: 0, Lon2: 0, Ref: 949.22314453125, BinScale: -4, DecScale: -2, Bits: 12, X: []uint64{16}, TimeUnit: 1}
	values, err := decodeOne(t, s.Encode()).Values(nil)
	require.NoError(t, err)
	require.InDelta(t, (949.22314453125+1)*100, values[0], 1e-9)
}

// TestSignedOctetsAreSignMagnitude covers every width the templates use,
// with a negative value and the all-ones missing pattern (ADR-0292 §R5).
func TestSignedOctetsAreSignMagnitude(t *testing.T) {
	r := rd{b: []byte{0x81, 0x80, 0x05, 0x80, 0x00, 0x00, 0x07, 0xff, 0xff, 0xff, 0xff, 0x7f}}
	require.EqualValues(t, -1, r.s8())
	require.EqualValues(t, -5, r.s16())
	require.EqualValues(t, -7, r.s32())
	v, missing := r.s32m()
	require.True(t, missing)
	require.EqualValues(t, 0, v)
	require.EqualValues(t, 127, r.s8())
	require.NoError(t, r.truncation("test"))
	short := rd{b: []byte{0x01}}
	short.u16()
	require.ErrorIs(t, short.truncation("short"), ErrMalformed)
}

// TestForecastTimeSignAndUnit: a negative forecast time in minutes stays
// negative and in minutes (the sign-magnitude case that read as
// 2 147 483 647 hours elsewhere).
func TestForecastTimeSignAndUnit(t *testing.T) {
	s := SynthField{Ni: 1, Nj: 1, Ref: 1, Bits: 0, Forecast: -90, TimeUnit: 0}
	f := decodeOne(t, s.Encode())
	require.True(t, f.Product.ForecastOffsetOK)
	require.Equal(t, -90*time.Minute, f.Product.ForecastOffset)
	require.Equal(t, time.Date(2026, 9, 27, 12, 30, 15, 0, time.UTC), f.Message.Ident.RefTime)
	valid, ok := f.Product.ValidTime(f.Message.Ident.RefTime)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 9, 27, 11, 0, 15, 0, time.UTC), valid)
	s.TimeUnit = 3 // months: no duration
	f = decodeOne(t, s.Encode())
	require.False(t, f.Product.ForecastOffsetOK)
}

// TestDataEndingInEndMarkerIsNotTheEnd: four values that pack to the bytes
// "7777" sit inside the data section; the message length, not the marker,
// says where the message ends.
func TestDataEndingInEndMarkerIsNotTheEnd(t *testing.T) {
	s := SynthField{Ni: 4, Nj: 1, Ref: 0, Bits: 8, X: []uint64{'7', '7', '7', '7'}, TimeUnit: 1}
	values, err := decodeOne(t, s.Encode()).Values(nil)
	require.NoError(t, err)
	require.Equal(t, []float64{55, 55, 55, 55}, values)
}

// TestShortDataSectionIsInconsistent: a data section shorter than its bit
// count is refused with both numbers, never read past.
func TestShortDataSectionIsInconsistent(t *testing.T) {
	s := SynthField{Ni: 4, Nj: 1, Ref: 0, Bits: 8, X: []uint64{1, 2, 3, 4}, TimeUnit: 1}
	msg := s.Encode()
	// Shrink section 7 by one byte and fix the lengths.
	cut := bytes.LastIndex(msg, []byte{0, 0, 0, 9, 7})
	require.Positive(t, cut)
	msg = append(msg[:cut+5], msg[cut+6:]...)
	msg[cut+3] = 8
	total := uint64(len(msg))
	for i := 0; i < 8; i++ {
		msg[15-i] = byte(total >> (8 * i))
	}
	_, err := decodeOne(t, msg).Values(nil)
	require.ErrorIs(t, err, ErrInconsistent)
}

// TestGrib1LargeMessageLength: the ECMWF convention for edition 1 messages
// past 2²³−1 bytes, on a synthetic skeleton with the field values of the
// survey's 11.4 MB sample (coded 0x8172e8, data section coded length 14).
func TestGrib1LargeMessageLength(t *testing.T) {
	head := []byte{'G', 'R', 'I', 'B', 0x81, 0x72, 0xe8, 1}
	// Product definition section of 28 bytes with GDS and BMS flags set.
	pds := make([]byte, 28)
	pds[0], pds[1], pds[2] = 0, 0, 28
	pds[7] = 0xc0
	gds := make([]byte, 32)
	gds[2] = 32
	bms := make([]byte, 1166406)
	bms[0], bms[1], bms[2] = 0x11, 0xcc, 0x46
	bds := []byte{0, 0, 14}
	src := bytesSource{buf: append(append(append(append(head, pds...), gds...), bms...), bds...)}
	length, err := grib1Length(src, 0, head)
	require.NoError(t, err)
	require.EqualValues(t, 11394230, length)
	// Without the flag the field is the length.
	small := []byte{'G', 'R', 'I', 'B', 0x00, 0x27, 0x6e, 1}
	length, err = grib1Length(bytesSource{buf: small}, 0, small)
	require.NoError(t, err)
	require.EqualValues(t, 10094, length)
}

// TestScanFromOffsetFindsTheMessage: an index need not be exact.
func TestScanFromOffsetFindsTheMessage(t *testing.T) {
	a := SynthField{Ni: 1, Nj: 1, Ref: 1, Bits: 0, TimeUnit: 1}.Encode()
	b := SynthField{Ni: 1, Nj: 1, Ref: 2, Bits: 0, TimeUnit: 1}.Encode()
	buf := append(append(append([]byte("junk"), a...), []byte("pad")...), b...)
	var seen []float64
	for m, err := range ScanFrom(bytes.NewReader(buf), int64(len(buf)), int64(len(a)+2)) {
		require.NoError(t, err)
		v, err := m.Fields[0].Values(nil)
		require.NoError(t, err)
		seen = append(seen, v[0])
		require.EqualValues(t, 5, m.Skipped)
	}
	require.Equal(t, []float64{2}, seen)
	n := 0
	for m, err := range Scan(bytes.NewReader(buf), int64(len(buf))) {
		require.NoError(t, err)
		require.Equal(t, []int64{4, 3}[n], m.Skipped)
		n++
	}
	require.Equal(t, 2, n)
}

// TestTruncatedTailIsAnErrorAfterTheGoodMessages: the first message stands,
// the cut one is reported, and the scan stops.
func TestTruncatedTailIsAnErrorAfterTheGoodMessages(t *testing.T) {
	a := SynthField{Ni: 1, Nj: 1, Ref: 1, Bits: 0, TimeUnit: 1}.Encode()
	b := SynthField{Ni: 1, Nj: 1, Ref: 2, Bits: 0, TimeUnit: 1}.Encode()
	buf := append(append([]byte{}, a...), b[:len(b)-10]...)
	var got []error
	n := 0
	for m, err := range ScanBytes(buf) {
		if err != nil {
			got = append(got, err)
			continue
		}
		n++
		require.NotNil(t, m)
	}
	require.Equal(t, 1, n)
	require.Len(t, got, 1)
	require.ErrorIs(t, got[0], ErrMalformed)
}

// TestDamagedMessagesFailWithoutPanicking is the property the bounds-checked
// cursors exist for: whatever is cut off or overwritten, the reader returns
// with one of the three documented errors or with values, never a panic.
func TestDamagedMessagesFailWithoutPanicking(t *testing.T) {
	names := []string{"ec_regular_latlon_surface.grib2", "ec_gfs.c255.grib2", "ec_grid_complex_spatial_differencing.grib2", "ec_reduced_gaussian_surface.grib2", "ec_constant_field.grib2", "ec_multi_created.grib2", "ec_grid_ieee.grib", "ec_mercator.grib2", "ec_gts.grib", "ec_tigge_pf_ecmwf.grib2"}
	originals := make([][]byte, 0, len(names)+1)
	for _, n := range names {
		originals = append(originals, readFixture(t, n))
	}
	originals = append(originals, SynthField{Ni: 3, Nj: 2, Ref: 1, Bits: 5, X: []uint64{1, 2, 3, 4, 5, 6}, TimeUnit: 1}.Encode())
	rapid.Check(t, func(rt *rapid.T) {
		buf := bytes.Clone(rapid.SampledFrom(originals).Draw(rt, "file"))
		if rapid.Bool().Draw(rt, "truncate") {
			buf = buf[:rapid.IntRange(0, len(buf)).Draw(rt, "length")]
		}
		span := min(len(buf), 1<<rapid.IntRange(4, 16).Draw(rt, "spanLog2"))
		for range rapid.IntRange(0, 24).Draw(rt, "flips") {
			if span == 0 {
				break
			}
			buf[rapid.IntRange(0, span-1).Draw(rt, "at")] = rapid.Byte().Draw(rt, "byte")
		}
		for m, err := range ScanBytes(buf) {
			if err != nil {
				if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrInconsistent) {
					rt.Fatalf("scan failed outside the documented errors: %v", err)
				}
				break
			}
			for _, f := range m.Fields {
				_, err = f.Values(nil)
				if err != nil && !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrInconsistent) {
					rt.Fatalf("values failed outside the documented errors: %v", err)
				}
				points, err := f.Grid.Points()
				if err == nil {
					for range points {
					}
				}
				_, _, _, _ = f.Raster(nil)
			}
		}
	})
}

// TestGaussianLatitudesMatchTheKnownN32Roots: the first and last latitude
// of the N=32 grid the ECMWF fixtures use, to the oracle's precision.
func TestGaussianLatitudesMatchTheKnownN32Roots(t *testing.T) {
	lats := gaussianLatitudes(32)
	require.Len(t, lats, 64)
	require.InDelta(t, 87.86380, lats[0], 1e-5)
	require.InDelta(t, -87.86380, lats[63], 1e-5)
	require.InDelta(t, 1.39531, lats[31], 1e-5)
	for i := 1; i < len(lats); i++ {
		require.Less(t, lats[i], lats[i-1])
	}
}
