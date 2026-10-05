package jackstay

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeafSet_Arithmetic(t *testing.T) {
	a := leafSet{0: {n: 2, kd: 10, rd: 20}, 3: {n: 1, kd: ^uint64(0), rd: 1}}
	b := leafSet{3: {n: 1, kd: 2, rd: 2}, 5: {n: 4, kd: 4, rd: 4}}
	sum := a.plus(b)
	assert.Equal(t, leafDigest{n: 2, kd: 1, rd: 3}, sum[3], "sums wrap")
	maxU := ^uint64(0)
	assert.Equal(t, leafDigest{n: 8, kd: 10 + maxU + 2 + 4, rd: 27}, sum.total())
	assert.True(t, a.equal(leafSet{0: {n: 2, kd: 10, rd: 20}, 3: {n: 1, kd: ^uint64(0), rd: 1}, 9: {}}), "empty leaves do not count")
	assert.False(t, a.equal(b))
	assert.Equal(t, []uint32{0, 3, 5}, a.differing(b))
	assert.Equal(t, leafSet{3: b[3]}, b.restrict([]uint32{3, 7}))
	assert.True(t, coversAll([]uint32{0, 3, 5}, a, b))
	assert.False(t, coversAll([]uint32{0, 3}, a, b))
}

func TestJournal_ReopenAndRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.journal")
	j, err := OpenJournal(path, "r1")
	require.NoError(t, err)
	now := time.Unix(1, 0)
	require.NoError(t, j.RecordStart("a.t", true, TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}, "", now))
	require.NoError(t, j.RecordChunk("a.t", "0", SyncModeFull, leafDigest{n: 3, kd: 4, rd: 5}, now))
	require.NoError(t, j.RecordAttempt("a.u", "1", now))
	require.NoError(t, j.Close())

	j, err = OpenJournal(path, "r1")
	require.NoError(t, err)
	e0, started := j.Started("a.t")
	assert.True(t, e0.Owned && started)
	assert.Equal(t, "full", e0.Mode)
	assert.Equal(t, "replace", e0.Existing)
	e, done := j.Done("a.t", "0")
	require.True(t, done)
	assert.Equal(t, uint64(3), e.N)
	assert.True(t, j.Attempted("a.u", "1"))
	require.NoError(t, j.Close())

	other, err := OpenJournal(path, "r2")
	require.NoError(t, err)
	_, started = other.Started("a.t")
	assert.False(t, started, "another run's lines are ignored")
	require.NoError(t, other.Close())

	// A crash can cut the last line short; the journal still opens.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(`{"run":"r1","table":"a.t","ev`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	j, err = OpenJournal(path, "r1")
	require.NoError(t, err)
	_, done = j.Done("a.t", "0")
	assert.True(t, done)
	require.NoError(t, j.Close())
}

func TestPredicates(t *testing.T) {
	r := Chunking{Kind: ChunkingRange, Exprs: []string{"k"}, BoundType: "UInt64", Bounds: []string{"10", "20"}, Leaves: 4}
	assert.Equal(t, "(k) < CAST('10' AS UInt64)", r.ChunkPredicate("0", ""))
	assert.Equal(t, "(k) >= CAST('10' AS UInt64) AND (k) < CAST('20' AS UInt64)", r.ChunkPredicate("1", ""))
	assert.Equal(t, "(k) >= CAST('20' AS UInt64)", r.ChunkPredicate("2", ""))
	assert.Equal(t, "0", r.ChunkPredicate("9", ""), "an unknown chunk selects nothing")
	p := Chunking{Kind: ChunkingPartition, Exprs: []string{"k % 4"}}
	assert.Equal(t, "_partition_id = '2'", p.ChunkPredicate("x", "2"))
	assert.Equal(t, "hex(formatRowNoNewline('RowBinary', k % 4)) = 'x'", p.ChunkPredicate("x", ""))

	spec := DigestSpec{Ref: ref("d", "t"), KeyExprs: []string{"k"}, CopyColumns: []string{"k", "v"}, Chunking: r}
	assert.Equal(t, "(cityHash64(tuple(k))) % 4 IN (1, 3)", spec.LeafPredicate([]uint32{1, 3}))
	assert.Equal(t, "cityHash64('jackstay-sample', tuple(k)) % 100 < 3", spec.SamplePredicate(3, 100))
	w := spec.With("a = 1").With("b = 2").With("1")
	assert.Equal(t, "(a = 1) AND (b = 2)", w.Where)
	assert.Equal(t, "SELECT `k`, `v` FROM `d`.`t` WHERE (a = 1) AND (b = 2) SETTINGS output_format_binary_encode_types_in_binary_format = 0, output_format_binary_write_json_as_string = 1 FORMAT Native", w.SelectNative())
	assert.Equal(t, "INSERT INTO `d`.`t` (`k`, `v`) SETTINGS insert_deduplicate = 0 FORMAT Native", spec.InsertNative())
}
