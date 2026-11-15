package decode

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// fuzzMaxPixels keeps a fuzzed image small enough that a run spends its
// time in the parsers, not in allocating planes.
const fuzzMaxPixels = 1 << 16

// fuzzSeeds are well-formed files of every PNG colour type and of the JPEG
// layouts the decoder handles, with the chunks the scanner reads.
func fuzzSeeds(f *testing.F) {
	ramp := func(x, y, c int) uint16 { return uint16(x*37 + y*11 + c*5) }
	for _, s := range []struct {
		colorType, depth uint8
		extra            []pngChunk
	}{
		{pngColorGray, 1, nil}, {pngColorGray, 4, nil}, {pngColorGray, 8, nil}, {pngColorGray, 16, []pngChunk{{"gAMA", []byte{0, 0, 0xb1, 0x8f}}}},
		{pngColorRGB, 8, []pngChunk{{"sRGB", []byte{0}}}}, {pngColorRGB, 16, []pngChunk{{"tRNS", []byte{0, 1, 0, 2, 0, 3}}}},
		{pngColorPalette, 8, []pngChunk{{"PLTE", bytes.Repeat([]byte{10, 200, 30}, 256)}, {"tRNS", []byte{0, 128, 255}}}},
		{pngColorGrayAlpha, 8, []pngChunk{{"iCCP", []byte("profile\x00\x00x")}}}, {pngColorGrayAlpha, 16, nil},
		{pngColorRGBA, 8, []pngChunk{{"acTL", []byte{0, 0, 0, 1, 0, 0, 0, 0}}}}, {pngColorRGBA, 16, []pngChunk{{"cHRM", make([]byte, 32)}}},
	} {
		mask := uint16(1)<<s.depth - 1
		f.Add(rawPNG(f, 5, 3, s.colorType, s.depth, s.extra, func(x, y, c int) uint16 { return ramp(x, y, c) & mask }))
	}
	gray := image.NewGray(image.Rect(0, 0, 9, 7))
	col := image.NewRGBA(image.Rect(0, 0, 9, 7))
	for y := range 7 {
		for x := range 9 {
			gray.SetGray(x, y, color.Gray{Y: uint8(x*20 + y)})
			col.SetRGBA(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 30), B: 99, A: 255})
		}
	}
	f.Add(jpegOf(f, gray))
	f.Add(jpegOf(f, col))
	f.Add([]byte{})
	f.Add(pngSignature)
	f.Add([]byte{0xff, 0xd8, 0xff, 0xe2, 0x00, 0x02})
}

// FuzzDecode holds the decoder to what it promises on any input: it does not
// panic; it refuses what is over its pixel limit; and an image it returns is
// well formed, carries its own digest, and holds the samples the standard
// library's decoder reads from the same file, with alpha straight.
func FuzzDecode(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		img, err := Decode(data, "fuzz", Options{MaxPixels: fuzzMaxPixels})
		if err != nil {
			if !img.IsZero() {
				t.Fatalf("an error with an image: %v", err)
			}
			return
		}
		w, h := img.Width(), img.Height()
		if w <= 0 || h <= 0 || int64(w)*int64(h) > fuzzMaxPixels {
			t.Fatalf("image of %d × %d passed a limit of %d pixels", w, h, fuzzMaxPixels)
		}
		if img.PlaneCount() != img.Model().PlaneCount() {
			t.Fatalf("%d planes for model %s", img.PlaneCount(), img.Model())
		}
		for i := range img.PlaneCount() {
			p := img.Plane(i)
			if p.Width() != w || p.Height() != h || p.Kind() != img.Kind() {
				t.Fatalf("plane %d is %d × %d %s in a %d × %d %s image", i, p.Width(), p.Height(), p.Kind(), w, h, img.Kind())
			}
		}
		if img.Hash() != img.Provenance().SampleHash {
			t.Fatal("the provenance's digest is not the image's")
		}
		if got, want := img.Provenance().Format, Sniff(data).String(); got != want {
			t.Fatalf("provenance says %s of a file that sniffs as %s", got, want)
		}
		compareWithStandardDecoder(t, data, img)
	})
}

// compareWithStandardDecoder checks every pixel against the standard
// library's reading of the file, converted to straight 16-bit colour: where
// a pixel is opaque all of its planes, and the alpha plane everywhere. An
// 8-bit plane holds the top byte. Colour planes under transparency are not
// compared: a premultiplied source has lost them, and that is the case the
// decoder's slow path exists for.
func compareWithStandardDecoder(t *testing.T, data []byte, img imaging.Image) {
	var src image.Image
	var err error
	if Sniff(data) == FormatPNG {
		src, err = png.Decode(bytes.NewReader(data))
	} else {
		src, err = jpeg.Decode(bytes.NewReader(data))
	}
	if err != nil {
		t.Fatalf("the decoder read a file the standard library does not: %v", err)
	}
	b := src.Bounds()
	shift := uint(0)
	if img.Kind() == imaging.SampleKindU8 {
		shift = 8
	}
	model := img.Model()
	for y := range img.Height() {
		for x := range img.Width() {
			c := color.NRGBA64Model.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA64)
			want := [4]uint16{c.R >> shift, c.G >> shift, c.B >> shift, c.A >> shift}
			planes := []int{0, 1, 2}
			switch model {
			case imaging.ColorModelGray, imaging.ColorModelGrayAlpha:
				planes = []int{0}
			}
			if model.HasAlpha() {
				a := img.PlaneCount() - 1
				got, _ := img.Plane(a).StoredAt(x, y)
				if uint16(got) != want[3] {
					t.Fatalf("alpha at (%d,%d): %v, the standard decoder says %d", x, y, got, want[3])
				}
			} else if c.A != 0xffff {
				t.Fatalf("pixel (%d,%d) is translucent in a model without alpha", x, y)
			}
			if c.A != 0xffff {
				continue
			}
			for _, i := range planes {
				got, _ := img.Plane(i).StoredAt(x, y)
				if uint16(got) != want[i] {
					t.Fatalf("plane %d at (%d,%d): %v, the standard decoder says %d (%T)", i, x, y, got, want[i], src)
				}
			}
		}
	}
}

// FuzzScanners runs the two chunk and marker scanners on any bytes: they
// read declared lengths from the input and must stay inside it.
func FuzzScanners(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = scanPNG(data)
		_ = scanJPEG(data)
		_ = scanJPEG(data).frameName()
	})
}

// FuzzDecodeTiled holds the row decoder to the whole decode on any input:
// each accepts what the other accepts, and what they accept they read to the
// same samples.
func FuzzDecodeTiled(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		img, errW := Decode(data, "fuzz", Options{MaxPixels: fuzzMaxPixels})
		s, errS := DecodeTiled(context.Background(), data, "fuzz", Options{MaxPixels: fuzzMaxPixels}, tiles.Options{}, nil)
		switch {
		case errW != nil && errS != nil:
			return
		case errW == nil && errS != nil:
			t.Fatalf("the row decoder refused a file the whole decode reads: %v", errS)
		case errW != nil && errS == nil:
			t.Fatalf("the row decoder read a file the whole decode refuses: %v", errW)
		}
		got, err := s.Read(context.Background(), image.Rect(0, 0, img.Width(), img.Height()))
		if err != nil {
			t.Fatalf("the store does not read back: %v", err)
		}
		if got.Model() != img.Model() || got.Kind() != img.Kind() {
			t.Fatalf("%s %s from the rows, %s %s whole", got.Model(), got.Kind(), img.Model(), img.Kind())
		}
		for i := range img.PlaneCount() {
			if !slices.Equal(img.Plane(i).U8(), got.Plane(i).U8()) || !slices.Equal(img.Plane(i).U16(), got.Plane(i).U16()) {
				t.Fatalf("plane %d differs between the row decoder and the whole decode", i)
			}
		}
	})
}
