package tiff

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// The limits a fuzzed file is decoded under, small enough that a run spends
// its time in the parser and not in allocating planes.
const (
	fuzzMaxPixels = 1 << 14
	fuzzMaxBytes  = 1 << 20
)

// fuzzSeeds are well-formed files across the layouts the reader takes, and
// the reference fixtures.
func fuzzSeeds(f *testing.F) {
	kinds := imaging.AllSampleKinds
	compressions := []uint16{compressionNone, compressionPackBits, compressionLZW, compressionDeflate}
	for i, kind := range kinds {
		for j, comp := range compressions {
			pred := predictorsFor(kind)[(i+j)%2]
			m := wimage{w: 7, h: 5, kind: kind, planes: samplePlanes(kind, 7, 5, 1+2*((i+j)%2), i), photometric: photoBlackIsZero,
				compression: comp, predictor: pred, planar: j%2 == 1, rps: 2}
			if len(m.planes) == 3 {
				m.photometric = photoRGB
			}
			if (i+j)%3 == 0 {
				m.tiled, m.tw, m.th = true, 16, 16
			}
			var order byteOrderI = binary.LittleEndian
			if i%2 == 1 {
				order = binary.BigEndian
			}
			f.Add(wfile{big: j%2 == 0, order: order, images: []wimage{m, m}}.bytes(f))
		}
	}
	geo := grayU8()
	geo.tags = []wtag{doubleTag(tagModelPixelScale, 1, 1, 0), doubleTag(tagModelTiepoint, 0, 0, 0, 1, 2, 0),
		geoKeyTag(keyModelType, 1, keyRasterType, 2, keyProjectedCRS, 2056), asciiTag(tagGDALNoData, "7"),
		asciiTag(tagGDALMetadata, `<GDALMetadata><Item name="SCALE" sample="0" role="scale">2</Item></GDALMetadata>`)}
	f.Add(wfile{images: []wimage{geo}}.bytes(f))
	if names, err := filepath.Glob(filepath.Join("testdata", "*.tif")); err == nil {
		for _, n := range names {
			if b, rerr := os.ReadFile(n); rerr == nil {
				f.Add(b)
			}
		}
	}
	f.Add([]byte{})
	f.Add([]byte("II*\x00\x08\x00\x00\x00"))
	f.Add([]byte("MM\x00+\x00\x08\x00\x00\x00\x00\x00\x00\x00\x10"))
}

// FuzzDecode holds the reader to what it promises on any input: Inspect
// and Decode do not panic; an error is one of the three sentinels; a stack
// stays within the limits and agrees with what Inspect says of the file.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		info, ierr := Inspect(context.Background(), r, int64(len(data)))
		st, err := Decode(context.Background(), r, int64(len(data)), "fuzz", Options{MaxPixels: fuzzMaxPixels, MaxBytes: fuzzMaxBytes})
		if err != nil {
			if !st.IsZero() {
				t.Fatalf("an error with a stack: %v", err)
			}
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("an error outside the sentinels: %v", err)
			}
			return
		}
		if ierr != nil {
			t.Fatalf("Decode read a file Inspect refuses: %v", ierr)
		}
		w, h := st.Width(), st.Height()
		if w <= 0 || h <= 0 || int64(w)*int64(h) > fuzzMaxPixels {
			t.Fatalf("a plane of %d × %d passed a limit of %d pixels", w, h, fuzzMaxPixels)
		}
		if st.Bytes() > fuzzMaxBytes {
			t.Fatalf("a stack of %d bytes passed a limit of %d", st.Bytes(), fuzzMaxBytes)
		}
		pages := 1
		if a := st.Axes(); a[0].Name == "page" {
			pages = a[0].Size
		}
		if pages != info.Pages {
			t.Fatalf("Decode read %d pages, Inspect says %d", pages, info.Pages)
		}
		if st.Hash() != st.Provenance().SampleHash {
			t.Fatal("the provenance's digest is not the stack's")
		}
		if err := st.Placement().Validate(); err != nil {
			t.Fatalf("an invalid placement: %v", err)
		}
	})
}
