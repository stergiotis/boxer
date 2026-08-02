package matrixprofile

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultExclusionDivisor sets the default exclusion zone to Window/4, the
// convention the matrix profile literature uses. A subsequence's nearest
// neighbour is otherwise itself shifted by a sample or two, which makes both
// motifs and discords meaningless.
const DefaultExclusionDivisor = 4

// DefaultStdDevFloorRel is the default constant-window threshold, expressed as
// a fraction of the whole series' standard deviation. A window whose own
// standard deviation falls at or below that fraction counts as constant.
//
// The threshold is *relative* rather than absolute on purpose. Z-normalized
// distance is invariant to scaling the series, so its constant-window rule must
// be too; an absolute floor silently reclassifies windows when the same signal
// arrives in millivolts instead of volts, and the profile changes with the
// unit. The value sits far below any real variation and far above the ~1e-16
// relative residue that exact arithmetic would have left at zero.
const DefaultStdDevFloorRel = 1e-12

// Series holds a time series together with the per-window statistics that
// z-normalized subsequence comparison needs.
//
// The values are kept twice, in both conditionings, because the two paths
// through this package want opposite things and neither is free:
//
//   - Centering on the global mean is what keeps the dot-product search away
//     from catastrophic cancellation on a series with a large constant offset,
//     which real recorded signals frequently carry (epoch timestamps, absolute
//     counters, Kelvin temperatures).
//   - Centering *hurts* whenever a window's internal variation is tiny relative
//     to the whole series' range — a window spanning 1e-6 inside a series
//     spanning 30 lands near a value whose ULP swamps the variation being
//     measured. The original values are correctly conditioned for exactly that
//     case.
//
// So the search runs on centered values and the per-window statistics and the
// refinement pass run on the originals. Centering changes no z-normalized
// distance either way — the measure is offset-invariant — only the floating
// point available to express it.
//
// A Series is immutable once built and safe for concurrent readers.
type Series struct {
	// values is the caller's series, unmodified.
	values []float64
	// centered is values minus the global mean, used only for dot products.
	centered []float64
	// mean is the per-window mean of centered, which is what the 2m(1−ρ)
	// identity needs alongside a dot product over centered.
	mean []float64
	// invStd is 1/σ per window, or 0 where σ fell to or below the std-dev floor.
	// Storing the reciprocal keeps a division out of the inner loop and encodes
	// the constant-window case in the same slot. σ is computed from values, not
	// centered, for the conditioning reason above.
	invStd []float64

	window         int32
	stdDevFloorRel float64
	// stdDevFloor is the absolute threshold derived from stdDevFloorRel and the
	// series' own standard deviation.
	stdDevFloor float64
}

// NewSeriesE precomputes the per-window statistics for values under the given
// subsequence length. stdDevFloorRel may be 0 to accept
// [DefaultStdDevFloorRel].
//
// The returned Series aliases nothing: values is copied, because centering
// mutates it.
func NewSeriesE(values []float64, window int32, stdDevFloorRel float64) (inst *Series, err error) {
	n := int32(len(values))
	if window < 2 {
		err = eb.Build().Int32("window", window).Errorf("window must be at least 2")
		return
	}
	if n < window {
		err = eb.Build().Int32("window", window).Int32("len", n).Errorf("series shorter than window")
		return
	}
	if stdDevFloorRel <= 0.0 {
		stdDevFloorRel = DefaultStdDevFloorRel
	}

	original := make([]float64, n)
	copy(original, values)
	var globalSum float64
	for _, v := range original {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			err = eb.Build().Float64("value", v).Errorf("series contains a non-finite value")
			return
		}
		globalSum += v
	}
	globalMean := globalSum / float64(n)

	centered := make([]float64, n)
	var globalSs float64
	for i, v := range original {
		centered[i] = v - globalMean
		globalSs += centered[i] * centered[i]
	}
	seriesStd := math.Sqrt(globalSs / float64(n))

	numWindows := int(n - window + 1)
	inst = &Series{
		values:         original,
		centered:       centered,
		mean:           make([]float64, numWindows),
		invStd:         make([]float64, numWindows),
		window:         window,
		stdDevFloorRel: stdDevFloorRel,
		stdDevFloor:    stdDevFloorRel * seriesStd,
	}
	inst.computeWindowStats()
	return
}

// windowStats returns the mean and standard deviation of the window at idx over
// src, two-pass and therefore accurate near zero variance.
//
// The O(1) prefix-sum alternative — variance as E[x²]−E[x]² — is not usable
// here. Near zero variance it cancels catastrophically, leaving an exactly
// constant window with an apparent standard deviation many orders of magnitude
// above its true value, which then reads as real structure and produces a
// garbage correlation.
func (inst *Series) windowStats(src []float64, idx int32) (mean float64, std float64) {
	seg := src[idx : idx+inst.window]
	invM := 1.0 / float64(inst.window)

	var sum float64
	for _, v := range seg {
		sum += v
	}
	mean = sum * invM

	var ss float64
	for _, v := range seg {
		d := v - mean
		ss += d * d
	}
	std = math.Sqrt(ss * invM)
	return
}

// computeWindowStats fills mean and invStd at O(n·Window), the same order as
// the single direct distance profile [Series.Compute] already pays and far
// below its O(n²) body.
//
// mean comes from the centered values because that is what pairs with a dot
// product over centered values in the identity; σ comes from the originals
// because it is identical either way in exact arithmetic and better conditioned
// there.
func (inst *Series) computeWindowStats() {
	for i := range inst.mean {
		centeredMean, _ := inst.windowStats(inst.centered, int32(i))
		_, std := inst.windowStats(inst.values, int32(i))

		inst.mean[i] = centeredMean
		if std > inst.stdDevFloor {
			inst.invStd[i] = 1.0 / std
		} else {
			inst.invStd[i] = 0.0
		}
	}
}

// StdDevFloor returns the absolute standard-deviation threshold below which a
// window counts as constant, derived from the relative floor and the series'
// own standard deviation.
func (inst *Series) StdDevFloor() (floor float64) {
	floor = inst.stdDevFloor
	return
}

// Window returns the subsequence length.
func (inst *Series) Window() (window int32) {
	window = inst.window
	return
}

// NumWindows returns the number of subsequences, len(values)-Window+1.
func (inst *Series) NumWindows() (numWindows int32) {
	numWindows = int32(len(inst.mean))
	return
}

// IsConstantAt reports whether the subsequence at idx has a standard deviation
// at or below [Series.StdDevFloor], and so normalizes to the zero vector.
func (inst *Series) IsConstantAt(idx int32) (constant bool) {
	constant = inst.invStd[idx] == 0.0
	return
}

// ExclusionZone returns the half-width of the trivial-match exclusion window.
func (inst *Series) ExclusionZone() (zone int32) {
	zone = (inst.window + DefaultExclusionDivisor - 1) / DefaultExclusionDivisor
	return
}

// distanceFromDot converts a raw dot product between the subsequences at i and
// j into their z-normalized Euclidean distance.
//
// The identity is d² = 2m(1−ρ) with ρ the Pearson correlation, so
// d = sqrt(2m(1−ρ)) where ρ = (QT − m·μᵢ·μⱼ)/(m·σᵢ·σⱼ). A z-normalized
// subsequence has norm sqrt(m); a constant one normalizes to the zero vector,
// which fixes the two degenerate cases: both constant gives 0, exactly one
// constant gives sqrt(m).
func (inst *Series) distanceFromDot(dot float64, i int32, j int32) (dist float64) {
	m := float64(inst.window)
	invStdI := inst.invStd[i]
	invStdJ := inst.invStd[j]
	if invStdI == 0.0 || invStdJ == 0.0 {
		if invStdI == 0.0 && invStdJ == 0.0 {
			dist = 0.0
			return
		}
		dist = math.Sqrt(m)
		return
	}
	corr := (dot - m*inst.mean[i]*inst.mean[j]) * invStdI * invStdJ / m
	if corr > 1.0 {
		corr = 1.0
	} else if corr < -1.0 {
		corr = -1.0
	}
	dist = math.Sqrt(2.0 * m * (1.0 - corr))
	return
}

// exactDistance computes the distance between the subsequences at i and j from
// their materialized z-normalized values, in O(Window).
//
// This avoids the 2m(1−ρ) identity entirely, and with it the cancellation that
// makes the identity inaccurate near ρ = 1. It is too expensive for the search
// itself — that is what the identity buys — but affordable once per reported
// result.
// It recomputes both windows' statistics from the original values rather than
// reading the cached ones, so it inherits none of the search path's
// conditioning.
func (inst *Series) exactDistance(i int32, j int32) (dist float64) {
	if inst.invStd[i] == 0.0 || inst.invStd[j] == 0.0 {
		if inst.invStd[i] == 0.0 && inst.invStd[j] == 0.0 {
			dist = 0.0
			return
		}
		dist = math.Sqrt(float64(inst.window))
		return
	}
	meanI, stdI := inst.windowStats(inst.values, i)
	meanJ, stdJ := inst.windowStats(inst.values, j)
	invStdI := 1.0 / stdI
	invStdJ := 1.0 / stdJ

	a := inst.values[i : i+inst.window]
	b := inst.values[j : j+inst.window]
	var ss float64
	for k := range a {
		d := (a[k]-meanI)*invStdI - (b[k]-meanJ)*invStdJ
		ss += d * d
	}
	dist = math.Sqrt(ss)
	return
}

// dotAt computes the raw dot product between the subsequences at i and j
// directly, in O(Window). It reads the centered values: a dot product over a
// series with a large constant offset is where cancellation does the most
// damage to the search.
func (inst *Series) dotAt(i int32, j int32) (dot float64) {
	a := inst.centered[i : i+inst.window]
	b := inst.centered[j : j+inst.window]
	for k := range a {
		dot += a[k] * b[k]
	}
	return
}

// DistanceProfile computes the z-normalized Euclidean distance from the
// subsequence at queryIdx to every subsequence of the series — the operation
// the literature calls MASS.
//
// This is the direct O(n·Window) form. It is deliberately not FFT-accelerated:
// [Series.Compute] needs exactly one distance profile regardless of series
// length (every subsequent row follows from the O(1) STOMP recurrence), so the
// transform would buy nothing on the path that dominates. See ADR-0150.
//
// dst is filled and returned when it has room for NumWindows values; otherwise
// a fresh slice is allocated. Excluded neighbours are not masked — the caller
// decides, because a discord search and a motif search mask differently.
func (inst *Series) DistanceProfile(queryIdx int32, dst []float64) (out []float64) {
	numWindows := inst.NumWindows()
	if cap(dst) >= int(numWindows) {
		out = dst[:numWindows]
	} else {
		out = make([]float64, numWindows)
	}
	for j := range numWindows {
		out[j] = inst.distanceFromDot(inst.dotAt(queryIdx, j), queryIdx, j)
	}
	return
}

// Profile is the matrix profile of a series: for every subsequence, the
// distance to its nearest non-trivial neighbour and that neighbour's index.
//
// Held struct-of-arrays because both readers are whole-vector scans — the
// minima are motifs, the maxima are discords.
//
// Index holds -1 where every candidate fell inside the exclusion zone, which
// happens only when the series is barely longer than the window. Distance is
// +Inf at those positions.
type Profile struct {
	Distance []float64
	Index    []int32
	Window   int32
}

// Compute returns the matrix profile of the series using STOMP.
//
// Complexity is O(n²) time and O(n) space, independent of Window. The first
// distance profile costs O(n·Window); every later row follows from the
// dot-product recurrence
//
//	QT[i][j] = QT[i-1][j-1] − t[i-1]·t[j-1] + t[i+m-1]·t[j+m-1]
//
// at O(1) per cell.
//
// # Accuracy
//
// The reported distances are computed from materialized z-normalized values,
// not from the identity the search runs on, so they do not inherit its
// behaviour near ρ = 1. That matters: through the identity alone an exactly
// matching pair reports something around 1e-6 rather than 0, and worse when the
// two subsequences differ by many orders of magnitude in scale, because
// d = sqrt(2m(1−ρ)) turns a correlation error δ into sqrt(2mδ) of distance. The
// refinement pass costs O(n·Window) against the O(n²) search and removes the
// effect from everything a caller sees.
//
// What remains is that neighbour *selection* still runs on the identity, over
// globally centered values, and the recurrence accumulates drift across rows.
// Two consequences, in increasing order of how much they should worry a caller:
//
// Where several candidates sit within the identity's resolution of each other,
// which one is reported is arbitrary. They are equally near, so this costs
// nothing.
//
// Where a series mixes local scales across many orders of magnitude — a long
// stretch near 1.0 followed by a tail near 1e-8 — the centered representation
// cannot resolve the small-scale region's shape, and the search can return a
// genuinely worse neighbour than exists. The reported distance is still the
// true distance to whatever was returned, so the result understates similarity
// rather than inventing it, but on such a series the profile is not the true
// minimum. Rescaling the offending region, or profiling it separately, is the
// available remedy; there is no conditioning of a single shared value array
// that serves both regions, which is inherent to STOMP rather than to this
// implementation.
//
// [Series.DistanceProfile] is the raw search primitive and does not refine; its
// values carry the identity's behaviour described above.
func (inst *Series) Compute() (prof *Profile) {
	numWindows := inst.NumWindows()
	zone := inst.ExclusionZone()

	prof = &Profile{
		Distance: make([]float64, numWindows),
		Index:    make([]int32, numWindows),
		Window:   inst.window,
	}
	for i := range prof.Distance {
		prof.Distance[i] = math.Inf(1)
		prof.Index[i] = -1
	}

	sc := inst.newRowScanner()
	inst.reduceRow(prof, 0, sc.qt, zone)
	for i := int32(1); i < numWindows; i++ {
		sc.advance(i)
		inst.reduceRow(prof, i, sc.qt, zone)
	}

	// The search above ranks candidates through the 2m(1−ρ) identity, which is
	// accurate enough to pick the right neighbour but not to report the distance
	// to it: near ρ = 1 the identity cancels, and an exactly-matching pair comes
	// out at 1e-6 or worse rather than 0. Recompute each surviving pair from its
	// z-normalized values, at O(n·Window) against the O(n²) body above.
	for i := range prof.Index {
		if prof.Index[i] < 0 {
			continue
		}
		prof.Distance[i] = inst.exactDistance(int32(i), prof.Index[i])
	}
	return
}

// rowScanner carries the STOMP dot-product state for one series: the current
// row of the QT matrix, and the first column every later row needs.
//
// It exists so that the univariate search and the subdimensional one in
// multi.go drive the same recurrence rather than two transcriptions of it. The
// multivariate path needs d rows advanced in lockstep and read together, which
// is the one thing [Series.Compute]'s original inlined loop could not give.
type rowScanner struct {
	series *Series
	// qt holds row i of the dot-product matrix once advance has been called for
	// i. Callers read it and must not write it.
	qt []float64
	// firstCol is column 0 of the matrix, which the recurrence cannot produce
	// because it has no j-1 to read.
	firstCol []float64
}

// newRowScanner computes row 0 directly, at O(n·Window), and returns a scanner
// positioned there.
//
// By symmetry QT[i][0] == QT[0][i], so this one pass also supplies the first
// column.
func (inst *Series) newRowScanner() (sc *rowScanner) {
	numWindows := inst.NumWindows()
	firstRow := make([]float64, numWindows)
	for j := range numWindows {
		firstRow[j] = inst.dotAt(0, j)
	}
	firstCol := make([]float64, numWindows)
	copy(firstCol, firstRow)

	sc = &rowScanner{
		series:   inst,
		qt:       firstRow,
		firstCol: firstCol,
	}
	return
}

// advance moves the scanner from row i-1 to row i in O(n), via the recurrence
//
//	QT[i][j] = QT[i-1][j-1] − t[i-1]·t[j-1] + t[i+m-1]·t[j+m-1]
//
// Rows must be visited in order, starting at 1.
func (sc *rowScanner) advance(i int32) {
	inst := sc.series
	numWindows := inst.NumWindows()
	m := inst.window
	// The recurrence must run over the same values the initial dot products did.
	values := inst.centered

	// Descending j so the in-place update reads QT[i-1][j-1] before overwriting
	// it.
	dropI := values[i-1]
	addI := values[i+m-1]
	for j := numWindows - 1; j >= 1; j-- {
		sc.qt[j] = sc.qt[j-1] - dropI*values[j-1] + addI*values[j+m-1]
	}
	sc.qt[0] = sc.firstCol[i]
}

// reduceRow folds one row of dot products into the running profile, updating
// only endpoint i. Compute evaluates every row, so each ordered pair is still
// visited exactly once and the full matrix is covered.
func (inst *Series) reduceRow(prof *Profile, i int32, qt []float64, zone int32) {
	numWindows := inst.NumWindows()
	lo := i - zone
	hi := i + zone
	for j := range numWindows {
		if j >= lo && j <= hi {
			continue
		}
		d := inst.distanceFromDot(qt[j], i, j)
		if d < prof.Distance[i] {
			prof.Distance[i] = d
			prof.Index[i] = j
		}
	}
}

// Motif returns the index pair with the smallest distance in the profile — the
// most similar non-trivial pair of subsequences in the series.
//
// found is false when no position had an admissible neighbour.
func (prof *Profile) Motif() (first int32, second int32, dist float64, found bool) {
	first = -1
	second = -1
	dist = math.Inf(1)
	for i, d := range prof.Distance {
		if prof.Index[i] < 0 || d >= dist {
			continue
		}
		dist = d
		first = int32(i)
		second = prof.Index[i]
		found = true
	}
	return
}

// Discord returns the index whose nearest neighbour is furthest away — the
// standard non-parametric definition of an anomalous subsequence.
//
// This is the batch, bidirectional form: a subsequence may be explained by one
// that arrives after it. Online scoring needs the causal left-discord variant,
// which lands with DAMP at M3 of ADR-0150.
//
// found is false when no position had an admissible neighbour.
func (prof *Profile) Discord() (idx int32, dist float64, found bool) {
	idx = -1
	dist = math.Inf(-1)
	for i, d := range prof.Distance {
		if prof.Index[i] < 0 || math.IsInf(d, 1) || d <= dist {
			continue
		}
		dist = d
		idx = int32(i)
		found = true
	}
	return
}

// PositionScores expands a profile into a per-position anomaly score vector of
// length n — the shape
// [github.com/stergiotis/boxer/public/analytics/timeseries/adscore] expects,
// where n is the length of the series the profile was computed from.
//
// **Each window's score is attributed to the window's centre**, i + Window/2,
// not to its start. A profile is indexed by window start, and handing those
// indices to a per-position scorer displaces every peak by half a window.
// Measured on this repository's own fixtures that costs more than half the
// achievable accuracy, which is why this conversion is provided rather than
// left to callers. The same convention is documented at
// [github.com/stergiotis/boxer/public/analytics/timeseries/damp.Reading].
//
// Positions no window covers, and positions whose window had no admissible
// neighbour, keep zero. Where windows overlap the larger score wins.
//
// dst is filled and returned when it has room for n values; otherwise a fresh
// slice is allocated.
func (prof *Profile) PositionScores(n int32, dst []float64) (out []float64) {
	out = scoreBuffer(n, dst)
	accumulateScores(out, prof.Distance, prof.Index, prof.Window)
	return
}

// scoreBuffer returns a zeroed slice of length n, reusing dst when it fits.
func scoreBuffer(n int32, dst []float64) (out []float64) {
	if n < 0 {
		n = 0
	}
	if cap(dst) >= int(n) {
		out = dst[:n]
		clear(out)
	} else {
		out = make([]float64, n)
	}
	return
}

// accumulateScores writes each window's distance to its centre position,
// keeping the larger where windows overlap. Positions without an admissible
// neighbour carry +Inf in dist and are skipped rather than saturating the
// vector.
func accumulateScores(out []float64, dist []float64, idx []int32, window int32) {
	n := int32(len(out))
	half := window / 2
	for i, d := range dist {
		if idx[i] < 0 || math.IsInf(d, 0) || math.IsNaN(d) {
			continue
		}
		centre := int32(i) + half
		if centre < 0 || centre >= n {
			continue
		}
		if d > out[centre] {
			out[centre] = d
		}
	}
}
