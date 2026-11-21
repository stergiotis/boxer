package ops

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
)

// Sizes that are not a multiple of any band size the tests use, nor of the
// tile size.
const testW, testH = 37, 23

var testBands = []int{1, 7, 512, 0}

// eachBand runs f with options at every band size of testBands — 0 the
// whole image — and with one and with several workers.
func eachBand(t *testing.T, h int, f func(t *testing.T, opts Options)) {
	for _, rows := range testBands {
		for _, workers := range []int{1, 3} {
			if rows == 0 {
				rows = h
			}
			f(t, Options{BandRows: rows, Workers: workers})
		}
	}
}

func TestExprAgainstDirectEvaluation(t *testing.T) {
	for _, ka := range imaging.AllSampleKinds {
		for _, kb := range imaging.AllSampleKinds {
			a := kindImage(t, ka, testW, testH, 1)
			b := kindImage(t, kb, testW, testH, 2)
			want := func(_, x, y int) (float64, bool) {
				va, oka := valueAt(a, 0, x, y)
				vb, okb := valueAt(b, 0, x, y)
				if !oka || !okb {
					return 0, false
				}
				d, s := va-vb, va+vb
				if !isFinite(d) || !isFinite(s) {
					return 0, false
				}
				return d / s, true
			}
			wantKind := imaging.SampleKindF32
			if !slices.Contains([]imaging.SampleKindE{imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindI16, imaging.SampleKindF32}, ka) ||
				!slices.Contains([]imaging.SampleKindE{imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindI16, imaging.SampleKindF32}, kb) {
				wantKind = imaging.SampleKindF64
			}
			var digest Digest
			eachBand(t, testH, func(t *testing.T, opts Options) {
				res := run(t, "expr", Params{"expression": Text("(a-b)/(a+b)")}, opts, in("a", a), in("b", b))
				require.Equal(t, wantKind, res.Store.Kind(), "%s %s", ka, kb)
				require.Equal(t, []string{"(a - b) / (a + b)"}, res.Store.PlaneLabels())
				checkDirect(t, res, want, 0)
				if digest.IsZero() {
					digest = res.Record.Output.Digest
				}
				require.Equal(t, digest, res.Record.Output.Digest, "band %d", opts.BandRows)
			})
		}
	}
}

func TestExprReadsInvalidSamplesOnlyThroughIsvalidAndWhere(t *testing.T) {
	for _, k := range imaging.AllSampleKinds {
		a := kindImage(t, k, testW, testH, 3)
		res := run(t, "expr", Params{"expression": Text("where(isvalid(a), a * 2, -1) + isvalid(a / 0)")}, Options{}, in("a", a))
		checkDirect(t, res, func(_, x, y int) (float64, bool) {
			v, ok := valueAt(a, 0, x, y)
			if !ok {
				return -1, true
			}
			if !isFinite(v * 2) {
				return 0, false
			}
			return v * 2, true
		}, 0)
	}
}

var axisStats = []string{statMean, statMedian, statMin, statMax, statStdDev, statCount}

func TestAxisStatAgainstDirectEvaluation(t *testing.T) {
	// Every kind among the inputs, so that pixels have any number of valid
	// samples from zero to six.
	imgs := make([]imaging.Image, len(imaging.AllSampleKinds))
	inputs := make([]Input, len(imgs))
	for i, k := range imaging.AllSampleKinds {
		imgs[i] = kindImage(t, k, testW, testH, uint64(10+i))
		inputs[i] = in("in"+k.String(), imgs[i])
	}
	// Four inputs, so that an even count of valid samples happens often.
	evenImgs := imgs[:4]
	for _, set := range [][]imaging.Image{imgs, evenImgs, imgs[:1]} {
		for _, stat := range axisStats {
			want := func(_, x, y int) (float64, bool) {
				var v []float64
				for _, img := range set {
					if s, ok := valueAt(img, 0, x, y); ok {
						v = append(v, s)
					}
				}
				if stat == statCount {
					return float64(len(v)), true
				}
				if len(v) == 0 {
					return 0, false
				}
				var sum float64
				for _, s := range v {
					sum += s
				}
				mean := sum / float64(len(v))
				switch stat {
				case statMean:
					return mean, true
				case statMin:
					return slices.Min(v), true
				case statMax:
					return slices.Max(v), true
				case statStdDev:
					var ss float64
					for _, s := range v {
						ss += (s - mean) * (s - mean)
					}
					return math.Sqrt(ss / float64(len(v))), true
				}
				slices.Sort(v)
				// The lower middle of an even count: the nearest-rank 50th
				// percentile, as the stats package gives it.
				return v[(len(v)-1)/2], true
			}
			var digest Digest
			eachBand(t, testH, func(t *testing.T, opts Options) {
				res := run(t, "axis-stat", Params{"stat": Text(stat)}, opts, inputs[:len(set)]...)
				checkDirect(t, res, want, 0)
				if digest.IsZero() {
					digest = res.Record.Output.Digest
				}
				require.Equal(t, digest, res.Record.Output.Digest, "%s band %d", stat, opts.BandRows)
			})
		}
	}
}

func TestAxisStatMedianOfAnEvenCountIsTheLowerMiddle(t *testing.T) {
	vals := []float64{4, 1, 3, 2}
	inputs := make([]Input, len(vals))
	for i, v := range vals {
		p, err := imaging.NewPlaneF64(1, 1, []float64{v})
		require.NoError(t, err)
		img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
		require.NoError(t, err)
		inputs[i] = in(string(rune('a'+i)), img)
	}
	res := run(t, "axis-stat", Params{"stat": Text(statMedian)}, Options{}, inputs...)
	got, ok := readAll(t, res.Store).Plane(0).ValueAt(0, 0)
	require.True(t, ok)
	require.Equal(t, 2.0, got)
}

func gaussianDirect(img imaging.Image, sigma float64, border BorderE) directF {
	w := gaussianWeights(sigma)
	r := len(w) / 2
	return func(p, x, y int) (float64, bool) {
		if border == BorderInvalid && nearEdge(img, x, y, r) {
			return 0, false
		}
		var s float64
		for j := -r; j <= r; j++ {
			for i := -r; i <= r; i++ {
				v, ok := windowAt(img, p, x+i, y+j, border)
				if !ok {
					return 0, false
				}
				s += w[j+r] * w[i+r] * v
			}
		}
		return s, true
	}
}

func TestGaussianAgainstDirectEvaluation(t *testing.T) {
	for _, k := range imaging.AllSampleKinds {
		img := imageOf(t, imaging.ColorModelRGB, k, testW, testH, 20)
		for _, border := range AllBorders {
			for _, sigma := range []float64{0.6, 1.3} {
				var digest Digest
				eachBand(t, testH, func(t *testing.T, opts Options) {
					res := run(t, "gaussian", Params{"sigma": Number(sigma), "border": Text(border.String())}, opts, in("img", img))
					checkDirect(t, res, gaussianDirect(img, sigma, border), 1e-6)
					if digest.IsZero() {
						digest = res.Record.Output.Digest
					}
					require.Equal(t, digest, res.Record.Output.Digest, "%s %s band %d", k, border, opts.BandRows)
				})
			}
		}
	}
}

func medianDirect(img imaging.Image, r int, border BorderE) directF {
	return func(p, x, y int) (float64, bool) {
		if border == BorderInvalid && nearEdge(img, x, y, r) {
			return 0, false
		}
		var win []float64
		for j := -r; j <= r; j++ {
			for i := -r; i <= r; i++ {
				v, ok := windowAt(img, p, x+i, y+j, border)
				if !ok {
					return 0, false
				}
				win = append(win, v)
			}
		}
		slices.Sort(win)
		return win[len(win)/2], true
	}
}

func TestMedianFilterAgainstDirectEvaluation(t *testing.T) {
	for _, k := range imaging.AllSampleKinds {
		img := imageOf(t, imaging.ColorModelRGB, k, testW, testH, 30)
		for _, border := range AllBorders {
			for _, r := range []int64{1, 3} {
				var digest Digest
				eachBand(t, testH, func(t *testing.T, opts Options) {
					res := run(t, "median-filter", Params{"radius": Integer(r), "border": Text(border.String())}, opts, in("img", img))
					checkDirect(t, res, medianDirect(img, int(r), border), 0)
					if digest.IsZero() {
						digest = res.Record.Output.Digest
					}
					require.Equal(t, digest, res.Record.Output.Digest, "%s %s band %d", k, border, opts.BandRows)
				})
			}
		}
	}
}

func TestSelectNthAgreesWithASort(t *testing.T) {
	for n := 1; n < 200; n++ {
		v := make([]float64, n)
		for i := range v {
			v[i] = float64((i*7919 + n*31) % 13)
		}
		sorted := slices.Sorted(slices.Values(v))
		for _, k := range []int{0, n / 2, n - 1} {
			require.Equal(t, sorted[k], selectNth(slices.Clone(v), k), "n %d k %d", n, k)
		}
	}
}

func TestStandardizeAgainstDirectEvaluation(t *testing.T) {
	for _, k := range imaging.AllSampleKinds {
		model := imaging.ColorModelRGB
		if k.IsFloat() {
			// A float plane here holds an infinity, whose mean is not
			// finite: one plane without it.
			model = imaging.ColorModelGray
		}
		img := imageOf(t, model, k, testW, testH, 40)
		if k.IsFloat() {
			img = withoutInfinities(t, img)
		}
		means, sds := make([]float64, img.PlaneCount()), make([]float64, img.PlaneCount())
		for p := range img.PlaneCount() {
			var v []float64
			for y := range testH {
				for x := range testW {
					if s, ok := valueAt(img, p, x, y); ok {
						v = append(v, s)
					}
				}
			}
			var sum float64
			for _, s := range v {
				sum += s
			}
			means[p] = sum / float64(len(v))
			var ss float64
			for _, s := range v {
				ss += (s - means[p]) * (s - means[p])
			}
			sds[p] = math.Sqrt(ss / float64(len(v)))
		}
		var digest Digest
		eachBand(t, testH, func(t *testing.T, opts Options) {
			res := run(t, "standardize", nil, opts, in("img", img))
			checkDirect(t, res, func(p, x, y int) (float64, bool) {
				v, ok := valueAt(img, p, x, y)
				return (v - means[p]) / sds[p], ok
			}, 1e-6)
			for _, q := range res.Record.Quantities {
				switch q.Name {
				case "mean":
					require.InEpsilon(t, means[q.Plane], q.Value, 1e-12)
				case "stddev":
					require.InEpsilon(t, sds[q.Plane], q.Value, 1e-9)
				}
			}
			if digest.IsZero() {
				digest = res.Record.Output.Digest
			}
			require.Equal(t, digest, res.Record.Output.Digest, "%s band %d", k, opts.BandRows)
		})
	}
}

// withoutInfinities replaces the infinities of a float image by NaN.
func withoutInfinities(t *testing.T, img imaging.Image) imaging.Image {
	planes := make([]imaging.Plane, img.PlaneCount())
	for i := range planes {
		p := img.Plane(i)
		var err error
		switch p.Kind() {
		case imaging.SampleKindF32:
			s := slices.Clone(p.F32())
			for j, v := range s {
				if math.IsInf(float64(v), 0) {
					s[j] = float32(math.NaN())
				}
			}
			planes[i], err = imaging.NewPlaneF32(p.Width(), p.Height(), s)
		case imaging.SampleKindF64:
			s := slices.Clone(p.F64())
			for j, v := range s {
				if math.IsInf(v, 0) {
					s[j] = math.NaN()
				}
			}
			planes[i], err = imaging.NewPlaneF64(p.Width(), p.Height(), s)
		default:
			planes[i] = p
		}
		require.NoError(t, err)
	}
	out, err := imaging.NewImage(img.Model(), planes, img.Provenance())
	require.NoError(t, err)
	return out
}

// TestOnImagesLargerThanABand runs the neighbourhood operators over an
// image of several bands and tiles, neither a multiple of the band nor of
// the tile size, against the direct evaluation, and every operator over the
// sample images at band sizes 1, 7, 512 and the whole image.
func TestOnImagesLargerThanABand(t *testing.T) {
	field, err := samples.ByName(samples.NameField)
	require.NoError(t, err)
	res := run(t, "gaussian", Params{"sigma": Number(1.5), "border": Text("nearest")}, Options{}, in("field", field))
	checkDirect(t, res, gaussianDirect(field, 1.5, BorderNearest), 1e-6)
	res = run(t, "median-filter", Params{"radius": Integer(2), "border": Text("invalid")}, Options{}, in("field", field))
	checkDirect(t, res, medianDirect(field, 2, BorderInvalid), 0)

	if testing.Short() {
		t.Skip("the band sizes over the sample images take a while")
	}
	for _, name := range []string{samples.NameElevation, samples.NameDouble, samples.NameCounts, samples.NameFloat, samples.NameScaled} {
		img, err := samples.ByName(name)
		require.NoError(t, err)
		for _, req := range []Request{
			{Operator: "expr", Params: Params{"expression": Text("where(img > 0, sqrt(img), -img) * 2")}},
			{Operator: "axis-stat", Params: Params{"stat": Text("stddev")}},
			{Operator: "gaussian", Params: Params{"sigma": Number(2.2), "border": Text("invalid")}},
			{Operator: "median-filter", Params: Params{"radius": Integer(2), "border": Text("nearest")}},
			{Operator: "standardize"},
		} {
			req.Inputs = []Input{in("img", img)}
			if req.Operator == "axis-stat" {
				req.Inputs = append(req.Inputs, in("again", img))
			}
			var digest Digest
			for _, rows := range []int{1, 7, 512, img.Height()} {
				res, err := Run(context.Background(), req, Options{BandRows: rows})
				require.NoError(t, err, "%s %s", name, req.Operator)
				if digest.IsZero() {
					digest = res.Record.Output.Digest
				}
				require.Equal(t, digest, res.Record.Output.Digest, "%s %s band %d", name, req.Operator, rows)
			}
		}
	}
}
