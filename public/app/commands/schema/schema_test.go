package schema

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/schemaplan"
)

func TestContractsNameEveryStoredTableOnce(t *testing.T) {
	cs, err := Contracts()
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, c := range cs {
		require.NotEmpty(t, c.Columns, c.Table)
		require.False(t, seen[c.Table], "listed twice: %s", c.Table)
		seen[c.Table] = true
	}
	assert.True(t, seen["boxer.persiststate"])
	assert.GreaterOrEqual(t, len(cs), 10)
}

func TestPrintWritesRunnableSQL(t *testing.T) {
	var b bytes.Buffer
	Print(&b, []schemaplan.Plan{
		{Verdict: recordstore.SchemaVerdict{Table: "db.t", Verdict: recordstore.SchemaVerdictMigratable, Missing: []string{"c"}}, Statements: []string{"ALTER TABLE `db`.`t` ADD COLUMN IF NOT EXISTS `c` UInt8 AFTER `b`"}},
		{Verdict: recordstore.SchemaVerdict{Table: "db.u", Verdict: recordstore.SchemaVerdictIncompatible}, Refused: []string{"column x is UInt16 on the table, UInt8 generated"}},
	})
	assert.Equal(t, "-- db.t: migratable — 1 column(s) missing (c); `boxer schema plan` lists the statements\n"+
		"ALTER TABLE `db`.`t` ADD COLUMN IF NOT EXISTS `c` UInt8 AFTER `b`;\n"+
		"-- db.u: incompatible; see doc/migration/\n"+
		"--   refused: column x is UInt16 on the table, UInt8 generated\n", b.String())
}
