package jackstay

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// leafRow is one leaf-digest line of a chunk.
func leafRow(chunk string, leaf uint32, n uint64, kd uint64, rd uint64) (line string) {
	return `{"chunk":"` + chunk + `","display":"","pid":"","leaf":` + itoa(uint64(leaf)) + `,"n":` + itoa(n) + `,"kd":` + itoa(kd) + `,"rd":` + itoa(rd) + "}\n"
}

func quietClient() (c *fakeClient) {
	return &fakeClient{answer: func(sql string) (string, error) { return "", nil }}
}

func testSyncer(pt *PlanTable, dst ClientI, sameKey bool) (ts *tableSyncer) {
	ts = &tableSyncer{dst: dst, pt: pt, sameKey: sameKey, opts: DefaultSyncOptions(), now: time.Now}
	ts.srcSpec, ts.dstSpec, _ = pt.DigestSpecs(false)
	return
}

func TestClear_Statements(t *testing.T) {
	partitioned := func(engine string, filter string) (pt *PlanTable) {
		pt = singleChunkTable("t", "t")
		pt.TargetEngine, pt.Filter = engine, filter
		pt.PartitionKey, pt.TargetPartitionKey = "p", "p"
		pt.Chunking = &Chunking{Kind: ChunkingPartition, Exprs: []string{"p"}, Leaves: 4}
		return
	}
	single := func(engine string, filter string) (pt *PlanTable) {
		pt = singleChunkTable("t", "t")
		pt.TargetEngine, pt.Filter = engine, filter
		pt.Chunking.Leaves = 4
		return
	}
	cases := []struct {
		name    string
		pt      *PlanTable
		sameKey bool
		pid     string
		leaves  []uint32
		prefix  string
		has     []string
	}{
		{name: "whole partition, same key", pt: partitioned("MergeTree", ""), sameKey: true, pid: "7", prefix: "ALTER TABLE `d`.`t` DROP PARTITION ID '7'"},
		{name: "whole partition, no pid", pt: partitioned("MergeTree", ""), sameKey: true, prefix: "DELETE FROM `d`.`t` WHERE "},
		{name: "whole partition, other key", pt: partitioned("MergeTree", ""), pid: "7", prefix: "DELETE FROM `d`.`t` WHERE "},
		{name: "whole partition under a filter", pt: partitioned("ReplicatedMergeTree", "k > 1"), sameKey: true, pid: "7", prefix: "DELETE FROM `d`.`t` WHERE ", has: []string{"k > 1"}},
		{name: "leaves of a partition", pt: partitioned("MergeTree", ""), sameKey: true, pid: "7", leaves: []uint32{1, 3}, prefix: "DELETE FROM `d`.`t` WHERE ", has: []string{"IN (1, 3)"}},
		{name: "single chunk", pt: single("MergeTree", ""), prefix: "TRUNCATE TABLE `d`.`t`"},
		{name: "single chunk under a filter", pt: single("MergeTree", "k > 1"), prefix: "DELETE FROM `d`.`t` WHERE ", has: []string{"k > 1", "lightweight_deletes_sync = 2"}},
		{name: "single chunk, leaves", pt: single("MergeTree", ""), leaves: []uint32{2}, prefix: "DELETE FROM `d`.`t` WHERE ", has: []string{"IN (2)"}},
		{name: "outside MergeTree, whole", pt: single("Log", ""), prefix: "TRUNCATE TABLE `d`.`t`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := quietClient()
			require.NoError(t, testSyncer(c.pt, dst, c.sameKey).clear(context.Background(), "AA", c.pid, c.leaves))
			require.Len(t, dst.execs, 1)
			assert.True(t, strings.HasPrefix(dst.execs[0], c.prefix), dst.execs[0])
			for _, h := range c.has {
				assert.Contains(t, dst.execs[0], h)
			}
		})
	}

	// Outside the MergeTree family nothing but the whole table can go.
	for name, ts := range map[string]*tableSyncer{
		"filter":    testSyncer(single("Log", "k > 1"), quietClient(), false),
		"leaves":    testSyncer(single("Log", ""), quietClient(), false),
		"partition": testSyncer(partitioned("Log", ""), quietClient(), true),
	} {
		var leaves []uint32
		if name == "leaves" {
			leaves = []uint32{1}
		}
		err := ts.clear(context.Background(), "AA", "7", leaves)
		assert.ErrorContains(t, err, "cannot delete a subset", name)
		assert.Empty(t, ts.dst.(*fakeClient).execs, name)
	}
}

func TestDifferingAllowed(t *testing.T) {
	ts := &tableSyncer{}
	s := leafSet{0: {n: 1, kd: 1, rd: 1}, 1: {n: 2, kd: 2, rd: 2}}
	d := leafSet{0: {n: 1, kd: 1, rd: 1}, 1: {n: 5, kd: 5, rd: 5}}
	none := map[uint32]uint64{}

	assert.Nil(t, ts.differingAllowed(&ChunkDiff{}, none, s, s), "nothing differs")

	assert.Equal(t, []uint32{0, 1}, ts.differingAllowed(&ChunkDiff{AbsentOnTarget: true}, none, s, leafSet{}))
	assert.Nil(t, ts.differingAllowed(&ChunkDiff{AbsentOnTarget: true}, none, s, d), "the target gained rows since the diff")

	assert.Equal(t, []uint32{0, 1}, ts.differingAllowed(&ChunkDiff{AbsentOnSource: true, DstRows: 6}, none, leafSet{}, d))
	assert.Nil(t, ts.differingAllowed(&ChunkDiff{AbsentOnSource: true, DstRows: 4}, none, leafSet{}, d), "the target holds another count than the diff showed")
	assert.Nil(t, ts.differingAllowed(&ChunkDiff{AbsentOnSource: true, DstRows: 6}, none, s, d), "the source gained rows since the diff")

	assert.Equal(t, []uint32{1}, ts.differingAllowed(&ChunkDiff{}, map[uint32]uint64{1: 5}, s, d))
	assert.Nil(t, ts.differingAllowed(&ChunkDiff{}, map[uint32]uint64{1: 4}, s, d), "the leaf holds another count than the diff showed")
	assert.Nil(t, ts.differingAllowed(&ChunkDiff{}, map[uint32]uint64{0: 1}, s, d), "the differing leaf was not listed")
}

func TestClearsWholeChunk(t *testing.T) {
	d := leafSet{0: {n: 1, kd: 1, rd: 1}, 2: {n: 3, kd: 3, rd: 3}}
	absent := &ChunkDiff{AbsentOnSource: true}
	assert.True(t, clearsWholeChunk(absent, []uint32{0, 2}, leafSet{}, d))
	assert.False(t, clearsWholeChunk(absent, []uint32{0}, leafSet{}, d), "a target leaf left out of the clear")
	assert.False(t, clearsWholeChunk(absent, []uint32{0, 2}, leafSet{2: {n: 3, kd: 3, rd: 3}}, d), "the source holds rows again")
	assert.False(t, clearsWholeChunk(absent, []uint32{0, 3}, leafSet{3: {n: 1, kd: 1, rd: 1}}, d), "as many leaves as the target holds, but not the same ones")
	assert.False(t, clearsWholeChunk(&ChunkDiff{}, []uint32{0, 2}, leafSet{}, d), "the chunk was not shown absent on the source")
}

// A chunk the diff showed absent on the source, still empty there, is
// cleared whole.
func TestRepairChunk_AbsentOnSourceClearsWhole(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Chunking.Leaves = 4
	pt.Sync = &TableSync{Mode: SyncModeRepair}
	pt.Diff = &TableDiff{Differing: []ChunkDiff{{Id: "", DstRows: 4, AbsentOnSource: true}}}
	dstDigests := 0
	dst := &fakeClient{answer: func(sql string) (string, error) {
		if isDigestQuery(sql, "t") {
			dstDigests++
			if dstDigests == 1 {
				return leafRow("", 0, 1, 1, 1) + leafRow("", 2, 3, 3, 3), nil
			}
		}
		return "", nil
	}}
	j := openTestJournal(t, "r")
	rep, err := SyncTable(context.Background(), ServerSource(quietClient()), dst, pt, j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Copied, rep.Problems)
	assert.Equal(t, uint64(4), rep.Cleared)
	require.Len(t, dst.execs, 1)
	assert.Equal(t, "TRUNCATE TABLE `d`.`t`", dst.execs[0])
	assert.Zero(t, dst.inserts)
}

// A chunk the diff showed absent on the source that the source holds rows of
// again is stale: nothing is cleared.
func TestRepairChunk_AbsentOnSourceRefilledIsStale(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Chunking.Leaves = 4
	pt.Sync = &TableSync{Mode: SyncModeRepair}
	pt.Diff = &TableDiff{Differing: []ChunkDiff{{Id: "", DstRows: 4, AbsentOnSource: true}}}
	src := &fakeClient{answer: func(sql string) (string, error) {
		if isDigestQuery(sql, "t") {
			return leafRow("", 0, 1, 1, 1), nil
		}
		return "", nil
	}}
	dst := &fakeClient{answer: func(sql string) (string, error) {
		if isDigestQuery(sql, "t") {
			return leafRow("", 0, 1, 1, 1) + leafRow("", 2, 3, 3, 3), nil
		}
		return "", nil
	}}
	j := openTestJournal(t, "r")
	rep, err := SyncTable(context.Background(), ServerSource(src), dst, pt, j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Stale)
	assert.Empty(t, dst.execs)
	assert.Zero(t, dst.inserts)
}

// Under append the run owns nothing: it records an attempt before the copy,
// does not retry a copy that fails verification, and a resumed run refuses a
// chunk it attempted or whose source moved since it was copied.
func TestCopyChunk_Append(t *testing.T) {
	newTable := func() (pt *PlanTable) {
		pt = singleChunkTable("t", "t")
		pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyAppend}
		return
	}
	srcRow := digestRow("", 2, 3, 3)
	src := &fakeClient{answer: func(sql string) (string, error) {
		switch {
		case isChunkList(sql):
			return `{"chunk":"","pid":"","display":""}` + "\n", nil
		case isDigestQuery(sql, "t"):
			return srcRow, nil
		}
		return "", nil
	}}
	// The target holds 1 row; after a good copy it holds 3.
	landed := digestRow("", 3, 4, 4)
	dstDigests := 0
	dst := &fakeClient{answer: func(sql string) (string, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT count()"):
			return `{"n":1}` + "\n", nil
		case isDigestQuery(sql, "t"):
			dstDigests++
			if dstDigests == 1 {
				return digestRow("", 1, 1, 1), nil
			}
			return landed, nil
		}
		return "", nil
	}}
	ctx := context.Background()

	j := openTestJournal(t, "r")
	rep, err := SyncTable(ctx, ServerSource(src), dst, newTable(), j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Copied, rep.Problems)
	assert.True(t, j.Attempted("s.t", ""))
	assert.Empty(t, dst.execs, "nothing of the target's is cleared")
	e, started := j.Started("s.t")
	require.True(t, started)
	assert.False(t, e.Owned)

	// The source moved since the copy: refused, as the target's rows are
	// not the run's to clear.
	srcRow = digestRow("", 5, 5, 5)
	rep, err = SyncTable(ctx, ServerSource(src), dst, newTable(), j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	require.Equal(t, 1, rep.Failed)
	assert.Contains(t, rep.Problems[0], "source changed since this chunk was copied")
	assert.Equal(t, 1, dst.inserts)

	// A copy that fails verification is not retried, and a resumed run
	// refuses the chunk it attempted.
	srcRow, dstDigests, dst.inserts = digestRow("", 2, 3, 3), 0, 0
	landed = digestRow("", 2, 2, 2)
	j = openTestJournal(t, "r2")
	opts := DefaultSyncOptions()
	opts.MaxAttempts = 3
	rep, err = SyncTable(ctx, ServerSource(src), dst, newTable(), j, opts, time.Now)
	require.NoError(t, err)
	require.Equal(t, 1, rep.Failed)
	assert.Contains(t, rep.Problems[0], "cannot retry")
	assert.Equal(t, 1, dst.inserts)
	rep, err = SyncTable(ctx, ServerSource(src), dst, newTable(), j, opts, time.Now)
	require.NoError(t, err)
	require.Equal(t, 1, rep.Failed)
	assert.Contains(t, rep.Problems[0], "an earlier attempt may have left rows")
	assert.Equal(t, 1, dst.inserts, "the attempted chunk is not copied again")
}

// The rows counter is wound back for an attempt that fails verification, so
// a retried chunk counts once; a chunk skipped as done is credited.
func TestSyncTable_RowsCounter(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}
	srcCalls, dstCalls := 0, 0
	src := &fakeClient{answer: func(sql string) (string, error) {
		switch {
		case isChunkList(sql):
			return `{"chunk":"","pid":"","display":""}` + "\n", nil
		case isDigestQuery(sql, "t"):
			srcCalls++
			if srcCalls == 1 {
				return digestRow("", 1, 1, 1), nil
			}
			return digestRow("", 2, 3, 3), nil
		}
		return "", nil
	}}
	dst := &fakeClient{answer: func(sql string) (string, error) {
		if isDigestQuery(sql, "t") {
			dstCalls++
			if dstCalls == 1 {
				return "", nil
			}
			return digestRow("", 2, 3, 3), nil
		}
		return "", nil
	}}
	j := openTestJournal(t, "r")
	opts := DefaultSyncOptions()
	var rows atomic.Int64
	opts.Rows = &rows
	rep, err := SyncTable(context.Background(), ServerSource(src), dst, pt, j, opts, time.Now)
	require.NoError(t, err)
	require.Equal(t, 1, rep.Copied, rep.Problems)
	assert.Equal(t, 2, dst.inserts, "the first attempt failed verification")
	assert.Equal(t, int64(2), rows.Load(), "the failed attempt's row is wound back")

	rows.Store(0)
	rep, err = SyncTable(context.Background(), ServerSource(src), dst, pt, j, opts, time.Now)
	require.NoError(t, err)
	require.Equal(t, 1, rep.Done)
	assert.Equal(t, int64(2), rows.Load(), "a chunk done earlier is credited")
}

// The free-space floor is waited for before each chunk.
func TestSyncTable_FreeFloor(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeRepair}
	pt.Diff = &TableDiff{Differing: []ChunkDiff{{Id: "", SrcRows: 1, Leaves: []LeafDiff{{Leaf: 0, SrcRows: 1}}}}}
	dst := &fakeClient{answer: func(sql string) (string, error) {
		if strings.Contains(sql, "system.") {
			return "", eh.Errorf("disks unreadable")
		}
		return "", nil
	}}
	opts := DefaultSyncOptions()
	opts.FreeFloor = &FreeFloor{MinFreeBytes: 1, Poll: time.Second}
	lows := 0
	opts.OnLowDisk = func([]DiskInfo) { lows++ }
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := SyncTable(ctx, ServerSource(quietClient()), dst, pt, openTestJournal(t, "r"), opts, time.Now)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, lows)
	assert.Zero(t, dst.inserts)
}

func TestCheckStart_Sample(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 2}
	j := openTestJournal(t, "r")
	require.NoError(t, j.RecordStart("s.t", true, TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 4}, "", time.Now()))
	e, _ := j.Started("s.t")
	assert.Equal(t, uint32(4), e.SampleDen)
	assert.ErrorContains(t, checkStart(e, pt), "another sample")

	pt.Sync.SampleDen = 4
	assert.NoError(t, checkStart(e, pt))

	// A start entry from before the fraction was recorded is taken as
	// matching.
	e.SampleNum, e.SampleDen = 0, 0
	pt.Sync.SampleDen = 2
	assert.NoError(t, checkStart(e, pt))
}

func TestParseFractionAndCompression(t *testing.T) {
	n, d, err := ParseFraction(" 1 / 100 ")
	require.NoError(t, err)
	assert.Equal(t, [2]uint32{1, 100}, [2]uint32{n, d})
	for _, bad := range []string{"", "1", "0/4", "5/4", "1/0", "a/b", "-1/4"} {
		_, _, err = ParseFraction(bad)
		assert.ErrorContains(t, err, "0 < num <= den", bad)
	}
	for in, want := range map[string]string{"zstd": "zstd", "gzip": "gzip", "none": "", "": ""} {
		got, err := ParseCompression(in)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err = ParseCompression("brotli")
	assert.Error(t, err)
}

// A resumed repair credits a verified chunk with the rows it copies, the
// measure ExpectedRows counts in; other modes credit the whole chunk.
func TestDoneRowsCountsWhatRepairCopies(t *testing.T) {
	pt := &PlanTable{
		Sync: &TableSync{Mode: SyncModeRepair},
		Diff: &TableDiff{Differing: []ChunkDiff{
			{Id: "1", SrcRows: 900, Leaves: []LeafDiff{{SrcRows: 7}, {SrcRows: 3}}},
			{Id: "2", SrcRows: 50, AbsentOnTarget: true},
		}},
	}
	assert.Equal(t, uint64(10), doneRows(pt, ChunkResult{Chunk: "1", Rows: 900}))
	assert.Equal(t, uint64(50), doneRows(pt, ChunkResult{Chunk: "2", Rows: 50}))
	assert.Equal(t, uint64(0), doneRows(pt, ChunkResult{Chunk: "3", Rows: 80}), "a chunk the diff did not list copies nothing")
	pt.Sync.Mode = SyncModeFull
	assert.Equal(t, uint64(900), doneRows(pt, ChunkResult{Chunk: "1", Rows: 900}))
}
