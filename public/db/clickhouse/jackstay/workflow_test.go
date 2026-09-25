package jackstay

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareSync(t *testing.T) {
	layout := &Chunking{Kind: ChunkingSingle, Leaves: 1}
	plan := Plan{Tables: []PlanTable{
		{Source: ref("a", "same"), Rows: 10, TableVerdict: TableVerdict{Verdict: VerdictIdentical}, Chunking: layout, Diff: &TableDiff{}},
		{Source: ref("a", "differs"), Rows: 10, TableVerdict: TableVerdict{Verdict: VerdictNarrower}, Chunking: layout, Diff: &TableDiff{Differing: []ChunkDiff{{Id: ""}}}},
		{Source: ref("a", "undiffed"), Rows: 10, TableVerdict: TableVerdict{Verdict: VerdictIdentical}, Chunking: layout},
		{Source: ref("a", "pending"), TableVerdict: TableVerdict{Verdict: VerdictCreate}},
		{Source: ref("a", "view"), TableVerdict: TableVerdict{Verdict: VerdictUnsupported}},
	}}
	ctx := context.Background()

	chosen, skipped, err := PrepareSync(ctx, nil, &plan, TableSync{Mode: SyncModeFull}, nil, DefaultChunkingOptions())
	require.NoError(t, err)
	require.Len(t, chosen, 3)
	assert.Equal(t, []string{"a.pending: verdict create (apply the DDL first)"}, skipped, "unsupported tables are not worth a line")
	assert.Equal(t, SyncModeFull, chosen[0].Sync.Mode)

	chosen, skipped, err = PrepareSync(ctx, nil, &plan, TableSync{Mode: SyncModeRepair}, nil, DefaultChunkingOptions())
	require.NoError(t, err)
	require.Len(t, chosen, 1, "repair skips identical diffs and needs one")
	assert.Equal(t, ref("a", "differs"), chosen[0].Source)
	assert.Contains(t, skipped, "a.undiffed: repair needs a diff (run the diff first)")

	chosen, _, err = PrepareSync(ctx, nil, &plan, TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 4}, nil, DefaultChunkingOptions())
	require.NoError(t, err)
	assert.Equal(t, int64(3*(10/4)), ExpectedRows(chosen), "each table rounds down")
}

func TestBeginRun(t *testing.T) {
	var p Plan
	now := time.Unix(100, 0)
	run, resumed, err := BeginRun(&p, false, now)
	require.NoError(t, err)
	assert.False(t, resumed)
	assert.NotEmpty(t, run.RunId)
	require.NotNil(t, p.SyncRun)

	again, resumed, err := BeginRun(&p, false, now)
	require.NoError(t, err)
	assert.True(t, resumed)
	assert.Equal(t, run.RunId, again.RunId)

	fresh, resumed, err := BeginRun(&p, true, now)
	require.NoError(t, err)
	assert.False(t, resumed)
	assert.NotEqual(t, run.RunId, fresh.RunId)
}

func TestPlan_Clone(t *testing.T) {
	p := Plan{FormatVersion: PlanFormatVersion, Tables: []PlanTable{{Source: ref("a", "t"), TableVerdict: TableVerdict{DDL: []string{"x"}}}}}
	q, err := p.Clone()
	require.NoError(t, err)
	q.Tables[0].DDL[0] = "y"
	assert.Equal(t, "x", p.Tables[0].DDL[0], "a clone shares nothing")
}
