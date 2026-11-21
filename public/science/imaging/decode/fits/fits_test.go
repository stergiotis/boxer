package fits

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func decodeBytes(b []byte, opts Options) (imaging.Stack, error) {
	return Decode(context.Background(), bytes.NewReader(b), int64(len(b)), "test.fits", opts)
}

func inspectBytes(b []byte) (Info, error) {
	return Inspect(context.Background(), bytes.NewReader(b), int64(len(b)))
}

// storedAtFITS is the stack's stored sample at a 0-based FITS index
// (NAXIS1 first): stack axes are NAXIS … 3, and rows are flipped.
func storedAtFITS(t *testing.T, s imaging.Stack, idx []int64) (float64, bool) {
	t.Helper()
	sel := make([]int, 0, len(idx)-2)
	for k := len(idx) - 1; k >= 2; k-- {
		sel = append(sel, int(idx[k]))
	}
	p, err := s.Plane(sel)
	require.NoError(t, err)
	return p.StoredAt(int(idx[0]), s.Height()-1-int(idx[1]))
}

func eachIndex(axes []int64, f func(idx []int64)) {
	n := int64(1)
	for _, a := range axes {
		n *= a
	}
	idx := make([]int64, len(axes))
	for range n {
		f(slices.Clone(idx))
		for i := range idx {
			idx[i]++
			if idx[i] < axes[i] {
				break
			}
			idx[i] = 0
		}
	}
}

func TestEveryBitpixAndAxisCountReadsBackSampleForSample(t *testing.T) {
	kinds := map[int]imaging.SampleKindE{8: imaging.SampleKindU8, 16: imaging.SampleKindI16, 32: imaging.SampleKindI32,
		-32: imaging.SampleKindF32, -64: imaging.SampleKindF64}
	for bitpix, kind := range kinds {
		for _, axes := range [][]int64{{5, 3}, {1, 7}, {6, 1}, {4, 3, 2}, {5, 4, 3, 2}} {
			s, err := decodeBytes(writeFITS(rampHDU(bitpix, axes)), Options{})
			require.NoError(t, err, "BITPIX %d axes %v", bitpix, axes)
			assert.Equal(t, kind, s.Kind())
			assert.Equal(t, int(axes[0]), s.Width())
			assert.Equal(t, int(axes[1]), s.Height())
			require.Len(t, s.Axes(), len(axes)-2)
			for i, a := range s.Axes() {
				k := len(axes) - i // FITS number
				assert.Equal(t, int(axes[k-1]), a.Size)
				assert.Equal(t, "axis"+string(rune('0'+k)), a.Name)
				assert.Equal(t, imaging.AxisRoleSelector, a.Role)
			}
			eachIndex(axes, func(idx []int64) {
				got, valid := storedAtFITS(t, s, idx)
				require.True(t, valid)
				require.Equal(t, sampleAt(bitpix, idx, axes), got, "BITPIX %d axes %v at %v", bitpix, axes, idx)
			})
			prov := s.Provenance()
			assert.Equal(t, "fits", prov.Format)
			assert.Equal(t, DecoderName, prov.Decoder)
			assert.Equal(t, uint8(abs(bitpix)), prov.StoredBitDepth)
			assert.True(t, prov.Derivations.Has(imaging.DerivationRowsFlipped))
			assert.False(t, prov.Derivations.Has(imaging.DerivationAxesReordered))
			assert.True(t, s.Placement().IsZero())
			assert.Empty(t, prov.Notes)
		}
	}
}

// The first row of the file is the bottom row: a 2 × 2 image stored as
// 1 2 / 3 4 shows 3 4 on top.
func TestTheFirstRowOfTheFileIsTheBottomRow(t *testing.T) {
	s, err := decodeBytes(writeFITS(testHDU{bitpix: 8, axes: []int64{2, 2}, data: []byte{1, 2, 3, 4}}), Options{})
	require.NoError(t, err)
	p, err := s.Plane(nil)
	require.NoError(t, err)
	assert.Equal(t, []uint8{3, 4, 1, 2}, p.U8())
}

// NAXIS3 varies faster than NAXIS4 in the file, so it is the last stack
// axis, and the planes are in the file's order.
func TestCubeAxesAreSlowestFirstAndNamedByCtype(t *testing.T) {
	axes := []int64{2, 2, 3, 2}
	s, err := decodeBytes(writeFITS(rampHDU(16, axes,
		kv{"CTYPE3", quote("FREQ")}, kv{"CUNIT3", quote("Hz")}, kv{"CRVAL3", "1.0E9"}, kv{"CDELT3", "2.0D6"}, kv{"CRPIX3", "2"},
		kv{"CTYPE4", quote("STOKES")})), Options{})
	require.NoError(t, err)
	got := s.Axes()
	require.Len(t, got, 2)
	assert.Equal(t, "STOKES", got[0].Name)
	assert.Equal(t, 2, got[0].Size)
	assert.Nil(t, got[0].Coords, "no CRVAL4: no coordinates")
	assert.Equal(t, "FREQ", got[1].Name)
	assert.Equal(t, 3, got[1].Size)
	assert.Equal(t, "Hz", got[1].Unit)
	assert.Equal(t, []float64{0.998e9, 1.0e9, 1.002e9}, got[1].Coords)
	// The plane at stack index (1, 2) is FITS (·, ·, 3, 2): plane 5 of the
	// file.
	p, err := s.Plane([]int{1, 2})
	require.NoError(t, err)
	v, _ := p.StoredAt(0, 1)
	assert.Equal(t, sampleAt(16, []int64{0, 0, 2, 1}, axes), v)
}

func TestDuplicateOrMissingCtypesFallBackToAxisNumbers(t *testing.T) {
	s, err := decodeBytes(writeFITS(rampHDU(8, []int64{2, 2, 2, 2, 2},
		kv{"CTYPE3", quote("TIME")}, kv{"CTYPE4", quote("TIME")})), Options{})
	require.NoError(t, err)
	names := make([]string, 0, 3)
	for _, a := range s.Axes() {
		names = append(names, a.Name)
	}
	assert.Equal(t, []string{"axis5", "axis4", "axis3"}, names)
}

func TestBscaleBzeroBlankAndBunit(t *testing.T) {
	axes := []int64{3, 2}
	h := testHDU{bitpix: 16, axes: axes, cards: []kv{{"BSCALE", "0.5"}, {"BZERO", "10."}, {"BLANK", "-32768"}, {"BUNIT", quote("Jy/beam")}},
		data: dataOf(16, axes, func(idx []int64) float64 {
			if idx[0] == 1 && idx[1] == 0 {
				return -32768
			}
			return float64(idx[0]*10 + idx[1])
		})}
	s, err := decodeBytes(writeFITS(h), Options{})
	require.NoError(t, err)
	p, _ := s.Plane(nil)
	assert.Equal(t, imaging.ValueTransform{Scale: 0.5, Offset: 10}, p.Transform())
	inv, ok := p.Invalid()
	assert.True(t, ok)
	assert.Equal(t, -32768.0, inv)
	_, valid := p.StoredAt(1, 1) // FITS (2, 1) is the bottom row
	assert.False(t, valid)
	v, valid := p.ValueAt(2, 0) // FITS (3, 2)
	assert.True(t, valid)
	assert.Equal(t, 10+0.5*21, v)
	assert.Contains(t, s.Provenance().Notes, "values are in Jy/beam")
}

func TestUnsignedSixteenIsSignedWithAnOffset(t *testing.T) {
	axes := []int64{2, 1}
	h := testHDU{bitpix: 16, axes: axes, cards: []kv{{"BZERO", "32768"}, {"BSCALE", "1"}},
		data: dataOf(16, axes, func(idx []int64) float64 { return []float64{-32768, 32767}[idx[0]] })}
	s, err := decodeBytes(writeFITS(h), Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.SampleKindI16, s.Kind())
	p, _ := s.Plane(nil)
	lo, _ := p.ValueAt(0, 0)
	hi, _ := p.ValueAt(1, 0)
	assert.Equal(t, 0.0, lo)
	assert.Equal(t, 65535.0, hi)
}

func TestBlankIsIgnoredOnFloatsAndWhenOutOfRange(t *testing.T) {
	s, err := decodeBytes(writeFITS(rampHDU(-32, []int64{2, 2}, kv{"BLANK", "-1"})), Options{})
	require.NoError(t, err)
	p, _ := s.Plane(nil)
	_, ok := p.Invalid()
	assert.False(t, ok)
	assert.True(t, slices.ContainsFunc(s.Provenance().Notes, func(n string) bool { return strings.Contains(n, "BLANK was ignored") }))

	s, err = decodeBytes(writeFITS(rampHDU(8, []int64{2, 2}, kv{"BLANK", "-1"})), Options{})
	require.NoError(t, err)
	p, _ = s.Plane(nil)
	_, ok = p.Invalid()
	assert.False(t, ok)
	assert.True(t, slices.ContainsFunc(s.Provenance().Notes, func(n string) bool { return strings.Contains(n, "BITPIX 8 can store") }))
}

func TestMultiExtensionDefaultAndSelection(t *testing.T) {
	empty := testHDU{bitpix: 8}
	line := testHDU{ext: "IMAGE", bitpix: 16, axes: []int64{4}, data: make([]byte, 8)}
	table := testHDU{ext: "BINTABLE", bitpix: 8, axes: []int64{4, 2}, data: make([]byte, 8), cards: []kv{{"TFIELDS", "1"}, {"TFORM1", quote("J")}}}
	img := rampHDU(-32, []int64{3, 2})
	img.ext = "IMAGE"
	img.cards = []kv{{"EXTNAME", quote("SCI")}, {"EXTVER", "2"}}
	img2 := rampHDU(8, []int64{2, 2})
	img2.ext = "IMAGE"
	b := writeFITS(empty, line, table, img, img2)

	info, err := inspectBytes(b)
	require.NoError(t, err)
	require.Len(t, info.HDUs, 5)
	assert.Equal(t, 3, info.Default)
	assert.Equal(t, []HDUTypeE{HDUTypePrimary, HDUTypeImage, HDUTypeBinTable, HDUTypeImage, HDUTypeImage},
		[]HDUTypeE{info.HDUs[0].Type, info.HDUs[1].Type, info.HDUs[2].Type, info.HDUs[3].Type, info.HDUs[4].Type})
	assert.Equal(t, "SCI", info.HDUs[3].ExtName)
	assert.Equal(t, int64(2), info.HDUs[3].ExtVer)
	assert.Equal(t, []int64{3, 2}, info.HDUs[3].Axes)
	assert.Equal(t, -32, info.HDUs[3].BitPix)
	assert.False(t, info.HDUs[0].Readable)
	assert.Contains(t, info.HDUs[1].Reason, "NAXIS < 2")
	assert.Contains(t, info.HDUs[2].Reason, "BINTABLE")
	assert.True(t, info.HDUs[4].Readable)

	s, err := decodeBytes(b, Options{})
	require.NoError(t, err)
	assert.Equal(t, imaging.SampleKindF32, s.Kind())
	notes := strings.Join(s.Provenance().Notes, "\n")
	assert.Contains(t, notes, "HDU 3 (SCI) of 5 was read")
	assert.Contains(t, notes, "1 table extension(s) (BINTABLE)")
	assert.Contains(t, notes, "1 other image HDU(s)")

	s, err = decodeBytes(b, Options{UseHDU: true, HDU: 4})
	require.NoError(t, err)
	assert.Equal(t, imaging.SampleKindU8, s.Kind())

	_, err = decodeBytes(b, Options{UseHDU: true, HDU: 1})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Contains(t, err.Error(), "NAXIS < 2")
	_, err = decodeBytes(b, Options{UseHDU: true, HDU: 2})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Contains(t, err.Error(), "BINTABLE")
	_, err = decodeBytes(b, Options{UseHDU: true, HDU: 9})
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestHeaderCardsAreReadForAPerson(t *testing.T) {
	h := rampHDU(8, []int64{2, 2},
		kv{"OBJECT", "'M 31 ''core''      ' / the target"},
		kv{"LONGSTR", "'first part &' / one"},
		kv{"CONTINUE  'second part&'", ""},
		kv{"CONTINUE  'third' / two", ""},
		kv{"EXPTIME", "1.5D+02 / seconds"},
		kv{"NCOMBINE", "+12"},
		kv{"FLAG", "F"},
		kv{"UNDEF", " / nothing"},
		kv{"CPLX", "(1.5, -2)"},
		kv{"HIERARCH ESO DET CHIP NAME = 'CCD-44' / chip", ""},
		kv{"COMMENT   a comment card", ""},
		kv{"HISTORY   made by hand", ""},
	)
	info, err := inspectBytes(writeFITS(h))
	require.NoError(t, err)
	cards := info.HDUs[0].Cards
	find := func(kw string) Card {
		for _, c := range cards {
			if c.Keyword == kw {
				return c
			}
		}
		t.Fatalf("no card %s", kw)
		return Card{}
	}
	assert.Equal(t, Card{Keyword: "OBJECT", Value: "M 31 'core'", Comment: "the target", Kind: ValueKindString}, find("OBJECT"))
	assert.Equal(t, Card{Keyword: "LONGSTR", Value: "first part second partthird", Comment: "one two", Kind: ValueKindString}, find("LONGSTR"))
	assert.Equal(t, Card{Keyword: "EXPTIME", Value: "1.5D+02", Comment: "seconds", Kind: ValueKindReal}, find("EXPTIME"))
	assert.Equal(t, ValueKindInteger, find("NCOMBINE").Kind)
	assert.Equal(t, Card{Keyword: "FLAG", Value: "F", Kind: ValueKindLogical}, find("FLAG"))
	assert.Equal(t, Card{Keyword: "UNDEF", Comment: "nothing", Kind: ValueKindUndefined}, find("UNDEF"))
	assert.Equal(t, Card{Keyword: "CPLX", Value: "1.5, -2", Kind: ValueKindComplex}, find("CPLX"))
	assert.Equal(t, ValueKindOpaque, find("HIERARCH").Kind)
	assert.Contains(t, find("HIERARCH").Value, "ESO DET CHIP NAME")
	assert.Equal(t, "a comment card", find("COMMENT").Comment)
	assert.NotContains(t, slices.Collect(func(yield func(string) bool) {
		for _, c := range cards {
			if !yield(c.Keyword) {
				return
			}
		}
	}), "CONTINUE", "CONTINUE cards are joined onto the string they continue")
	h2 := newHeader()
	for _, c := range cards {
		h2.add(c)
	}
	v, _, ok := h2.real("EXPTIME")
	assert.True(t, ok)
	assert.Equal(t, 150.0, v)
}

func TestGzipWrappedFile(t *testing.T) {
	plain := writeFITS(rampHDU(16, []int64{5, 3}))
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(plain)
	require.NoError(t, zw.Close())
	assert.True(t, Sniff(z.Bytes()))
	assert.True(t, Sniff(plain))
	assert.False(t, Sniff([]byte("PK\x03\x04")))

	s, err := decodeBytes(z.Bytes(), Options{})
	require.NoError(t, err)
	want, err := decodeBytes(plain, Options{})
	require.NoError(t, err)
	assert.Equal(t, want.Hash(), s.Hash())
	assert.Contains(t, s.Provenance().Notes, "the file is gzip-compressed and was decompressed in memory")
	info, err := inspectBytes(z.Bytes())
	require.NoError(t, err)
	assert.True(t, info.Gzipped)

	// A stream that decompresses past the limit is the limit error.
	big := writeFITS(rampHDU(8, []int64{4000, 6000}))
	z.Reset()
	zw = gzip.NewWriter(&z)
	_, _ = zw.Write(big)
	require.NoError(t, zw.Close())
	_, err = decodeBytes(z.Bytes(), Options{MaxBytes: 1 << 10})
	assert.ErrorIs(t, err, ErrLimit)
}

func TestRefusals(t *testing.T) {
	cases := []struct {
		name string
		file []byte
		opts Options
		want error
		says string
	}{
		{"BITPIX 64", writeFITS(rampHDU(64, []int64{2, 2})), Options{}, ErrUnsupported, "BITPIX 64"},
		{"random groups", writeFITS(testHDU{bitpix: -32, axes: []int64{0, 2, 2}, cards: []kv{{"GROUPS", "T"}, {"PCOUNT", "1"}, {"GCOUNT", "2"}}, data: make([]byte, 40)}), Options{}, ErrUnsupported, "random groups"},
		{"tile-compressed", writeFITS(testHDU{bitpix: 8}, testHDU{ext: "BINTABLE", bitpix: 8, axes: []int64{8, 1}, data: make([]byte, 8), cards: []kv{{"ZIMAGE", "T"}}}), Options{}, ErrUnsupported, "tile-compressed"},
		{"one axis", writeFITS(rampHDU(8, []int64{9})), Options{}, ErrUnsupported, "no HDU holds an image"},
		{"one axis selected", writeFITS(rampHDU(8, []int64{9})), Options{UseHDU: true}, ErrUnsupported, "NAXIS < 2"},
		{"no data", writeFITS(testHDU{bitpix: 8, axes: []int64{3, 0}}), Options{UseHDU: true}, ErrUnsupported, "no data"},
		{"SIMPLE = F", []byte(strings.Replace(string(writeFITS(rampHDU(8, []int64{2, 2}))), "SIMPLE  = T", "SIMPLE  = F", 1)), Options{}, ErrUnsupported, "SIMPLE = F"},
		{"pixels", writeFITS(rampHDU(8, []int64{10, 10})), Options{MaxPixels: 99}, ErrLimit, "pixel limit"},
		{"bytes", writeFITS(rampHDU(8, []int64{10, 10, 2})), Options{MaxBytes: 199}, ErrLimit, "byte limit"},
		{"planes", writeFITS(rampHDU(8, []int64{1, 1, 5, 3})), Options{MaxPlanes: 14}, ErrLimit, "plane limit"},
		{"not FITS", bytes.Repeat([]byte("x"), 3000), Options{}, ErrMalformed, "not a FITS file"},
		{"short", []byte("SIMPLE  = T"), Options{}, ErrMalformed, "shorter than one"},
		{"no END", []byte(padRight(rawCard("SIMPLE", "T")+rawCard("BITPIX", "8"), blockLen)), Options{}, ErrMalformed, "no END card"},
		{"truncated data", writeFITS(rampHDU(16, []int64{40, 40}))[:blockLen+100], Options{}, ErrMalformed, "past the end"},
		{"bad BITPIX", writeFITS(testHDU{bitpix: 12, axes: []int64{2, 2}}), Options{}, ErrMalformed, "BITPIX is not"},
		{"bad NAXISn", []byte(strings.Replace(string(writeFITS(rampHDU(8, []int64{2, 2}))), "NAXIS2  = 2 ", "NAXIS2  = -2", 1)), Options{}, ErrMalformed, "NAXISn"},
		{"overflow", writeFITS(testHDU{bitpix: -64, axes: []int64{1 << 40, 1 << 40}}), Options{}, ErrMalformed, "overflow"},
		{"BSCALE 0", writeFITS(rampHDU(8, []int64{2, 2}, kv{"BSCALE", "0"})), Options{}, ErrMalformed, "BSCALE = 0"},
	}
	for _, c := range cases {
		_, err := decodeBytes(c.file, c.opts)
		require.Error(t, err, c.name)
		assert.ErrorIs(t, err, c.want, c.name)
		assert.Contains(t, err.Error(), c.says, c.name)
	}
	// Truncated at every length, the reader refuses without panicking.
	full := writeFITS(rampHDU(16, []int64{7, 5, 2}), testHDU{ext: "IMAGE", bitpix: 8, axes: []int64{3, 3}, data: make([]byte, 9)})
	for n := 0; n < len(full); n += 97 {
		_, _ = decodeBytes(full[:n], Options{})
		_, _ = inspectBytes(full[:n])
	}
}

// A file whose headers declare a vast image but whose data is not there
// is refused before anything of that size is allocated.
func TestLimitsComeFromTheHeaderBeforeReading(t *testing.T) {
	h := testHDU{bitpix: -64, axes: []int64{100_000, 100_000}}
	b := []byte(padRight(h.header(), blockLen))
	_, err := decodeBytes(b, Options{})
	assert.ErrorIs(t, err, ErrMalformed, "the data is not in the file")
	assert.Contains(t, err.Error(), "past the end")
}

func TestContextIsHonoured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := writeFITS(rampHDU(8, []int64{4, 4}))
	_, err := Decode(ctx, bytes.NewReader(b), int64(len(b)), "x", Options{})
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestCardValueGrammar(t *testing.T) {
	for _, s := range []string{"1", "-1", "+1", "1.", ".5", "1.5E3", "1.5D-3", "-.5e+2", "12E4"} {
		assert.True(t, isReal(s) || isInteger(s), s)
	}
	for _, s := range []string{"", "E3", "1.5E", "inf", "NaN", "1..2", "0x10", "1_000", "--1"} {
		assert.False(t, isReal(s) || isInteger(s), s)
	}
	c := parseValue("'unterminated")
	assert.Equal(t, ValueKindOpaque, c.Kind)
	c = parseValue("  'a''b'  / c")
	assert.Equal(t, "a'b", c.Value)
	assert.Equal(t, "c", c.Comment)
	c = parseValue(" junk value")
	assert.Equal(t, ValueKindOpaque, c.Kind)
	_ = math.Pi
}
