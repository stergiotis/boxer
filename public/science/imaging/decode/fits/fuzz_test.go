package fits

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const (
	fuzzMaxPixels = 1 << 16
	fuzzMaxBytes  = 1 << 20
)

func fuzzSeeds(f *testing.F) {
	f.Add(writeFITS(rampHDU(16, []int64{5, 3})))
	f.Add(writeFITS(rampHDU(-32, []int64{3, 2, 2}, kv{"CTYPE1", quote("RA---TAN")}, kv{"CTYPE2", quote("DEC--TAN")},
		kv{"CD1_1", "-1E-3"}, kv{"CD2_2", "1E-3"}, kv{"CTYPE3", quote("FREQ")}, kv{"CRVAL3", "1"}, kv{"CRPIX3", "1"}, kv{"CDELT3", "1"})))
	f.Add(writeFITS(rampHDU(8, []int64{2, 2}, kv{"BSCALE", "2"}, kv{"BZERO", "-1"}, kv{"BLANK", "3"},
		kv{"LONG", "'a&'"}, kv{"CONTINUE  'b'", ""})))
	f.Add(writeFITS(testHDU{bitpix: 8}, testHDU{ext: "BINTABLE", bitpix: 8, axes: []int64{4, 1}, data: make([]byte, 4), cards: []kv{{"ZIMAGE", "T"}}},
		func() testHDU { h := rampHDU(32, []int64{2, 3}); h.ext = "IMAGE"; return h }()))
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(writeFITS(rampHDU(-64, []int64{2, 2})))
	_ = zw.Close()
	f.Add(z.Bytes())
	for _, name := range []string{"tan_cd.fits", "mef.fits", "scaled.fits", "tilecomp.fits"} {
		if b, err := os.ReadFile(filepath.Join("testdata", name)); err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte{})
	f.Add(simpleMagic)
}

// FuzzDecode holds the reader to what it promises on any input: it does
// not panic, it allocates no plane beyond its limits, and a stack it
// returns is well formed and carries its own digest.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Inspect(context.Background(), bytes.NewReader(data), int64(len(data)))
		_ = Sniff(data)
		s, err := Decode(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz",
			Options{MaxPixels: fuzzMaxPixels, MaxBytes: fuzzMaxBytes, MaxPlanes: 64})
		if err != nil {
			if !s.IsZero() {
				t.Fatalf("an error with a stack: %v", err)
			}
			return
		}
		if int64(s.Width())*int64(s.Height()) > fuzzMaxPixels || s.Bytes() > fuzzMaxBytes || s.PlaneCount() > 64 {
			t.Fatalf("stack of %d × %d × %d passed the limits", s.Width(), s.Height(), s.PlaneCount())
		}
		if s.Hash() != s.Provenance().SampleHash {
			t.Fatal("the provenance's digest is not the stack's")
		}
		if err := s.Placement().Validate(); err != nil {
			t.Fatalf("an invalid placement: %v", err)
		}
	})
}
