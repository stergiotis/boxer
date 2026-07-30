package damp

import (
	"math"

	"github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config parameterizes a [Detector].
type Config struct {
	// Window is the subsequence length. Choose it to span the pattern that an
	// anomaly would violate — typically the signal's period — rather than the
	// expected length of the anomaly itself. A window shorter than the pattern
	// cannot see the violation.
	Window int32

	// TrainLength is how many samples arrive before scoring starts. Nothing is
	// reported until the detector holds this much history to compare against.
	// Zero accepts 8×Window.
	TrainLength int32

	// HistoryLimit caps retained samples. Zero retains everything, which makes
	// the result an exact left discord over the whole stream at unbounded
	// memory. A finite limit bounds memory and narrows the guarantee to "exact
	// within the retained horizon" — an anomaly that recurs only outside the
	// horizon reads as novel again.
	HistoryLimit int32

	// Exact computes every position's true left-discord distance instead of
	// running DAMP's early abandoning. Slower per sample and independent of how
	// anomalous the data is, but it is the only mode whose whole score vector
	// means anything — see [Detector.Push].
	Exact bool

	// StdDevFloorRel is the constant-window threshold as a fraction of the
	// training prefix's standard deviation. Zero accepts
	// [matrixprofile.DefaultStdDevFloorRel].
	StdDevFloorRel float64
}

// Reading is one scored subsequence.
type Reading struct {
	// Score is the distance from this subsequence to its nearest predecessor.
	// Larger means more anomalous.
	Score float64

	// Start is the subsequence's absolute position in the stream.
	Start int64

	// Centre is Start + Window/2. A window's score describes its whole span, so
	// this is where it belongs when it is plotted or handed to a scorer that
	// works per position. Attributing it to Start instead displaces every peak
	// by half a window, which measurably costs more than half the achievable
	// accuracy.
	Centre int64

	// Exact reports whether Score is this subsequence's true left-discord
	// distance. It is always true in [Config.Exact] mode. Under DAMP it is false
	// wherever early abandoning stopped the search, in which case Score is an
	// upper bound that is known only to be below the running maximum.
	Exact bool
}

// Detector finds left discords — subsequences whose nearest neighbour among
// everything that came *before* them is far away — over an arriving stream.
//
// Restricting the neighbour search to the past is what makes the result
// deployable. A bidirectional nearest neighbour lets the future explain the
// present, which scores well offline and cannot be computed online.
//
// A Detector is not safe for concurrent use.
type Detector struct {
	cfg  Config
	zone int32

	// base is the absolute stream position of raw[0].
	base int64
	// count is how many samples have been pushed in total.
	count int64

	// raw holds retained samples unmodified; centred holds the same values
	// shifted by ref. The split follows the matrixprofile package: dot products
	// run on centred values, which is what survives a large constant offset,
	// while per-window statistics come from raw, which is what survives a window
	// whose variation is tiny against the series range.
	raw     []float64
	centred []float64
	ref     float64

	// mean is the per-window mean of centred; invStd is 1/σ from raw, or 0 for a
	// window at or below the floor.
	mean   []float64
	invStd []float64

	stdDevFloor float64
	trained     bool

	// bestSoFar is the running maximum left-discord distance, the bound DAMP
	// abandons against.
	bestSoFar float64

	// dots holds the current query's dot product against every retained window,
	// maintained by the diagonal recurrence. Exact mode only.
	dots     []float64
	dotQuery int32

	scratch []float64
}

// NewDetectorE validates cfg and returns a detector.
func NewDetectorE(cfg Config) (inst *Detector, err error) {
	if cfg.Window < 2 {
		err = eb.Build().Int32("window", cfg.Window).Errorf("window must be at least 2")
		return
	}
	if cfg.TrainLength == 0 {
		cfg.TrainLength = cfg.Window * 8
	}
	if cfg.TrainLength < cfg.Window*2 {
		err = eb.Build().Int32("trainLength", cfg.TrainLength).Int32("window", cfg.Window).
			Errorf("train length must cover at least two windows")
		return
	}
	if cfg.HistoryLimit != 0 && cfg.HistoryLimit < cfg.TrainLength {
		err = eb.Build().Int32("historyLimit", cfg.HistoryLimit).Int32("trainLength", cfg.TrainLength).
			Errorf("history limit must not be below the train length")
	}
	if err != nil {
		return
	}
	if cfg.StdDevFloorRel <= 0.0 {
		cfg.StdDevFloorRel = matrixprofile.DefaultStdDevFloorRel
	}

	inst = &Detector{
		cfg:       cfg,
		zone:      (cfg.Window + matrixprofile.DefaultExclusionDivisor - 1) / matrixprofile.DefaultExclusionDivisor,
		bestSoFar: math.Inf(-1),
		dotQuery:  -1,
	}
	capacity := int(cfg.TrainLength) * 2
	inst.raw = make([]float64, 0, capacity)
	inst.centred = make([]float64, 0, capacity)
	inst.mean = make([]float64, 0, capacity)
	inst.invStd = make([]float64, 0, capacity)
	return
}

// Window returns the configured subsequence length.
func (inst *Detector) Window() (window int32) {
	window = inst.cfg.Window
	return
}

// Count returns how many samples have been pushed.
func (inst *Detector) Count() (count int64) {
	count = inst.count
	return
}

// BestSoFar returns the largest left-discord distance seen, which is the score
// of the most anomalous subsequence so far.
func (inst *Detector) BestSoFar() (score float64, found bool) {
	found = !math.IsInf(inst.bestSoFar, -1)
	if found {
		score = inst.bestSoFar
	}
	return
}

// numWindows returns how many complete windows the retained history holds.
func (inst *Detector) numWindows() (count int32) {
	count = int32(len(inst.raw)) - inst.cfg.Window + 1
	if count < 0 {
		count = 0
	}
	return
}

// windowStats returns a window's mean and standard deviation over src, two-pass
// so it stays accurate near zero variance. The single-pass E[x²]−E[x]² form
// cancels there and reports an exactly constant window as structure.
func (inst *Detector) windowStats(src []float64, idx int32) (mean float64, std float64) {
	seg := src[idx : idx+inst.cfg.Window]
	invM := 1.0 / float64(inst.cfg.Window)

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

// appendWindowStats computes statistics for every window that became complete
// since the last call.
func (inst *Detector) appendWindowStats() {
	want := int(inst.numWindows())
	for len(inst.mean) < want {
		idx := int32(len(inst.mean))
		centredMean, _ := inst.windowStats(inst.centred, idx)
		_, std := inst.windowStats(inst.raw, idx)

		inst.mean = append(inst.mean, centredMean)
		if std > inst.stdDevFloor {
			inst.invStd = append(inst.invStd, 1.0/std)
		} else {
			inst.invStd = append(inst.invStd, 0.0)
		}
	}
}

// finishTraining freezes the centring reference and builds the prefix's
// statistics.
//
// The reference is the training prefix's mean and never moves afterwards. A
// running mean would make dot products taken at different times incomparable,
// which is the one thing the recurrence cannot tolerate.
func (inst *Detector) finishTraining() {
	var sum float64
	for _, v := range inst.raw {
		sum += v
	}
	inst.ref = sum / float64(len(inst.raw))

	var ss float64
	inst.centred = inst.centred[:0]
	for _, v := range inst.raw {
		c := v - inst.ref
		inst.centred = append(inst.centred, c)
		ss += c * c
	}
	inst.stdDevFloor = inst.cfg.StdDevFloorRel * math.Sqrt(ss/float64(len(inst.raw)))

	inst.trained = true
	inst.appendWindowStats()
}

// compact drops the oldest history once it grows past twice the limit, keeping
// the copy amortized at O(1) per sample.
//
// The buffer is owned here rather than taken from
// [github.com/stergiotis/boxer/public/observability/slidingwindow] because that
// one memmoves on every push, which is right at a 1 Hz sampler cadence and
// wrong here, and because the dot product needs its history contiguous — which
// a head-index ring, the obvious fix there, would not give.
func (inst *Detector) compact() {
	limit := int(inst.cfg.HistoryLimit)
	if limit == 0 || len(inst.raw) <= limit*2 {
		return
	}
	drop := len(inst.raw) - limit
	inst.raw = inst.raw[:copy(inst.raw, inst.raw[drop:])]
	inst.centred = inst.centred[:copy(inst.centred, inst.centred[drop:])]
	inst.mean = inst.mean[:copy(inst.mean, inst.mean[drop:])]
	inst.invStd = inst.invStd[:copy(inst.invStd, inst.invStd[drop:])]
	if inst.dots != nil {
		inst.dots = inst.dots[:copy(inst.dots, inst.dots[drop:])]
		inst.dotQuery -= int32(drop)
		if inst.dotQuery < 0 {
			inst.dotQuery = -1
		}
	}
	inst.base += int64(drop)
}

// distanceFromDot converts a raw dot product between windows i and j into their
// z-normalized Euclidean distance, via d² = 2m(1−ρ).
func (inst *Detector) distanceFromDot(dot float64, i int32, j int32) (dist float64) {
	m := float64(inst.cfg.Window)
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

// exactDistance recomputes a pair's distance from materialized z-normalized
// values, avoiding the identity's loss of accuracy as ρ approaches 1.
func (inst *Detector) exactDistance(i int32, j int32) (dist float64) {
	if inst.invStd[i] == 0.0 || inst.invStd[j] == 0.0 {
		if inst.invStd[i] == 0.0 && inst.invStd[j] == 0.0 {
			return
		}
		dist = math.Sqrt(float64(inst.cfg.Window))
		return
	}
	meanI, stdI := inst.windowStats(inst.raw, i)
	meanJ, stdJ := inst.windowStats(inst.raw, j)
	invStdI := 1.0 / stdI
	invStdJ := 1.0 / stdJ

	a := inst.raw[i : i+inst.cfg.Window]
	b := inst.raw[j : j+inst.cfg.Window]
	var ss float64
	for k := range a {
		d := (a[k]-meanI)*invStdI - (b[k]-meanJ)*invStdJ
		ss += d * d
	}
	dist = math.Sqrt(ss)
	return
}

func (inst *Detector) dotAt(i int32, j int32) (dot float64) {
	a := inst.centred[i : i+inst.cfg.Window]
	b := inst.centred[j : j+inst.cfg.Window]
	for k := range a {
		dot += a[k] * b[k]
	}
	return
}

// scanRange returns the smallest distance from the query to any window starting
// in [lo, hi), and the index that achieved it. Direct, at O((hi−lo)·Window).
func (inst *Detector) scanRange(query int32, lo int32, hi int32) (dist float64, at int32) {
	dist = math.Inf(1)
	at = -1
	for j := lo; j < hi; j++ {
		d := inst.distanceFromDot(inst.dotAt(query, j), query, j)
		if d < dist {
			dist = d
			at = j
		}
	}
	return
}

// advanceDots moves the maintained dot-product row to the given query using the
// diagonal recurrence
//
//	QT[q][j] = QT[q-1][j-1] − c[q-1]·c[j-1] + c[q+m-1]·c[j+m-1]
//
// at O(1) per entry, so a whole row costs O(retained) rather than O(retained ×
// Window). Rebuilding directly when the row is not one step behind.
func (inst *Detector) advanceDots(query int32) {
	numWindows := inst.numWindows()
	if cap(inst.dots) < int(numWindows) {
		grown := make([]float64, numWindows, numWindows*2)
		copy(grown, inst.dots)
		inst.dots = grown
	}
	inst.dots = inst.dots[:numWindows]

	if inst.dotQuery != query-1 || inst.dotQuery < 0 {
		for j := range numWindows {
			inst.dots[j] = inst.dotAt(query, j)
		}
		inst.dotQuery = query
		return
	}

	m := inst.cfg.Window
	c := inst.centred
	drop := c[query-1]
	add := c[query+m-1]
	for j := numWindows - 1; j >= 1; j-- {
		inst.dots[j] = inst.dots[j-1] - drop*c[j-1] + add*c[j+m-1]
	}
	inst.dots[0] = inst.dotAt(query, 0)
	inst.dotQuery = query
}

// scoreExact returns the query's true left-discord distance over the retained
// history.
func (inst *Detector) scoreExact(query int32) (score float64, at int32) {
	inst.advanceDots(query)

	score = math.Inf(1)
	at = -1
	limit := query - inst.zone
	for j := int32(0); j <= limit; j++ {
		d := inst.distanceFromDot(inst.dots[j], query, j)
		if d < score {
			score = d
			at = j
		}
	}
	return
}

// scoreDAMP returns the query's left-discord distance, abandoning as soon as a
// neighbour closer than the running maximum turns up.
//
// History is scanned backwards in doubling, non-overlapping blocks: recent
// history first, because a subsequence that is not anomalous almost always has
// a close neighbour nearby, and each block that fails to abandon doubles the
// reach. The published algorithm re-scans the whole span on each expansion;
// scanning only the newly reached part gives the same answer for less work,
// since a minimum over a union is the minimum of the parts' minima.
//
// exact reports whether the scan ran to the start of history. When it did not,
// score is an upper bound on the true distance, known only to be below
// [Detector.BestSoFar].
func (inst *Detector) scoreDAMP(query int32) (score float64, at int32, exact bool) {
	score = math.Inf(1)
	at = -1

	right := query - inst.zone
	if right < 0 {
		exact = true
		return
	}

	block := int32(1)
	for block < inst.cfg.Window*8 {
		block *= 2
	}

	for right >= 0 {
		lo := query - block + 1
		if lo < 0 {
			lo = 0
		}
		if lo > right {
			lo = right
		}
		d, idx := inst.scanRange(query, lo, right+1)
		if d < score {
			score = d
			at = idx
		}
		if score < inst.bestSoFar {
			// Cannot be the discord; nothing further would change that.
			return
		}
		if lo == 0 {
			exact = true
			return
		}
		right = lo - 1
		block *= 2
	}
	exact = true
	return
}

// Push adds one sample and reports the subsequence that just became complete.
//
// ok is false while the detector is still filling its training prefix, and for
// the first Window−1 samples after that.
//
// # What the score means
//
// In [Config.Exact] mode every Reading carries that subsequence's true
// left-discord distance, and the whole sequence of scores is a usable anomaly
// score vector.
//
// Under DAMP it is not. Early abandoning stops as soon as the subsequence is
// shown not to be the discord, so a Reading with Exact false carries an upper
// bound rather than a distance — enough to rank it below the maximum, not
// enough to rank it against its peers. DAMP answers "where is the anomaly",
// and answers it exactly; it does not produce a calibrated score everywhere.
// Feed a DAMP score vector to a scorer expecting one and the result is
// meaningless.
func (inst *Detector) Push(v float64) (reading Reading, ok bool) {
	inst.raw = append(inst.raw, v)
	inst.count++

	if !inst.trained {
		if int32(len(inst.raw)) < inst.cfg.TrainLength {
			return
		}
		inst.finishTraining()
		return
	}

	inst.centred = append(inst.centred, v-inst.ref)
	inst.compact()
	inst.appendWindowStats()

	query := inst.numWindows() - 1
	if query < 0 || query-inst.zone < 0 {
		return
	}

	var score float64
	var at int32
	var exact bool
	if inst.cfg.Exact {
		score, at = inst.scoreExact(query)
		exact = true
	} else {
		score, at, exact = inst.scoreDAMP(query)
	}
	if at < 0 {
		return
	}

	// Report the distance recomputed from z-normalized values; the identity the
	// search runs on loses absolute accuracy as ρ approaches 1, and an exactly
	// matching pair would otherwise report ~1e-6 rather than 0.
	score = inst.exactDistance(query, at)
	if exact && score > inst.bestSoFar {
		inst.bestSoFar = score
	}

	start := inst.base + int64(query)
	reading = Reading{
		Score:  score,
		Start:  start,
		Centre: start + int64(inst.cfg.Window/2),
		Exact:  exact,
	}
	ok = true
	return
}

// ScoreE drives a detector over a stored series and returns every reading, for
// evaluation and testing. Streaming callers use [Detector.Push] directly.
func ScoreE(values []float64, cfg Config) (readings []Reading, err error) {
	inst, err := NewDetectorE(cfg)
	if err != nil {
		return
	}
	readings = make([]Reading, 0, len(values))
	for _, v := range values {
		r, ok := inst.Push(v)
		if ok {
			readings = append(readings, r)
		}
	}
	return
}

// PositionScores spreads readings onto a per-position score vector of length n,
// each reading landing on its [Reading.Centre]. Positions no reading covers
// keep zero.
//
// This is the shape [github.com/stergiotis/boxer/public/analytics/timeseries/adscore]
// expects. Note the caveat on [Detector.Push]: only an exact-mode run produces
// a score vector worth scoring.
func PositionScores(readings []Reading, n int32, dst []float64) (out []float64) {
	if cap(dst) >= int(n) {
		out = dst[:n]
		clear(out)
	} else {
		out = make([]float64, n)
	}
	for _, r := range readings {
		if r.Centre < 0 || r.Centre >= int64(n) {
			continue
		}
		if r.Score > out[r.Centre] {
			out[r.Centre] = r.Score
		}
	}
	return
}
