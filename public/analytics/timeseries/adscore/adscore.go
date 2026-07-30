package adscore

import (
	"math"
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Ranges holds labelled anomaly intervals, both endpoints inclusive, in
// ascending order and non-overlapping.
//
// Struct-of-arrays: every consumer here scans one endpoint at a time.
type Ranges struct {
	Start []int32
	End   []int32
}

// Len returns the number of ranges.
func (inst Ranges) Len() (count int32) {
	count = int32(len(inst.Start))
	return
}

// RangesFromLabels collapses a point-wise boolean label vector into the
// intervals it describes.
func RangesFromLabels(labels []bool) (inst Ranges) {
	n := len(labels)
	inst.Start = make([]int32, 0, 8)
	inst.End = make([]int32, 0, 8)
	i := 0
	for i < n {
		if !labels[i] {
			i++
			continue
		}
		start := i
		for i < n && labels[i] {
			i++
		}
		inst.Start = append(inst.Start, int32(start))
		inst.End = append(inst.End, int32(i-1))
	}
	return
}

// DefaultMaxBuffer returns the mean labelled range length, which is the buffer
// ceiling TSB-AD integrates VUS over. It is a convention, not a derivation:
// tolerating a detection that misses by about the length of the event itself is
// what the measure is trying to express.
//
// Returns 0 when there are no ranges.
func DefaultMaxBuffer(ranges Ranges) (buffer int32) {
	count := ranges.Len()
	if count == 0 {
		return
	}
	var total int64
	for i := range count {
		total += int64(ranges.End[i] - ranges.Start[i] + 1)
	}
	buffer = int32(total / int64(count))
	return
}

// BufferLabels builds the continuous label vector for a given buffer length —
// 1 inside each labelled range, decaying outward across buffer/2 positions on
// either side, 0 beyond.
//
// The ramp is sqrt(1 - k/buffer) at distance k, so it starts just below 1 at the
// range edge and stops at sqrt(1/2) ≈ 0.707 where the buffer ends rather than
// reaching 0. That discontinuity is in the published definition and in the
// reference implementation, not an approximation introduced here; it makes the
// buffer a region of partial credit rather than a smooth taper.
//
// Where two buffers overlap the larger value wins. dst is filled and returned
// when it has room for n values, otherwise a fresh slice is allocated.
func BufferLabels(ranges Ranges, n int32, buffer int32, dst []float64) (out []float64) {
	if cap(dst) >= int(n) {
		out = dst[:n]
		clear(out)
	} else {
		out = make([]float64, n)
	}

	half := buffer / 2
	invBuffer := 0.0
	if buffer > 0 {
		invBuffer = 1.0 / float64(buffer)
	}

	for r := range ranges.Len() {
		start := ranges.Start[r]
		end := ranges.End[r]
		for i := start; i <= end && i < n; i++ {
			out[i] = 1.0
		}
		for k := int32(1); k <= half; k++ {
			w := math.Sqrt(1.0 - float64(k)*invBuffer)
			if before := start - k; before >= 0 && w > out[before] {
				out[before] = w
			}
			if after := end + k; after < n && w > out[after] {
				out[after] = w
			}
		}
	}
	return
}

// Curve is one range-based ROC and PR curve, ordered by decreasing threshold so
// FPR and Recall are non-decreasing.
type Curve struct {
	FPR       []float64
	TPR       []float64
	Recall    []float64
	Precision []float64
}

// Measures collects the accuracy measures for one scored series.
//
// AUCROC and AUCPR are the classic point-wise measures — no buffer, no
// existence reward — included because they are the baseline the literature
// criticizes, and because seeing them next to the VUS values is the whole point.
type Measures struct {
	AUCROC float64
	AUCPR  float64
	VUSROC float64
	VUSPR  float64
}

// evaluator holds the parts of an evaluation that do not change with the buffer
// length, so a VUS sweep sorts once rather than once per buffer.
type evaluator struct {
	n      int32
	scores []float64
	ranges Ranges

	// order lists positions by decreasing score; groupLast marks the last offset
	// of each run of equal scores, since tied points must enter the prediction
	// set together or the curve depends on sort stability.
	order     []int32
	groupLast []int32

	// segId maps a position to the labelled range covering it, or -1.
	segId []int32

	binarySum float64

	contLabel []float64
	segHits   []int32
	curve     Curve
}

func newEvaluatorE(scores []float64, ranges Ranges, n int32) (inst *evaluator, err error) {
	inst = &evaluator{
		n:      n,
		scores: scores,
		ranges: ranges,
	}

	inst.order = make([]int32, n)
	for i := range n {
		inst.order[i] = i
	}
	sort.SliceStable(inst.order, func(a int, b int) bool {
		return scores[inst.order[a]] > scores[inst.order[b]]
	})

	inst.groupLast = make([]int32, 0, n)
	for i := int32(0); i < n; i++ {
		if i+1 == n || scores[inst.order[i]] != scores[inst.order[i+1]] {
			inst.groupLast = append(inst.groupLast, i)
		}
	}

	inst.segId = make([]int32, n)
	for i := range inst.segId {
		inst.segId[i] = -1
	}
	for r := range ranges.Len() {
		for i := ranges.Start[r]; i <= ranges.End[r] && i < n; i++ {
			inst.segId[i] = r
			inst.binarySum += 1.0
		}
	}

	inst.contLabel = make([]float64, n)
	inst.segHits = make([]int32, ranges.Len())

	pts := len(inst.groupLast) + 2
	inst.curve = Curve{
		FPR:       make([]float64, 0, pts),
		TPR:       make([]float64, 0, pts),
		Recall:    make([]float64, 0, pts),
		Precision: make([]float64, 0, pts),
	}
	return
}

// sweep rebuilds the curve for one buffer length.
//
// Positions enter the prediction set in decreasing score order, so true- and
// false-positive mass accumulate incrementally and the whole curve costs O(n)
// once the sort is paid. The existence ratio — the fraction of labelled ranges
// with at least one predicted point inside — is maintained the same way, by
// counting a range the first time one of its positions is admitted.
func (inst *evaluator) sweep(buffer int32, withExistence bool) {
	inst.contLabel = BufferLabels(inst.ranges, inst.n, buffer, inst.contLabel)

	var contSum float64
	for _, v := range inst.contLabel {
		contSum += v
	}
	// Positives are the mean of the binary and buffered label mass, so widening
	// the buffer does not simply inflate the positive class.
	positives := (inst.binarySum + contSum) / 2.0
	negatives := float64(inst.n) - positives

	clear(inst.segHits)
	numSeg := float64(inst.ranges.Len())

	inst.curve.FPR = inst.curve.FPR[:0]
	inst.curve.TPR = inst.curve.TPR[:0]
	inst.curve.Recall = inst.curve.Recall[:0]
	inst.curve.Precision = inst.curve.Precision[:0]

	var tp, fp float64
	var detected int32
	next := int32(0)

	for _, last := range inst.groupLast {
		for ; next <= last; next++ {
			pos := inst.order[next]
			l := inst.contLabel[pos]
			tp += l
			fp += 1.0 - l
			if seg := inst.segId[pos]; seg >= 0 {
				inst.segHits[seg]++
				if inst.segHits[seg] == 1 {
					detected++
				}
			}
		}

		predCount := float64(last + 1)
		recall := tp / positives
		if recall > 1.0 {
			recall = 1.0
		}
		if withExistence && numSeg > 0 {
			recall *= float64(detected) / numSeg
		}

		inst.curve.Recall = append(inst.curve.Recall, recall)
		inst.curve.TPR = append(inst.curve.TPR, recall)
		inst.curve.FPR = append(inst.curve.FPR, fp/negatives)
		inst.curve.Precision = append(inst.curve.Precision, tp/predCount)
	}
}

// areas integrates the swept curve, trapezoidally in both cases.
//
// Trapezoidal PR area is a deliberate match to the VUS reference
// implementation, which averages adjacent precisions exactly as it averages
// adjacent TPRs. It is not the step-wise average-precision convention, and it
// reads slightly higher; the choice is here so numbers stay comparable with
// published VUS results rather than because it is the better estimator.
func (inst *evaluator) areas() (rocArea float64, prArea float64) {
	c := inst.curve
	k := len(c.FPR)
	if k == 0 {
		return
	}

	// ROC runs from the origin to (1,1); the sweep itself never reaches either.
	prevFpr, prevTpr := 0.0, 0.0
	for i := range k {
		rocArea += (c.FPR[i] - prevFpr) * (c.TPR[i] + prevTpr) / 2.0
		prevFpr, prevTpr = c.FPR[i], c.TPR[i]
	}
	rocArea += (1.0 - prevFpr) * (1.0 + prevTpr) / 2.0

	// PR starts at recall 0 holding the first precision, which is the usual
	// convention and the only defensible value there.
	prevRecall := 0.0
	prevPrec := c.Precision[0]
	for i := range k {
		prArea += (c.Recall[i] - prevRecall) * (c.Precision[i] + prevPrec) / 2.0
		prevRecall, prevPrec = c.Recall[i], c.Precision[i]
	}
	return
}

func validateE(scores []float64, labels []bool) (ranges Ranges, n int32, err error) {
	n = int32(len(scores))
	if n == 0 {
		err = eb.Build().Errorf("empty score vector")
		return
	}
	if len(labels) != len(scores) {
		err = eb.Build().Int("scores", len(scores)).Int("labels", len(labels)).
			Errorf("score and label vectors differ in length")
		return
	}
	for _, v := range scores {
		if math.IsNaN(v) {
			err = eb.Build().Errorf("score vector contains NaN")
			return
		}
	}
	ranges = RangesFromLabels(labels)
	if ranges.Len() == 0 {
		err = eb.Build().Errorf("label vector marks no anomaly")
		return
	}
	if int(ranges.Len()) == 1 && ranges.Start[0] == 0 && int(ranges.End[0]) == len(labels)-1 {
		err = eb.Build().Errorf("label vector marks the whole series as anomalous")
		return
	}
	return
}

// RangeAUCE computes the range-based ROC and PR areas at a single buffer
// length. A buffer of 0 reduces the continuous label to the binary one, leaving
// the existence reward as the only difference from the point-wise measures.
func RangeAUCE(scores []float64, labels []bool, buffer int32) (rocArea float64, prArea float64, err error) {
	ranges, n, err := validateE(scores, labels)
	if err != nil {
		return
	}
	if buffer < 0 {
		err = eb.Build().Int32("buffer", buffer).Errorf("buffer length must not be negative")
		return
	}
	ev, err := newEvaluatorE(scores, ranges, n)
	if err != nil {
		return
	}
	ev.sweep(buffer, true)
	rocArea, prArea = ev.areas()
	return
}

// EvaluateE scores a detector's output against labelled anomalies, returning
// both the classic point-wise measures and the VUS measures.
//
// VUS averages the range-based area over every buffer length from 0 to
// maxBuffer inclusive, which is what makes it free of the buffer parameter that
// range-based measures otherwise carry. Pass 0 for maxBuffer to accept
// [DefaultMaxBuffer].
//
// The averaging is a plain mean over buffer lengths, matching the reference
// implementation. The paper states a trapezoidal rule over that axis too; the
// two differ only in how they weight the endpoints.
func EvaluateE(scores []float64, labels []bool, maxBuffer int32) (m Measures, err error) {
	ranges, n, err := validateE(scores, labels)
	if err != nil {
		return
	}
	if maxBuffer < 0 {
		err = eb.Build().Int32("maxBuffer", maxBuffer).Errorf("buffer length must not be negative")
		return
	}
	if maxBuffer == 0 {
		maxBuffer = DefaultMaxBuffer(ranges)
	}

	ev, err := newEvaluatorE(scores, ranges, n)
	if err != nil {
		return
	}

	ev.sweep(0, false)
	m.AUCROC, m.AUCPR = ev.areas()

	var rocSum, prSum float64
	for buffer := int32(0); buffer <= maxBuffer; buffer++ {
		ev.sweep(buffer, true)
		roc, pr := ev.areas()
		rocSum += roc
		prSum += pr
	}
	count := float64(maxBuffer + 1)
	m.VUSROC = rocSum / count
	m.VUSPR = prSum / count
	return
}

// CurveE returns the range-based curve at one buffer length, for inspection and
// plotting. The returned slices are freshly allocated.
func CurveE(scores []float64, labels []bool, buffer int32) (c Curve, err error) {
	ranges, n, err := validateE(scores, labels)
	if err != nil {
		return
	}
	if buffer < 0 {
		err = eb.Build().Int32("buffer", buffer).Errorf("buffer length must not be negative")
		return
	}
	ev, err := newEvaluatorE(scores, ranges, n)
	if err != nil {
		return
	}
	ev.sweep(buffer, true)

	c = Curve{
		FPR:       append([]float64(nil), ev.curve.FPR...),
		TPR:       append([]float64(nil), ev.curve.TPR...),
		Recall:    append([]float64(nil), ev.curve.Recall...),
		Precision: append([]float64(nil), ev.curve.Precision...),
	}
	return
}
