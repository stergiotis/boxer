package adscore

import (
	"math"
	"math/rand/v2"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AnomalyKindE selects how an injected anomaly differs from the background.
//
// Every kind rewrites a segment with content that is *locally* plausible — drawn
// from, or built the same way as, the background itself — and wrong only in
// relation to the rest of the series. That is the demanding part. Wu and
// Keogh's first flaw is anomalies a one-liner finds, and a spike, a level shift
// or any locally novel waveform is exactly such an anomaly: a moving-average
// residual finds it immediately.
//
// Segment edges are cross-faded into the surrounding signal, because an abrupt
// join is itself a locally detectable discontinuity and would hand the same
// one-liners the answer.
//
// Use [TrivialityE] to check a generated fixture rather than trusting this
// comment; the residual detectability differs by kind and is measured, not
// assumed.
type AnomalyKindE uint8

const (
	// AnomalyKindReversal plays a segment backwards. The values are exactly the
	// background's own, in a different order.
	AnomalyKindReversal AnomalyKindE = iota
	// AnomalyKindTransplant substitutes a distant stretch of the background chosen
	// to match the displaced segment's level and spread, so it is a legitimate
	// piece of signal in an illegitimate place.
	AnomalyKindTransplant
	// AnomalyKindPhaseJump advances the oscillation, by the offset whose local
	// statistics best match what it displaces.
	AnomalyKindPhaseJump
	// AnomalyKindFrequencyShift compresses the oscillation in time. Frequency is
	// a local property, so this stays the most locally visible of the four —
	// measurably so, though still far below what a shape-aware detector reaches.
	AnomalyKindFrequencyShift
)

// String returns the kind's name.
func (inst AnomalyKindE) String() (name string) {
	switch inst {
	case AnomalyKindReversal:
		name = "reversal"
	case AnomalyKindTransplant:
		name = "transplant"
	case AnomalyKindPhaseJump:
		name = "phase-jump"
	case AnomalyKindFrequencyShift:
		name = "frequency-shift"
	default:
		name = "unknown"
	}
	return
}

// AllAnomalyKinds lists every kind, for sweeping.
var AllAnomalyKinds = [...]AnomalyKindE{
	AnomalyKindReversal,
	AnomalyKindTransplant,
	AnomalyKindPhaseJump,
	AnomalyKindFrequencyShift,
}

// golden and its reciprocal give the background components incommensurate
// periods, so the waveform never repeats exactly. A background that does repeat
// makes a transplanted segment identical to what belonged there, and therefore
// not an anomaly at all.
const (
	golden    = 1.6180339887498949
	invGolden = 0.6180339887498949
)

// background is the clean signal: three sinusoids at incommensurate periods.
// Locally smooth and statistically homogeneous, globally non-repeating.
func background(t float64, period float64) (v float64) {
	v = math.Sin(2.0*math.Pi*t/period) +
		0.6*math.Sin(2.0*math.Pi*t/(period*invGolden)+0.7) +
		0.35*math.Sin(2.0*math.Pi*t/(period*golden)+1.9)
	return
}

// smoothstep is the usual cubic ease, 0 at 0 and 1 at 1 with zero slope at both.
func smoothstep(x float64) (v float64) {
	if x <= 0.0 {
		return
	}
	if x >= 1.0 {
		v = 1.0
		return
	}
	v = x * x * (3.0 - 2.0*x)
	return
}

// FixtureSpec describes a synthetic labelled series.
//
// The defaults exist to keep a fixture clear of the four flaws catalogued in
// Wu and Keogh (2021): anomalies that a one-liner finds, unrealistic anomaly
// density, mislabelled ground truth, and anomalies bunched at the end of the
// series. The first is addressed by [AnomalyKindE], the second by
// AnomalyCount and AnomalyLength staying small against Length, the third by
// construction — the generator knows exactly what it injected — and the fourth
// by TailExclusionFrac.
type FixtureSpec struct {
	Length      int32
	Period      float64
	NoiseStdDev float64

	AnomalyCount  int32
	AnomalyLength int32
	Kind          AnomalyKindE

	// TailExclusionFrac keeps injected anomalies out of the final fraction of
	// the series. Run-to-failure recordings put their anomalies at the end, so a
	// detector with nothing but a positive time bias scores well on them; a
	// fixture that reproduces that accidentally rewards the same bias.
	TailExclusionFrac float64

	Seed uint64
}

// DefaultFixtureSpec returns a spec whose anomaly density (about 2%) and tail
// exclusion (the final 20%) are in the range real recordings show.
func DefaultFixtureSpec(kind AnomalyKindE, seed uint64) (spec FixtureSpec) {
	spec = FixtureSpec{
		Length:            4000,
		Period:            50.0,
		NoiseStdDev:       0.05,
		AnomalyCount:      4,
		AnomalyLength:     20,
		Kind:              kind,
		TailExclusionFrac: 0.2,
		Seed:              seed,
	}
	return
}

// Fixture is a generated series with exact labels.
type Fixture struct {
	Values []float64
	Labels []bool
	Ranges Ranges
}

// AnomalyFraction returns the share of positions marked anomalous.
func (inst *Fixture) AnomalyFraction() (frac float64) {
	var count int
	for _, l := range inst.Labels {
		if l {
			count++
		}
	}
	frac = float64(count) / float64(len(inst.Labels))
	return
}

// GenerateE builds a synthetic labelled series from spec. The result is a pure
// function of spec, including its Seed.
func GenerateE(spec FixtureSpec) (inst *Fixture, err error) {
	if spec.Length < 16 {
		err = eb.Build().Int32("length", spec.Length).Errorf("series too short")
		return
	}
	if spec.Period <= 1.0 {
		err = eb.Build().Float64("period", spec.Period).Errorf("period must exceed 1")
		return
	}
	if spec.AnomalyLength < 2 {
		err = eb.Build().Int32("anomalyLength", spec.AnomalyLength).Errorf("anomaly length must be at least 2")
		return
	}
	if spec.AnomalyCount < 1 {
		err = eb.Build().Int32("anomalyCount", spec.AnomalyCount).Errorf("need at least one anomaly")
		return
	}
	if spec.TailExclusionFrac < 0.0 || spec.TailExclusionFrac >= 1.0 {
		err = eb.Build().Float64("tailExclusionFrac", spec.TailExclusionFrac).
			Errorf("tail exclusion must be in [0,1)")
		return
	}

	n := spec.Length
	usable := int32(float64(n) * (1.0 - spec.TailExclusionFrac))
	// Leave room for a full anomaly plus a margin at each end so no injected
	// range touches a boundary, where a detector cannot see both sides.
	margin := spec.AnomalyLength
	lo := margin
	hi := usable - spec.AnomalyLength - margin
	if hi <= lo {
		err = eb.Build().Int32("length", n).Int32("anomalyLength", spec.AnomalyLength).
			Float64("tailExclusionFrac", spec.TailExclusionFrac).
			Errorf("no room to place anomalies clear of the boundaries and the excluded tail")
		return
	}

	// Spread placements over disjoint slots so anomalies cannot merge into one
	// long range, which would quietly change the anomaly count.
	slots := spec.AnomalyCount
	if (hi-lo)/slots < spec.AnomalyLength*2 {
		err = eb.Build().Int32("anomalyCount", spec.AnomalyCount).Int32("length", n).
			Errorf("too many anomalies for the usable span")
		return
	}

	rng := rand.New(rand.NewPCG(spec.Seed, spec.Seed^0x9e3779b97f4a7c15))

	// Build the clean background first, inject into it, then add noise to the
	// whole series. Noising afterwards keeps the noise character identical
	// inside and outside the injected segments, which matters: a change in noise
	// level is itself a one-liner-detectable anomaly.
	values := make([]float64, n)
	for i := range values {
		values[i] = background(float64(i), spec.Period)
	}
	pristine := make([]float64, n)
	copy(pristine, values)

	labels := make([]bool, n)
	slotWidth := (hi - lo) / slots
	for s := range slots {
		slotLo := lo + s*slotWidth
		jitter := int32(rng.IntN(int(slotWidth - spec.AnomalyLength)))
		start := slotLo + jitter
		injectAnomaly(values, pristine, start, spec.AnomalyLength, spec.Period, spec.Kind)
		for i := start; i < start+spec.AnomalyLength; i++ {
			labels[i] = true
		}
	}

	if spec.NoiseStdDev > 0.0 {
		for i := range values {
			values[i] += rng.NormFloat64() * spec.NoiseStdDev
		}
	}

	inst = &Fixture{
		Values: values,
		Labels: labels,
		Ranges: RangesFromLabels(labels),
	}
	return
}

// segStats returns a segment's mean and standard deviation.
func segStats(seg []float64) (mean float64, std float64) {
	for _, v := range seg {
		mean += v
	}
	mean /= float64(len(seg))
	var ss float64
	for _, v := range seg {
		d := v - mean
		ss += d * d
	}
	std = math.Sqrt(ss / float64(len(seg)))
	return
}

// shapeCorrelation is the Pearson correlation between two equal-length
// segments, 0 when either is flat.
func shapeCorrelation(a []float64, b []float64) (corr float64) {
	meanA, stdA := segStats(a)
	meanB, stdB := segStats(b)
	if stdA == 0.0 || stdB == 0.0 {
		return
	}
	var cov float64
	for i := range a {
		cov += (a[i] - meanA) * (b[i] - meanB)
	}
	cov /= float64(len(a))
	corr = cov / (stdA * stdB)
	return
}

// pickDonor finds the stretch of background that best impersonates the segment
// at start without resembling it.
//
// Donors come from the pristine background rather than the series being
// written, so an anomaly injected earlier in the same pass can never become the
// donor for a later one.
//
// A transplant is only a fair anomaly if it is locally indistinguishable from
// what it replaced — same level, same spread — because a moving-average
// residual keys on exactly those. It is only an anomaly at all if the waveform
// genuinely differs, which the correlation ceiling enforces. Minimising local
// statistic mismatch subject to that ceiling is what makes the result findable
// by a detector that compares against the whole series and invisible to one
// that looks only nearby.
func pickDonor(pristine []float64, start int32, length int32, period float64) (donor []float64) {
	recMean, recStd := segStats(pristine[start : start+length])
	minGap := int32(period * 3.0)
	limit := int32(len(pristine)) - length

	const maxResemblance = 0.3
	bestCost := math.Inf(1)
	bestAt := int32(-1)
	for cand := int32(0); cand <= limit; cand++ {
		gap := cand - start
		if gap < 0 {
			gap = -gap
		}
		if gap < minGap {
			continue
		}
		seg := pristine[cand : cand+length]
		if math.Abs(shapeCorrelation(pristine[start:start+length], seg)) > maxResemblance {
			continue
		}
		mean, std := segStats(seg)
		cost := math.Abs(mean-recMean) + math.Abs(std-recStd)
		if cost < bestCost {
			bestCost = cost
			bestAt = cand
		}
	}
	if bestAt < 0 {
		// Nothing sufficiently unlike the original; fall back to the far end of
		// the series rather than silently producing no anomaly.
		bestAt = (start + limit/2) % limit
	}
	donor = make([]float64, length)
	copy(donor, pristine[bestAt:bestAt+length])
	return
}

// pickPhaseOffset chooses how far to advance the oscillation, on the same
// principle as [pickDonor]: the shift whose local level and spread best match
// what it replaces, among those that change the waveform enough to count.
//
// A fixed half-period shift is the obvious choice and the wrong one. On a
// signal with more than one component it lands wherever it lands, and where
// that happens to change the local spread a moving-average residual finds it
// without knowing anything about phase.
func pickPhaseOffset(pristine []float64, start int32, length int32, period float64) (phase float64) {
	recMean, recStd := segStats(pristine[start : start+length])

	const steps = 512
	const maxResemblance = 0.3
	span := period * golden
	candidate := make([]float64, length)

	bestCost := math.Inf(1)
	phase = period / 2.0
	for s := 1; s <= steps; s++ {
		p := span * float64(s) / float64(steps)
		for i := range candidate {
			candidate[i] = background(float64(start+int32(i))+p, period)
		}
		if math.Abs(shapeCorrelation(pristine[start:start+length], candidate)) > maxResemblance {
			continue
		}
		mean, std := segStats(candidate)
		cost := math.Abs(mean-recMean) + math.Abs(std-recStd)
		if cost < bestCost {
			bestCost = cost
			phase = p
		}
	}
	return
}

// injectAnomaly rewrites clean[start:start+length] in place, cross-fading the
// replacement into the surrounding signal so neither edge leaves a step.
func injectAnomaly(clean []float64, pristine []float64, start int32, length int32, period float64, kind AnomalyKindE) {
	seg := clean[start : start+length]

	repl := make([]float64, length)
	switch kind {
	case AnomalyKindReversal:
		for i := range repl {
			repl[i] = seg[length-1-int32(i)]
		}
	case AnomalyKindTransplant:
		copy(repl, pickDonor(pristine, start, length, period))
	case AnomalyKindPhaseJump:
		phase := pickPhaseOffset(pristine, start, length, period)
		for i := range repl {
			repl[i] = background(float64(start+int32(i))+phase, period)
		}
	case AnomalyKindFrequencyShift:
		for i := range repl {
			repl[i] = background(float64(start)+float64(i)*1.7, period)
		}
	}

	fade := min(max(length/8, 2), length/2)
	for i := range length {
		w := 1.0
		if i < fade {
			w = smoothstep(float64(i) / float64(fade))
		} else if tail := length - 1 - i; tail < fade {
			w = smoothstep(float64(tail) / float64(fade))
		}
		seg[i] = (1.0-w)*seg[i] + w*repl[i]
	}
}

// BaselineE names one of the one-line heuristics Wu and Keogh used to show that
// the field's benchmark datasets were solvable without a detector.
type BaselineE uint8

const (
	// BaselineAbsValue scores each point by its distance from the series mean.
	BaselineAbsValue BaselineE = iota
	// BaselineFirstDifference scores by the magnitude of the change from the
	// previous point.
	BaselineFirstDifference
	// BaselineMovingAverageResidual scores by distance from a centred moving
	// average.
	BaselineMovingAverageResidual
)

// String returns the baseline's name.
func (inst BaselineE) String() (name string) {
	switch inst {
	case BaselineAbsValue:
		name = "abs-value"
	case BaselineFirstDifference:
		name = "first-difference"
	case BaselineMovingAverageResidual:
		name = "moving-average-residual"
	default:
		name = "unknown"
	}
	return
}

// AllBaselines lists every baseline, for sweeping.
var AllBaselines = [...]BaselineE{
	BaselineAbsValue,
	BaselineFirstDifference,
	BaselineMovingAverageResidual,
}

// BaselineScores computes a one-liner's per-position anomaly score. window is
// used only by [BaselineMovingAverageResidual].
func BaselineScores(values []float64, baseline BaselineE, window int32) (scores []float64) {
	n := len(values)
	scores = make([]float64, n)
	switch baseline {
	case BaselineAbsValue:
		var mean float64
		for _, v := range values {
			mean += v
		}
		mean /= float64(n)
		for i, v := range values {
			scores[i] = math.Abs(v - mean)
		}
	case BaselineFirstDifference:
		for i := 1; i < n; i++ {
			scores[i] = math.Abs(values[i] - values[i-1])
		}
	case BaselineMovingAverageResidual:
		if window < 2 {
			window = 2
		}
		half := int(window / 2)
		for i := range values {
			lo := max(i-half, 0)
			hi := min(i+half+1, n)
			var sum float64
			for _, v := range values[lo:hi] {
				sum += v
			}
			scores[i] = math.Abs(values[i] - sum/float64(hi-lo))
		}
	}
	return
}

// BaselineResult pairs a baseline with how well it did.
type BaselineResult struct {
	Baseline BaselineE
	Measures Measures
}

// TrivialityE scores every one-liner against a fixture and returns the results
// alongside the highest VUS-PR any of them reached.
//
// This is the check Wu and Keogh's paper implies but no benchmark performs: a
// fixture on which a one-liner scores well is measuring the wrong thing, and
// any detector validated against it is unvalidated. Treat a high worst value as
// a defect in the fixture, not a result.
//
// The bar is a judgement call, not a derivation — see [TrivialityThreshold].
func TrivialityE(fixture *Fixture, maxBuffer int32) (results []BaselineResult, worstVUSPR float64, err error) {
	window := int32(16)
	if fixture.Ranges.Len() > 0 {
		window = DefaultMaxBuffer(fixture.Ranges)
	}

	results = make([]BaselineResult, 0, len(AllBaselines))
	for _, b := range AllBaselines {
		var m Measures
		m, err = EvaluateE(BaselineScores(fixture.Values, b, window), fixture.Labels, maxBuffer)
		if err != nil {
			return
		}
		results = append(results, BaselineResult{Baseline: b, Measures: m})
		if m.VUSPR > worstVUSPR {
			worstVUSPR = m.VUSPR
		}
	}
	return
}

// TrivialityThreshold is the VUS-PR above which a one-liner counts as having
// solved a fixture.
//
// There is no principled value. This one is set well above the prevalence a
// random scorer reaches on the default spec and well below what a detector
// that genuinely finds the injected shape reaches, which leaves a wide band
// where the check says nothing. Prefer comparing a baseline against a real
// detector on the same fixture over reading this constant as a verdict.
const TrivialityThreshold = 0.30
