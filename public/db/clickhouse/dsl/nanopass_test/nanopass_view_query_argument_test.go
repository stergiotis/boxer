package nanopass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// A bare query is admitted as a table function's argument: `view(SELECT …)`
// did not parse in grammar1, so a buffer using it could be neither
// canonicalised nor classified. Its canonical form parses under grammar2, and
// the tables inside it are inventoried like any subquery's.
func TestParse_ViewTakesAQuery(t *testing.T) {
	canonical := passes.CanonicalizeFull(10)
	for _, sql := range []string{
		"SELECT * FROM view(SELECT 1 AS x)",
		"SELECT * FROM view(SELECT a FROM t WHERE b = 1 UNION ALL SELECT a FROM u)",
		"WITH c AS (SELECT 1) SELECT * FROM view(WITH d AS (SELECT 2) SELECT * FROM d)",
		"SELECT * FROM remote('h', view(SELECT 1))",
	} {
		t.Run(sql, func(t *testing.T) {
			_, err := nanopass.Parse(sql)
			require.NoError(t, err)
			out, err := canonical.Run(sql)
			require.NoError(t, err)
			_, err = nanopass.ParseCanonical(out)
			require.NoError(t, err, "the canonical form parses under grammar2: %s", out)
		})
	}
	pr, err := nanopass.Parse("SELECT * FROM view(SELECT a FROM db.t)")
	require.NoError(t, err)
	assert.Contains(t, analysis.ExtractTables(pr), analysis.TableRef{Database: "db", Table: "t"})
}
