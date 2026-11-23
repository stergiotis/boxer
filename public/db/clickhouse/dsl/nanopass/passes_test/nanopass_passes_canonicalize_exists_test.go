package passes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// existsSugar pairs an EXISTS with the call ClickHouse's parser holds it as:
// formatQuery and EXPLAIN SYNTAX on ClickHouse 26.9 print
// `exists((query))` for every spelling. The rule quotes the name, because
// bare the word followed by a parenthesis is the predicate again; the
// server reads the quoted call alike.
var existsSugar = []struct {
	name string
	in   string
	want string
}{
	{"keyword", "SELECT a FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.k = t.k)",
		`SELECT a FROM t WHERE "exists"((SELECT 1 FROM u WHERE u.k = t.k))`},
	{"call_spelling", "SELECT exists(SELECT 1 FROM u) AS e", `SELECT "exists"((SELECT 1 FROM u)) AS e`},
	{"any_case", "SELECT Exists (SELECT 1), eXiStS(SELECT 1)", `SELECT "exists"((SELECT 1)), "exists"((SELECT 1))`},
	{"negated", "SELECT a FROM t WHERE NOT EXISTS (SELECT 1 FROM u)", `SELECT a FROM t WHERE NOT "exists"((SELECT 1 FROM u))`},
	{"own_parentheses_kept", "SELECT exists((SELECT 1)), EXISTS ( (SELECT 1) )", `SELECT "exists"((SELECT 1)), "exists"( (SELECT 1) )`},
	{"union", "SELECT exists(SELECT 1 UNION ALL SELECT 2)", `SELECT "exists"((SELECT 1 UNION ALL SELECT 2))`},
	{"union_of_parenthesised", "SELECT exists((SELECT 1) UNION ALL (SELECT 2))", `SELECT "exists"(((SELECT 1) UNION ALL (SELECT 2)))`},
	{"with", "SELECT exists(WITH c AS (SELECT 1) SELECT * FROM c)", `SELECT "exists"((WITH c AS (SELECT 1) SELECT * FROM c))`},
	{"nested", "SELECT exists(SELECT 1 FROM t WHERE EXISTS (SELECT 2 FROM u))",
		`SELECT "exists"((SELECT 1 FROM t WHERE "exists"((SELECT 2 FROM u))))`},
	{"sugar_inside", "SELECT EXISTS (SELECT 1 FROM t WHERE d = DATE '2020-01-01')",
		`SELECT "exists"((SELECT 1 FROM t WHERE d = toDate('2020-01-01')))`},
	{"inside_a_filter", "SELECT sum(x) FILTER (WHERE EXISTS (SELECT 1 FROM u)) FROM t",
		`SELECT sumIf(x, "exists"((SELECT 1 FROM u))) FROM t`},
	{"comment_kept", "SELECT EXISTS /* why */ (SELECT 1)", `SELECT "exists" /* why */ ((SELECT 1))`},
	{"operand", "SELECT EXISTS (SELECT 1) + 1, -exists(SELECT 1), exists(SELECT 1)::UInt8",
		`SELECT "exists"((SELECT 1)) + 1, -"exists"((SELECT 1)), "exists"((SELECT 1))::UInt8`},
	{"select_and_from_as_words", "SELECT exists(SELECT * FROM db.t)", `SELECT "exists"((SELECT * FROM db.t))`},
}

func TestCanonicalizeSugarExists(t *testing.T) {
	for _, tc := range existsSugar {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nanopass.FixedPoint(passes.CanonicalizeSugar, 16).Run(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			again, err := passes.CanonicalizeSugar.Run(got)
			require.NoError(t, err)
			assert.Equal(t, got, again, "not idempotent")

			canonical, err := passes.CanonicalizeFull(100).Run(tc.in)
			require.NoError(t, err)
			_, err = nanopass.ParseCanonical(canonical)
			assert.NoError(t, err, "canonical form outside grammar2: %s", canonical)
			again, err = passes.CanonicalizeFull(100).Run(canonical)
			require.NoError(t, err)
			assert.Equal(t, canonical, again, "pipeline not idempotent")

			// The canonical form reads the tables and calls the authored
			// text reads, in the same order.
			authored, err := nanopass.Parse(tc.in)
			require.NoError(t, err)
			rewritten, err := nanopass.Parse(canonical)
			require.NoError(t, err)
			assert.Equal(t, analysis.ExtractTables(authored), analysis.ExtractTables(rewritten), canonical)
			assert.Equal(t, len(nanopass.FlattenScopes(mustScopes(t, authored))), len(nanopass.FlattenScopes(mustScopes(t, rewritten))), canonical)
		})
	}
}

func mustScopes(t *testing.T, pr *nanopass.ParseResult) []*nanopass.SelectScope {
	t.Helper()
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	return scopes
}

// TestCanonicalizeFullExists: the canonical form is the quoted call with the
// query as its one subquery argument; the word as a name keeps its spelling.
func TestCanonicalizeFullExists(t *testing.T) {
	for sql, canonical := range map[string]string{
		"SELECT a FROM t WHERE NOT EXISTS (SELECT 1 FROM u WHERE u.k = t.k)": `SELECT "a" FROM "t" WHERE NOT "exists"((SELECT 1 FROM "u" WHERE "u"."k" = "t"."k"))`,
		"SELECT exists(SELECT * FROM db.t) AS Exists":                        `SELECT "exists"((SELECT * FROM "db"."t")) AS "Exists"`,
		"SELECT exists((SELECT 1))":                                          `SELECT "exists"((SELECT 1))`,
		`SELECT "exists"((SELECT 1))`:                                        `SELECT "exists"((SELECT 1))`,
		"SELECT Exists, t.EXISTS FROM t":                                     `SELECT "Exists", "t"."EXISTS" FROM "t"`,
	} {
		got, err := passes.CanonicalizeFull(100).Run(sql)
		require.NoError(t, err, sql)
		assert.Equal(t, canonical, got, sql)
	}
}
