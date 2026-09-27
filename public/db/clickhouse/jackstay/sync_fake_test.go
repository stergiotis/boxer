package jackstay

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// fakeClient answers queries from a script, so the decision branches of a
// sync run in the default lane. answer gets every SQL text and returns the
// JSONEachRow body; the relay's stream calls are counted and can fail on
// demand, and every Exec is kept.
type fakeClient struct {
	mu        sync.Mutex
	answer    func(sql string) (body string, err error)
	insertErr error
	execs     []string
	inserts   int
}

func (inst *fakeClient) Query(ctx context.Context, sql string) (body io.ReadCloser, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var s string
	s, err = inst.answer(sql)
	if err != nil {
		return
	}
	return io.NopCloser(strings.NewReader(s)), nil
}

func (inst *fakeClient) Exec(ctx context.Context, sql string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.execs = append(inst.execs, sql)
	return
}

func (inst *fakeClient) QueryStream(ctx context.Context, sql string, opts chclient.StreamOptions) (body io.ReadCloser, contentEncoding string, err error) {
	return io.NopCloser(strings.NewReader("")), "", nil
}

func (inst *fakeClient) InsertStream(ctx context.Context, insertSQL string, body io.Reader, opts chclient.StreamOptions) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.inserts++
	_, _ = io.Copy(io.Discard, body)
	return inst.insertErr
}

// digestRow is one leaf-digest line of a single-chunk table.
func digestRow(chunk string, n uint64, kd uint64, rd uint64) (line string) {
	if n == 0 {
		return ""
	}
	return `{"chunk":"` + chunk + `","display":"","pid":"","leaf":0,"n":` + itoa(n) + `,"kd":` + itoa(kd) + `,"rd":` + itoa(rd) + "}\n"
}

func itoa(n uint64) (s string) {
	return strconv.FormatUint(n, 10)
}

// isDigestQuery tells the leaf-digest scan of one side apart from the
// chunk list, the row count and the progress poll.
func isDigestQuery(sql string, table string) (is bool) {
	return strings.Contains(sql, "sumWithOverflow") && strings.Contains(sql, QuoteIdent(table))
}

// isChunkList recognises ChunkListQuery; the digest scan shares its
// any(pid) fragment, so the prefix is what tells them apart.
func isChunkList(sql string) (is bool) {
	return strings.HasPrefix(sql, "SELECT chunk, any(pid) AS pid")
}

func singleChunkTable(src string, dst string) (pt *PlanTable) {
	return &PlanTable{
		Source: ref("s", src), Target: ref("d", dst), Engine: "MergeTree", TargetEngine: "MergeTree",
		SortingKey: "k", Rows: 2,
		TableVerdict: TableVerdict{Verdict: VerdictIdentical, CopyColumns: []string{"k", "v"}},
		Chunking:     &Chunking{Kind: ChunkingSingle, Leaves: 1},
	}
}

func openTestJournal(t *testing.T, run string) (j *Journal) {
	t.Helper()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "p.journal"), run)
	require.NoError(t, err)
	t.Cleanup(func() { _ = j.Close() })
	return
}

// A source that moves between the digest and the copy: the first attempt
// verifies against the digest it read, which the landed rows no longer
// match; the second attempt re-reads the source and matches.
func TestCopyChunk_SourceMovedBetweenAttempts(t *testing.T) {
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
		switch {
		case isDigestQuery(sql, "t"):
			dstCalls++
			if dstCalls == 1 {
				return "", nil // empty before the first copy
			}
			return digestRow("", 2, 3, 3), nil // what the moved source delivered
		}
		return "", nil
	}}
	j := openTestJournal(t, "r")
	rep, err := SyncTable(context.Background(), src, dst, pt, j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Copied)
	assert.Equal(t, 0, rep.Failed, rep.Problems)
	assert.Equal(t, uint64(2), rep.Rows)
	assert.Equal(t, uint64(0), rep.Cleared, "a retry clears the run's own copy, not the operator's rows")
	assert.Equal(t, 2, dst.inserts)
	e, done := j.Done("s.t", "")
	require.True(t, done)
	assert.Equal(t, uint64(2), e.N, "journaled at the digest the copy was verified against")
}

// A relay that fails is reported as the relay's error, not as a digest
// mismatch, and no rows are counted for it.
func TestRepairChunk_RelayErrorIsReported(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeRepair}
	pt.Diff = &TableDiff{Differing: []ChunkDiff{{Id: "", SrcRows: 1, Leaves: []LeafDiff{{Leaf: 0, SrcRows: 1, DstRows: 0}}}}}
	src := &fakeClient{answer: func(sql string) (string, error) {
		if isDigestQuery(sql, "t") {
			return digestRow("", 1, 1, 1), nil
		}
		return "", nil
	}}
	dst := &fakeClient{answer: func(sql string) (string, error) { return "", nil }, insertErr: eh.Errorf("target went away")}
	j := openTestJournal(t, "r")
	opts := DefaultSyncOptions()
	opts.MaxAttempts = 2
	rep, err := SyncTable(context.Background(), src, dst, pt, j, opts, time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Failed)
	assert.Equal(t, uint64(0), rep.Rows)
	require.Len(t, rep.Problems, 1)
	assert.Contains(t, rep.Problems[0], "target went away")
	assert.Equal(t, 2, dst.inserts)
	assert.Empty(t, dst.execs, "repair claims nothing and counted nothing")
}

// A run resumed under other settings than it began with is refused.
func TestSyncTable_ResumeSettingsMismatch(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}
	j := openTestJournal(t, "r")
	require.NoError(t, j.RecordStart("s.t", false, TableSync{Mode: SyncModeFull, Existing: ExistingPolicyAppend}, time.Now()))
	quiet := &fakeClient{answer: func(sql string) (string, error) { return "", nil }}
	_, err := SyncTable(context.Background(), quiet, quiet, pt, j, DefaultSyncOptions(), time.Now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "other settings")
	assert.Empty(t, quiet.execs)
}

// Under replace, a chunk the target holds and the source lacks is cleared
// and journaled, so the target ends up equal to the source.
func TestSyncTable_ReplaceClearsTargetOnlyChunks(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.PartitionKey, pt.TargetPartitionKey = "p", "p"
	pt.Chunking = &Chunking{Kind: ChunkingPartition, Exprs: []string{"p"}, Leaves: 1}
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}
	row := func(chunk string, pid string, n uint64) string {
		return `{"chunk":"` + chunk + `","display":"","pid":"` + pid + `","leaf":0,"n":` + itoa(n) + `,"kd":` + itoa(n) + `,"rd":` + itoa(n) + "}\n"
	}
	src := &fakeClient{answer: func(sql string) (string, error) {
		switch {
		case isChunkList(sql):
			return `{"chunk":"AA","pid":"1","display":"1"}` + "\n", nil
		case isDigestQuery(sql, "t"):
			return row("AA", "1", 1), nil
		}
		return "", nil
	}}
	dstDigests := 0
	dst := &fakeClient{answer: func(sql string) (string, error) {
		switch {
		case isChunkList(sql):
			return `{"chunk":"AA","pid":"1","display":"1"}` + "\n" + `{"chunk":"BB","pid":"2","display":"2"}` + "\n", nil
		case isDigestQuery(sql, "t") && strings.Contains(sql, "_partition_id = '2'"):
			return row("BB", "2", 5), nil
		case isDigestQuery(sql, "t"):
			dstDigests++
			if dstDigests == 1 {
				return "", nil // before the copy
			}
			return row("AA", "1", 1), nil // after it
		}
		return "", nil
	}}
	j := openTestJournal(t, "r")
	rep, err := SyncTable(context.Background(), src, dst, pt, j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 2, rep.Copied)
	assert.Equal(t, 0, rep.Failed, rep.Problems)
	assert.Equal(t, uint64(5), rep.Cleared)
	require.Len(t, dst.execs, 1)
	assert.Contains(t, dst.execs[0], "DROP PARTITION ID '2'")
	e, done := j.Done("s.t", "BB")
	require.True(t, done)
	assert.Equal(t, uint64(0), e.N)

	// Resumed while the source still lacks the chunk: nothing is cleared again.
	dst.execs = nil
	rep, err = SyncTable(context.Background(), src, dst, pt, j, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 2, rep.Done)
	assert.Empty(t, dst.execs)
}

// A resume under other settings is refused before the plan is saved, so the
// refused request leaves no trace in the plan file.
func TestRunSync_RefusesMismatchBeforeSaving(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyAppend}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	j, err := OpenJournal(JournalPath(planPath), "run")
	require.NoError(t, err)
	require.NoError(t, j.RecordStart("s.t", false, TableSync{Mode: SyncModeRepair}, time.Now()))
	require.NoError(t, j.Close())
	prep := SyncPrepared{Plan: Plan{FormatVersion: PlanFormatVersion, Source: Endpoint{URL: "http://s/"}, Target: Endpoint{URL: "http://d/"},
		Tables: []PlanTable{*pt}, SyncRun: &SyncRun{RunId: "run"}}}
	prep.Chosen = []*PlanTable{&prep.Plan.Tables[0]}
	quiet := &fakeClient{answer: func(sql string) (string, error) { return "", nil }}
	_, err = RunSync(context.Background(), quiet, quiet, &prep, planPath, false, DefaultSyncOptions(), time.Now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "other settings")
	_, serr := LoadPlan(planPath)
	assert.Error(t, serr, "nothing was written")
	assert.Empty(t, quiet.execs)
}
