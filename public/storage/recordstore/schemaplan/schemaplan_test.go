package schemaplan

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// fixture reads a generated DDL golden copied from history, as the
// contract a store declares and as the live table an older binary left.
func fixture(t *testing.T, name string) (ddl string, cols []recordstore.ColumnSpec) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	ddl = string(raw)
	cols, err = recordstore.ParseCreateTableColumns(ddl)
	require.NoError(t, err)
	return
}

func live(cols []recordstore.ColumnSpec) (out []recordstore.LiveColumn) {
	for _, c := range cols {
		out = append(out, recordstore.LiveColumn{Name: c.Name, Type: c.Type, Codec: "CODEC(" + c.Codec + ")"})
	}
	return
}

func contract(table string, cols []recordstore.ColumnSpec) recordstore.TableContract {
	stamp := recordstore.TableStamp{Kind: "t", Columns: recordstore.ColumnsDigest(cols), Naming: recordstore.LeewayNamingVersion}.String()
	return recordstore.TableContract{Kind: "t", Table: table, Columns: cols, Stamp: stamp}
}

// A section added to the state table (two typed sections, twenty columns)
// is an additive change: each column is added after the one before it in
// the generated order, the window is set, and the table is stamped last.
func TestAdditiveSectionsAreAddedInOrder(t *testing.T) {
	beforeDDL, before := fixture(t, "persiststate_before.sql")
	_, after := fixture(t, "persiststate_after.sql")
	c := contract("boxer.persiststate", after)
	p := Derive(c, recordstore.TableFacts{Exists: true, Engine: "MergeTree", CreateQuery: beforeDDL, Columns: live(before)})
	require.False(t, p.IsRefused(), p.Refused)
	assert.Equal(t, recordstore.SchemaVerdictMigratable, p.Verdict.Verdict)
	require.Len(t, p.Statements, 20+2)
	assert.Equal(t, "ALTER TABLE `boxer`.`persiststate` ADD COLUMN IF NOT EXISTS `tv:stateRunId:value:val:s:24:::0::data` Array(LowCardinality(String)) CODEC(ZSTD(3)) AFTER `tv:stateKey:lmrcard:lmrcard:u64:4E:::0::data` SETTINGS allow_suspicious_low_cardinality_types = 1", p.Statements[0])
	assert.Equal(t, "ALTER TABLE `boxer`.`persiststate` ADD COLUMN IF NOT EXISTS `tv:stateRunId:hr:hr:u64:47:::0::data` Array(UInt64) CODEC(Delta,ZSTD(3)) AFTER `tv:stateRunId:value:val:s:24:::0::data`", p.Statements[1])
	assert.Equal(t, "ALTER TABLE `boxer`.`persiststate` MODIFY SETTING non_replicated_deduplication_window = 1000", p.Statements[20])
	assert.Equal(t, "ALTER TABLE `boxer`.`persiststate` MODIFY COMMENT '"+c.Stamp+"'", p.Statements[21])
}

// The aspects v2 codec flip renamed every column, the key columns among
// them: ClickHouse renames no key column, so the plan is refused.
func TestAspectRenameOfAKeyColumnIsRefused(t *testing.T) {
	v1DDL, v1 := fixture(t, "device_aspects_v1.sql")
	_, v2 := fixture(t, "device_aspects_v2.sql")
	p := Derive(contract("device", v2), recordstore.TableFacts{Exists: true, Engine: "MergeTree", CreateQuery: v1DDL, Columns: live(v1)})
	assert.Equal(t, recordstore.SchemaVerdictMigratable, p.Verdict.Verdict)
	assert.Len(t, p.Verdict.Renames, len(v2))
	assert.Empty(t, p.Verdict.Missing)
	require.True(t, p.IsRefused())
	assert.Empty(t, p.Statements)
	assert.Contains(t, strings.Join(p.Refused, "\n"), "id:id:u64:2k:0:0:")
}

// An aspect-only rename of a column nothing else names is a RENAME and a
// MODIFY to the generated codec.
func TestAspectRenameOfAPlainColumnIsMechanical(t *testing.T) {
	v2DDL, v2 := fixture(t, "device_aspects_v2.sql")
	_, v1 := fixture(t, "device_aspects_v1.sql")
	const at = 4 // tv:symbol:hr:… — not in a key or the index
	require.True(t, strings.HasPrefix(v2[at].Name, "tv:symbol:hr:"))
	old := append([]recordstore.ColumnSpec(nil), v2...)
	old[at] = v1[at]
	oldDDL := strings.Replace(v2DDL, `"`+v2[at].Name+`"`, `"`+v1[at].Name+`"`, 1)
	c := contract("device", v2)
	p := Derive(c, recordstore.TableFacts{Exists: true, Engine: "MergeTree", Comment: c.Stamp, CreateQuery: oldDDL + " SETTINGS non_replicated_deduplication_window = 1000", Columns: live(old)})
	require.False(t, p.IsRefused(), p.Refused)
	assert.Equal(t, []string{
		"ALTER TABLE `device` RENAME COLUMN `" + v1[at].Name + "` TO `" + v2[at].Name + "`",
		"ALTER TABLE `device` MODIFY COLUMN `" + v2[at].Name + "` Array(UInt64) CODEC(Delta,ZSTD(3))",
	}, p.Statements)
}

func TestIncompatibleAndAbsentAreRefused(t *testing.T) {
	_, cols := fixture(t, "persiststate_after.sql")
	c := contract("boxer.persiststate", cols)
	changed := live(cols)
	changed[2].Type = "UInt16"
	p := Derive(c, recordstore.TableFacts{Exists: true, Engine: "MergeTree", Columns: changed})
	require.True(t, p.IsRefused())
	assert.Empty(t, p.Statements)
	assert.Contains(t, p.Refused[0], MigrationDocs)

	p = Derive(c, recordstore.TableFacts{})
	require.True(t, p.IsRefused())
	assert.Equal(t, recordstore.SchemaVerdictAbsent, p.Verdict.Verdict)
}

// A table already in line yields nothing; a comment no store wrote is left
// alone.
func TestNothingToDoAndAForeignComment(t *testing.T) {
	_, cols := fixture(t, "persiststate_after.sql")
	c := contract("boxer.persiststate", cols)
	p := Derive(c, recordstore.TableFacts{Exists: true, Engine: "MergeTree", Comment: c.Stamp, CreateQuery: "… non_replicated_deduplication_window = 1000", Columns: live(cols)})
	assert.Empty(t, p.Statements)
	assert.False(t, p.IsRefused())

	p = Derive(c, recordstore.TableFacts{Exists: true, Engine: "ReplicatedMergeTree", Comment: "an operator's note", Columns: live(cols)})
	assert.Empty(t, p.Statements, "a replicated table keeps its own window, and the note stays")
	assert.NotEmpty(t, p.Verdict.Notes)
}
