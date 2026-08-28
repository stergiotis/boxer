package peaks

import (
	"sync/atomic"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

const (
	// MinBaseBin is the smallest base bin a pyramid accepts. Below this the
	// level-0 array stops being a summary.
	MinBaseBin int32 = 16
	// MaxBaseBin keeps the frames-per-bin arithmetic far inside int64 for
	// any frame count.
	MaxBaseBin int32 = 1 << 24
	// QuantFullScale is what a sample of magnitude 1 quantises to.
	QuantFullScale int8 = 127
	// MaxPyramidBytes caps the level arrays of one pyramid. It is also what
	// stops a corrupt cache header from asking for a petabyte.
	MaxPyramidBytes int64 = 1 << 32
)

// Pyramid is a multi-resolution min/max summary of one audio signal
// (ADR-0208 §SD2). Build it with [NewPyramidE], [BuildE] or [ReadFromE];
// the zero value carries no arrays and answers nothing useful.
//
// One goroutine builds while any number read; see the package
// documentation for the contract.
type Pyramid struct {
	format pcm.Format
	frames int64

	// backing holds every level array of every channel in serialisation
	// order, so the cache body is this slice verbatim.
	backing []int8
	// mins and maxs index backing by level*channels+channel.
	mins [][]int8
	maxs [][]int8
	// binCounts and framesPerBin are per level; framesPerBin is
	// baseBin<<level, precomputed so no call site shifts.
	binCounts    []int64
	framesPerBin []int64

	// Builder-owned state. Only the building goroutine touches these.
	accMin     []int8
	accMax     []int8
	readBuf    []float32
	storedBins []int64
	folded     int64
	peak       int8
	binFill    int32

	baseBin int32
	levels  int32

	// Published state, read by any goroutine.
	built      atomic.Int64
	globalPeak atomic.Int32
	complete   atomic.Bool
}

// DefaultBaseBin is the base bin a caller with no opinion uses: 256 frames,
// about 5 ms at 48 kHz.
func DefaultBaseBin() (baseBin int32) {
	return 256
}

// NewPyramidE preallocates every level of a pyramid over frames frames of
// format. Nothing is folded yet, so [Pyramid.Built] is 0. baseBin must be a
// power of two in [MinBaseBin, MaxBaseBin].
func NewPyramidE(format pcm.Format, frames int64, baseBin int32) (inst *Pyramid, err error) {
	err = format.ValidateE()
	if err != nil {
		return nil, err
	}
	if frames < 0 {
		return nil, eb.Build().Int64("frames", frames).Errorf("negative frame count")
	}
	if baseBin < MinBaseBin || baseBin > MaxBaseBin || baseBin&(baseBin-1) != 0 {
		return nil, eb.Build().
			Int32("baseBin", baseBin).
			Int32("min", MinBaseBin).
			Int32("max", MaxBaseBin).
			Errorf("base bin is not a power of two in range")
	}

	channels := int(format.Channels)
	levels := levelsFor(frames, baseBin)
	binCounts := make([]int64, levels)
	framesPerBin := make([]int64, levels)
	totalBins := int64(0)
	for l := range int(levels) {
		fpb := int64(baseBin) << uint(l)
		framesPerBin[l] = fpb
		binCounts[l] = ceilDiv(frames, fpb)
		totalBins += binCounts[l]
	}
	bytes := totalBins * 2 * int64(channels)
	if bytes > MaxPyramidBytes {
		return nil, eb.Build().
			Int64("bytes", bytes).
			Int64("max", MaxPyramidBytes).
			Int64("frames", frames).
			Uint16("channels", format.Channels).
			Errorf("pyramid would exceed the size cap")
	}

	inst = &Pyramid{
		format:       format,
		frames:       frames,
		backing:      make([]int8, bytes),
		mins:         make([][]int8, int(levels)*channels),
		maxs:         make([][]int8, int(levels)*channels),
		binCounts:    binCounts,
		framesPerBin: framesPerBin,
		accMin:       make([]int8, channels),
		accMax:       make([]int8, channels),
		storedBins:   make([]int64, levels),
		baseBin:      baseBin,
		levels:       levels,
	}
	off := int64(0)
	for l := range int(levels) {
		n := binCounts[l]
		for c := range channels {
			inst.mins[l*channels+c] = inst.backing[off : off+n : off+n]
			off += n
			inst.maxs[l*channels+c] = inst.backing[off : off+n : off+n]
			off += n
		}
	}
	inst.resetAcc()
	return inst, nil
}

// levelsFor counts the levels needed until the top one holds at most one
// bin. An empty signal still has a level 0, with no bins.
func levelsFor(frames int64, baseBin int32) (levels int32) {
	bins := ceilDiv(frames, int64(baseBin))
	levels = 1
	for bins > 1 {
		bins = (bins + 1) / 2
		levels++
	}
	return levels
}

func ceilDiv(a int64, b int64) (q int64) {
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

func (inst *Pyramid) resetAcc() {
	for c := range inst.accMin {
		inst.accMin[c] = QuantFullScale
		inst.accMax[c] = -QuantFullScale
	}
}

// Format returns the format of the summarised signal.
func (inst *Pyramid) Format() (format pcm.Format) {
	return inst.format
}

// Frames returns the frame count the pyramid was sized for.
func (inst *Pyramid) Frames() (frames int64) {
	return inst.frames
}

// BaseBin returns the frames per level-0 bin.
func (inst *Pyramid) BaseBin() (baseBin int32) {
	return inst.baseBin
}

// Levels returns the number of levels; the top level holds at most one bin.
func (inst *Pyramid) Levels() (levels int32) {
	return inst.levels
}

// FramesPerBin returns baseBin<<level. Levels outside [0, Levels()) are
// clamped into range.
func (inst *Pyramid) FramesPerBin(level int32) (frames int64) {
	return inst.framesPerBin[inst.clampLevel(level)]
}

// Bins returns how many bins the level was allocated, which is
// ceil(Frames()/FramesPerBin(level)) — not how many are readable, for which
// see [Pyramid.Query]. Levels outside [0, Levels()) are clamped into range.
func (inst *Pyramid) Bins(level int32) (bins int64) {
	return inst.binCounts[inst.clampLevel(level)]
}

// MemoryBytes returns the size of the level arrays, which is what a pyramid
// costs beyond a few hundred bytes of bookkeeping.
func (inst *Pyramid) MemoryBytes() (bytes int64) {
	return int64(len(inst.backing))
}

func (inst *Pyramid) clampLevel(level int32) (idx int) {
	if level < 0 {
		return 0
	}
	if level >= inst.levels {
		return int(inst.levels) - 1
	}
	return int(level)
}

// Built returns how many frames have been folded in and published. Safe
// from any goroutine.
func (inst *Pyramid) Built() (frames int64) {
	return inst.built.Load()
}

// IsComplete reports whether [Pyramid.Finish] has run, after which every
// bin is readable and no further folding is accepted.
func (inst *Pyramid) IsComplete() (complete bool) {
	return inst.complete.Load()
}

// GlobalPeak returns max(|min|, |max|) over the bins built so far — the
// gain input for normalisation, which ADR-0208 §SD4 applies only once
// [Pyramid.IsComplete] holds. It grows monotonically during a build.
func (inst *Pyramid) GlobalPeak() (peak int8) {
	return int8(inst.globalPeak.Load())
}
