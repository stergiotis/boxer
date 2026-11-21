package trail_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The golden states the layout chstore creates boxer.facts with (ADR-0397):
// one engine clause, read by both.
func TestTheGoldenStatesTheAppliedLayout(t *testing.T) {
	golden, err := os.ReadFile("facts_ddl_clickhouse.out.sql")
	require.NoError(t, err)
	applied, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, factsschema.DefaultEngineClause)
	require.NoError(t, err)
	require.True(t, engineLayout(t, string(golden)).Equal(engineLayout(t, applied)))
}

// engineLayout reads the layout out of a CREATE TABLE: the clause from
// ENGINE to the SETTINGS line.
func engineLayout(t *testing.T, ddl string) recordstore.TableLayout {
	t.Helper()
	at := strings.LastIndex(ddl, ") ENGINE")
	require.GreaterOrEqual(t, at, 0)
	clause := ddl[at+1:]
	if end := strings.Index(clause, "\nSETTINGS"); end >= 0 {
		clause = clause[:end]
	}
	l, _, err := recordstore.ParseEngineClause(clause)
	require.NoError(t, err)
	return l
}
