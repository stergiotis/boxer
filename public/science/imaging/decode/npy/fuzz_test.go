package npy

import (
	"archive/zip"
	"bytes"
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
	f.Add(array(1, "<i2", false, []int64{3, 4}))
	f.Add(array(2, ">f8", true, []int64{2, 3, 2}))
	f.Add(array(3, "|u1", false, []int64{2, 2, 2, 2}))
	f.Add(npyFile(1, "{'descr': [('x', '<f4')], 'fortran_order': False, 'shape': (2,)}", make([]byte, 8)))
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, e := range []struct {
		name   string
		data   []byte
		method uint16
	}{{"a.npy", array(1, "<u2", false, []int64{2, 3}), zip.Store}, {"b.npy", array(1, "<f4", true, []int64{3, 2}), zip.Deflate}} {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: e.method})
		_, _ = w.Write(e.data)
	}
	_ = zw.Close()
	f.Add(b.Bytes())
	for _, name := range []string{"single.npz", "multi.npz", "fortran_i4.npy", "object.npy"} {
		if d, err := os.ReadFile(filepath.Join("testdata", name)); err == nil {
			f.Add(d)
		}
	}
	f.Add([]byte{})
	f.Add(magic)
}

// FuzzDecode holds the reader to what it promises on any input: it does
// not panic, it allocates no plane beyond its limits, and a stack it
// returns is well formed and carries its own digest.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = Sniff(data)
		_, _ = Inspect(context.Background(), bytes.NewReader(data), int64(len(data)))
		for _, member := range []string{"", "b"} {
			s, err := Decode(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz",
				Options{MaxPixels: fuzzMaxPixels, MaxBytes: fuzzMaxBytes, MaxPlanes: 64, Member: member})
			if err != nil {
				if !s.IsZero() {
					t.Fatalf("an error with a stack: %v", err)
				}
				continue
			}
			if int64(s.Width())*int64(s.Height()) > fuzzMaxPixels || s.Bytes() > fuzzMaxBytes || s.PlaneCount() > 64 {
				t.Fatalf("stack of %d × %d × %d passed the limits", s.Width(), s.Height(), s.PlaneCount())
			}
			if s.Hash() != s.Provenance().SampleHash {
				t.Fatal("the provenance's digest is not the stack's")
			}
		}
	})
}

// FuzzHeader runs the header parser on any text.
func FuzzHeader(f *testing.F) {
	f.Add([]byte(headerDict("<f4", true, []int64{2, 3})))
	f.Add([]byte("{'descr': [('x', '<f4', (2,))], 'fortran_order': False, 'shape': (2,), }"))
	f.Fuzz(func(t *testing.T, text []byte) {
		var h Header
		if h.parse(text) == nil {
			_, _ = h.dtype()
		}
	})
}
