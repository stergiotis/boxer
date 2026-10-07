package grib

import "testing"

// BenchmarkCCSDS decodes the ICON-EU precipitation field (904 689 values,
// 16 bits, CCSDS) — the reference implementation took 6.9 ms for this field
// on the survey machine (2026-09-27), 131 Mval/s.
func BenchmarkCCSDS(b *testing.B) {
	buf := readFixtureB(b, "syn_ccsds_b16_j32_r128_f14.grib2")
	var f *Field
	for m, err := range ScanBytes(buf) {
		if err != nil {
			b.Fatal(err)
		}
		f = m.Fields[0]
	}
	dst := make([]float64, f.NumPoints())
	b.ResetTimer()
	for range b.N {
		var err error
		dst, err = f.ValuesE(dst)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(f.NumPoints())*float64(b.N)/b.Elapsed().Seconds()/1e6, "Mval/s")
}

// BenchmarkComplex decodes the GFS precipitation field (1 038 240 values,
// complex packing with spatial differencing) — 11.8 ms, 88 Mval/s in the
// reference implementation on the same machine.
func BenchmarkComplex(b *testing.B) {
	buf := readFixtureB(b, "gfs_apcp6.grib2")
	var f *Field
	for m, err := range ScanBytes(buf) {
		if err != nil {
			b.Fatal(err)
		}
		f = m.Fields[0]
	}
	dst := make([]float64, f.NumPoints())
	b.ResetTimer()
	for range b.N {
		var err error
		dst, err = f.ValuesE(dst)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(f.NumPoints())*float64(b.N)/b.Elapsed().Seconds()/1e6, "Mval/s")
}

func readFixtureB(b *testing.B, name string) (buf []byte) {
	b.Helper()
	var err error
	buf, err = readFixtureE(name)
	if err != nil {
		b.Fatal(err)
	}
	return
}

// BenchmarkJPEG2000 decodes the ECCC RDPS field (1 191 300 values, 12-bit
// JPEG 2000 written by JasPer) — the reference implementation through
// JasPer ran at 5.6 Mval/s on the survey machine (2026-09-27).
func BenchmarkJPEG2000(b *testing.B) {
	buf := readFixtureB(b, "ec_jpeg.grib2")
	var f *Field
	for m, err := range ScanBytes(buf) {
		if err != nil {
			b.Fatal(err)
		}
		f = m.Fields[0]
	}
	dst := make([]float64, f.NumPoints())
	b.ResetTimer()
	for range b.N {
		var err error
		dst, err = f.ValuesE(dst)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(f.NumPoints())*float64(b.N)/b.Elapsed().Seconds()/1e6, "Mval/s")
}
