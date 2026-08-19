package sysmreplay

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeedsDecimation(t *testing.T) {
	assert.False(t, NeedsDecimation(600, 600), "exactly full does not need it")
	assert.True(t, NeedsDecimation(601, 600))
	assert.False(t, NeedsDecimation(10, 600))
	assert.False(t, NeedsDecimation(10_000, 0), "no budget, no claim")
}

func TestPlanDecimation_RefusesBadInput(t *testing.T) {
	inst := &Reader{host: "h"}
	now := time.Now().UTC()

	_, err := inst.PlanDecimation(t.Context(), Window{}, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bounded")

	_, err = inst.PlanDecimation(t.Context(), Window{From: now.Add(-time.Hour), To: now}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slot budget")
}

// TestDecimatedPredicate_RestrictsToRecordedInstants pins the shape that makes
// decimation cheap: a key and a set of stamps, both envelope columns. If this
// ever needs a section, decimation has stopped being the cheap query.
func TestDecimatedPredicate_RestrictsToRecordedInstants(t *testing.T) {
	inst := &Reader{host: "some-host"}
	pred := inst.decimatedPredicate(DomainCPU, []int64{1000, 2000, 3000})

	assert.Contains(t, pred, formatKey(EntityKey("some-host", DomainCPU)))
	assert.Contains(t, pred, "fromUnixTimestamp64Milli(1000)")
	assert.Contains(t, pred, "fromUnixTimestamp64Milli(3000)")
	assert.NotContains(t, pred, "LW_", "decimation must not need the leeway read surface")
	assert.NotContains(t, pred, "arrayCumSum")
}

// TestPreviewSQL_UsesTheGeneratedArtefact is the structural claim of the other
// half. The preview DOES read a section — and must do it through the store's
// published projection rather than by hand, which is the documented mistake.
func TestPreviewSQL_UsesTheGeneratedArtefact(t *testing.T) {
	inst := &Reader{host: "some-host"}
	now := time.Now().UTC()
	sql := inst.previewSQL(Window{From: now.Add(-time.Hour), To: now}, time.Minute)

	assert.Contains(t, sql, "LW_LIST_BY_TAG_EQUAL", "the projection is the generated artefact")
	assert.Contains(t, sql, ".TotalPercent", "the field comes off the tuple by name, not by position")
	assert.Contains(t, sql, "GROUP BY b")
	// The filter must ride with the projection: a projection locates an
	// attribute by indexOf and answers plausibly and wrongly on a row carrying
	// a membership twice (ADR-0066).
	assert.Contains(t, sql, "countEqual", "the conformance filter must ride along")
}

func TestPreviewBucket_FloorsAtASecond(t *testing.T) {
	w := Window{From: time.Unix(0, 0), To: time.Unix(0, 0).Add(time.Second)}
	assert.Equal(t, time.Second, previewBucket(w, 0))
}

func TestPreview_RefusesWithoutAnExecutor(t *testing.T) {
	inst := &Reader{host: "h"}
	now := time.Now().UTC()
	_, err := inst.Preview(t.Context(), Window{From: now.Add(-time.Hour), To: now}, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Exec")
}
