package recordstore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A relation whose columns the catalogue lists and whose read fails is
// incompatible whatever its columns say, for a relation the store only
// reads as for one it owns, and the summary does not point at a migration.
func TestJudgeFacts_ARefusedReadIsIncompatible(t *testing.T) {
	cols := []ColumnSpec{{Name: "id", Type: "LowCardinality(UInt64)"}}
	live := []LiveColumn{{Name: "id", Type: "LowCardinality(UInt64)"}}
	c := TableContract{Kind: "k", Table: "db.v", Columns: cols, ColumnsOnly: true}

	ok := JudgeFacts(c, TableFacts{Exists: true, Engine: "View", Columns: live})
	require.Equal(t, SchemaVerdictOk, ok.Verdict, ok.Summary())
	assert.Empty(t, ok.ReadError)

	const refusal = "Code: 455. DB::Exception: Creating columns of type LowCardinality(UInt64) is prohibited by default. (SUSPICIOUS_TYPE_FOR_LOW_CARDINALITY)"
	v := JudgeFacts(c, TableFacts{Exists: true, Engine: "View", Columns: live, ReadError: refusal})
	require.Equal(t, SchemaVerdictIncompatible, v.Verdict)
	assert.Equal(t, refusal, v.ReadError)
	require.Len(t, v.Reasons, 1)
	assert.Contains(t, v.Reasons[0], refusal)
	assert.Contains(t, v.Reasons[0], "allow_suspicious_low_cardinality_types")
	assert.Contains(t, v.Summary(), "a read of it fails")
	assert.NotContains(t, v.Summary(), "doc/migration", "no migration answers a refused read")
	require.Error(t, v.Err())

	other := JudgeFacts(c, TableFacts{Exists: true, Engine: "Merge", Columns: live, ReadError: "Code: 60. Unknown table"})
	require.Equal(t, SchemaVerdictIncompatible, other.Verdict)
	assert.False(t, strings.Contains(other.Reasons[0], "allow_suspicious"), "the remedy is named only for the refusal it answers")
}

func TestComputesOnRead(t *testing.T) {
	for _, engine := range []string{"View", "LiveView", "WindowView", "Merge"} {
		assert.True(t, ComputesOnRead(engine), engine)
	}
	for _, engine := range []string{"MergeTree", "ReplicatedMergeTree", "MaterializedView", "Dictionary", "Memory", ""} {
		assert.False(t, ComputesOnRead(engine), engine)
	}
}
