package display

import (
	"bufio"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// zscaleStream is splitmix64, the stream testdata/gen_zscale_oracle.py.txt
// draws its fields from.
type zscaleStream struct{ state uint64 }

func (inst *zscaleStream) uniform() float64 {
	inst.state += 0x9E3779B97F4A7C15
	z := inst.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z ^= z >> 31
	return float64(z>>11) * (1.0 / 9007199254740992.0)
}

var zscaleBlobs = [...]struct{ cx, cy, a, r float64 }{
	{40, 60, 8000, 3}, {200, 30, 20000, 2}, {128, 128, 5000, 6}, {90, 210, 12000, 1.5}, {230, 220, 30000, 4},
}

// zscaleField is the generator's field of a name as a plane: f64, NaN for
// an invalid sample, or for "declared" the value −9999 declared invalid, or
// for a "-u16" name unsigned 16-bit under the transform stored · −0.5 + 100.
// Every product is converted on its own so no multiply-add is fused.
func zscaleField(t *testing.T, name string, w, h int) imaging.Plane {
	var seed uint64 = 0x5EED0401
	for _, c := range name {
		seed += uint64(c)
	}
	s := zscaleStream{state: seed}
	sqrt3 := math.Sqrt(3)
	base, _, _ := strings.Cut(name, "-")
	values := make([]float64, w*h)
	for y := range h {
		for x := range w {
			var u [5]float64
			for i := range u {
				u[i] = s.uniform()
			}
			n := float64(((u[0]+u[1])+(u[2]+u[3]))-2) * sqrt3
			var v float64
			switch base {
			case "flat":
				v = 1000 + float64(20*n)
			case "blobs":
				v = 500 + float64(10*n)
				for _, b := range zscaleBlobs {
					dx, dy := float64(x)-b.cx, float64(y)-b.cy
					v += b.a / (1 + (float64(dx*dx)+float64(dy*dy))/float64(b.r*b.r))
				}
			case "gradient":
				v = float64(float64(2*float64(x))+float64(5*float64(y))) + float64(3*n)
			case "nan", "declared":
				v = 100 + float64(5*n)
				if u[4] < 0.1 {
					v = math.NaN()
				}
			case "heavy":
				v = 100 + float64(5*n)
				if u[4] < 0.05 {
					v += float64(2000 * u[0])
				}
			case "small", "tiny", "four":
				v = 50 + float64(2*n)
			case "strip":
				v = 10 + n
			default:
				t.Fatalf("unknown field %s", name)
			}
			values[y*w+x] = v
		}
	}
	if strings.HasSuffix(name, "-u16") {
		stored := make([]uint16, len(values))
		for i, v := range values {
			stored[i] = uint16(min(max(math.Floor(v+0.5), 0), 65535))
		}
		p, err := imaging.NewPlaneU16(w, h, stored)
		require.NoError(t, err)
		p, err = p.WithTransform(imaging.ValueTransform{Scale: -0.5, Offset: 100})
		require.NoError(t, err)
		return p
	}
	if base == "declared" {
		for i, v := range values {
			if v != v {
				values[i] = -9999
			}
		}
	}
	p, err := imaging.NewPlaneF64(w, h, values)
	require.NoError(t, err)
	if base == "declared" {
		p, err = p.WithInvalid(-9999)
		require.NoError(t, err)
	}
	return p
}

// zscaleSampled is the sorted sample ZScaleLimits takes of a plane.
func zscaleSampled(p imaging.Plane, samples int) []float64 {
	v := zscaleSample(p.Width(), p.Height(), samples, p.ValueAt)
	slices.Sort(v)
	return v
}

// The fit is held to astropy's ZScaleInterval over the same sample: the
// rejection, the median and the slope agree to 1e-9 of the range. astropy
// places the ends one sample further up than IRAF does — c−1 samples below
// the centre and m−c above it — and the package takes IRAF's (c below,
// m−1−c above), so the oracle's limits are compared with the fit under
// astropy's ends, and the package's limits are checked to be the same fit
// under IRAF's.
func TestZScaleAgreesWithTheReferenceOverTheSameSample(t *testing.T) {
	f, err := os.Open("testdata/zscale_oracle.tsv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	var rows, fitted int
	worst := 0.0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		c := strings.Split(line, "\t")
		require.Len(t, c, 8, line)
		num := func(i int) float64 {
			v, err := strconv.ParseFloat(c[i], 64)
			require.NoError(t, err, line)
			return v
		}
		name, w, h, contrast, samples, m, wantLo, wantHi := c[0], int(num(1)), int(num(2)), num(3), int(num(4)), int(num(5)), num(6), num(7)
		p := zscaleField(t, name, w, h)
		values := zscaleSampled(p, samples)
		require.Len(t, values, m, "%s: the grid takes the generator's points", name)

		fit := fitZScale(values)
		lo, hi := fit.lo, fit.hi
		if fit.fitted {
			fitted++
			cc := (fit.m - 1) / 2
			step := fit.slope / contrast
			lo = max(fit.lo, fit.median-float64(cc-1)*step)
			hi = min(fit.hi, fit.median+float64(fit.m-cc)*step)
		}
		tol := 1e-9 * (fit.hi - fit.lo)
		assert.InDelta(t, wantLo, lo, tol, "%s lo", name)
		assert.InDelta(t, wantHi, hi, tol, "%s hi", name)
		worst = max(worst, math.Abs(wantLo-lo)/(fit.hi-fit.lo), math.Abs(wantHi-hi)/(fit.hi-fit.lo))

		got, ok := ZScaleLimits(p, contrast, samples)
		require.True(t, ok, name)
		gotLo, gotHi := fit.limits(contrast)
		assert.Equal(t, Limits{Lo: gotLo, Hi: gotHi, Source: LimitsSourceZScale, ZScaleContrast: contrast, ZScaleSamples: int32(samples)}, got, name)
		if fit.fitted {
			cc := (fit.m - 1) / 2
			step := fit.slope / contrast
			assert.Equal(t, max(fit.lo, fit.median-float64(cc)*step), got.Lo, "%s: IRAF's lower end", name)
			assert.Equal(t, min(fit.hi, fit.median+float64(fit.m-1-cc)*step), got.Hi, "%s: IRAF's upper end", name)
		} else {
			assert.Equal(t, fit.lo, got.Lo, "%s: too few samples left: the extremes", name)
			assert.Equal(t, fit.hi, got.Hi, name)
		}
		again, _ := ZScaleLimits(p, contrast, samples)
		assert.Equal(t, got, again, "%s: the same plane gives the same limits", name)
		rows++
	}
	require.NoError(t, sc.Err())
	assert.Equal(t, 15, rows)
	assert.GreaterOrEqual(t, fitted, 10, "most fields are fitted, not cut to their extremes")
	t.Logf("largest difference from the reference: %.3g of the sampled range", worst)
}

// The grid takes every pixel of an image no larger than the sample, at most
// the sample count otherwise, and is spread over the whole image.
func TestZScaleGridCoversTheImage(t *testing.T) {
	for _, tc := range []struct{ w, h, n int }{
		{20, 30, 1000}, {200, 150, 1000}, {1, 5000, 1000}, {5000, 1, 1000}, {4096, 4096, 1000}, {3, 3, 1}, {1000, 3, 7},
	} {
		nx, ny := zscaleGrid(tc.w, tc.h, tc.n)
		if tc.w*tc.h <= tc.n {
			assert.Equal(t, [2]int{tc.w, tc.h}, [2]int{nx, ny}, "%+v: every pixel", tc)
			continue
		}
		assert.LessOrEqual(t, nx*ny, tc.n, "%+v", tc)
		assert.Greater(t, nx*ny, tc.n/4, "%+v: not much fewer than asked", tc)
		var cols, rows []int
		_ = zscaleSample(tc.w, tc.h, tc.n, func(col, row int) (float64, bool) {
			if len(rows) == 0 || rows[len(rows)-1] != row {
				rows = append(rows, row)
			}
			if len(rows) == 1 {
				cols = append(cols, col)
			}
			return 0, true
		})
		require.Len(t, cols, nx)
		require.Len(t, rows, ny)
		assert.True(t, slices.IsSorted(cols) && cols[0] >= 0 && cols[nx-1] < tc.w, "%+v", tc)
		assert.True(t, slices.IsSorted(rows) && rows[0] >= 0 && rows[ny-1] < tc.h, "%+v", tc)
		assert.Less(t, cols[0], max(1, tc.w/nx), "%+v: the first column is in the first cell", tc)
		assert.Greater(t, cols[nx-1], tc.w-1-max(1, tc.w/nx), "%+v: the last in the last", tc)
	}
}

func TestZScaleEdgeCases(t *testing.T) {
	constant, err := imaging.NewPlaneF32(10, 10, make([]float32, 100))
	require.NoError(t, err)
	_, ok := ZScaleLimits(constant, 0, 0)
	assert.False(t, ok, "a constant plane has no range")

	nan := make([]float32, 100)
	for i := range nan {
		nan[i] = float32(math.NaN())
	}
	allInvalid, err := imaging.NewPlaneF32(10, 10, nan)
	require.NoError(t, err)
	_, ok = ZScaleLimits(allInvalid, 0, 0)
	assert.False(t, ok, "no valid sample")

	declared, err := imaging.NewPlaneU8(2, 2, []uint8{7, 7, 7, 7})
	require.NoError(t, err)
	declared, err = declared.WithInvalid(7)
	require.NoError(t, err)
	_, ok = ZScaleLimits(declared, 0, 0)
	assert.False(t, ok, "every sample declared invalid")

	few, err := imaging.NewPlaneU8(2, 2, []uint8{3, 9, 1, 40})
	require.NoError(t, err)
	l, ok := ZScaleLimits(few, 0, 0)
	require.True(t, ok)
	assert.Equal(t, Limits{Lo: 1, Hi: 40, Source: LimitsSourceZScale, ZScaleContrast: ZScaleContrast, ZScaleSamples: ZScaleSamples}, l,
		"fewer than five samples: the extremes, and the defaults recorded")

	// Mostly one value with a few others: the fit is flat and the limits are
	// no range.
	mostly := make([]uint16, 400)
	mostly[3], mostly[200] = 1000, 50000
	flat, err := imaging.NewPlaneU16(20, 20, mostly)
	require.NoError(t, err)
	_, ok = ZScaleLimits(flat, 0, 0)
	assert.False(t, ok)

	ramp := make([]float64, 64)
	for i := range ramp {
		ramp[i] = float64(i)
	}
	rp, err := imaging.NewPlaneF64(8, 8, ramp)
	require.NoError(t, err)
	for _, bad := range []struct {
		c float64
		n int
	}{{-1, 0}, {math.NaN(), 0}, {math.Inf(1), 0}, {0.25, -1}} {
		_, ok = ZScaleLimits(rp, bad.c, bad.n)
		assert.False(t, ok, "%+v", bad)
	}
	// A ramp is a line of slope 1 through its median 31.5 at the centre
	// index 31: under contrast 1 the ends are 31.5 − 31 and 31.5 + 32, the
	// upper cut to the largest value.
	l, ok = ZScaleLimits(rp, 1, 0)
	require.True(t, ok)
	assert.InDelta(t, 0.5, l.Lo, 1e-9)
	assert.Equal(t, 63.0, l.Hi)

	// A recipe naming zscale without its values is not compiled.
	_, err = CompilePlane(Recipe{Limits: Limits{Source: LimitsSourceZScale}}, imaging.SampleKindU8, imaging.IdentityTransform, nil)
	assert.Error(t, err)
	_, err = CompilePlane(Recipe{Limits: Limits{Lo: 0, Hi: 1, Source: LimitsSourceZScale, ZScaleContrast: -1}}, imaging.SampleKindU8, imaging.IdentityTransform, nil)
	assert.Error(t, err)
	_, err = CompilePlane(Recipe{Limits: l}, imaging.SampleKindU8, imaging.IdentityTransform, nil)
	assert.NoError(t, err)
}

// A brute-force zscale written from the description alone: rejection
// recomputed from scratch each round, the neighbourhood as a window. It
// agrees with the package on random samples, including those with ties and
// outliers.
func TestZScaleAgreesWithABruteForceReading(t *testing.T) {
	s := zscaleStream{state: 42}
	for trial := range 200 {
		m := 1 + int(s.uniform()*1500)
		values := make([]float64, m)
		for i := range values {
			v := math.Floor(s.uniform() * 50)
			if s.uniform() < 0.03 {
				v += s.uniform() * 1e4
			}
			values[i] = v
		}
		slices.Sort(values)
		fit := fitZScale(values)

		minGood := max(5, m/2)
		g := max(1, m/100)
		bad := make([]bool, m)
		good, last := m, m+1
		var slope float64
		did := false
		for range 5 {
			if good >= last || good < minGood {
				break
			}
			var xs, ys []float64
			for i, v := range values {
				if !bad[i] {
					xs, ys = append(xs, float64(i)), append(ys, v)
				}
			}
			mx, my := mean(xs), mean(ys)
			var sxy, sxx float64
			for i := range xs {
				sxy += (xs[i] - mx) * (ys[i] - my)
				sxx += (xs[i] - mx) * (xs[i] - mx)
			}
			b := sxy / sxx
			a := my - b*mx
			var res []float64
			for i := range xs {
				res = append(res, ys[i]-(a+b*xs[i]))
			}
			rm := mean(res)
			var ss float64
			for _, r := range res {
				ss += (r - rm) * (r - rm)
			}
			thr := 2.5 * math.Sqrt(ss/float64(len(res)))
			next := make([]bool, m)
			for i, v := range values {
				r := v - (a + b*float64(i))
				if !(bad[i] || r < -thr || r > thr) {
					continue
				}
				for k := i - (g-1)/2; k <= i+g-1-(g-1)/2; k++ {
					if k >= 0 && k < m {
						next[k] = true
					}
				}
			}
			bad, slope, did = next, b, true
			last, good = good, 0
			for _, x := range bad {
				if !x {
					good++
				}
			}
		}
		require.Equal(t, did && good >= minGood, fit.fitted, "trial %d", trial)
		if fit.fitted {
			assert.InDelta(t, slope, fit.slope, 1e-9*math.Max(1, math.Abs(slope)), "trial %d", trial)
		}
	}
}

func mean(v []float64) (m float64) {
	for _, x := range v {
		m += x
	}
	return m / float64(len(v))
}

// ZScaleGrid names the pixels ZScaleLimitsAt reads, in the order it reads
// them, so a tiled reader can collect them before it reads a tile.
func TestZScaleGridIsWhatZScaleReads(t *testing.T) {
	for _, tc := range []struct{ w, h, n int }{{20, 30, 1000}, {4096, 4096, 1000}, {1000, 3, 7}, {640, 480, 0}} {
		var read [][2]int
		_, _ = ZScaleLimitsAt(tc.w, tc.h, func(col, row int) (float64, bool) {
			read = append(read, [2]int{col, row})
			return float64(col + row), true
		}, 0, tc.n)
		var grid [][2]int
		for col, row := range ZScaleGrid(tc.w, tc.h, tc.n) {
			grid = append(grid, [2]int{col, row})
		}
		assert.Equal(t, read, grid, "%+v", tc)
	}
	for range ZScaleGrid(0, 10, 100) {
		t.Fatal("an empty image has no points")
	}
	for range ZScaleGrid(10, 10, -1) {
		t.Fatal("a negative count has no points")
	}
}
