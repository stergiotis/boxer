package jackstay

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDigestSpec_RowHashSettingsPinned(t *testing.T) {
	// A keyless table's leaves and samples hash the row's RowBinary bytes, so
	// every statement whose WHERE may hold them pins the same settings as the
	// digests.
	spec := DigestSpec{Ref: ref("d", "t"), CopyColumns: []string{"j"}, Chunking: Chunking{Kind: ChunkingSingle, Leaves: 4}}
	leaves := spec.ForLeaves([]uint32{1})
	assert.Contains(t, leaves.Where, "formatRowNoNewline('RowBinary'")
	sel := leaves.SelectNative()
	assert.True(t, strings.HasSuffix(sel, " SETTINGS "+rowBinarySettings+" FORMAT Native"), sel)
	assert.Contains(t, digestSettings, rowBinarySettings)
	assert.Contains(t, rowBinarySettings, "output_format_binary_write_json_as_string = 1")

	del := deleteQuery(spec.Ref, leaves.SlicePredicate())
	assert.Equal(t, "DELETE FROM `d`.`t` WHERE "+leaves.Where+" SETTINGS lightweight_deletes_sync = 2, "+rowBinarySettings, del)
}

func TestDigestSpec_Identity(t *testing.T) {
	// The forms are kept in journals and packs, so they are pinned here.
	keyed := DigestSpec{KeyExprs: []string{"a", "b"}, CopyColumns: []string{"a", "b", "v"}, Chunking: Chunking{Leaves: 8}}
	assert.Equal(t, "cityHash64(tuple(a, b))", keyed.KeyHashExpr())
	assert.Equal(t, "substring(toString(tuple(a, b)), 1, 200)", keyed.keyTextExpr())
	assert.Equal(t, "cityHash64('jackstay-sample', tuple(a, b)) % 4 < 1", keyed.SamplePredicate(1, 4))
	assert.Equal(t, "(cityHash64(tuple(a, b))) % 8 IN (0, 7)", keyed.LeafPredicate([]uint32{0, 7}))
	assert.Equal(t, "kh % 8", keyed.leafExpr())

	keyless := DigestSpec{CopyColumns: []string{"a", "v"}}
	row := "cityHash64(formatRowNoNewline('RowBinary', `a`, `v`))"
	assert.Equal(t, row, keyless.RowHashExpr())
	assert.Equal(t, row, keyless.KeyHashExpr(), "a keyless table is keyed by its row")
	assert.Equal(t, "substring(toString(tuple(`a`, `v`)), 1, 200)", keyless.keyTextExpr())
	assert.Equal(t, "cityHash64('jackstay-sample', "+row+") % 2 < 1", keyless.SamplePredicate(1, 2))
	assert.Equal(t, "("+row+") % 1 IN (0)", keyless.LeafPredicate([]uint32{0}), "zero leaves read as one")
}

func TestDigestSpecs_TargetWithoutPartitionIds(t *testing.T) {
	c := &Chunking{Kind: ChunkingPartition, Exprs: []string{"toYYYYMM(ts)"}, Leaves: 2}
	pt := PlanTable{Source: ref("s", "t"), Target: ref("d", "t"), Engine: "MergeTree", TargetEngine: "StripeLog", SortingKey: "k", PartitionKey: "toYYYYMM(ts)", Chunking: c, TableVerdict: TableVerdict{CopyColumns: []string{"k", "ts"}}}
	src, dst, _ := pt.DigestSpecs(false)
	assert.False(t, src.NoPartitionIds)
	assert.True(t, dst.NoPartitionIds)

	assert.Contains(t, src.LeafDigestQuery(), "_partition_id AS pid")
	for _, sql := range []string{dst.LeafDigestQuery(), dst.ChunkListQuery()} {
		assert.NotContains(t, sql, "_partition_id", sql)
		assert.Contains(t, sql, "'' AS pid", sql)
	}
	// A partition id handed over from the source is not one the target has.
	ch := dst.ForChunk("AB", "202401")
	assert.Equal(t, "hex(formatRowNoNewline('RowBinary', toYYYYMM(ts))) = 'AB'", ch.Where)
	ch = src.ForChunk("AB", "202401")
	assert.Equal(t, "_partition_id = '202401'", ch.Where)

	for _, engine := range []string{"Log", "TinyLog", "Memory", ""} {
		pt.TargetEngine = engine
		_, dst, _ = pt.DigestSpecs(false)
		assert.True(t, dst.NoPartitionIds, engine)
	}
	pt.TargetEngine = "ReplicatedMergeTree"
	_, dst, _ = pt.DigestSpecs(false)
	assert.False(t, dst.NoPartitionIds)
}

// The row hash reads a JSON column as text, so no session setting moves it.
func TestRowHashExprReadsJSONAsText(t *testing.T) {
	spec := DigestSpec{CopyColumns: []string{"n", "j"}, HashAsText: []string{"j"}}
	assert.Equal(t, "cityHash64(formatRowNoNewline('RowBinary', `n`, toJSONString(`j`)))", spec.RowHashExpr())
	spec.HashAsText = nil
	assert.Equal(t, "cityHash64(formatRowNoNewline('RowBinary', `n`, `j`))", spec.RowHashExpr())
}
