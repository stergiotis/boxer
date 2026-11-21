package imaging

import (
	"encoding/hex"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planeOfKind makes a w × h plane of kind whose sample i is at(i), converted
// to the kind.
func planeOfKind(t *testing.T, kind SampleKindE, w, h int, at func(i int) float64) Plane {
	t.Helper()
	n := w * h
	var p Plane
	var err error
	switch kind {
	case SampleKindU8:
		s := make([]uint8, n)
		for i := range s {
			s[i] = uint8(at(i))
		}
		p, err = NewPlaneU8(w, h, s)
	case SampleKindU16:
		s := make([]uint16, n)
		for i := range s {
			s[i] = uint16(at(i))
		}
		p, err = NewPlaneU16(w, h, s)
	case SampleKindF32:
		s := make([]float32, n)
		for i := range s {
			s[i] = float32(at(i))
		}
		p, err = NewPlaneF32(w, h, s)
	case SampleKindI16:
		s := make([]int16, n)
		for i := range s {
			s[i] = int16(at(i))
		}
		p, err = NewPlaneI16(w, h, s)
	case SampleKindI32:
		s := make([]int32, n)
		for i := range s {
			s[i] = int32(at(i))
		}
		p, err = NewPlaneI32(w, h, s)
	case SampleKindF64:
		s := make([]float64, n)
		for i := range s {
			s[i] = at(i)
		}
		p, err = NewPlaneF64(w, h, s)
	default:
		t.Fatalf("no kind %v", kind)
	}
	require.NoError(t, err)
	return p
}

func TestSampleKindPredicates(t *testing.T) {
	type row struct {
		kind                   SampleKindE
		name                   string
		integer, float, signed bool
		bytes                  int
		lo, hi                 float64
		hasRange               bool
		codesLo                int64
		codes                  int
		hasCodes               bool
		nominalMax             float64
		hasNominalMax          bool
	}
	rows := []row{
		{SampleKindU8, "u8", true, false, false, 1, 0, 255, true, 0, 256, true, 255, true},
		{SampleKindU16, "u16", true, false, false, 2, 0, 65535, true, 0, 65536, true, 65535, true},
		{SampleKindF32, "f32", false, true, false, 4, 0, 0, false, 0, 0, false, 0, false},
		{SampleKindI16, "i16", true, false, true, 2, -32768, 32767, true, -32768, 65536, true, 0, false},
		{SampleKindI32, "i32", true, false, true, 4, -2147483648, 2147483647, true, 0, 0, false, 0, false},
		{SampleKindF64, "f64", false, true, false, 8, 0, 0, false, 0, 0, false, 0, false},
		{0, "none", false, false, false, 0, 0, 0, false, 0, 0, false, 0, false},
	}
	require.Len(t, AllSampleKinds, 6)
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			k := r.kind
			assert.Equal(t, r.name, k.String())
			assert.Equal(t, r.integer, k.IsInteger())
			assert.Equal(t, r.float, k.IsFloat())
			assert.Equal(t, r.signed, k.IsSigned())
			assert.Equal(t, r.bytes, k.Bytes())
			lo, hi, ok := k.NominalRange()
			assert.Equal(t, r.hasRange, ok)
			assert.Equal(t, r.lo, lo)
			assert.Equal(t, r.hi, hi)
			cl, cn, ok := k.Codes()
			assert.Equal(t, r.hasCodes, ok)
			assert.Equal(t, r.codesLo, cl)
			assert.Equal(t, r.codes, cn)
			if ok {
				assert.Equal(t, float64(cl), lo, "the codes start at the nominal minimum")
				assert.Equal(t, float64(cl)+float64(cn)-1, hi, "and end at the nominal maximum")
			}
			m, ok := k.NominalMax()
			assert.Equal(t, r.hasNominalMax, ok)
			assert.Equal(t, r.nominalMax, m)
		})
	}
	// The kind byte is in the digest header: the values never move.
	assert.Equal(t, []SampleKindE{1, 2, 3, 4, 5, 6},
		[]SampleKindE{SampleKindU8, SampleKindU16, SampleKindF32, SampleKindI16, SampleKindI32, SampleKindF64})
}

func TestEveryKindStoresReadsAndRangesItsSamples(t *testing.T) {
	type row struct {
		kind     SampleKindE
		samples  []float64
		lo, hi   float64
		accessor func(Plane) int
	}
	rows := []row{
		{SampleKindU8, []float64{7, 0, 255, 3}, 0, 255, func(p Plane) int { return len(p.U8()) }},
		{SampleKindU16, []float64{7, 0, 65535, 3}, 0, 65535, func(p Plane) int { return len(p.U16()) }},
		{SampleKindF32, []float64{7, -0.5, 1e30, 3}, -0.5, float64(float32(1e30)), func(p Plane) int { return len(p.F32()) }},
		{SampleKindI16, []float64{7, -32768, 32767, -3}, -32768, 32767, func(p Plane) int { return len(p.I16()) }},
		{SampleKindI32, []float64{7, -2147483648, 2147483647, -3}, -2147483648, 2147483647, func(p Plane) int { return len(p.I32()) }},
		{SampleKindF64, []float64{7, -1e300, 1e300, 0.1}, -1e300, 1e300, func(p Plane) int { return len(p.F64()) }},
	}
	for _, r := range rows {
		t.Run(r.kind.String(), func(t *testing.T) {
			p := planeOfKind(t, r.kind, 2, 2, func(i int) float64 { return r.samples[i] })
			assert.Equal(t, r.kind, p.Kind())
			assert.Equal(t, 4, r.accessor(p), "the kind's accessor holds the samples")
			for _, k := range AllSampleKinds {
				if k == r.kind {
					continue
				}
				other := map[SampleKindE]int{SampleKindU8: len(p.U8()), SampleKindU16: len(p.U16()), SampleKindF32: len(p.F32()),
					SampleKindI16: len(p.I16()), SampleKindI32: len(p.I32()), SampleKindF64: len(p.F64())}
				assert.Zero(t, other[k], "the %s accessor of a %s plane is nil", k, r.kind)
			}
			for i, want := range r.samples {
				if r.kind == SampleKindF32 {
					want = float64(float32(want))
				}
				got, ok := p.StoredAt(i%2, i/2)
				assert.True(t, ok)
				assert.Equal(t, want, got, "sample %d is held exactly", i)
			}
			lo, hi, ok := p.StoredRange()
			assert.True(t, ok)
			assert.Equal(t, r.lo, lo)
			assert.Equal(t, r.hi, hi)

			p2, err := p.WithTransform(ValueTransform{Scale: 2, Offset: 1})
			require.NoError(t, err)
			v, ok := p2.ValueAt(1, 1)
			assert.True(t, ok)
			s, _ := p.StoredAt(1, 1)
			assert.Equal(t, 2*s+1, v)
		})
	}
}

func TestNaNIsInvalidInAF64Plane(t *testing.T) {
	p, err := NewPlaneF64(3, 1, []float64{math.NaN(), 4, -2})
	require.NoError(t, err)
	_, ok := p.StoredAt(0, 0)
	assert.False(t, ok)
	_, ok = p.ValueAt(0, 0)
	assert.False(t, ok)
	lo, hi, ok := p.StoredRange()
	assert.True(t, ok)
	assert.Equal(t, -2.0, lo)
	assert.Equal(t, 4.0, hi)
	_, err = NewPlaneI16(2, 2, make([]int16, 3))
	assert.Error(t, err)
	_, err = NewPlaneI32(0, 1, nil)
	assert.Error(t, err)
	_, err = NewPlaneF64(1, 2, make([]float64, 1))
	assert.Error(t, err)
}

func TestTheDeclaredInvalidValueIsHonoured(t *testing.T) {
	for _, k := range AllSampleKinds {
		t.Run(k.String(), func(t *testing.T) {
			// Samples 5, 9, 5, 2: 5 is declared invalid.
			p := planeOfKind(t, k, 4, 1, func(i int) float64 { return []float64{5, 9, 5, 2}[i] })
			_, ok := p.Invalid()
			assert.False(t, ok, "a new plane declares nothing")
			q, err := p.WithInvalid(5)
			require.NoError(t, err)
			v, ok := q.Invalid()
			assert.True(t, ok)
			assert.Equal(t, 5.0, v)
			_, ok = q.StoredAt(0, 0)
			assert.False(t, ok)
			_, ok = q.ValueAt(2, 0)
			assert.False(t, ok)
			s, ok := q.StoredAt(1, 0)
			assert.True(t, ok)
			assert.Equal(t, 9.0, s)
			lo, hi, ok := q.StoredRange()
			assert.True(t, ok)
			assert.Equal(t, 2.0, lo)
			assert.Equal(t, 9.0, hi)

			// The declaration survives a transform, and leaves the plane it
			// came from alone.
			r, err := q.WithTransform(ValueTransform{Scale: 3})
			require.NoError(t, err)
			_, ok = r.Invalid()
			assert.True(t, ok)
			_, ok = p.StoredAt(0, 0)
			assert.True(t, ok)

			// A plane of nothing but the invalid value has no range.
			all := planeOfKind(t, k, 2, 1, func(int) float64 { return 5 })
			all, err = all.WithInvalid(5)
			require.NoError(t, err)
			_, _, ok = all.StoredRange()
			assert.False(t, ok)

			// The digest is of the samples, not the declaration.
			a, err := NewImage(ColorModelGray, []Plane{p}, Provenance{})
			require.NoError(t, err)
			b, err := NewImage(ColorModelGray, []Plane{q}, Provenance{})
			require.NoError(t, err)
			assert.Equal(t, a.Hash(), b.Hash())
		})
	}
}

func TestAnInvalidValueTheKindCannotHoldIsRefused(t *testing.T) {
	mk := func(k SampleKindE) Plane { return planeOfKind(t, k, 1, 1, func(int) float64 { return 0 }) }
	refused := []struct {
		kind  SampleKindE
		value float64
	}{
		{SampleKindU8, -1}, {SampleKindU8, 256}, {SampleKindU8, 1.5},
		{SampleKindU16, 65536}, {SampleKindU16, -0.5},
		{SampleKindI16, -32769}, {SampleKindI16, 32768}, {SampleKindI16, 0.25},
		{SampleKindI32, -2147483649}, {SampleKindI32, 2147483648}, {SampleKindI32, 1e-9},
		{SampleKindF32, 0.1}, {SampleKindF32, 1e300}, {SampleKindF32, math.NaN()}, {SampleKindF32, math.Inf(1)},
		{SampleKindF64, math.NaN()}, {SampleKindF64, math.Inf(-1)},
		{SampleKindU8, math.NaN()},
	}
	for _, r := range refused {
		_, err := mk(r.kind).WithInvalid(r.value)
		assert.Error(t, err, "%s refuses %v", r.kind, r.value)
	}
	accepted := []struct {
		kind  SampleKindE
		value float64
	}{
		{SampleKindU8, 0}, {SampleKindU8, 255}, {SampleKindU16, 65535},
		{SampleKindI16, -32768}, {SampleKindI32, -2147483648}, {SampleKindI32, 2147483647},
		{SampleKindF32, -3.4028234663852886e+38}, {SampleKindF32, 0.5}, {SampleKindF64, 0.1}, {SampleKindF64, -9999},
	}
	for _, r := range accepted {
		_, err := mk(r.kind).WithInvalid(r.value)
		assert.NoError(t, err, "%s accepts %v", r.kind, r.value)
	}
	_, err := Plane{}.WithInvalid(0)
	assert.Error(t, err)
}

func TestNewImageWantsOneInvalidValueAndNoneOnAlpha(t *testing.T) {
	a := planeOfKind(t, SampleKindI16, 2, 1, func(i int) float64 { return float64(i) })
	b := planeOfKind(t, SampleKindI16, 2, 1, func(i int) float64 { return float64(i + 1) })
	an, err := a.WithInvalid(-9999)
	require.NoError(t, err)
	bn, err := b.WithInvalid(-9999)
	require.NoError(t, err)
	bm, err := b.WithInvalid(-1)
	require.NoError(t, err)

	_, err = NewImage(ColorModelRGB, []Plane{an, bn, an}, Provenance{})
	assert.NoError(t, err, "one value on every plane")
	_, err = NewImage(ColorModelRGB, []Plane{an, b, an}, Provenance{})
	assert.Error(t, err, "declared on some planes only")
	_, err = NewImage(ColorModelRGB, []Plane{an, bm, an}, Provenance{})
	assert.Error(t, err, "two values")
	_, err = NewImage(ColorModelGrayAlpha, []Plane{an, b}, Provenance{})
	assert.NoError(t, err, "alpha is left out of the agreement")
	_, err = NewImage(ColorModelGrayAlpha, []Plane{an, bn}, Provenance{})
	assert.Error(t, err, "an alpha plane declares none")
}

// The digests of images that could exist before ADR-0398 were computed with
// the code as it stood then, and must not move.
func TestDigestsOfEarlierImagesDoNotChange(t *testing.T) {
	p8, _ := NewPlaneU8(3, 2, []uint8{0, 1, 2, 250, 254, 255})
	gray, err := NewImage(ColorModelGray, []Plane{p8}, Provenance{})
	require.NoError(t, err)
	r, _ := NewPlaneU16(2, 2, []uint16{0, 1, 65535, 300})
	g, _ := NewPlaneU16(2, 2, []uint16{7, 8, 9, 10})
	b, _ := NewPlaneU16(2, 2, []uint16{40000, 2, 3, 4})
	a, _ := NewPlaneU16(2, 2, []uint16{65535, 65535, 0, 1})
	rgba, err := NewImage(ColorModelRGBA, []Plane{r, g, b, a}, Provenance{})
	require.NoError(t, err)
	f, _ := NewPlaneF32(2, 1, []float32{-1.5, 3.25})
	fgray, err := NewImage(ColorModelGray, []Plane{f}, Provenance{})
	require.NoError(t, err)
	fga, err := NewImage(ColorModelGrayAlpha, []Plane{f, f}, Provenance{})
	require.NoError(t, err)
	for _, c := range []struct {
		img  Image
		want string
	}{
		{gray, "94d001438ec4c631f1f0a4b48519bcffd3143f5d598aa155a9c2868328b09ec1"},
		{rgba, "496bc1e0293cd9ad85f82bffe463b95c6600a2ef3fc1993fd72a96e8c40b1147"},
		{fgray, "6fe32a91f172c78e777c4741bc5ad97476e051339149539c06d453f313f87828"},
		{fga, "ba5321a92d9362f447287ed9dcf7db19dcb1edb33bf57b43efc41c673d93f664"},
	} {
		h := c.img.Hash()
		assert.Equal(t, c.want, hex.EncodeToString(h[:]))
		assert.Equal(t, c.img.digest(), h, "the streaming digest agrees")
	}
}

// A digest of each new kind written row by row is the image's hash, the
// kinds are told apart, and signed samples hash as their two's complement.
func TestTheNewKindsDigest(t *testing.T) {
	const w, h = 5, 3
	i16 := make([]int16, w*h)
	i32 := make([]int32, w*h)
	f64 := make([]float64, w*h)
	for i := range i16 {
		i16[i], i32[i], f64[i] = int16(i*4099-30000), int32(i*1_000_003-7), float64(i)/3-2
	}
	pi16, _ := NewPlaneI16(w, h, i16)
	pi32, _ := NewPlaneI32(w, h, i32)
	pf64, _ := NewPlaneF64(w, h, f64)
	for _, c := range []struct {
		p     Plane
		write func(d *Digest, row int)
	}{
		{pi16, func(d *Digest, row int) { d.WriteI16(0, i16[row*w:(row+1)*w]) }},
		{pi32, func(d *Digest, row int) { d.WriteI32(0, i32[row*w:(row+1)*w]) }},
		{pf64, func(d *Digest, row int) { d.WriteF64(0, f64[row*w:(row+1)*w]) }},
	} {
		img, err := NewImage(ColorModelGray, []Plane{c.p}, Provenance{})
		require.NoError(t, err)
		d := NewDigest(ColorModelGray, c.p.Kind(), w, h)
		for row := range h {
			c.write(d, row)
		}
		assert.Equal(t, img.Hash(), d.Sum(), "%s row by row", c.p.Kind())
		assert.Equal(t, img.Hash(), img.digest(), "%s through WritePlane", c.p.Kind())
	}

	// The same 16 bits as unsigned and signed are two images.
	u, _ := NewPlaneU16(2, 1, []uint16{0xffff, 1})
	s, _ := NewPlaneI16(2, 1, []int16{-1, 1})
	iu, _ := NewImage(ColorModelGray, []Plane{u}, Provenance{})
	is, _ := NewImage(ColorModelGray, []Plane{s}, Provenance{})
	assert.NotEqual(t, iu.Hash(), is.Hash())
	du := NewDigest(ColorModelGray, SampleKindI16, 2, 1)
	du.WriteU16(0, []uint16{0xffff, 1})
	assert.Equal(t, is.Hash(), du.Sum(), "a signed sample hashes as its two's complement bits")

	// A plane longer than the hasher's buffer hashes every sample.
	long := make([]float64, 100_000)
	pl, _ := NewPlaneF64(len(long), 1, long)
	long2 := make([]float64, 100_000)
	long2[99_999] = 1
	pl2, _ := NewPlaneF64(len(long2), 1, long2)
	assert.NotEqual(t, planeDigest(pl), planeDigest(pl2))
}

func TestImageLabelsAndPlacement(t *testing.T) {
	p := planeOfKind(t, SampleKindU8, 2, 1, func(i int) float64 { return float64(i) })
	img, err := NewImage(ColorModelRGB, []Plane{p, p, p}, Provenance{Notes: []string{"read"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"red", "green", "blue"}, img.PlaneLabels())
	_, err = img.WithPlaneLabels([]string{"B4", "B3"})
	assert.Error(t, err)
	labels := []string{"B4", "B3", "B2"}
	l, err := img.WithPlaneLabels(labels)
	require.NoError(t, err)
	labels[0] = "changed"
	assert.Equal(t, []string{"B4", "B3", "B2"}, l.PlaneLabels(), "the labels are copied")
	l.PlaneLabels()[0] = "x"
	assert.Equal(t, "B4", l.PlaneLabels()[0], "and handed out as a copy")
	assert.Equal(t, img.Hash(), l.Hash(), "labels are not hashed")

	assert.True(t, img.Placement().IsZero())
	pl := Placement{Affine: [6]float64{100, 2, 0, 50, 0, -2}, Frame: Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:2056"},
		Unapplied: []string{"a second system"}}
	placed := img.WithPlacement(pl)
	pl.Unapplied[0] = "changed"
	assert.Equal(t, "a second system", placed.Placement().Unapplied[0])
	assert.Equal(t, "EPSG:2056", placed.Placement().Frame.Code)
	assert.Equal(t, img.Hash(), placed.Hash(), "the placement is not hashed")
	assert.True(t, img.Placement().IsZero(), "the image it came from is unchanged")

	prov := img.Provenance()
	prov.Notes[0] = "changed"
	assert.Equal(t, "read", img.Provenance().Notes[0], "notes are handed out as a copy")
}

func TestDerivationsFillTheirByte(t *testing.T) {
	assert.Len(t, AllDerivations, 8)
	all := DerivationE(0)
	for _, d := range AllDerivations {
		all |= d
	}
	assert.Equal(t, DerivationE(math.MaxUint8), all)
	assert.True(t, (DerivationRowsFlipped | DerivationAxesReordered).Has(DerivationRowsFlipped))
}
