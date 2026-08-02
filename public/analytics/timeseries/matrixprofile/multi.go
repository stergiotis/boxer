package matrixprofile

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// MaxChannels is the largest number of channels [NewMultiSeriesE] accepts.
//
// The ceiling comes from [MultiProfile.Dims], which records the selected
// channel subset as a bitmask. It sits well past the point where the method
// stops being a good idea: the published evidence has the subdimensional
// matrix profile's advantage over its competitors *inverting* somewhere between
// a handful of channels and a few dozen, so a series wide enough to hit this
// limit wants a different algorithm rather than a wider mask.
const MaxChannels = 64

// DefaultMDLBits is the discretization width [MultiSeries.SelectDimsMDLE] uses
// when none is given: each z-normalized subsequence is quantized to 2⁸ levels
// before its description length is measured.
//
// The value is not critical, but it is not free either. It sets the cost of a
// channel that does *not* join the subset, and so the bar a channel must clear
// to join one — see [MultiSeries.SelectDimsMDLE].
const DefaultMDLBits = 8

// MultiSeries is a multivariate series: d channels of equal length, sharing one
// subsequence length, each carrying its own per-window statistics.
//
// Channels are compared only ever within themselves — every distance is
// z-normalized per channel — so channels in unlike units need no rescaling and
// a channel's contribution does not depend on its amplitude.
//
// A MultiSeries is immutable once built and safe for concurrent readers.
type MultiSeries struct {
	channels []*Series
	window   int32
}

// NewMultiSeriesE precomputes the per-window statistics of every channel under
// a shared subsequence length. stdDevFloorRel may be 0 to accept
// [DefaultStdDevFloorRel].
//
// All channels must have the same length; a ragged input is an error rather
// than something to truncate, because silently profiling a prefix of one
// channel against the whole of another would misalign every reported index.
//
// The returned MultiSeries aliases nothing.
func NewMultiSeriesE(channels [][]float64, window int32, stdDevFloorRel float64) (inst *MultiSeries, err error) {
	d := int32(len(channels))
	if d < 1 {
		err = eb.Build().Errorf("need at least one channel")
		return
	}
	if d > MaxChannels {
		err = eb.Build().Int32("channels", d).Int32("max", MaxChannels).Errorf("too many channels")
		return
	}
	n := len(channels[0])
	for i, ch := range channels {
		if len(ch) != n {
			err = eb.Build().Int32("channel", int32(i)).Int32("len", int32(len(ch))).Int32("want", int32(n)).Errorf("channels have unequal lengths")
			return
		}
	}

	series := make([]*Series, d)
	for i, ch := range channels {
		var s *Series
		s, err = NewSeriesE(ch, window, stdDevFloorRel)
		if err != nil {
			err = eb.Build().Int32("channel", int32(i)).Errorf("unable to prepare channel: %w", err)
			return
		}
		series[i] = s
	}

	inst = &MultiSeries{
		channels: series,
		window:   window,
	}
	return
}

// NumChannels returns d, the number of channels.
func (inst *MultiSeries) NumChannels() (numChannels int32) {
	numChannels = int32(len(inst.channels))
	return
}

// Window returns the shared subsequence length.
func (inst *MultiSeries) Window() (window int32) {
	window = inst.window
	return
}

// NumWindows returns the number of subsequences, len(values)-Window+1.
func (inst *MultiSeries) NumWindows() (numWindows int32) {
	numWindows = inst.channels[0].NumWindows()
	return
}

// ExclusionZone returns the half-width of the trivial-match exclusion window,
// which is shared by every channel.
func (inst *MultiSeries) ExclusionZone() (zone int32) {
	zone = inst.channels[0].ExclusionZone()
	return
}

// Channel returns the univariate view of one channel, for callers that want a
// per-channel profile alongside the subdimensional one.
func (inst *MultiSeries) Channel(idx int32) (series *Series) {
	series = inst.channels[idx]
	return
}

// MultiProfile is the k-dimensional matrix profile of a multivariate series,
// for every k from 1 to d simultaneously.
//
// A motif in a wide series usually shows up in a *subset* of its channels, and
// which subset is not known in advance. So for each subsequence and each k, this
// records the nearest neighbour under the best-matching k channels: the
// distance, that neighbour's index, and which k channels carried it.
//
// # Indexing
//
// The three slices are indexed [k-1][i]: k is 1-based because a zero-channel
// motif is not a thing, i is the subsequence's start position. [MultiProfile.K]
// exists to keep that off-by-one out of caller code.
//
// Index holds -1, Distance +Inf and Dims 0 where every candidate fell inside the
// exclusion zone.
//
// # The k-dimensional distance
//
// The distance at k is the *mean* of the k smallest per-channel distances, not
// their sum, so values are comparable across k — which is what makes the elbow
// in k readable, and what [MultiSeries.SelectDimsMDLE] exists to locate
// automatically.
//
// # What is not true of it
//
// **The motif at k is not necessarily contained in the motif at k+1.** Within
// one candidate pair the channel subsets are nested by construction, but
// different k may pick different pairs, and the paper's Fig. 5 is a worked
// example of exactly that. So a caller must not treat Dims[k] as a refinement
// of Dims[k-1]. This is also why no pruning across k is possible, and why
// [MultiSeries.Compute] pays for every k whether or not the caller reads them.
type MultiProfile struct {
	// Distance[k-1][i] is the mean distance from subsequence i to its nearest
	// neighbour over the best-matching k channels.
	Distance [][]float64
	// Index[k-1][i] is that neighbour's start position, or -1.
	Index [][]int32
	// Dims[k-1][i] is the bitmask of the k channels that carried it. Bit c is
	// set when channel c is in the subset.
	Dims [][]uint64

	Window      int32
	NumChannels int32
}

// K returns the three per-position slices for a 1-based k, so callers index by
// position only. ok is false when k is outside [1, NumChannels].
func (prof *MultiProfile) K(k int32) (dist []float64, idx []int32, dims []uint64, ok bool) {
	if k < 1 || k > prof.NumChannels {
		return
	}
	dist = prof.Distance[k-1]
	idx = prof.Index[k-1]
	dims = prof.Dims[k-1]
	ok = true
	return
}

// Compute returns the k-dimensional matrix profile for every k from 1 to d.
//
// This is mSTAMP (Yeh et al., Matrix Profile VI, 2017) over this package's
// STOMP recurrence. Complexity is O(d·n²·log d) time and O(d·n) space: the d
// channels' dot-product rows advance in lockstep, and at each candidate pair
// the d per-channel distances are sorted so that the k smallest — and hence the
// distance for every k at once — fall out of one cumulative pass.
//
// No per-row distance matrix is materialized; the sort runs in a d-wide scratch
// buffer, which is why the space term carries no n·d row.
//
// # Ties
//
// The sort is stable in channel order, so where two channels are equidistant
// the lower-numbered one enters the subset first. That makes results
// reproducible but arbitrary: a caller reading Dims on a series with duplicated
// channels is reading a tie-break, not a finding.
//
// # Accuracy
//
// As in [Series.Compute], neighbour *selection* runs on the 2m(1−ρ) identity
// while reported distances are recomputed from materialized z-normalized
// values. Here that refinement re-averages the selected channels only. The
// channel subset itself is chosen on the identity's values and is not revisited
// afterwards, so on a pair whose channels sit within the identity's resolution
// of each other the subset is as arbitrary as the ties above.
//
// One consequence worth naming: the profile is non-decreasing in k by
// construction, since each step averages in a worse channel — but only up to
// that identity resolution, because different k may select different
// neighbours and each is refined independently.
func (inst *MultiSeries) Compute() (prof *MultiProfile) {
	d := inst.NumChannels()
	numWindows := inst.NumWindows()
	zone := inst.ExclusionZone()

	prof = &MultiProfile{
		Distance:    make([][]float64, d),
		Index:       make([][]int32, d),
		Dims:        make([][]uint64, d),
		Window:      inst.window,
		NumChannels: d,
	}
	for k := range d {
		dist := make([]float64, numWindows)
		idx := make([]int32, numWindows)
		for i := range dist {
			dist[i] = math.Inf(1)
			idx[i] = -1
		}
		prof.Distance[k] = dist
		prof.Index[k] = idx
		prof.Dims[k] = make([]uint64, numWindows)
	}

	scanners := make([]*rowScanner, d)
	for c := range d {
		scanners[c] = inst.channels[c].newRowScanner()
	}
	// Scratch for the per-candidate sort, allocated once per call so that
	// concurrent Compute calls on one MultiSeries stay independent.
	sortDist := make([]float64, d)
	sortChan := make([]int32, d)

	inst.reduceRow(prof, 0, scanners, zone, sortDist, sortChan)
	for i := int32(1); i < numWindows; i++ {
		for _, sc := range scanners {
			sc.advance(i)
		}
		inst.reduceRow(prof, i, scanners, zone, sortDist, sortChan)
	}

	inst.refine(prof)
	return
}

// reduceRow folds one row of every channel's dot products into the running
// profiles, updating only endpoint i. Compute evaluates every row, so each
// ordered pair is still visited exactly once.
//
// sortDist and sortChan are caller-owned scratch of length d.
func (inst *MultiSeries) reduceRow(prof *MultiProfile, i int32, scanners []*rowScanner, zone int32, sortDist []float64, sortChan []int32) {
	d := inst.NumChannels()
	numWindows := inst.NumWindows()
	lo := i - zone
	hi := i + zone

	for j := range numWindows {
		if j >= lo && j <= hi {
			continue
		}

		// Insertion sort of d values, ascending. d is small — this is the
		// column-wise sort of the mSTAMP algorithm, and at these sizes an
		// insertion sort beats anything with a comparator call in it. The
		// strict > keeps equal distances in channel order.
		for c := range d {
			dc := inst.channels[c].distanceFromDot(scanners[c].qt[j], i, j)
			k := c
			for k > 0 && sortDist[k-1] > dc {
				sortDist[k] = sortDist[k-1]
				sortChan[k] = sortChan[k-1]
				k--
			}
			sortDist[k] = dc
			sortChan[k] = c
		}

		// One cumulative pass yields the k-dimensional distance for every k:
		// the mean of the k smallest per-channel distances.
		var cum float64
		var dims uint64
		for k := range d {
			cum += sortDist[k]
			dims |= uint64(1) << uint(sortChan[k])
			dk := cum / float64(k+1)
			if dk < prof.Distance[k][i] {
				prof.Distance[k][i] = dk
				prof.Index[k][i] = j
				prof.Dims[k][i] = dims
			}
		}
	}
}

// refine recomputes every reported distance from materialized z-normalized
// values, for the reason [Series.Compute] documents: the identity the search
// runs on ranks candidates well but cannot report the distance to one.
//
// Only the selected channels are recomputed and re-averaged; the selection
// itself stands. Cost is O(d²·n·Window) across all k, against the O(d·n²·log d)
// search.
func (inst *MultiSeries) refine(prof *MultiProfile) {
	d := inst.NumChannels()
	numWindows := inst.NumWindows()
	for k := range d {
		for i := range numWindows {
			j := prof.Index[k][i]
			if j < 0 {
				continue
			}
			dims := prof.Dims[k][i]
			var sum float64
			for c := range d {
				if dims&(uint64(1)<<uint(c)) == 0 {
					continue
				}
				sum += inst.channels[c].exactDistance(i, j)
			}
			prof.Distance[k][i] = sum / float64(k+1)
		}
	}
}

// Motif returns the most similar non-trivial pair of subsequences under the
// best-matching k channels, and which channels those were.
//
// found is false when k is outside [1, NumChannels] or no position had an
// admissible neighbour.
func (prof *MultiProfile) Motif(k int32) (first int32, second int32, dims uint64, dist float64, found bool) {
	first = -1
	second = -1
	dist = math.Inf(1)
	distances, indices, masks, ok := prof.K(k)
	if !ok {
		return
	}
	for i, d := range distances {
		if indices[i] < 0 || d >= dist {
			continue
		}
		dist = d
		first = int32(i)
		second = indices[i]
		dims = masks[i]
		found = true
	}
	return
}

// Discord returns the subsequence whose nearest neighbour under the
// best-matching k channels is furthest away — the subdimensional anomaly.
//
// # Why the k *smallest* distances find an anomaly
//
// It reads backwards — an anomaly confined to a few channels does not show up
// in the channels that match best — and the intuition is wrong. The neighbour
// is chosen *jointly*: one position j has to serve all k channels at once. An
// anomaly does not have to make the selected channels look bad, it only has to
// remove them from the pool, and the remaining channels then have to find a
// single j that suits all of them. Measured on composed multivariate fixtures,
// this peaks at k equal to the number of affected channels, and an aggregation
// over the k *largest* distances — which is what the backwards intuition
// suggests — scores three to seven times worse at every k.
//
// # k matters more than anything else here
//
// Accuracy varies by a factor of ten across k on the same series, and both ends
// are bad: k = 1 finds some channel matching somewhere and is near chance,
// k = d dilutes the affected channels among all the rest. Sweep it.
//
// This is the batch, bidirectional form, so a subsequence may be explained by
// one that arrives after it; there is no multivariate counterpart to
// [github.com/stergiotis/boxer/public/analytics/timeseries/damp] yet.
//
// found is false when k is outside [1, NumChannels] or no position had an
// admissible neighbour.
func (prof *MultiProfile) Discord(k int32) (idx int32, dims uint64, dist float64, found bool) {
	idx = -1
	dist = math.Inf(-1)
	distances, indices, masks, ok := prof.K(k)
	if !ok {
		return
	}
	for i, d := range distances {
		if indices[i] < 0 || math.IsInf(d, 1) || d <= dist {
			continue
		}
		dist = d
		idx = int32(i)
		dims = masks[i]
		found = true
	}
	return
}

// PositionScores expands the k-dimensional profile into a per-position anomaly
// score vector of length n, attributing each window's score to the window's
// centre. See [Profile.PositionScores] for why the centre and not the start.
//
// out is empty when k is outside [1, NumChannels].
func (prof *MultiProfile) PositionScores(k int32, n int32, dst []float64) (out []float64) {
	distances, indices, _, ok := prof.K(k)
	if !ok {
		out = scoreBuffer(0, dst)
		return
	}
	out = scoreBuffer(n, dst)
	accumulateScores(out, distances, indices, prof.Window)
	return
}

// DimChannels expands a [MultiProfile.Dims] bitmask into ascending channel
// indices, so callers need no bit arithmetic.
//
// dst is filled and returned when it has room; otherwise a fresh slice is
// allocated.
func DimChannels(dims uint64, dst []int32) (out []int32) {
	out = dst[:0]
	for c := range int32(MaxChannels) {
		if dims&(uint64(1)<<uint(c)) != 0 {
			out = append(out, c)
		}
	}
	return
}

// SelectDimsMDLE chooses k — the natural number of channels a motif spans —
// by the Minimum Description Length principle, and returns the channel subset
// at that k.
//
// bits is the discretization width; pass 0 for [DefaultMDLBits].
//
// # How it decides
//
// The k-dimensional profile's value rises with k, so its elbow marks the point
// where a motif stops being explained by more channels and starts being diluted
// by them. MDL turns finding that elbow into a compression question: encode the
// motif pair at each k, and take the k that encodes smallest.
//
// The encoding is the paper's difference scheme. Both subsequences of the pair
// are quantized to 2^bits levels per channel. A channel *inside* the subset
// stores one side raw and the other as a difference from it, at whatever
// integer width the difference's range needs — narrow, when the two windows
// genuinely match. A channel *outside* the subset stores both sides raw.
//
// So a channel earns its place in the subset exactly when difference-coding it
// is cheaper than storing it twice, and bitSize[k-1] is the total over all d
// channels. The returned k is the argmin.
//
// # How to read a flat curve
//
// An unrelated channel is **break-even, not expensive**: its difference spans
// the whole quantization grid, so coding it costs one raw side plus one full
// width — exactly what storing both sides raw costs. That is the correct MDL
// answer, and it means the curve is flat rather than rising once past the
// natural k, with the flat region's ties resolved to the *smallest* k. A curve
// that is flat from k = 1 says the pair has no dimensionality worth reporting,
// not that k = 1 is a finding.
//
// # Two documented deviations
//
// The difference width is taken **per channel** rather than pooled across the
// selected ones. Pooling would charge a well-matched channel for a badly
// matched channel's outliers, which is both a worse code and non-local: it
// would make one channel's cost depend on which others were selected.
//
// The paper's own worked example is followed for the arithmetic — a pair of
// 10-sample series at 4 bits costs 80 bits stored directly and 50 stored as
// reference-plus-difference. The widely used reference implementation omits the
// cost of the reference side, which makes a matched channel cost only its
// difference; that biases the argmin toward larger k, and does not reproduce
// the paper's 50.
//
// # Scope
//
// This is defined on the *motif* pair — the most similar pair at each k — which
// is what the paper defines it on. It says nothing about a discord, whose
// dimensionality has no compression argument behind it.
func (inst *MultiSeries) SelectDimsMDLE(prof *MultiProfile, bits int32) (k int32, dims uint64, bitSize []float64, err error) {
	if prof == nil {
		err = eb.Build().Errorf("nil profile")
		return
	}
	d := inst.NumChannels()
	if prof.NumChannels != d || prof.Window != inst.window {
		err = eb.Build().Int32("profileChannels", prof.NumChannels).Int32("channels", d).Errorf("profile does not belong to this series")
		return
	}
	if bits <= 0 {
		bits = DefaultMDLBits
	}
	if bits > 16 {
		err = eb.Build().Int32("bits", bits).Errorf("discretization width must be at most 16")
		return
	}

	m := inst.window
	levels := int32(1) << uint(bits)
	rawPerChannel := float64(m) * float64(bits)

	a := make([]float64, m)
	b := make([]float64, m)

	bitSize = make([]float64, d)
	k = -1
	best := math.Inf(1)
	for kk := int32(1); kk <= d; kk++ {
		first, second, mask, _, found := prof.Motif(kk)
		if !found {
			bitSize[kk-1] = math.Inf(1)
			continue
		}
		total := 0.0
		for c := range d {
			if mask&(uint64(1)<<uint(c)) == 0 {
				// Not in the subset: both sides stored raw.
				total += 2.0 * rawPerChannel
				continue
			}
			ch := inst.channels[c]
			ch.znormInto(first, a)
			ch.znormInto(second, b)
			// One side raw, the other as a difference at its own width.
			total += rawPerChannel + float64(m)*float64(diffCodeBits(a, b, levels))
		}
		bitSize[kk-1] = total
		if total < best {
			best = total
			k = kk
			dims = mask
		}
	}
	if k < 0 {
		err = eb.Build().Errorf("no admissible motif at any k")
		return
	}
	return
}

// znormInto materializes the z-normalized window at idx into dst, which must
// have room for Window values. A constant window normalizes to the zero vector.
//
// Statistics come from the original values rather than the cached centered
// ones, for the conditioning reason [Series] documents.
func (inst *Series) znormInto(idx int32, dst []float64) {
	if inst.invStd[idx] == 0.0 {
		clear(dst[:inst.window])
		return
	}
	mean, std := inst.windowStats(inst.values, idx)
	invStd := 1.0 / std
	seg := inst.values[idx : idx+inst.window]
	for k, v := range seg {
		dst[k] = (v - mean) * invStd
	}
}

// diffCodeBits quantizes a and b onto a shared 0..levels-1 grid and returns the
// fixed integer width their difference needs — the paper's "we can use 10
// 1-bit integers to store Δ".
//
// The grid is shared between the two windows on purpose: quantizing each on its
// own range would encode a constant offset between them as zero difference,
// which is exactly the information the difference is supposed to carry.
//
// The width comes from the difference's *range*, not from a count of the
// distinct values it takes. The two readings agree on the paper's example — a Δ
// of 0s and 1s is one bit either way — but the count degenerates: at most m
// distinct values can occur among m samples, so on any window shorter than
// 2^bits a count-based width is below the raw width by construction, every
// channel compresses, and the argmin is always d. The range does not depend on
// m and stays break-even for an unrelated channel, which is the answer MDL
// should give.
func diffCodeBits(a []float64, b []float64, levels int32) (bits int32) {
	lo := a[0]
	hi := a[0]
	for _, v := range a {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	for _, v := range b {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}

	span := hi - lo
	if span <= 0.0 {
		// Both windows constant and equal: the difference never varies.
		return
	}
	scale := float64(levels-1) / span

	minDiff := int32(math.MaxInt32)
	maxDiff := int32(math.MinInt32)
	for k := range a {
		qa := int32(math.Round((a[k] - lo) * scale))
		qb := int32(math.Round((b[k] - lo) * scale))
		diff := qa - qb
		if diff < minDiff {
			minDiff = diff
		}
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	bits = ceilLog2(maxDiff - minDiff + 1)
	return
}

// ceilLog2 returns the number of bits needed to name n distinct symbols, which
// is 0 for a single one — a difference that never varies costs nothing to
// store beyond knowing it does not vary.
func ceilLog2(n int32) (bits int32) {
	if n <= 1 {
		return
	}
	n--
	for n > 0 {
		bits++
		n >>= 1
	}
	return
}
