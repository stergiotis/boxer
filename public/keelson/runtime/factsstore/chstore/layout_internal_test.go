package chstore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// legacyEngineClause is the clause every facts table was created with
// before ADR-0397.
const legacyEngineClause = `MergeTree() ORDER BY "` + factsschema.TsColumn + `"`

// The setting's default is the one source both the creator and the
// facts-bound stores' goldens read.
func TestTheEngineSettingDefaultsToTheFactsLayout(t *testing.T) {
	require.Equal(t, factsschema.DefaultEngineClause, FactsEngine.Spec().Default)
}

// A new table is partitioned by month, sorted by time, keeps the insert
// deduplication window and is stamped with its columns and its layout.
func TestComposeSetupSQL_TheDefaultLayout(t *testing.T) {
	ft, err := composeFactsTable(Config{Database: "db", Table: "t"}, factsschema.DefaultEngineClause)
	require.NoError(t, err)
	sql := ft.sql
	assert.Contains(t, sql, "CREATE TABLE IF NOT EXISTS db.t (")
	assert.Contains(t, sql, "\n) ENGINE "+factsschema.DefaultEngineClause+"\n")
	assert.Contains(t, sql, "SETTINGS allow_suspicious_low_cardinality_types=1, non_replicated_deduplication_window=1000\nCOMMENT '")
	assert.True(t, strings.HasSuffix(sql, "';\n"), "the script ends with the stamp")

	stamp, ok := recordstore.ParseTableStamp(ft.stamp)
	require.True(t, ok)
	assert.Equal(t, factsschema.TableName, stamp.Kind)
	assert.Equal(t, recordstore.ColumnsDigest(ft.columns), stamp.Columns)
	assert.Equal(t, ft.layout.Digest(), stamp.Layout)
	assert.Empty(t, stamp.Vocab, "the table is shared by several vocabularies")
	assert.Equal(t, recordstore.TableLayout{Engine: "MergeTree", PartitionKey: `toYYYYMM("` + factsschema.TsColumn + `")`, SortingKey: `"` + factsschema.TsColumn + `"`}, ft.layout)
}

// The legacy clause composes as it did, with the window and a stamp naming
// its own layout.
func TestComposeSetupSQL_AnExplicitClauseIsKept(t *testing.T) {
	legacy, err := composeFactsTable(Config{Database: "boxer", Table: "facts"}, legacyEngineClause)
	require.NoError(t, err)
	assert.Contains(t, legacy.sql, "\n) ENGINE "+legacyEngineClause+"\n")
	assert.NotContains(t, legacy.sql, "PARTITION BY")
	current, err := composeFactsTable(Config{Database: "boxer", Table: "facts"}, factsschema.DefaultEngineClause)
	require.NoError(t, err)
	assert.Equal(t, current.columns, legacy.columns)
	assert.NotEqual(t, current.stamp, legacy.stamp, "the stamp records the layout")

	replicated, err := composeFactsTable(Config{Database: "boxer", Table: "facts"}, "ReplicatedMergeTree('/t', 'r') ORDER BY `"+factsschema.TsColumn+"`")
	require.NoError(t, err)
	assert.NotContains(t, replicated.sql, recordstore.DedupWindowSetting, "the window is a non-replicated table's")
}

// SETTINGS in the clause would turn the table's settings into the
// statement's; the composer refuses rather than drop the window.
func TestComposeSetupSQL_RefusesSettingsInTheClause(t *testing.T) {
	_, err := ComposeSetupSQL(Config{Database: "boxer", Table: "facts"}, legacyEngineClause+" SETTINGS index_granularity = 8192")
	require.Error(t, err)
	_, err = ComposeSetupSQL(Config{Database: "boxer", Table: "facts"}, "MergeTree() ORDER BY (")
	require.Error(t, err)
}

// The contract declares the build's layout: a table left by an older build
// is ok with a note pointing at the recipe, and the build's own is ok
// without one.
func TestSchemaContract_JudgesTheLayout(t *testing.T) {
	c, err := SchemaContract(Config{Database: "boxer", Table: "facts"})
	require.NoError(t, err)
	assert.Equal(t, factsschema.MigrationRecipe, c.LayoutRecipe)
	require.False(t, c.Layout.IsZero())
	live := make([]recordstore.LiveColumn, 0, len(c.Columns))
	for _, col := range c.Columns {
		live = append(live, recordstore.LiveColumn{Name: col.Name, Type: col.Type})
	}
	old := recordstore.JudgeFacts(c, recordstore.TableFacts{Exists: true, Engine: "MergeTree", SortingKey: "`" + factsschema.TsColumn + "`", Columns: live})
	assert.Equal(t, recordstore.SchemaVerdictOk, old.Verdict)
	assert.True(t, old.LayoutDiffers)
	assert.Contains(t, strings.Join(old.Notes, "\n"), factsschema.MigrationRecipe)

	fresh := recordstore.JudgeFacts(c, recordstore.TableFacts{Exists: true, Engine: "MergeTree",
		PartitionKey: "toYYYYMM(`" + factsschema.TsColumn + "`)", SortingKey: "`" + factsschema.TsColumn + "`",
		Comment: c.Stamp, Columns: live})
	assert.False(t, fresh.LayoutDiffers)
	assert.Equal(t, recordstore.StampAgeSame, fresh.Age)
}

// A store whose lease cannot be had writes all the same: the id is minted
// under the process-local stream, and the lease is not retried at once.
func TestMintId_FallsBackToTheLocalStream(t *testing.T) {
	s, err := New(Config{URL: "http://127.0.0.1:1/", Database: "boxer", Table: "facts"})
	require.NoError(t, err)
	a, err := s.mintId()
	require.NoError(t, err)
	b, err := s.mintId()
	require.NoError(t, err)
	for _, id := range []uint64{a, b} {
		tagged := identifier.TaggedId(id)
		require.True(t, tagged.IsValid())
		assert.Equal(t, vocab.IdStreamFactsRowLocal.Value(), tagged.GetTag().GetValue())
	}
	assert.Less(t, a, b, "local ids increase within the process")
	assert.Greater(t, s.ids.retryAt.Load(), int64(0), "the failed lease is not retried before the interval")
}
