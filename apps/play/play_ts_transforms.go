package play

import (
	"sort"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/damp"
	"github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile"
	"github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_ts_transforms.go holds the four v1 transforms (ADR-0163 §SD3). Each is
// a thin adapter: read the arguments, call the substrate package, build the
// declared Arrow output. The maths lives in public/analytics/timeseries and
// stays there — a transform that reimplemented any of it would be a second
// copy to keep honest, and the tests here would have nothing independent left
// to check the adapter against.
//
// All of them are CENTRE-ATTRIBUTED where the substrate is: a window's score
// describes its whole span, so placing it at the window's start displaces
// every peak by half a window.

// tsSmoothDegree is fixed at 4 — the filter degree ADR-0152 settled on, and
// the same one the display smoother uses. Exposing it would be exposing a
// filter-design knob, which is not what a workbench user is choosing between.
const tsSmoothDegree int32 = 4

// tsApplySmooth is the MS-kernel smoother: (t, smooth), same length as input.
func tsApplySmooth(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	halfWidth, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	if min := mssmooth.MinHalfWidth(tsSmoothDegree); halfWidth < min {
		err = eh.Errorf("tsSmooth: halfWidth %d is below the degree-%d kernel's minimum of %d",
			halfWidth, tsSmoothDegree, min)
		return
	}
	if int(halfWidth)*2+1 > len(vals) {
		err = eh.Errorf("tsSmooth: halfWidth %d needs at least %d samples; the input has %d",
			halfWidth, halfWidth*2+1, len(vals))
		return
	}
	kernel, err := mssmooth.NewKernelE(tsSmoothDegree, halfWidth)
	if err != nil {
		err = eh.Errorf("tsSmooth: %w", err)
		return
	}
	smoothed, err := kernel.SmoothE(vals, nil)
	if err != nil {
		err = eh.Errorf("tsSmooth: %w", err)
		return
	}
	b := newTsBuilder(call.Spec, alloc, len(ts))
	defer b.Release()
	b.time(0, ts)
	b.floats(1, smoothed)
	return b.build(len(ts))
}

// tsApplyProfile is the z-normalised matrix profile: (t, profile), one row per
// WINDOW rather than per sample, each attributed to its window's centre.
func tsApplyProfile(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	window, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	series, err := matrixprofile.NewSeriesE(vals, window, matrixprofile.DefaultStdDevFloorRel)
	if err != nil {
		err = eh.Errorf("tsProfile: %w", err)
		return
	}
	prof := series.Compute()
	n := len(prof.Distance)
	centres := make([]int64, n)
	for i := range centres {
		centres[i] = ts[tsCentreOf(int32(i), window, len(ts))]
	}
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.time(0, centres)
	b.floats(1, prof.Distance)
	return b.build(n)
}

// tsApplyScores is DAMP in exact mode: (t, score, warm_up), one row per input
// sample.
//
// Exact mode only, and not as a default anyone may change: DAMP's ordinary
// mode early-abandons, which makes most of its numbers upper BOUNDS rather
// than scores. A bound vector drawn as a score curve is a picture of the
// algorithm's pruning, not of the data (ADR-0163 S2).
//
// `warm_up` marks every position carrying no score — the training prefix and
// the tail whose window has no room. It is the honest complement of the score
// column: shading it is what stops a reader taking a flat zero for calm.
func tsApplyScores(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	window, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	scores, warm, err := tsScoreSeries(vals, window, len(ts))
	if err != nil {
		return
	}
	b := newTsBuilder(call.Spec, alloc, len(ts))
	defer b.Release()
	b.time(0, ts)
	b.floats(1, scores)
	b.bools(2, warm)
	return b.build(len(ts))
}

// tsScoreSeries runs the detector over the whole series and lays each reading
// at its centre. Shared by the score and span transforms so the spans are
// provably the extents of the scores the other one reports.
func tsScoreSeries(vals []float64, window int32, n int) (scores []float64, warm []bool, err error) {
	det, dErr := damp.NewDetectorE(damp.Config{Window: window, Exact: true})
	if dErr != nil {
		err = eh.Errorf("tsAnomalyScores: %w", dErr)
		return
	}
	scores = make([]float64, n)
	warm = make([]bool, n)
	for i := range warm {
		warm[i] = true
	}
	for _, v := range vals {
		reading, ok := det.Push(v)
		if !ok {
			continue
		}
		c := reading.Centre
		if c < 0 || c >= int64(n) {
			continue
		}
		scores[c] = reading.Score
		warm[c] = false
	}
	return
}

// tsSpanPlateauFrac is the share of a peak's score a neighbouring position
// must still reach to count as part of the same event. An initial constant,
// not a measured one: it is what turns a peak into an EXTENT, and reporting
// the argmax alone would understate an anomaly that lasted.
const tsSpanPlateauFrac = 0.5

// tsApplySpans reports the top-k flagged extents as Timeline background bands.
//
// The output speaks the band contract directly (`_tl_band_*`), because the
// terminal-leaf rule means no downstream SQL can rename anything: a client
// node's columns ARE what its consumer sees. Two details of that contract are
// load-bearing — the extents must be Arrow Timestamps, and the colour must be
// an IDS token NAME rather than a hex literal, since the band reader resolves
// names against a fixed map and counts a miss as a skipped row.
func tsApplySpans(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	window, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	k, err := tsIntArg(call, 3, params)
	if err != nil {
		return
	}
	if k < 1 {
		err = eh.Errorf("tsAnomalySpans: k must be at least 1")
		return
	}
	scores, warm, err := tsScoreSeries(vals, window, len(ts))
	if err != nil {
		return
	}
	spans := tsTopSpans(scores, warm, window, int(k))
	n := len(spans)
	from := make([]int64, n)
	to := make([]int64, n)
	labels := make([]string, n)
	colors := make([]string, n)
	peaks := make([]float64, n)
	for i, s := range spans {
		from[i] = ts[s.lo]
		to[i] = ts[s.hi]
		labels[i] = "#" + strconv.Itoa(i+1) + " score " + formatTsScore(s.score)
		colors[i] = tsSpanColorToken(i)
		peaks[i] = s.score
	}
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.time(0, from)
	b.time(1, to)
	b.strings(2, labels)
	b.strings(3, colors)
	b.floats(4, peaks)
	return b.build(n)
}

// tsSpan is one flagged extent, as sample indices into the input.
type tsSpan struct {
	lo, hi int
	score  float64
}

// tsTopSpans picks the k highest-scoring disjoint extents. Each pick takes the
// remaining argmax, widens it over neighbours still above the plateau
// fraction, and then excludes a window either side — the same exclusion
// discipline the matrix profile uses, and for the same reason: adjacent
// windows overlap, so without it the top k would be one event reported k
// times.
func tsTopSpans(scores []float64, warm []bool, window int32, k int) (spans []tsSpan) {
	n := len(scores)
	taken := make([]bool, n)
	for i := range taken {
		taken[i] = warm[i]
	}
	spans = make([]tsSpan, 0, k)
	for range k {
		best, bestScore := -1, 0.0
		for i := range n {
			if taken[i] || scores[i] <= bestScore {
				continue
			}
			best, bestScore = i, scores[i]
		}
		if best < 0 {
			break
		}
		floor := bestScore * tsSpanPlateauFrac
		lo, hi := best, best
		for lo > 0 && !taken[lo-1] && scores[lo-1] >= floor {
			lo--
		}
		for hi < n-1 && !taken[hi+1] && scores[hi+1] >= floor {
			hi++
		}
		// The extent covers the WINDOW the score describes, not just the
		// positions above the floor: the anomalous stretch is a window long
		// by construction.
		exHi := min(n-1, hi+int(window))
		spans = append(spans, tsSpan{lo: lo, hi: exHi, score: bestScore})
		for i := max(0, lo-int(window)); i <= exHi; i++ {
			taken[i] = true
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
	return
}

// tsSpanColorToken is the band colour for rank i, as an IDS dot-notation token
// NAME. Warmer for the top ranks; anything past the roster repeats the last
// rather than inventing a token the reader's map would not resolve.
func tsSpanColorToken(rank int) string {
	tokens := []string{"error.default", "warning.default", "accent.default", "info.default"}
	if rank >= len(tokens) {
		return tokens[len(tokens)-1]
	}
	return tokens[rank]
}

// tsCentreOf maps a window index to the sample its score belongs at.
func tsCentreOf(windowIdx int32, window int32, n int) int {
	c := int(windowIdx) + int(window)/2
	return min(c, n-1)
}

// tsBuilder assembles a transform's declared output. It exists so a transform
// states WHICH column it is filling by position and never restates the schema:
// the schema comes from the registry, so an output that drifted from its
// declared contract cannot compile past the column count.
type tsBuilder struct {
	schema *arrow.Schema
	fields []array.Builder
}

func newTsBuilder(spec tsFuncSpec, alloc memory.Allocator, capacity int) (inst *tsBuilder) {
	inst = &tsBuilder{schema: tsOutputSchema(spec)}
	inst.fields = make([]array.Builder, len(spec.Out))
	for i, f := range inst.schema.Fields() {
		b := array.NewBuilder(alloc, f.Type)
		b.Reserve(capacity)
		inst.fields[i] = b
	}
	return
}

func (inst *tsBuilder) time(col int, ms []int64) {
	b := inst.fields[col].(*array.TimestampBuilder)
	for _, v := range ms {
		b.Append(arrow.Timestamp(v))
	}
}

func (inst *tsBuilder) floats(col int, vs []float64) {
	b := inst.fields[col].(*array.Float64Builder)
	b.AppendValues(vs, nil)
}

func (inst *tsBuilder) bools(col int, vs []bool) {
	b := inst.fields[col].(*array.BooleanBuilder)
	b.AppendValues(vs, nil)
}

func (inst *tsBuilder) strings(col int, vs []string) {
	b := inst.fields[col].(*array.StringBuilder)
	b.AppendValues(vs, nil)
}

func (inst *tsBuilder) build(rows int) (rec arrow.RecordBatch, err error) {
	cols := make([]arrow.Array, len(inst.fields))
	for i := range inst.fields {
		cols[i] = inst.fields[i].NewArray()
	}
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()
	return array.NewRecordBatch(inst.schema, cols, int64(rows)), nil
}

func (inst *tsBuilder) Release() {
	for _, b := range inst.fields {
		if b != nil {
			b.Release()
		}
	}
}

// formatTsScore renders a score for a band label: four significant digits,
// which is where a z-normalised distance stops telling a reader anything new.
func formatTsScore(v float64) string {
	return strconv.FormatFloat(v, 'g', 4, 64)
}
