package peaks

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// defaultChunkFrames is the read granularity a caller with no opinion gets:
// 16384 frames is 128 KiB of stereo float32, small enough to stay in cache
// and large enough that the per-read overhead of a decoder disappears.
const defaultChunkFrames = 1 << 14

// quantise maps one sample to the outward-rounded pair the containment
// guarantee needs: lo is floor(q(s)), hi is ceil(q(s)) with
// q(s) = clamp(s, -1, 1) * 127. NaN reads as zero. The pair is int32 so the
// accumulators of a bin stay in registers; both values are inside
// [-127, 127].
func quantise(sample float32) (lo int32, hi int32) {
	if sample != sample {
		return 0, 0
	}
	if sample > 1 {
		sample = 1
	} else if sample < -1 {
		sample = -1
	}
	// float64 keeps the product exact — a float32 mantissa plus the seven
	// bits of 127 is 31 bits — so the floor below is the true one and the
	// containment guarantee is not a near miss.
	v := float64(sample) * float64(QuantFullScale)
	f := math.Floor(v)
	lo = int32(f)
	hi = lo
	if v != f {
		hi = lo + 1
	}
	// Both are inside [-127, 127] because v is clamped, so -128 is never
	// produced.
	return lo, hi
}

// FoldE folds interleaved frames in at the current build position. Chunks
// may be any length; a bin that a chunk leaves incomplete is buffered and
// continued by the next call. Exactly one goroutine may call this, and it
// must not call it after [Pyramid.Finish].
func (inst *Pyramid) FoldE(samples []float32) (err error) {
	if inst.complete.Load() {
		return eb.Build().Int64("built", inst.built.Load()).Errorf("pyramid is already complete")
	}
	channels := int(inst.format.Channels)
	if len(samples)%channels != 0 {
		return eb.Build().
			Int("samples", len(samples)).
			Uint16("channels", inst.format.Channels).
			Errorf("sample count is not a whole number of frames")
	}
	n := int64(len(samples) / channels)
	if inst.folded+n > inst.frames {
		return eb.Build().
			Int64("folded", inst.folded).
			Int64("frames", inst.frames).
			Int64("chunk", n).
			Errorf("fold would run past the declared frame count")
	}
	if n == 0 {
		return nil
	}

	// One block per bin, one pass per channel inside it, so a bin's
	// accumulators live in registers instead of being reloaded per sample.
	accMin := inst.accMin
	accMax := inst.accMax
	base := int(inst.baseBin)
	rest := samples
	for len(rest) > 0 {
		blockFrames := base - int(inst.binFill)
		if avail := len(rest) / channels; blockFrames > avail {
			blockFrames = avail
		}
		block := rest[:blockFrames*channels]
		for c := range channels {
			lo := int32(accMin[c])
			hi := int32(accMax[c])
			for i := c; i < len(block); i += channels {
				l, h := quantise(block[i])
				if l < lo {
					lo = l
				}
				if h > hi {
					hi = h
				}
			}
			accMin[c] = int8(lo)
			accMax[c] = int8(hi)
		}
		inst.binFill += int32(blockFrames)
		if int(inst.binFill) == base {
			inst.flushBin()
		}
		rest = rest[len(block):]
	}

	inst.folded += n
	inst.globalPeak.Store(int32(inst.peak))
	// The single store that publishes everything written above.
	inst.built.Store(inst.folded)
	return nil
}

// flushBin stores the accumulated level-0 bin and folds it upward.
func (inst *Pyramid) flushBin() {
	channels := int(inst.format.Channels)
	idx := inst.storedBins[0]
	for c := range channels {
		lo := inst.accMin[c]
		hi := inst.accMax[c]
		inst.mins[c][idx] = lo
		inst.maxs[c][idx] = hi
		if p := absPeak(lo); p > inst.peak {
			inst.peak = p
		}
		if p := absPeak(hi); p > inst.peak {
			inst.peak = p
		}
	}
	inst.resetAcc()
	inst.storedBins[0] = idx + 1
	inst.binFill = 0
	inst.cascade(0, idx)
}

// absPeak is |v| without the -128 overflow; the quantiser never emits -128
// but a cache file could.
func absPeak(v int8) (p int8) {
	if v >= 0 {
		return v
	}
	if v == -128 {
		return QuantFullScale
	}
	return -v
}

// foldOneUp merges bin idx of level into its parent at level+1, seeding the
// parent from an even child and combining an odd one.
func (inst *Pyramid) foldOneUp(level int32, idx int64) {
	if level+1 >= inst.levels {
		return
	}
	channels := int(inst.format.Channels)
	child := int(level) * channels
	parent := int(level+1) * channels
	pidx := idx / 2
	seed := idx%2 == 0
	for c := range channels {
		lo := inst.mins[child+c][idx]
		hi := inst.maxs[child+c][idx]
		pmin := inst.mins[parent+c]
		pmax := inst.maxs[parent+c]
		if seed {
			pmin[pidx] = lo
			pmax[pidx] = hi
			continue
		}
		if lo < pmin[pidx] {
			pmin[pidx] = lo
		}
		if hi > pmax[pidx] {
			pmax[pidx] = hi
		}
	}
}

// cascade propagates a freshly stored bin upward. It stops at the first
// parent that is still missing its odd child, which is exactly the parent a
// reader is not yet allowed to see; the dangling parents left at the end of
// the signal are closed by finishCascade.
func (inst *Pyramid) cascade(level int32, idx int64) {
	for level+1 < inst.levels {
		inst.foldOneUp(level, idx)
		if idx%2 == 0 {
			return
		}
		idx /= 2
		level++
	}
}

// finishCascade closes the truncated bins at the top of the signal. Every
// bin but the last of a level was combined from both its children during
// the build; the last one may still be waiting for a child that will never
// come, so it is recomputed from the children that exist — bottom-up, so
// each level is final before the one above reads it.
//
// It rewrites only bins a reader cannot yet see: a last bin whose frames
// lie inside the published prefix already has both children and is correct,
// and rewriting it would be a write racing a legitimate read.
func (inst *Pyramid) finishCascade() {
	channels := int(inst.format.Channels)
	for level := int32(0); level+1 < inst.levels; level++ {
		n := inst.storedBins[level]
		parents := (n + 1) / 2
		inst.storedBins[level+1] = parents
		if parents == 0 {
			continue
		}
		pidx := parents - 1
		if (pidx+1)*inst.framesPerBin[level+1] <= inst.folded {
			continue
		}
		childRow := int(level) * channels
		parentRow := int(level+1) * channels
		for c := range channels {
			childMin := inst.mins[childRow+c]
			childMax := inst.maxs[childRow+c]
			i := 2 * pidx
			lo := childMin[i]
			hi := childMax[i]
			if i+1 < n {
				if v := childMin[i+1]; v < lo {
					lo = v
				}
				if v := childMax[i+1]; v > hi {
					hi = v
				}
			}
			inst.mins[parentRow+c][pidx] = lo
			inst.maxs[parentRow+c][pidx] = hi
		}
	}
}

// Finish flushes the trailing partial bin and marks the pyramid complete,
// after which every bin is readable and folding is refused. It is
// idempotent. Finishing before the declared frame count has been folded
// publishes what was folded — the pyramid is then complete over a shorter
// signal, and [Pyramid.WriteToE] refuses it.
func (inst *Pyramid) Finish() {
	if inst.complete.Load() {
		return
	}
	if inst.binFill > 0 {
		inst.flushBin()
	}
	inst.finishCascade()
	inst.globalPeak.Store(int32(inst.peak))
	inst.built.Store(inst.folded)
	inst.complete.Store(true)
}

// BuildE builds a complete pyramid over src in one sequential pass
// (ADR-0208 §SD4). chunkFrames of zero or less picks a default; progress may
// be nil and is called with the published frame count after every chunk.
//
// The context is honoured between chunks and inside the source's reads. On
// cancellation BuildE returns (nil, ctx.Err()) and the half-built pyramid is
// dropped — a caller that wants to render a partial build owns the pyramid
// itself and uses [Pyramid.FillFromE].
func BuildE(ctx context.Context, src pcm.SourceI, baseBin int32, chunkFrames int, progress func(builtFrames int64)) (inst *Pyramid, err error) {
	if src == nil {
		return nil, eb.Build().Errorf("nil source")
	}
	inst, err = NewPyramidE(src.Format(), src.Frames(), baseBin)
	if err != nil {
		return nil, err
	}
	err = inst.FillFromE(ctx, src, chunkFrames, progress)
	if err != nil {
		return nil, err
	}
	return inst, nil
}

// FillFromE folds src into the pyramid from its current build position to
// the declared end and then calls [Pyramid.Finish]. It is the progressive
// form of [BuildE]: the caller holds the pyramid and may read the built
// prefix from another goroutine while this runs.
//
// It returns ctx.Err() when cancelled, leaving the pyramid unfinished and
// resumable by another call.
func (inst *Pyramid) FillFromE(ctx context.Context, src pcm.SourceI, chunkFrames int, progress func(builtFrames int64)) (err error) {
	if src == nil {
		return eb.Build().Errorf("nil source")
	}
	if inst.complete.Load() {
		return eb.Build().Int64("built", inst.built.Load()).Errorf("pyramid is already complete")
	}
	format := src.Format()
	if format != inst.format {
		return eb.Build().
			Uint32("srcSampleRate", format.SampleRate).
			Uint16("srcChannels", format.Channels).
			Uint32("sampleRate", inst.format.SampleRate).
			Uint16("channels", inst.format.Channels).
			Errorf("source format differs from the pyramid's")
	}
	if src.Frames() != inst.frames {
		return eb.Build().
			Int64("srcFrames", src.Frames()).
			Int64("frames", inst.frames).
			Errorf("source frame count differs from the pyramid's")
	}
	if chunkFrames <= 0 {
		chunkFrames = defaultChunkFrames
	}
	channels := int(inst.format.Channels)
	need := chunkFrames * channels
	if cap(inst.readBuf) < need {
		inst.readBuf = make([]float32, need)
	}
	buf := inst.readBuf[:need]

	for inst.folded < inst.frames {
		err = ctx.Err()
		if err != nil {
			return err
		}
		want := int64(chunkFrames)
		if remaining := inst.frames - inst.folded; want > remaining {
			want = remaining
		}
		var n int
		n, err = src.ReadFramesAtE(ctx, inst.folded, buf[:want*int64(channels)])
		if err != nil && !errors.Is(err, io.EOF) {
			return eh.Errorf("unable to read frames for the peaks build: %w", err)
		}
		if n <= 0 {
			return eb.Build().
				Int64("folded", inst.folded).
				Int64("frames", inst.frames).
				Errorf("source ended before the declared frame count")
		}
		err = inst.FoldE(buf[:n*channels])
		if err != nil {
			return err
		}
		if progress != nil {
			progress(inst.built.Load())
		}
	}
	inst.Finish()
	if progress != nil {
		progress(inst.built.Load())
	}
	return nil
}
