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
)

func (inst tsArgKindE) String() (name string) {
	if inst == tsArgColumn {
		return "a column name"
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
}

// tsProfileMaxLen is the published practical ceiling of the z-normalised
// matrix profile (public/analytics/timeseries/matrixprofile, doc.go). The
// algorithm is O(n²) in the number of windows; past this the wait stops being
// a wait and becomes a hang, so the refusal names the limit rather than
// letting a lane spin.
const tsProfileMaxLen int32 = 500_000

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
		Causal: true, MaxLen: tsProfileMaxLen, Shipped: true,
		Doc: "the top-k flagged EXTENTS, as Timeline background bands. Plateaus, not argmaxes: a discord is a stretch of time, and reporting its peak alone would understate what was anomalous.",
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
