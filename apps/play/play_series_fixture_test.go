package play

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_series_fixture_test.go covers ADR-0163 M4. The claim under test is
// that a fixture is ORDINARY DATA: its two tables must satisfy the same
// contracts the workbench applies to anything else, because the whole value
// of the lab is that nothing downstream knows it is synthetic.

func decodeStream(t *testing.T, raw []byte) arrow.RecordBatch {
	t.Helper()
	r, err := ipc.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	t.Cleanup(r.Release)
	require.True(t, r.Next())
	rec := r.RecordBatch()
	rec.Retain()
	t.Cleanup(rec.Release)
	return rec
}

// The generator is deterministic in (kind, seed) — which is what makes a
// fixture something two people can talk about rather than something one of
// them saw once.
func TestFixtureIsDeterministic(t *testing.T) {
	a, err := generateFixture(fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 7})
	require.NoError(t, err)
	b, err := generateFixture(fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 7})
	require.NoError(t, err)
	assert.Equal(t, a.Values, b.Values)
	assert.Equal(t, a.Labels, b.Labels)

	other, err := generateFixture(fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 8})
	require.NoError(t, err)
	assert.NotEqual(t, a.Values, other.Values, "a different seed is a different fixture")
}

// The published series must satisfy the Series tab's TYPED claim — the same
// resolution any other result goes through. A fixture that published an
// unusable time axis would fail in the one place it exists to work.
func TestFixtureSeriesSatisfiesTheSeriesClaim(t *testing.T) {
	fixture, err := generateFixture(fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 1})
	require.NoError(t, err)
	raw, err := encodeFixtureSeries(fixture, memory.NewGoAllocator())
	require.NoError(t, err)
	rec := decodeStream(t, raw)

	k, reason := resolveSeriesColumns(rec.Schema())
	require.Empty(t, reason, "the fixture must be chartable with no special case")
	assert.Equal(t, 0, k.tCol)
	assert.Equal(t, []int{1}, k.vCols)
	assert.Equal(t, int64(len(fixture.Values)), rec.NumRows())
}

// And it must satisfy the ts* transforms' input contract, since running the
// vocabulary on it is the whole point.
func TestFixtureSeriesFeedsTheVocabulary(t *testing.T) {
	fixture, err := generateFixture(fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 3})
	require.NoError(t, err)
	raw, err := encodeFixtureSeries(fixture, memory.NewGoAllocator())
	require.NoError(t, err)
	rec := decodeStream(t, raw)

	call := &tsCall{
		Spec: tsFuncSpec{Name: "tsAnomalyScores", Causal: true, MaxLen: tsProfileMaxLen,
			Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"window", tsArgInt}},
			Out: []tsOutputCol{{"t", tsTimeType()}, {"score", arrow.PrimitiveTypes.Float64},
				{"warm_up", arrow.FixedWidthTypes.Boolean}}},
		Args: []string{"t", "v", "64"},
	}
	out, err := call.Apply(rec, nil, memory.NewGoAllocator())
	require.NoError(t, err)
	defer out.Release()
	assert.Equal(t, rec.NumRows(), out.NumRows())
}

// The truth table speaks the Timeline band contract, so ground truth draws as
// bands with no translation — the same contract tsAnomalySpans emits, which
// is what lets the two pictures sit side by side.
func TestFixtureTruthSpeaksTheBandContract(t *testing.T) {
	fixture, err := generateFixture(fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 5})
	require.NoError(t, err)
	raw, err := encodeFixtureTruth(fixture, adscore.AnomalyKindTransplant, memory.NewGoAllocator())
	require.NoError(t, err)
	rec := decodeStream(t, raw)

	_, reason := acceptSeriesSpans(rec.Schema())
	assert.Empty(t, reason, "the truth table fills the span channel unchanged")

	spans, skipped := foldSeriesSpans(rec)
	assert.Zero(t, skipped, "every planted extent must resolve — colour token included")
	require.NotEmpty(t, spans)
	assert.Equal(t, int(rec.NumRows()), len(spans))
	for _, s := range spans {
		assert.GreaterOrEqual(t, s.to, s.from)
	}
}

// The extents must be the planted RUNS, not one band per anomalous sample.
func TestFixtureTruthRunsCollapseToExtents(t *testing.T) {
	assert.Equal(t, [][2]int{{2, 4}, {7, 7}},
		fixtureTruthRuns([]bool{false, false, true, true, true, false, false, true}))

	fixture, err := generateFixture(fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 11})
	require.NoError(t, err)
	runs := fixtureTruthRuns(fixture.Labels)
	require.NotEmpty(t, runs)
	var covered int
	for _, r := range runs {
		covered += r[1] - r[0] + 1
	}
	var labelled int
	for _, l := range fixture.Labels {
		if l {
			labelled++
		}
	}
	assert.Equal(t, labelled, covered, "the runs cover exactly the labelled positions")
	assert.Less(t, len(runs), labelled, "extents, not one band per sample")
}

// The time axis is a fixed epoch on a fixed step: a fixture whose timestamps
// moved every run would give the same data two identities, which the M3
// labels — keyed on the compiled input — would read as two different series.
func TestFixtureTimeAxisIsStable(t *testing.T) {
	assert.Equal(t, fixtureEpoch.UnixMilli(), fixtureSampleMS(0))
	assert.Equal(t, fixtureEpoch.UnixMilli()+fixtureStepSec*1000, fixtureSampleMS(1))
	assert.Equal(t, fixtureEpoch.UnixMilli()+10*fixtureStepSec*1000, fixtureSampleMS(10))
}

// The scaffold must be the whole workbench on the fixture, and must name the
// aliases it published under — a scaffold that referenced anything else would
// send the reader looking for a table that does not exist.
func TestFixtureScaffoldNamesThePublishedAliases(t *testing.T) {
	sql := fixtureScaffold()
	assert.Contains(t, sql, "keelson('"+fixtureSeriesAlias+"')")
	assert.Contains(t, sql, "keelson('"+fixtureTruthAlias+"')")
	assert.Contains(t, sql, "tsAnomalyScores")
	assert.Contains(t, sql, "tsAnomalySpans")

	// And it must be a buffer the split accepts, with the client nodes named
	// so the panel's channels actually fill.
	res, err := splitGraph(strippedComments(sql))
	require.NoError(t, err, "the scaffold must split cleanly")
	for _, id := range []NodeID{seriesScoresNodeID, seriesSpansNodeID} {
		node, ok := findSplitNode(res, id)
		require.True(t, ok, "the scaffold names a %q CTE", id)
		require.NotNil(t, node.Client, "%q is a client node", id)
	}
}

// strippedComments drops the scaffold's trailing comment lines, which carry a
// second query as prose rather than as SQL.
func strippedComments(sql string) (out string) {
	for line := range splitLinesSeq(sql) {
		if len(line) >= 2 && line[0] == '-' && line[1] == '-' {
			continue
		}
		out += line + "\n"
	}
	return
}

func splitLinesSeq(s string) func(func(string) bool) {
	return func(yield func(string) bool) {
		start := 0
		for i := 0; i <= len(s); i++ {
			if i == len(s) || s[i] == '\n' {
				if !yield(s[start:i]) {
					return
				}
				start = i + 1
			}
		}
	}
}

// --- the publish round ------------------------------------------------------

// recordingBus captures the publish requests and refuses them. Refusing is
// the point: the capability's reply encoding is its own, and reproducing it
// here would couple this test to internals it has no business knowing. What
// this file owns is what it ASKS for — the right subject, a decodable Arrow
// stream, and an error that names which dataset failed — and that is what is
// checked. The round against a real service is integration-lane work: it
// needs an encrypted store and a key custodian.
type recordingBus struct {
	subjects []string
	payloads [][]byte
}

func (inst *recordingBus) Publish(subject string, payload []byte) error { return nil }

func (inst *recordingBus) Subscribe(subject string, h app.MsgHandlerFunc) (func(), error) {
	return func() {}, nil
}

func (inst *recordingBus) Request(subject string, payload []byte) (reply []byte, err error) {
	inst.subjects = append(inst.subjects, subject)
	inst.payloads = append(inst.payloads, payload)
	return nil, eh.Errorf("no capability in this lane")
}

func TestPublishFixtureAsksTheCapability(t *testing.T) {
	bus := &recordingBus{}
	_, err := doPublishFixture(bus, fixtureSpec{kind: adscore.AnomalyKindTransplant, seed: 2})

	require.Error(t, err, "a refused publish is reported, never swallowed")
	assert.Contains(t, err.Error(), fixtureSeriesAlias, "the error names which dataset failed")

	require.Len(t, bus.subjects, 1, "the second publish is not attempted after the first fails")
	assert.Equal(t, adhocdata.SubjectPublish, bus.subjects[0])
	assert.NotEmpty(t, bus.payloads[0], "the request carries the encoded dataset")
}

// The generator runs BEFORE any bus call, so a bad spec fails without asking
// the capability for anything.
func TestPublishFixtureValidatesBeforeAsking(t *testing.T) {
	bus := &recordingBus{}
	_, err := doPublishFixture(bus, fixtureSpec{kind: adscore.AnomalyKindE(200), seed: 1})
	require.Error(t, err)
	assert.Empty(t, bus.subjects, "an unbuildable fixture never reaches the bus")
}
