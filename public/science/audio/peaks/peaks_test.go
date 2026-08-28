package peaks_test

import (
	"context"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
)

// refQuant is the containment quantiser written a second time — in float64
// with math.Floor/Ceil rather than an integer truncation plus two
// comparisons — so the package's version is checked against an independent
// one rather than against itself.
func refQuant(sample float32) (lo int8, hi int8) {
	v := float64(sample)
	if math.IsNaN(v) {
		v = 0
	}
	if v > 1 {
		v = 1
	} else if v < -1 {
		v = -1
	}
	v *= 127
	return int8(math.Floor(v)), int8(math.Ceil(v))
}

// refPyramid is the brute-force pyramid: level 0 straight from the samples,
// every higher level from the pair of children below it.
type refPyramid struct {
	mins [][][]int8 // [level][channel][bin]
	maxs [][][]int8
}

func buildRefPyramid(format pcm.Format, samples []float32, baseBin int32) (ref refPyramid) {
	channels := int(format.Channels)
	frames := len(samples) / channels
	base := int(baseBin)
	bins := (frames + base - 1) / base
	mins := make([][]int8, channels)
	maxs := make([][]int8, channels)
	for c := range channels {
		mins[c] = make([]int8, bins)
		maxs[c] = make([]int8, bins)
		for b := range bins {
			lo := int8(127)
			hi := int8(-127)
			for f := b * base; f < min((b+1)*base, frames); f++ {
				l, h := refQuant(samples[f*channels+c])
				if l < lo {
					lo = l
				}
				if h > hi {
					hi = h
				}
			}
			mins[c][b] = lo
			maxs[c][b] = hi
		}
	}
	ref.mins = append(ref.mins, mins)
	ref.maxs = append(ref.maxs, maxs)
	for len(ref.mins[len(ref.mins)-1][0]) > 1 {
		childMin := ref.mins[len(ref.mins)-1]
		childMax := ref.maxs[len(ref.maxs)-1]
		n := (len(childMin[0]) + 1) / 2
		mins = make([][]int8, channels)
		maxs = make([][]int8, channels)
		for c := range channels {
			mins[c] = make([]int8, n)
			maxs[c] = make([]int8, n)
			for b := range n {
				lo := childMin[c][2*b]
				hi := childMax[c][2*b]
				if 2*b+1 < len(childMin[c]) {
					if v := childMin[c][2*b+1]; v < lo {
						lo = v
					}
					if v := childMax[c][2*b+1]; v > hi {
						hi = v
					}
				}
				mins[c][b] = lo
				maxs[c][b] = hi
			}
		}
		ref.mins = append(ref.mins, mins)
		ref.maxs = append(ref.maxs, maxs)
	}
	return ref
}

func (inst refPyramid) peak() (peak int8) {
	top := len(inst.mins) - 1
	for c := range inst.mins[top] {
		for b := range inst.mins[top][c] {
			for _, v := range []int8{inst.mins[top][c][b], inst.maxs[top][c][b]} {
				if v < 0 {
					v = -v
				}
				if v > peak {
					peak = v
				}
			}
		}
	}
	return peak
}

// dumpLevel reads a whole level of one channel out of a complete pyramid.
func dumpLevel(t require.TestingT, p *peaks.Pyramid, level int32, ch int) (mins []int8, maxs []int8) {
	n := p.Bins(level)
	mins = make([]int8, n)
	maxs = make([]int8, n)
	got := p.Query(level, 0, ch, mins, maxs)
	require.Equal(t, int(n), got)
	return mins, maxs
}

func requireMatchesRef(t require.TestingT, p *peaks.Pyramid, ref refPyramid) {
	require.Equal(t, len(ref.mins), int(p.Levels()), "level count")
	for level := range int32(p.Levels()) {
		require.Equal(t, len(ref.mins[level][0]), int(p.Bins(level)), "bin count at level %d", level)
		for c := range int(p.Format().Channels) {
			mins, maxs := dumpLevel(t, p, level, c)
			require.Equal(t, ref.mins[level][c], mins, "minima at level %d channel %d", level, c)
			require.Equal(t, ref.maxs[level][c], maxs, "maxima at level %d channel %d", level, c)
		}
	}
	require.Equal(t, ref.peak(), p.GlobalPeak(), "global peak")
}

// genSamples produces a deterministic signal from a seed. The samples are
// generated rather than drawn one by one so a 20000-frame case costs a
// single rapid draw; frames and the shape parameters still shrink.
func genSamples(seed uint64, n int) (samples []float32) {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	samples = make([]float32, n)
	for i := range samples {
		switch r.IntN(48) {
		case 0:
			samples[i] = float32(math.NaN())
		case 1:
			samples[i] = float32(math.Inf(1))
		case 2:
			samples[i] = float32(math.Inf(-1))
		case 3:
			samples[i] = 1
		case 4:
			samples[i] = -1
		default:
			// Past full scale on purpose, so clamping is exercised.
			samples[i] = (r.Float32()*2 - 1) * 1.2
		}
	}
	return samples
}

func drawCase(t *rapid.T) (format pcm.Format, samples []float32, baseBin int32) {
	format = pcm.Format{
		SampleRate: 48000,
		Channels:   rapid.Uint16Range(1, 3).Draw(t, "channels"),
	}
	baseBin = rapid.SampledFrom([]int32{16, 32, 64, 256}).Draw(t, "baseBin")
	frames := rapid.IntRange(0, 20000).Draw(t, "frames")
	samples = genSamples(rapid.Uint64().Draw(t, "seed"), frames*int(format.Channels))
	return format, samples, baseBin
}

func foldWhole(t require.TestingT, format pcm.Format, samples []float32, baseBin int32) (p *peaks.Pyramid) {
	p, err := peaks.NewPyramidE(format, int64(len(samples)/int(format.Channels)), baseBin)
	require.NoError(t, err)
	require.NoError(t, p.FoldE(samples))
	p.Finish()
	require.True(t, p.IsComplete())
	return p
}

func TestPyramidShape(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	p, err := peaks.NewPyramidE(format, 1000, 256)
	require.NoError(t, err)
	require.Equal(t, int64(1000), p.Frames())
	require.Equal(t, int32(256), p.BaseBin())
	// 1000 frames is 4 base bins, then 2, then 1.
	require.Equal(t, int32(3), p.Levels())
	require.Equal(t, []int64{4, 2, 1}, []int64{p.Bins(0), p.Bins(1), p.Bins(2)})
	require.Equal(t, []int64{256, 512, 1024}, []int64{p.FramesPerBin(0), p.FramesPerBin(1), p.FramesPerBin(2)})
	require.Equal(t, int64((4+2+1)*2*2), p.MemoryBytes())
	require.Equal(t, int64(0), p.Built())
	require.False(t, p.IsComplete())

	// An empty signal still has a level 0, with no bins.
	empty, err := peaks.NewPyramidE(format, 0, peaks.DefaultBaseBin())
	require.NoError(t, err)
	require.Equal(t, int32(1), empty.Levels())
	require.Equal(t, int64(0), empty.Bins(0))
	require.Equal(t, int64(0), empty.MemoryBytes())
	empty.Finish()
	require.True(t, empty.IsComplete())
	require.Equal(t, int8(0), empty.GlobalPeak())
}

func TestNewPyramidRejects(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	for _, baseBin := range []int32{0, 1, 8, 15, 100, -256, 1 << 25} {
		_, err := peaks.NewPyramidE(format, 1000, baseBin)
		require.Error(t, err, "baseBin %d", baseBin)
	}
	_, err := peaks.NewPyramidE(pcm.Format{}, 1000, 256)
	require.Error(t, err)
	_, err = peaks.NewPyramidE(format, -1, 256)
	require.Error(t, err)
	// The size cap keeps a wild frame count from being an allocation.
	_, err = peaks.NewPyramidE(format, math.MaxInt32*1024, 16)
	require.Error(t, err)
}

// TestLevel0Containment checks ADR-0208 §SD2's containment guarantee at
// level 0: for every sample s of every frame of a bin,
//
//	binMin <= floor(q(s)) <= ceil(q(s)) <= binMax
//
// with q(s) = clamp(s, -1, 1)*127. The stronger claim — that the bin is the
// tightest such pair — is asserted too, since the reference computes it.
func TestLevel0Containment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format, samples, baseBin := drawCase(t)
		channels := int(format.Channels)
		p := foldWhole(t, format, samples, baseBin)
		frames := len(samples) / channels
		for c := range channels {
			mins, maxs := dumpLevel(t, p, 0, c)
			for f := range frames {
				b := f / int(baseBin)
				lo, hi := refQuant(samples[f*channels+c])
				require.LessOrEqual(t, mins[b], lo, "frame %d channel %d", f, c)
				require.LessOrEqual(t, hi, maxs[b], "frame %d channel %d", f, c)
			}
		}
		requireMatchesRef(t, p, buildRefPyramid(format, samples, baseBin))
	})
}

// TestParentsCombineChildren checks that every level-(k+1) bin is the
// min/max of its two children — its single child at a truncated tail.
func TestParentsCombineChildren(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format, samples, baseBin := drawCase(t)
		p := foldWhole(t, format, samples, baseBin)
		for level := int32(0); level+1 < p.Levels(); level++ {
			for c := range int(format.Channels) {
				childMin, childMax := dumpLevel(t, p, level, c)
				parentMin, parentMax := dumpLevel(t, p, level+1, c)
				require.Equal(t, (len(childMin)+1)/2, len(parentMin))
				for b := range parentMin {
					lo := childMin[2*b]
					hi := childMax[2*b]
					if 2*b+1 < len(childMin) {
						lo = min(lo, childMin[2*b+1])
						hi = max(hi, childMax[2*b+1])
					}
					require.Equal(t, lo, parentMin[b], "level %d bin %d channel %d", level+1, b, c)
					require.Equal(t, hi, parentMax[b], "level %d bin %d channel %d", level+1, b, c)
				}
			}
		}
	})
}

// TestChunkInvariance folds the same signal in random chunk sizes and in
// one call; the pyramids must be identical, global peak included.
func TestChunkInvariance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format, samples, baseBin := drawCase(t)
		channels := int(format.Channels)
		frames := int64(len(samples) / channels)
		chunked, err := peaks.NewPyramidE(format, frames, baseBin)
		require.NoError(t, err)
		off := 0
		for off < len(samples) {
			chunkFrames := rapid.IntRange(0, 700).Draw(t, "chunkFrames")
			end := min(off+chunkFrames*channels, len(samples))
			require.NoError(t, chunked.FoldE(samples[off:end]))
			require.Equal(t, int64(end/channels), chunked.Built())
			off = end
		}
		chunked.Finish()
		requireMatchesRef(t, chunked, buildRefPyramid(format, samples, baseBin))
	})
}

// TestBuildPathsAgree pins the three ways to build one pyramid to the same
// result: BuildE, NewPyramidE+FillFromE, and folding by hand.
func TestBuildPathsAgree(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format, samples, baseBin := drawCase(t)
		channels := int(format.Channels)
		frames := int64(len(samples) / channels)
		src, err := pcm.NewMemSourceE(format, samples)
		require.NoError(t, err)
		chunkFrames := rapid.IntRange(1, 900).Draw(t, "chunkFrames")

		var lastProgress int64
		built, err := peaks.BuildE(context.Background(), src, baseBin, chunkFrames, func(builtFrames int64) {
			require.GreaterOrEqual(t, builtFrames, lastProgress)
			lastProgress = builtFrames
		})
		require.NoError(t, err)
		require.Equal(t, frames, lastProgress)
		require.Equal(t, frames, built.Built())
		require.True(t, built.IsComplete())

		filled, err := peaks.NewPyramidE(format, frames, baseBin)
		require.NoError(t, err)
		require.NoError(t, filled.FillFromE(context.Background(), src, chunkFrames, nil))

		ref := buildRefPyramid(format, samples, baseBin)
		requireMatchesRef(t, built, ref)
		requireMatchesRef(t, filled, ref)
		requireMatchesRef(t, foldWhole(t, format, samples, baseBin), ref)
	})
}

func TestFoldRejects(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	p, err := peaks.NewPyramidE(format, 100, 16)
	require.NoError(t, err)
	require.Error(t, p.FoldE(make([]float32, 3)), "ragged frame")
	require.Error(t, p.FoldE(make([]float32, 202)), "past the declared end")
	require.NoError(t, p.FoldE(make([]float32, 200)))
	require.Error(t, p.FoldE(make([]float32, 2)), "past the declared end")
	p.Finish()
	require.Error(t, p.FoldE(make([]float32, 0)), "after Finish")
	p.Finish()
	require.True(t, p.IsComplete())
}

func TestFillFromRejectsMismatchedSource(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	src, err := pcm.NewMemSourceE(format, make([]float32, 200))
	require.NoError(t, err)
	ctx := context.Background()

	other, err := peaks.NewPyramidE(pcm.Format{SampleRate: 44100, Channels: 2}, 100, 16)
	require.NoError(t, err)
	require.Error(t, other.FillFromE(ctx, src, 0, nil))

	shorter, err := peaks.NewPyramidE(format, 50, 16)
	require.NoError(t, err)
	require.Error(t, shorter.FillFromE(ctx, src, 0, nil))

	p, err := peaks.NewPyramidE(format, 100, 16)
	require.NoError(t, err)
	require.NoError(t, p.FillFromE(ctx, src, 0, nil))
	require.Error(t, p.FillFromE(ctx, src, 0, nil), "already complete")

	_, err = peaks.BuildE(ctx, nil, 16, 0, nil)
	require.Error(t, err)
}

func TestBuildCancellation(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 1}
	samples := genSamples(7, 40000)
	src, err := pcm.NewMemSourceE(format, samples)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	p, err := peaks.NewPyramidE(format, int64(len(samples)), 16)
	require.NoError(t, err)
	err = p.FillFromE(ctx, src, 64, func(builtFrames int64) {
		if builtFrames >= 640 {
			cancel()
		}
	})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, p.IsComplete())
	require.GreaterOrEqual(t, p.Built(), int64(640))
	require.Less(t, p.Built(), int64(len(samples)))

	// The partial pyramid is resumable, and BuildE — which owns its
	// pyramid — reports the cancellation with nothing to show.
	require.NoError(t, p.FillFromE(context.Background(), src, 64, nil))
	require.True(t, p.IsComplete())
	requireMatchesRef(t, p, buildRefPyramid(format, samples, 16))

	cancelled, cancel2 := context.WithCancel(context.Background())
	cancel2()
	got, err := peaks.BuildE(cancelled, src, 16, 64, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

func TestQuantiseSpecials(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	samples := []float32{
		float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)),
		2, -2, 1, -1, 0, 0.5, -0.5,
	}
	p := foldWhole(t, format, samples, 16)
	mins, maxs := dumpLevel(t, p, 0, 0)
	require.Equal(t, []int8{-127}, mins)
	require.Equal(t, []int8{127}, maxs)
	require.Equal(t, int8(127), p.GlobalPeak())

	// 0.5 quantises to 63.5, so the bin must reach 63 downward and 64
	// upward: outward rounding, not nearest.
	half := foldWhole(t, format, []float32{0.5}, 16)
	mins, maxs = dumpLevel(t, half, 0, 0)
	require.Equal(t, []int8{63}, mins)
	require.Equal(t, []int8{64}, maxs)
}

func TestPickLevel(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 1}
	p, err := peaks.NewPyramidE(format, 1<<20, 256)
	require.NoError(t, err)
	require.Equal(t, int32(0), p.PickLevel(0))
	require.Equal(t, int32(0), p.PickLevel(255.9))
	require.Equal(t, int32(0), p.PickLevel(256))
	require.Equal(t, int32(0), p.PickLevel(511))
	require.Equal(t, int32(1), p.PickLevel(512))
	require.Equal(t, int32(2), p.PickLevel(1024))
	require.Equal(t, int32(2), p.PickLevel(2047.5))
	require.Equal(t, p.Levels()-1, p.PickLevel(1e18))
	require.Equal(t, int32(0), p.PickLevel(math.NaN()))
	for level := range p.Levels() {
		require.Equal(t, level, p.PickLevel(float64(p.FramesPerBin(level))))
	}
}

// TestColumnsAreExactWhenBinAligned pins the tight case: when the view
// range starts on a bin boundary of the picked level and spans a whole
// number of bins per column, a column is exactly the min/max of the samples
// under it — no tolerance.
func TestColumnsAreExactWhenBinAligned(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format := pcm.Format{SampleRate: 48000, Channels: rapid.Uint16Range(1, 2).Draw(t, "channels")}
		channels := int(format.Channels)
		baseBin := rapid.SampledFrom([]int32{16, 64}).Draw(t, "baseBin")
		frames := rapid.IntRange(1, 8000).Draw(t, "frames")
		samples := genSamples(rapid.Uint64().Draw(t, "seed"), frames*channels)
		p := foldWhole(t, format, samples, baseBin)

		level := rapid.Int32Range(0, p.Levels()-1).Draw(t, "level")
		fpb := p.FramesPerBin(level)
		// A power of two, so that the frames per column are exactly the bin
		// size of some level: whichever level Columns then picks, its bins
		// tile the columns instead of straddling them.
		binsPerColumn := rapid.SampledFrom([]int64{1, 2, 4}).Draw(t, "binsPerColumn")
		maxColumns := int(p.Bins(level) / binsPerColumn)
		if maxColumns < 1 {
			return
		}
		columns := rapid.IntRange(1, maxColumns).Draw(t, "columns")
		firstBin := int64(rapid.IntRange(0, maxColumns-columns).Draw(t, "firstColumn")) * binsPerColumn
		fromFrame := firstBin * fpb
		toFrame := fromFrame + int64(columns)*binsPerColumn*fpb

		dstMin := make([]int8, columns)
		dstMax := make([]int8, columns)
		ch := rapid.IntRange(0, channels-1).Draw(t, "channel")
		got := p.Columns(fromFrame, toFrame, ch, dstMin, dstMax)
		require.Equal(t, columns, got)
		for c := range columns {
			colFrom := fromFrame + int64(c)*binsPerColumn*fpb
			colTo := min(colFrom+binsPerColumn*fpb, int64(frames))
			lo := int8(127)
			hi := int8(-127)
			for f := colFrom; f < colTo; f++ {
				l, h := refQuant(samples[f*int64(channels)+int64(ch)])
				lo = min(lo, l)
				hi = max(hi, h)
			}
			require.Equal(t, lo, dstMin[c], "column %d", c)
			require.Equal(t, hi, dstMax[c], "column %d", c)
		}
	})
}

// TestColumnsContainAnyRange is the loose case: for an arbitrary view range
// and column count a column may inherit the min/max of a bin that straddles
// its edge — up to one bin of the picked level wider on each side — so what
// is asserted is containment, the guarantee a drawn column has to keep.
func TestColumnsContainAnyRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format, samples, baseBin := drawCase(t)
		channels := int(format.Channels)
		frames := int64(len(samples) / channels)
		if frames < 2 {
			return
		}
		p := foldWhole(t, format, samples, baseBin)
		fromFrame := rapid.Int64Range(0, frames-1).Draw(t, "fromFrame")
		toFrame := rapid.Int64Range(fromFrame+1, frames+frames/4+1).Draw(t, "toFrame")
		columns := rapid.IntRange(1, 200).Draw(t, "columns")
		ch := rapid.IntRange(0, channels-1).Draw(t, "channel")

		dstMin := make([]int8, columns)
		dstMax := make([]int8, columns)
		got := p.Columns(fromFrame, toFrame, ch, dstMin, dstMax)
		require.LessOrEqual(t, got, columns)
		span := toFrame - fromFrame
		for c := range got {
			// The column mapping is part of the contract: equal-width
			// columns by integer division over the view range.
			colFrom := fromFrame + span*int64(c)/int64(columns)
			colTo := min(fromFrame+span*int64(c+1)/int64(columns), frames)
			for f := colFrom; f < colTo; f++ {
				lo, hi := refQuant(samples[f*int64(channels)+int64(ch)])
				require.LessOrEqual(t, dstMin[c], lo, "column %d frame %d", c, f)
				require.LessOrEqual(t, hi, dstMax[c], "column %d frame %d", c, f)
			}
		}
	})
}

func TestColumnsAndQueryEdgeCases(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	samples := genSamples(11, 2*1000)
	p := foldWhole(t, format, samples, 16)
	dstMin := make([]int8, 8)
	dstMax := make([]int8, 8)

	require.Equal(t, 0, p.Columns(0, 0, 0, dstMin, dstMax), "empty range")
	require.Equal(t, 0, p.Columns(10, 5, 0, dstMin, dstMax), "reversed range")
	require.Equal(t, 0, p.Columns(-5, 100, 0, dstMin, dstMax), "negative start")
	require.Equal(t, 0, p.Columns(0, 100, 2, dstMin, dstMax), "channel out of range")
	require.Equal(t, 0, p.Columns(0, 100, 0, nil, nil), "no columns")
	require.Equal(t, 0, p.Columns(1000, 2000, 0, dstMin, dstMax), "range past the signal")
	// 900..1200 over 8 columns is 37.5 frames per column; columns 3..7
	// start at or past frame 1000 and are not built.
	require.Equal(t, 3, p.Columns(900, 1200, 0, dstMin, dstMax), "range overlapping the end")
	// More columns than frames: every column still resolves to a bin.
	require.Equal(t, 8, p.Columns(0, 4, 0, dstMin, dstMax))

	require.Equal(t, 0, p.Query(-1, 0, 0, dstMin, dstMax))
	require.Equal(t, 0, p.Query(p.Levels(), 0, 0, dstMin, dstMax))
	require.Equal(t, 0, p.Query(0, -1, 0, dstMin, dstMax))
	require.Equal(t, 0, p.Query(0, 0, 2, dstMin, dstMax))
	require.Equal(t, 0, p.Query(0, p.Bins(0), 0, dstMin, dstMax))
	require.Equal(t, 0, p.Query(0, 0, 0, nil, nil))
	require.Equal(t, 5, p.Query(0, p.Bins(0)-5, 0, dstMin, dstMax), "short read at the end of a level")
	// Out-of-range levels clamp on the shape accessors.
	require.Equal(t, p.FramesPerBin(0), p.FramesPerBin(-1))
	require.Equal(t, p.Bins(p.Levels()-1), p.Bins(p.Levels()+10))
}

// TestFinishEarlyPublishesWhatWasFolded covers the case where a caller
// stops a build short: the pyramid is complete over the folded prefix, and
// the bins beyond it stay unreadable rather than reading as silence.
func TestFinishEarlyPublishesWhatWasFolded(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 1}
	samples := genSamples(3, 1000)
	p, err := peaks.NewPyramidE(format, 1000, 16)
	require.NoError(t, err)
	require.NoError(t, p.FoldE(samples[:100]))
	p.Finish()
	require.True(t, p.IsComplete())
	require.Equal(t, int64(1000), p.Frames())
	require.Equal(t, int64(100), p.Built())

	dstMin := make([]int8, p.Bins(0))
	dstMax := make([]int8, p.Bins(0))
	require.Equal(t, 7, p.Query(0, 0, 0, dstMin, dstMax), "ceil(100/16) bins folded")

	ref := buildRefPyramid(format, samples[:100], 16)
	for level := range int32(len(ref.mins)) {
		mins := make([]int8, len(ref.mins[level][0]))
		maxs := make([]int8, len(mins))
		require.Equal(t, len(mins), p.Query(level, 0, 0, mins, maxs))
		require.Equal(t, ref.mins[level][0], mins)
		require.Equal(t, ref.maxs[level][0], maxs)
	}
}
