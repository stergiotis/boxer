package sysmreplay

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const covStep = time.Minute

func covBucket(minute int, rows int64) CoverageBucket {
	return CoverageBucket{StartMS: int64(minute) * covStep.Milliseconds(), Rows: rows}
}

func TestMergeCoverage_Empty(t *testing.T) {
	assert.Nil(t, MergeCoverage(nil, covStep))
	assert.Nil(t, MergeCoverage([]CoverageBucket{}, covStep))
}

func TestMergeCoverage_SingleBin(t *testing.T) {
	runs := MergeCoverage([]CoverageBucket{covBucket(3, 10)}, covStep)
	require.Len(t, runs, 1)
	assert.Equal(t, covBucket(3, 10).StartMS, runs[0].StartMS)
	assert.Equal(t, covBucket(4, 0).StartMS, runs[0].EndMS, "a run's end is exclusive, one step past its start")
	assert.Equal(t, int64(10), runs[0].Rows)
}

// TestMergeCoverage_AdjacentBinsBecomeOneRun is the whole point: an unbroken
// stretch must not cost a band per bin on the wire every frame.
func TestMergeCoverage_AdjacentBinsBecomeOneRun(t *testing.T) {
	runs := MergeCoverage([]CoverageBucket{
		covBucket(0, 1), covBucket(1, 2), covBucket(2, 3),
	}, covStep)
	require.Len(t, runs, 1)
	assert.Equal(t, int64(0), runs[0].StartMS)
	assert.Equal(t, 3*covStep.Milliseconds(), runs[0].EndMS)
	assert.Equal(t, int64(6), runs[0].Rows, "rows accumulate across the run")
}

// TestMergeCoverage_GapSplitsRuns pins the gaps, which are the reason coverage
// exists at all: a window the tee was down for must read as two stretches, not
// one long one.
func TestMergeCoverage_GapSplitsRuns(t *testing.T) {
	runs := MergeCoverage([]CoverageBucket{
		covBucket(0, 1), covBucket(1, 1),
		// minutes 2 and 3 have no rows at all — absent, not zero
		covBucket(4, 5), covBucket(5, 5),
	}, covStep)
	require.Len(t, runs, 2)
	assert.Equal(t, 2*covStep.Milliseconds(), runs[0].EndMS)
	assert.Equal(t, 4*covStep.Milliseconds(), runs[1].StartMS)
	assert.Equal(t, int64(10), runs[1].Rows)
}

func TestMergeCoverage_ZeroStepFallsBackToASecond(t *testing.T) {
	runs := MergeCoverage([]CoverageBucket{{StartMS: 0, Rows: 1}}, 0)
	require.Len(t, runs, 1)
	assert.Equal(t, time.Second.Milliseconds(), runs[0].EndMS)
}

func TestCoverageBucket_DividesTheWindow(t *testing.T) {
	w := Window{From: time.Unix(0, 0), To: time.Unix(0, 0).Add(24 * time.Hour)}
	got := coverageBucket(w, 0)
	assert.Equal(t, 24*time.Hour/DefaultCoverageBuckets, got)
}

// TestCoverageBucket_FloorsAtASecond pins why: the order column is stamped per
// bundle, so a finer bin cannot separate two of them and only multiplies rows
// nothing can draw.
func TestCoverageBucket_FloorsAtASecond(t *testing.T) {
	w := Window{From: time.Unix(0, 0), To: time.Unix(0, 0).Add(time.Second)}
	assert.Equal(t, time.Second, coverageBucket(w, 0))
	assert.Equal(t, time.Second, coverageBucket(w, time.Millisecond))
}

func TestCoverage_RefusesAnUnboundedWindow(t *testing.T) {
	inst := &Reader{host: "h"}
	_, err := inst.Coverage(t.Context(), Window{}, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bounded")
}

func TestCoverage_RefusesAnInvertedWindow(t *testing.T) {
	inst := &Reader{host: "h"}
	now := time.Now()
	_, err := inst.Coverage(t.Context(), Window{From: now, To: now.Add(-time.Hour)}, time.Minute)
	require.Error(t, err)
}

func TestCoverage_RefusesWithoutAnExecutor(t *testing.T) {
	inst := &Reader{host: "h"}
	now := time.Now()
	_, err := inst.Coverage(t.Context(), Window{From: now.Add(-time.Hour), To: now}, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Exec")
}

// TestCoverageSQL_TouchesOnlyEnvelopeColumns is the structural claim of
// ADR-0197 §SD10 written as a test: the moment this query reaches for a leeway
// section it stops being the cheap one, and that should fail here rather than
// be discovered from a query plan.
func TestCoverageSQL_TouchesOnlyEnvelopeColumns(t *testing.T) {
	inst := &Reader{host: "some-host"}
	now := time.Now().UTC()
	sql := inst.coverageSQL(Window{From: now.Add(-time.Hour), To: now}, time.Minute)

	assert.NotContains(t, sql, "LW_GET", "coverage must not need the leeway read surface")
	assert.NotContains(t, sql, "arrayCumSum", "coverage must not do run arithmetic")
	assert.NotContains(t, sql, "indexOf", "coverage must not resolve memberships")
	assert.NotContains(t, sql, ":lr:", "coverage must not name a membership column")
	assert.Contains(t, sql, "GROUP BY b")
	assert.Contains(t, sql, "toStartOfInterval")
}

// TestCoverageSQL_SpansEveryDomain pins that coverage asks about the host
// rather than about one kind: a window where only the topology was written is
// still a window the tee was alive for.
func TestCoverageSQL_SpansEveryDomain(t *testing.T) {
	inst := &Reader{host: "some-host"}
	now := time.Now().UTC()
	sql := inst.coverageSQL(Window{From: now.Add(-time.Hour), To: now}, time.Minute)

	assert.Len(t, allDomains, 13, "every stored series should be covered")
	for _, d := range allDomains {
		assert.Contains(t, sql, formatKey(EntityKey("some-host", d)),
			"domain %s missing from the key filter", d)
	}
}
