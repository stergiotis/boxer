package play

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
)

// play_ts_registry.go is the ADR-0163 §SD3 vocabulary: the `ts*` functions a
// buffer may spell, which play executes CLIENT-SIDE rather than sending to
// ClickHouse. The registry is the single place a name, its arity, its output
// contract and its causality are stated; recognition (play_ts_recognize.go),
// execution (play_ts_executor.go) and the honesty chrome all read it rather
// than restating any of it.
//
// The family is RESERVED as a whole, including names nothing implements yet
// (the motif set, held for its own ADR): a reserved name that is not shipped
// must refuse loudly rather than travel to the server, where it would fail as
// "unknown function" and read as a server problem.
//
// One algorithm per name, always. A variant arrives as a NEW name — never as
// a flag on an existing one — because these names are recorded in buffers,
// history and pins, and a name whose meaning drifts silently falsifies every
// artifact that already carries it (the ADR-0162 discipline).

// tsArgKindE is what one argument position accepts. The set is deliberately
// tiny: a client call's arguments must be readable without evaluating SQL,
// since nothing evaluates them before the transform runs.
type tsArgKindE uint8

const (
	// tsArgColumn is a bare column identifier of the input CTE.
	tsArgColumn tsArgKindE = iota
	// tsArgInt is an integer literal, or a `{name:Type}` param slot — which
	// is what makes an analysis parameter a live signal for free (the slot
	// already lands in splitNode.Reads).
	tsArgInt
	// tsArgNumber is a non-negative decimal literal, or a `{name:Type}` slot.
	// It exists for the parameters that are a scale rather than a count — a
	// penalty is the first — where rounding to a whole number would take the
	// useful part of the range away.
	tsArgNumber
)

func (inst tsArgKindE) String() (name string) {
	switch inst {
	case tsArgColumn:
		return "a column name"
	case tsArgNumber:
		return "a number or a {name:Type} slot"
	}
	return "an integer or a {name:Type} slot"
}

// tsArgSpec is one declared parameter of a `ts*` function.
type tsArgSpec struct {
	Name string
	Kind tsArgKindE
}

// tsOutputCol is one column of a function's output contract. The names are
// the CONSUMING contract's names, not private ones: under the terminal-leaf
// rule no downstream SQL can rename them, so a function that feeds Timeline
// emits `_tl_band_*` itself (§SD3).
type tsOutputCol struct {
	Name string
	Type arrow.DataType
}

// tsFuncSpec declares one member of the vocabulary.
type tsFuncSpec struct {
	Name string
	Args []tsArgSpec
	Out  []tsOutputCol
	// Causal marks a function whose value at t is computed from t and
	// earlier only. It drives the S1 lane labelling — a two-sided score
	// drawn as if it were an alert history is the specific dishonesty the
	// ADR's scientific commitments exist to prevent.
	Causal bool
	// MaxLen refuses an input longer than the algorithm's published
	// practical ceiling, naming the limit. It belongs HERE and not on the
	// Series claim: the ceiling is matrixprofile's, an analysis constraint,
	// and capping the display lane with it would cross the one-way
	// analysis→display contract (ADR-0163 §SD2, corrected at acceptance).
	MaxLen int32
	// Shipped is false for a name the family reserves but does not
	// implement. Reserved-but-unshipped refuses with its own reason.
	Shipped bool
	// Doc is the one-line description the chrome and the collision warning
	// show. Written for someone who has just met the name in a buffer.
	Doc string
	// NoBaseline, when set, is why the Series pane draws no moving-average
	// residual beside this function's scores. The baseline is mandated chrome
	// for an anomaly score (§SD5 S3); where it would answer a different
	// question than the score does, the pane says that instead of drawing a
	// comparison that is not one.
	NoBaseline string
	// Threshold, when set, says what the output's `threshold` column is: the
	// level the method itself supplies, and the condition it holds under. The
	// Series pane draws the level with this text; a function without one gets
	// no line (the survey's S4 — a threshold the method does not supply is
	// not drawn).
	Threshold string
	// Bands says what the rows of a band-contract output are, which is what
	// the Series pane's inventory line and its adjudication row read.
	Bands tsBandKindE
}

// tsBandKindE is what one row of a band-contract output stands for.
type tsBandKindE uint8

const (
	// tsBandNone is a function that emits no bands.
	tsBandNone tsBandKindE = iota
	// tsBandExtent is a flagged stretch: something happened here, and a
	// person can confirm it or call it a false alarm.
	tsBandExtent
	// tsBandSegment is one piece of a segmentation. The pieces tile the
	// series, so no piece is a finding on its own — the boundaries are.
	tsBandSegment
	// tsBandAlarm runs from where a sequential detector places a change to
	// where it raised the alarm; its width is the detection delay.
	tsBandAlarm
)

// String names the kind as get_series reports it.
func (inst tsBandKindE) String() (name string) {
	switch inst {
	case tsBandExtent:
		name = "flagged extent"
	case tsBandSegment:
		name = "segment"
	case tsBandAlarm:
		name = "alarm"
	default:
		name = "none"
	}
	return
}

// tsProfileMaxLen is the published practical ceiling of the z-normalised
// matrix profile (public/analytics/timeseries/matrixprofile, doc.go). The
// algorithm is O(n²) in the number of windows; past this the wait stops being
// a wait and becomes a hang, so the refusal names the limit rather than
// letting a lane spin.
const tsProfileMaxLen int32 = 500_000

// The change point ceilings (ADR-0163 Update 2026-11-11), each measured on the
// case that is slowest for its method rather than a typical one.
//
// PELT is linear while changes keep being found and quadratic over a stretch
// with none, which is also the most ordinary input there is. The ceilings are
// where that worst case stays a wait of a few seconds; the mean-and-variance
// cost takes a logarithm per candidate and reaches it sooner. The penalty
// path runs PELT some tens of times. The remaining methods are linear, the
// run-length posterior with a constant that grows with the expected run.
const (
	tsPeltMaxLen        int32 = 50_000
	tsPeltMeanVarMaxLen int32 = 25_000
	tsPeltPathMaxLen    int32 = 10_000
	tsBocpdMaxLen       int32 = 200_000
	tsChangeLinearMax   int32 = 5_000_000
)

// tsSegmentOut is the output every segmentation shares: the Timeline band
// contract, so the segments draw on the Series pane and the Timeline as they
// are, and beside it the numbers a band cannot carry.
func tsSegmentOut() []tsOutputCol {
	return []tsOutputCol{
		{timelineSlotBandFrom, tsTimeType()},
		{timelineSlotBandTo, tsTimeType()},
		{timelineSlotBandLabel, arrow.BinaryTypes.String},
		{timelineSlotBandColor, arrow.BinaryTypes.String},
		{"level", arrow.PrimitiveTypes.Float64},
		{"spread", arrow.PrimitiveTypes.Float64},
		{"samples", arrow.PrimitiveTypes.Int64},
		{"shift", arrow.PrimitiveTypes.Float64},
	}
}

// tsSegmentArgs is the argument list the penalised segmentations share.
func tsSegmentArgs() []tsArgSpec {
	return []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"minSegment", tsArgInt}, {"penalty", tsArgNumber}}
}

const (
	tsDocSegmentTail   = " `penalty` is in multiples of the BIC penalty: 1 is the convention, larger reports fewer changes. Two-sided, and it assumes independent noise within a segment — autocorrelation and trends come back as changes that are not there. Read the count against `tsPeltMeanPath`."
	tsNoBaselineChange = "a moving-average residual asks whether a sample is unlike its neighbours, which a lasting shift stops being after one window; it is not a baseline for a change score"
)

// tsSmoothMaxLen bounds the linear-time smoother far more loosely: it is a
// convolution, so the ceiling is about the wire and the plot, not the maths.
const tsSmoothMaxLen int32 = 5_000_000

// tsFuncs is the v1 roster (§SD3). Order is presentation order.
var tsFuncs = []tsFuncSpec{
	{
		Name: "tsSmooth",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"halfWidth", tsArgInt}},
		Out: []tsOutputCol{
			{"t", tsTimeType()},
			{"smooth", arrow.PrimitiveTypes.Float64},
		},
		Causal: false, MaxLen: tsSmoothMaxLen, Shipped: true,
		Doc: "modified-sinc smoothing, degree 4 (ADR-0152). Zero-phase, so it is for CONDITIONING an analysis, not for display — the Series tab's own smoothing is the display one.",
	},
	{
		Name: "tsProfile",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"window", tsArgInt}},
		Out: []tsOutputCol{
			{"t", tsTimeType()},
			{"profile", arrow.PrimitiveTypes.Float64},
		},
		Causal: false, MaxLen: tsProfileMaxLen, Shipped: true,
		Doc: "z-normalised matrix profile: the distance from each window to its nearest other window. Two-sided — it sees the whole series, so it is not what an alert would have known.",
	},
	{
		Name: "tsAnomalyScores",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"window", tsArgInt}},
		Out: []tsOutputCol{
			{"t", tsTimeType()},
			{"score", arrow.PrimitiveTypes.Float64},
			{"warm_up", arrow.FixedWidthTypes.Boolean},
		},
		Causal: true, MaxLen: tsProfileMaxLen, Shipped: true,
		Doc: "left-discord scores in DAMP's exact mode: each value uses only what came before it, so replaying them IS the backtest. `warm_up` marks the prefix with too little history to judge.",
	},
	{
		Name: "tsAnomalySpans",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"window", tsArgInt}, {"k", tsArgInt}},
		Out: []tsOutputCol{
			{timelineSlotBandFrom, tsTimeType()},
			{timelineSlotBandTo, tsTimeType()},
			{timelineSlotBandLabel, arrow.BinaryTypes.String},
			{timelineSlotBandColor, arrow.BinaryTypes.String},
			{"score", arrow.PrimitiveTypes.Float64},
		},
		Causal: true, MaxLen: tsProfileMaxLen, Shipped: true, Bands: tsBandExtent,
		Doc: "the top-k flagged EXTENTS, as Timeline background bands. Plateaus, not argmaxes: a discord is a stretch of time, and reporting its peak alone would understate what was anomalous.",
	},

	// The change point family (ADR-0163 Update 2026-11-11), over
	// public/analytics/timeseries/changepoint. Offline segmentations first,
	// then the sequential detectors.
	{
		Name: "tsPeltMean", Args: tsSegmentArgs(), Out: tsSegmentOut(),
		Causal: false, MaxLen: tsPeltMaxLen, Shipped: true, Bands: tsBandSegment,
		Doc: "segments between shifts in LEVEL, by PELT: the exact optimum of a penalised Gaussian mean cost, with the noise scale estimated from the first differences." + tsDocSegmentTail,
	},
	{
		Name: "tsPeltMeanVar", Args: tsSegmentArgs(), Out: tsSegmentOut(),
		Causal: false, MaxLen: tsPeltMeanVarMaxLen, Shipped: true, Bands: tsBandSegment,
		Doc: "segments between shifts in level OR spread, by PELT with a Gaussian mean-and-variance cost. Give `minSegment` room (tens of samples): a variance fitted to a handful of samples that happen to agree looks like a regime." + tsDocSegmentTail,
	},
	{
		Name: "tsPeltRank", Args: tsSegmentArgs(), Out: tsSegmentOut(),
		Causal: false, MaxLen: tsPeltMaxLen, Shipped: true, Bands: tsBandSegment,
		Doc: "segments between shifts in LOCATION, by PELT on the ranks of the values: outliers and skew do not move it, and a change in spread around an unchanged median is invisible to it. `level` and `spread` describe each segment; they are not what was fitted." + tsDocSegmentTail,
	},
	{
		Name: "tsBinSegMean", Args: tsSegmentArgs(), Out: tsSegmentOut(),
		Causal: false, MaxLen: tsChangeLinearMax, Shipped: true, Bands: tsBandSegment,
		Doc: "segments between shifts in level, by binary segmentation: greedy and approximate where `tsPeltMean` is exact, so a short excursion can hide between two splits — and fast on series PELT refuses." + tsDocSegmentTail,
	},
	{
		Name: "tsPeltMeanPath",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"minSegment", tsArgInt}},
		Out: []tsOutputCol{
			{"changes", arrow.PrimitiveTypes.Int64},
			{"penalty_from", arrow.PrimitiveTypes.Float64},
			{"penalty_to", arrow.PrimitiveTypes.Float64},
			{"octaves", arrow.PrimitiveTypes.Float64},
			{"cost", arrow.PrimitiveTypes.Float64},
			{"complete", arrow.FixedWidthTypes.Boolean},
		},
		Causal: false, MaxLen: tsPeltPathMaxLen, Shipped: true,
		Doc: "the penalty sensitivity of `tsPeltMean` (CROPS): one row per change count that is optimal for some penalty between 1/2 and 64 times BIC, with the range it holds over and that range's width in doublings. A count that holds over a wide range is the data's; one that moves with every step is the penalty's. Not a series — bind it to a Table.",
	},
	{
		Name: "tsMosumScores",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"bandwidth", tsArgInt}},
		Out: []tsOutputCol{
			{"t", tsTimeType()},
			{"score", arrow.PrimitiveTypes.Float64},
			{"warm_up", arrow.FixedWidthTypes.Boolean},
			{"threshold", arrow.PrimitiveTypes.Float64},
		},
		Causal: false, MaxLen: tsChangeLinearMax, Shipped: true,
		NoBaseline: tsNoBaselineChange,
		Threshold:  "the MOSUM critical value at 5%: exceeded somewhere with that probability in a series of independent noise with no change. It is asymptotic, and too low for autocorrelated noise",
		Doc:        "the MOSUM statistic: at each sample, the mean of the `bandwidth` samples before against the mean of the `bandwidth` after, in units of its standard error. Two-sided. `warm_up` marks the ends, where a window does not fit.",
	},
	{
		Name:   "tsMosumSegments",
		Args:   []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"bandwidth", tsArgInt}},
		Out:    tsSegmentOut(),
		Causal: false, MaxLen: tsChangeLinearMax, Shipped: true, Bands: tsBandSegment,
		Doc: "segments between the changes the MOSUM test reports at 5%: the positions where `tsMosumScores` exceeds its threshold and is the largest within 0.4 bandwidths. Two changes closer than a bandwidth blur into one. Two-sided; the level assumes independent noise.",
	},
	{
		Name: "tsCusumScores",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"warmup", tsArgInt}},
		Out: []tsOutputCol{
			{"t", tsTimeType()},
			{"score", arrow.PrimitiveTypes.Float64},
			{"warm_up", arrow.FixedWidthTypes.Boolean},
			{"threshold", arrow.PrimitiveTypes.Float64},
			{"upper", arrow.PrimitiveTypes.Float64},
			{"lower", arrow.PrimitiveTypes.Float64},
		},
		Causal: true, MaxLen: tsChangeLinearMax, Shipped: true,
		NoBaseline: tsNoBaselineChange,
		Threshold:  "the CUSUM decision interval h = 5σ: with the allowance of 0.5σ, about one false alarm per 465 samples of independent Gaussian noise whose mean and scale are KNOWN. They are estimated from the warm-up here, so false alarms are more frequent than that",
		Doc:        "Page's two-sided CUSUM chart: the larger of the upward and downward sums, in standard deviations of the first `warmup` samples. Causal — what a monitor would have shown. After an alarm the chart re-estimates over another `warmup` samples and is blind meanwhile (`warm_up`).",
	},
	{
		Name: "tsCusumAlarms",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"warmup", tsArgInt}},
		Out: []tsOutputCol{
			{timelineSlotBandFrom, tsTimeType()},
			{timelineSlotBandTo, tsTimeType()},
			{timelineSlotBandLabel, arrow.BinaryTypes.String},
			{timelineSlotBandColor, arrow.BinaryTypes.String},
			{"direction", arrow.BinaryTypes.String},
			{"delay", arrow.PrimitiveTypes.Int64},
		},
		Causal: true, MaxLen: tsChangeLinearMax, Shipped: true, Bands: tsBandAlarm,
		Doc: "the alarms of `tsCusumScores`, as bands from where the chart places the change to where it raised the alarm — the band's width is the detection delay, also in `delay` as samples. Causal.",
	},
	{
		Name: "tsBocpdScores",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"expectedRun", tsArgInt}, {"lag", tsArgInt}},
		Out: []tsOutputCol{
			{"t", tsTimeType()},
			{"score", arrow.PrimitiveTypes.Float64},
			{"warm_up", arrow.FixedWidthTypes.Boolean},
			{"run", arrow.PrimitiveTypes.Int64},
		},
		Causal: true, MaxLen: tsBocpdMaxLen, Shipped: true,
		NoBaseline: tsNoBaselineChange,
		Doc:        "Bayesian online change point detection: the posterior probability, given the samples so far, that the current regime began within the last `lag` samples; `run` is the most probable regime length. A probability under the model — Gaussian regimes, independent samples, a change every `expectedRun` samples on average — and only as good as that model. Causal. The prior is taken from the first 50 samples, which are not scored.",
	},

	// Reserved, not shipped. The motif vocabulary waits for the motif-set
	// ADR: the pair-plus-radius promotion these names would carry has a
	// known weakness, and shipping it would write that weakness into
	// buffers, history and pins — artifacts that outlive the shortcut
	// (ADR-0163 Q4).
	{Name: "tsMotifs", Shipped: false, Doc: "reserved for the motif-set ADR."},
	{Name: "tsMotifSpans", Shipped: false, Doc: "reserved for the motif-set ADR."},
}

// tsTimeType is the Arrow type every `ts*` time column carries. Millisecond
// UTC, matching what the Timeline band reader demands of `_tl_band_from` /
// `_tl_band_to` and what a DateTime64(3) arrives as, so a result flows into
// the existing contracts without a cast.
func tsTimeType() arrow.DataType {
	return &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}
}

// tsFuncByName resolves a name EXACTLY (§SD3). Case-insensitivity would be
// wrong here even though ClickHouse's own function lookup is loose about it:
// these names are play's, the buffer records them verbatim, and `tssmooth`
// resolving to `tsSmooth` would make two spellings of one recorded artifact.
func tsFuncByName(name string) (spec tsFuncSpec, found bool) {
	for i := range tsFuncs {
		if tsFuncs[i].Name == name {
			return tsFuncs[i], true
		}
	}
	return
}

// tsIsReservedName reports whether a name belongs to the reserved family —
// the `ts` prefix followed by an upper-case letter. This is what makes an
// unshipped or misspelled `ts*` refuse INSIDE play instead of travelling to
// the server, where "unknown function tsProfile" would read as the server's
// problem rather than as a name play owns.
//
// The upper-case requirement keeps the family from swallowing real server
// functions: ClickHouse has `tsv`-ish and `toString`-ish names in quantity,
// and `ts` alone would be a land grab.
func tsIsReservedName(name string) bool {
	if len(name) < 3 || !strings.HasPrefix(name, "ts") {
		return false
	}
	c := name[2]
	return c >= 'A' && c <= 'Z'
}

// tsOutputSchema builds the Arrow schema a function's result carries. Nullable
// false throughout: a transform that cannot produce a value for a row does not
// emit the row (the warm-up prefix is MARKED, not nulled), so a null here
// would mean a bug rather than missing data.
func tsOutputSchema(spec tsFuncSpec) *arrow.Schema {
	fields := make([]arrow.Field, len(spec.Out))
	for i, o := range spec.Out {
		fields[i] = arrow.Field{Name: o.Name, Type: o.Type, Nullable: false}
	}
	return arrow.NewSchema(fields, nil)
}
