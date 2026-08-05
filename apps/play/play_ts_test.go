package play

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/analytics/timeseries/damp"
	"github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile"
	"github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_ts_test.go covers the ADR-0163 M1 verification plan: split
// classification and the terminal-leaf errors, the transforms checked against
// the substrate packages THROUGH the executor, the output-schema contracts,
// and the memo-key distinctness the acceptance review added.
//
// The transform tests deliberately never oracle a function with itself: what
// they check is the ADAPTER — that the right columns reach the right package
// argument and the right output column comes back — against the package
// called directly. The maths has its own tests in its own package, and a test
// here that reimplemented it would only be checking a copy.

// --- split classification ---------------------------------------------------

func TestSplitRecognizesClientCall(t *testing.T) {
	res, err := splitGraph(`WITH
  base AS (SELECT toDateTime64(now(), 3) AS t, 1.0 AS v),
  scored AS (SELECT tsAnomalyScores(t, v, 64) FROM base)
SELECT 1`)
	require.NoError(t, err)

	node, ok := findSplitNode(res, "scored")
	require.True(t, ok)
	require.NotNil(t, node.Client, "the CTE body is a client call")
	assert.Equal(t, "tsAnomalyScores", node.Client.Spec.Name)
	assert.Equal(t, []string{"t", "v", "64"}, node.Client.Args)
	assert.Equal(t, NodeID("base"), node.Client.Input)
	assert.True(t, node.Client.Spec.Causal, "left discords are causal by construction")

	base, ok := findSplitNode(res, "base")
	require.True(t, ok)
	assert.Nil(t, base.Client, "an ordinary CTE is not a client call")
}

// A param slot in an argument makes the analysis parameter a live signal at no
// cost: it is already a signal edge of the node.
func TestSplitClientCallSlotIsASignalEdge(t *testing.T) {
	res, err := splitGraph(`WITH
  base AS (SELECT toDateTime64(now(), 3) AS t, 1.0 AS v),
  scored AS (SELECT tsProfile(t, v, {win:UInt32}) FROM base)
SELECT 1`)
	require.NoError(t, err)
	node, ok := findSplitNode(res, "scored")
	require.True(t, ok)
	require.NotNil(t, node.Client)
	assert.Contains(t, node.Client.Slots, "{win:UInt32}")
	assert.Contains(t, node.Reads, SignalID("win"), "the slot is a signal edge already")
}

// The terminal-leaf rule (§SD4): nothing may read a client node, because its
// output never exists as SQL.
func TestSplitTerminalLeafErrors(t *testing.T) {
	t.Run("the sink reads it", func(t *testing.T) {
		_, err := splitGraph(`WITH
  base AS (SELECT toDateTime64(now(), 3) AS t, 1.0 AS v),
  scored AS (SELECT tsAnomalyScores(t, v, 64) FROM base)
SELECT * FROM scored`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "computed client-side")
		assert.Contains(t, err.Error(), "Bind a pane", "the error names the fix")
	})

	t.Run("a downstream CTE reads it", func(t *testing.T) {
		_, err := splitGraph(`WITH
  base AS (SELECT toDateTime64(now(), 3) AS t, 1.0 AS v),
  scored AS (SELECT tsAnomalyScores(t, v, 64) FROM base),
  worst AS (SELECT max(score) FROM scored)
SELECT 1`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "terminal leaf")
	})
}

// Recognition refuses everything a client body could carry but whose effect
// the transform would swallow, and every refusal says where the clause goes.
func TestSplitClientCallShapeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, sql, want string
	}{
		{
			name: "not the sole select item",
			sql:  "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT t, tsProfile(t, v, 8) FROM b) SELECT 1",
			want: "ONLY select item",
		},
		{
			name: "nested in an expression",
			sql:  "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v, 8) + 1 FROM b) SELECT 1",
			want: "ONLY select item",
		},
		{
			name: "wrong arity",
			sql:  "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v) FROM b) SELECT 1",
			want: "3 arguments, not 2",
		},
		{
			name: "an expression where a column belongs",
			sql:  "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v * 2, 8) FROM b) SELECT 1",
			want: "must be a column name",
		},
		{
			name: "a carried clause the transform would swallow",
			sql:  "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v, 8) FROM b WHERE v > 0) SELECT 1",
			want: "cannot carry WHERE",
		},
		{
			name: "reading a table rather than a CTE",
			sql:  "WITH c AS (SELECT tsProfile(t, v, 8) FROM system.numbers) SELECT 1",
			want: "must read a CTE of this query",
		},
		{
			name: "an unknown ts name never reaches the server",
			sql:  "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsWibble(t, v, 8) FROM b) SELECT 1",
			want: "not a function play knows",
		},
		{
			name: "a reserved name that is not shipped",
			sql:  "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsMotifs(t, v, 8) FROM b) SELECT 1",
			want: "not implemented yet",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := splitGraph(tc.sql)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// Case matters (§SD3): the buffer records the name verbatim, so two spellings
// of one recorded artifact would be two artifacts.
//
// A lower-case `tsprofile` is not merely a different registry key — it is
// outside the reserved family altogether (`ts` plus an UPPER-case letter), so
// play does not claim it at all. It stays an ordinary CTE and travels to the
// server, which is the honest outcome: play only speaks for names it reserved,
// and a family of bare `ts` would be a land grab over the server's vocabulary.
func TestSplitClientCallNamesAreExactCase(t *testing.T) {
	res, err := splitGraph("WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsprofile(t, v, 8) FROM b) SELECT 1")
	require.NoError(t, err)
	node, ok := findSplitNode(res, "c")
	require.True(t, ok)
	assert.Nil(t, node.Client, "a lower-case spelling is not play's name; the server answers for it")
}

// An ordinary buffer must be untouched by any of this.
func TestSplitLeavesOrdinaryQueriesAlone(t *testing.T) {
	res, err := splitGraph("WITH a AS (SELECT 1 AS x), b AS (SELECT x * 2 AS y FROM a) SELECT * FROM b")
	require.NoError(t, err)
	for _, n := range res.Nodes {
		assert.Nil(t, n.Client, "node %q", n.ID)
	}
}

// --- the memo key -----------------------------------------------------------

// The aliasing trap the acceptance review found (§SD4): two calls differing
// only in a literal argument fuse to the SAME input SQL, so without the call
// in the key the second demand reads the first's result.
func TestClientTransformIsPartOfTheMemoKey(t *testing.T) {
	base := compiledNode{SQL: "SELECT 1", Params: map[string]string{"param_a": "1"}}
	c64 := base
	c64.Client = &tsCall{Text: "tsProfile(t, v, 64)"}
	c128 := base
	c128.Client = &tsCall{Text: "tsProfile(t, v, 128)"}

	assert.NotEqual(t, c64.key(), c128.key(), "two windows over one input are two results")
	assert.NotEqual(t, base.key(), c64.key(), "a transform is not the absence of one")
	assert.Equal(t, c64.key(), compiledNode{
		SQL: base.SQL, Params: base.Params, Client: &tsCall{Text: "tsProfile(t, v, 64)"},
	}.key(), "the same call is the same key")
}

// --- the transforms, through the executor -----------------------------------

// tsTestInput builds an Arrow record shaped like an input CTE's result.
func tsTestInput(t *testing.T, vals []float64) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t", Type: tsTimeType()},
		{Name: "v", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	tb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	vb := array.NewFloat64Builder(alloc)
	defer tb.Release()
	defer vb.Release()
	for i, v := range vals {
		tb.Append(arrow.Timestamp(int64(i) * 60_000)) // a clean one-minute grid
		vb.Append(v)
	}
	ta, va := tb.NewArray(), vb.NewArray()
	defer ta.Release()
	defer va.Release()
	return array.NewRecordBatch(schema, []arrow.Array{ta, va}, int64(len(vals)))
}

func tsRunCall(t *testing.T, sql string, vals []float64) (rec arrow.RecordBatch, err error) {
	t.Helper()
	res, sErr := splitGraph(sql)
	require.NoError(t, sErr)
	node, ok := findSplitNode(res, "c")
	require.True(t, ok)
	require.NotNil(t, node.Client)
	in := tsTestInput(t, vals)
	defer in.Release()
	return node.Client.Apply(in, nil, memory.NewGoAllocator())
}

func tsFloatCol(t *testing.T, rec arrow.RecordBatch, name string) []float64 {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	require.Len(t, idx, 1, "column %q", name)
	arr, ok := rec.Column(idx[0]).(*array.Float64)
	require.True(t, ok)
	return arr.Float64Values()
}

func tsSine(n int, period float64) (out []float64) {
	out = make([]float64, n)
	for i := range out {
		out[i] = math.Sin(2 * math.Pi * float64(i) / period)
	}
	return
}

// The adapter, not the kernel: the same values through the transform and
// through mssmooth directly must agree exactly.
func TestTsSmoothMatchesTheKernel(t *testing.T) {
	vals := tsSine(400, 50)
	rec, err := tsRunCall(t, "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsSmooth(t, v, 12) FROM b) SELECT 1", vals)
	require.NoError(t, err)
	defer rec.Release()

	kernel, kErr := mssmooth.NewKernelE(tsSmoothDegree, 12)
	require.NoError(t, kErr)
	want, sErr := kernel.SmoothE(vals, nil)
	require.NoError(t, sErr)

	assert.Equal(t, int64(len(vals)), rec.NumRows(), "smoothing is length-preserving")
	assert.Equal(t, want, tsFloatCol(t, rec, "smooth"))
	assert.Equal(t, []string{"t", "smooth"}, fieldNames(rec.Schema()), "the declared output contract")
}

func TestTsProfileMatchesTheMatrixProfile(t *testing.T) {
	vals := tsSine(300, 40)
	rec, err := tsRunCall(t, "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsProfile(t, v, 32) FROM b) SELECT 1", vals)
	require.NoError(t, err)
	defer rec.Release()

	series, sErr := matrixprofile.NewSeriesE(vals, 32, matrixprofile.DefaultStdDevFloorRel)
	require.NoError(t, sErr)
	want := series.Compute()

	assert.Equal(t, int64(len(want.Distance)), rec.NumRows(), "one row per WINDOW, not per sample")
	assert.Equal(t, want.Distance, tsFloatCol(t, rec, "profile"))
	assert.Equal(t, []string{"t", "profile"}, fieldNames(rec.Schema()))
}

func TestTsAnomalyScoresMatchTheDetector(t *testing.T) {
	vals := tsSine(600, 50)
	vals[400] += 8 // something to find
	rec, err := tsRunCall(t, "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsAnomalyScores(t, v, 32) FROM b) SELECT 1", vals)
	require.NoError(t, err)
	defer rec.Release()

	det, dErr := damp.NewDetectorE(damp.Config{Window: 32, Exact: true})
	require.NoError(t, dErr)
	want := make([]float64, len(vals))
	for _, v := range vals {
		reading, ok := det.Push(v)
		if ok && reading.Centre >= 0 && reading.Centre < int64(len(vals)) {
			want[reading.Centre] = reading.Score
		}
	}
	assert.Equal(t, int64(len(vals)), rec.NumRows(), "one row per sample")
	assert.Equal(t, want, tsFloatCol(t, rec, "score"))
	assert.Equal(t, []string{"t", "score", "warm_up"}, fieldNames(rec.Schema()))

	// warm_up is the honest complement of the score column: every position
	// carrying no score is marked, so a flat zero cannot read as calm.
	idx := rec.Schema().FieldIndices("warm_up")
	warm := rec.Column(idx[0]).(*array.Boolean)
	scores := tsFloatCol(t, rec, "score")
	for i := range scores {
		if scores[i] == 0 {
			assert.True(t, warm.Value(i), "position %d has no score and must be marked", i)
		}
	}
	assert.True(t, warm.Value(0), "the training prefix is warm-up")
}

// The spans are checked against a fixture with GROUND TRUTH — the adscore
// generator, whose whole purpose is to avoid the four flaws that let a trivial
// detector look good. That is a genuinely independent oracle, not this code
// grading its own work.
func TestTsAnomalySpansCoverAPlantedAnomaly(t *testing.T) {
	spec := adscore.DefaultFixtureSpec(adscore.AnomalyKindTransplant, 42)
	spec.Length = 1500
	spec.AnomalyCount = 1
	fixture, err := adscore.GenerateE(spec)
	require.NoError(t, err)

	rec, aErr := tsRunCall(t,
		"WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsAnomalySpans(t, v, 64, 3) FROM b) SELECT 1",
		fixture.Values)
	require.NoError(t, aErr)
	defer rec.Release()

	require.Positive(t, rec.NumRows(), "a planted anomaly should be flagged")
	assert.Equal(t, []string{
		timelineSlotBandFrom, timelineSlotBandTo, timelineSlotBandLabel, timelineSlotBandColor, "score",
	}, fieldNames(rec.Schema()), "the Timeline band contract, emitted directly")

	// The extents are Timestamps and the colours are resolvable token NAMES.
	// Both are load-bearing: the band reader demands the first and silently
	// draws nothing for a colour it cannot resolve.
	fromIdx := rec.Schema().FieldIndices(timelineSlotBandFrom)
	_, isTs := rec.Column(fromIdx[0]).(*array.Timestamp)
	assert.True(t, isTs, "%s must be a Timestamp", timelineSlotBandFrom)
	colIdx := rec.Schema().FieldIndices(timelineSlotBandColor)
	colors := rec.Column(colIdx[0]).(*array.String)
	for i := range colors.Len() {
		_, ok := bandColorByName(colors.Value(i))
		assert.True(t, ok, "colour %q must resolve in the band vocabulary", colors.Value(i))
	}

	// At least one reported extent overlaps a labelled anomaly. Spans are
	// index ranges over a one-minute grid, so a timestamp maps back by /60000.
	toIdx := rec.Schema().FieldIndices(timelineSlotBandTo)
	fromArr := rec.Column(fromIdx[0]).(*array.Timestamp)
	toArr := rec.Column(toIdx[0]).(*array.Timestamp)
	var covered bool
	for i := range int(rec.NumRows()) {
		lo := int(int64(fromArr.Value(i)) / 60_000)
		hi := int(int64(toArr.Value(i)) / 60_000)
		for p := lo; p <= hi && p < len(fixture.Labels); p++ {
			if fixture.Labels[p] {
				covered = true
			}
		}
	}
	assert.True(t, covered, "no reported extent overlaps the planted anomaly")
}

// A span is an EXTENT, not an argmax (§SD5): reporting the peak alone would
// understate an anomaly that lasted.
func TestTsAnomalySpansAreExtents(t *testing.T) {
	vals := tsSine(800, 50)
	for i := 500; i < 540; i++ {
		vals[i] += 6 // a sustained excursion, not a single spike
	}
	rec, err := tsRunCall(t,
		"WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT tsAnomalySpans(t, v, 32, 1) FROM b) SELECT 1", vals)
	require.NoError(t, err)
	defer rec.Release()
	require.Positive(t, rec.NumRows())

	fromIdx := rec.Schema().FieldIndices(timelineSlotBandFrom)
	toIdx := rec.Schema().FieldIndices(timelineSlotBandTo)
	from := int64(rec.Column(fromIdx[0]).(*array.Timestamp).Value(0))
	to := int64(rec.Column(toIdx[0]).(*array.Timestamp).Value(0))
	assert.Greater(t, to, from, "a span has width")
}

// --- the ceiling and the argument errors ------------------------------------

func TestTsCallRefusesPastTheCeiling(t *testing.T) {
	call := &tsCall{Spec: tsFuncSpec{Name: "tsProfile", MaxLen: 4,
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"window", tsArgInt}}},
		Args: []string{"t", "v", "2"}}
	in := tsTestInput(t, []float64{1, 2, 3, 4, 5})
	defer in.Release()
	_, err := call.Apply(in, nil, memory.NewGoAllocator())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "4-row ceiling", "the refusal names the limit")
}

// A null in the value column ends the read with a reason. Skipping it would
// close a gap that is really there — the one thing this feature refuses to do.
func TestTsCallRefusesNulls(t *testing.T) {
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t", Type: tsTimeType()},
		{Name: "v", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	}, nil)
	tb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	vb := array.NewFloat64Builder(alloc)
	defer tb.Release()
	defer vb.Release()
	for i := range 10 {
		tb.Append(arrow.Timestamp(int64(i) * 60_000))
		if i == 5 {
			vb.AppendNull()
			continue
		}
		vb.Append(1)
	}
	ta, va := tb.NewArray(), vb.NewArray()
	defer ta.Release()
	defer va.Release()
	in := array.NewRecordBatch(schema, []arrow.Array{ta, va}, 10)
	defer in.Release()

	call := &tsCall{Spec: tsFuncSpec{Name: "tsSmooth",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"halfWidth", tsArgInt}}},
		Args: []string{"t", "v", "2"}}
	_, err := call.Apply(in, nil, memory.NewGoAllocator())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NULL")
	assert.Contains(t, err.Error(), "input CTE", "the fix belongs upstream, where it is recorded")
}

// An unbound slot is a reason, not a zero.
func TestTsIntArgNeedsABoundSlot(t *testing.T) {
	call := &tsCall{Spec: tsFuncSpec{Name: "tsProfile",
		Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"window", tsArgInt}}},
		Args: []string{"t", "v", "{win:UInt32}"}}
	_, err := tsIntArg(call, 2, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no value")

	got, bErr := tsIntArg(call, 2, map[string]string{"param_win": "48"})
	require.NoError(t, bErr)
	assert.Equal(t, int32(48), got)
}

// --- the registry ------------------------------------------------------------

func TestTsRegistryContracts(t *testing.T) {
	for _, spec := range tsFuncs {
		t.Run(spec.Name, func(t *testing.T) {
			assert.True(t, tsIsReservedName(spec.Name), "every registry name is in the reserved family")
			assert.NotEmpty(t, spec.Doc, "a name a user meets in a buffer needs a sentence")
			if !spec.Shipped {
				assert.Empty(t, spec.Out, "an unshipped name declares no contract")
				return
			}
			require.NotEmpty(t, spec.Out)
			assert.Positive(t, spec.MaxLen, "a shipped transform names its ceiling")
			schema := tsOutputSchema(spec)
			assert.Equal(t, len(spec.Out), schema.NumFields())
		})
	}
}

func TestTsReservedFamily(t *testing.T) {
	for _, name := range []string{"tsSmooth", "tsProfile", "tsWibble", "tsX"} {
		assert.True(t, tsIsReservedName(name), name)
	}
	// The family must not swallow the server's own vocabulary: `ts` alone
	// would be a land grab over toString, tsv-ish names and more.
	for _, name := range []string{"toString", "tsv", "ts", "tsprofile", "count"} {
		assert.False(t, tsIsReservedName(name), name)
	}
}
