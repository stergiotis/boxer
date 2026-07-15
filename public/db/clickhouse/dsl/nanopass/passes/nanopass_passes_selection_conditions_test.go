package passes_test

import (
	"errors"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/require"
)

// schema for the tests: tt is the ADR's worked example, u a filter source, and
// collide a table that already carries a cond_1 column (§SD4).
func condTestSchema() passes.SchemaProviderI {
	return passes.NewStaticSchemaProvider(map[string][]string{
		"tt":      {"a", "b", "c", "d"},
		"u":       {"t"},
		"other":   {"mycol1", "mycol2"},
		"collide": {"a", "c", "cond_1"},
	})
}

func condPass() nanopass.Pass {
	return passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{Schema: condTestSchema()})
}

func TestExposeConditions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			// §SD1: the conjunction is OR-free, so it groups into ONE condition
			// — it has exactly one way to be satisfied.
			name:     "adr worked example",
			input:    "SELECT a,b FROM tt WHERE c = 1 AND d IN (SELECT t FROM u)",
			expected: "SELECT a,b, (c = 1 AND d IN (SELECT t FROM u)) AS cond_1 FROM tt WHERE cond_1",
		},
		{
			name:     "single predicate",
			input:    "SELECT a FROM tt WHERE c = 1",
			expected: "SELECT a, (c = 1) AS cond_1 FROM tt WHERE cond_1",
		},
		{
			name:     "star projection",
			input:    "SELECT * FROM tt WHERE c = 1",
			expected: "SELECT *, (c = 1) AS cond_1 FROM tt WHERE cond_1",
		},
		{
			// §SD1: the disjuncts are what discriminate, so they stay apart —
			// and the AND inside one of them groups. Structure is preserved
			// byte-for-byte around the substitutions.
			name:     "or splits, its conjunction groups",
			input:    "SELECT a FROM tt WHERE (a = 1 AND b = 2) OR c = 3",
			expected: "SELECT a, (a = 1 AND b = 2) AS cond_1, (c = 3) AS cond_2 FROM tt WHERE cond_1 OR cond_2",
		},
		{
			// A conjunction straddling an OR is NOT grouped whole — that would
			// throw away the inner OR, the only part that discriminates.
			// `NOT (x)` parses as a *function call* named NOT (not
			// ColumnExprNot), and it is how NOT is normally written, so the
			// call spelling has to be recognised as a connective.
			name:     "and containing an or recurses",
			input:    "SELECT a FROM tt WHERE NOT (a = 5) AND (b = 2 OR c = 3)",
			expected: "SELECT a, (NOT (a = 5)) AS cond_1, (b = 2) AS cond_2, (c = 3) AS cond_3 FROM tt WHERE cond_1 AND (cond_2 OR cond_3)",
		},
		{
			// The paren-free spelling is the one that parses as ColumnExprNot.
			// OR-free either way, so it is one condition whole.
			name:     "not operator form groups",
			input:    "SELECT a FROM tt WHERE NOT a = 5",
			expected: "SELECT a, (NOT a = 5) AS cond_1 FROM tt WHERE cond_1",
		},
		{
			name:     "and/or function spellings",
			input:    "SELECT a FROM tt WHERE and(a = 1, or(b = 2, c = 3))",
			expected: "SELECT a, (a = 1) AS cond_1, (b = 2) AS cond_2, (c = 3) AS cond_3 FROM tt WHERE and(cond_1, or(cond_2, cond_3))",
		},
		{
			// A non-connective call is a condition whole, never recursed into.
			name:     "ordinary function is one condition",
			input:    "SELECT a FROM tt WHERE startsWith(a, 'x') OR b = 2",
			expected: "SELECT a, (startsWith(a, 'x')) AS cond_1, (b = 2) AS cond_2 FROM tt WHERE cond_1 OR cond_2",
		},
		{
			// A lambda has no columnExpr argument; the call must stay a condition
			// rather than be mistaken for a connective.
			name:     "lambda argument stays one condition",
			input:    "SELECT a FROM tt WHERE arrayExists(x -> x > 1, d) OR b = 2",
			expected: "SELECT a, (arrayExists(x -> x > 1, d)) AS cond_1, (b = 2) AS cond_2 FROM tt WHERE cond_1 OR cond_2",
		},
		{
			// The load-bearing scope rule: an OR inside a filter subquery is
			// the subquery's structure, not this predicate's. Counting it would
			// wrongly split a conjunction that must group.
			name:     "or inside a filter subquery does not split the grouping",
			input:    "SELECT a FROM tt WHERE c = 1 AND d IN (SELECT t FROM u WHERE t = 1 OR t = 2)",
			expected: "SELECT a, (c = 1 AND d IN (SELECT t FROM u WHERE t = 1 OR t = 2)) AS cond_1 FROM tt WHERE cond_1",
		},
		{
			name:     "three disjuncts",
			input:    "SELECT a FROM tt WHERE a = 1 OR b = 2 OR c = 3",
			expected: "SELECT a, (a = 1) AS cond_1, (b = 2) AS cond_2, (c = 3) AS cond_3 FROM tt WHERE cond_1 OR cond_2 OR cond_3",
		},
		{
			// OR-free however it nests, so the whole thing is one condition.
			name:     "nested and groups whole",
			input:    "SELECT a FROM tt WHERE a = 1 AND (b = 2 AND c = 3)",
			expected: "SELECT a, (a = 1 AND (b = 2 AND c = 3)) AS cond_1 FROM tt WHERE cond_1",
		},
		{
			// A condition that is itself a parenthesised group must not be
			// wrapped a second time.
			name:     "parenthesised condition is not double-wrapped",
			input:    "SELECT a FROM tt WHERE (a = 1) OR c = 3",
			expected: "SELECT a, (a = 1) AS cond_1, (c = 3) AS cond_2 FROM tt WHERE cond_1 OR cond_2",
		},
		{
			name:     "qualified columns and a table alias",
			input:    "SELECT t.a FROM tt AS t WHERE t.c = 1",
			expected: "SELECT t.a, (t.c = 1) AS cond_1 FROM tt AS t WHERE cond_1",
		},
		{
			// The WHERE keeps its trivia; only the leaf spans are replaced.
			name:     "comments and whitespace survive",
			input:    "SELECT a FROM tt WHERE /* keep */ c = 1",
			expected: "SELECT a, (c = 1) AS cond_1 FROM tt WHERE /* keep */ cond_1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := condPass().Run(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// TestExposeConditionsGates covers §SD2/§SD3: everything the pass declines, each
// passing through byte-identical.
func TestExposeConditionsGates(t *testing.T) {
	tests := []struct {
		name  string
		input string
		reason string
	}{
		{name: "no where", input: "SELECT a FROM tt", reason: "no predicate to report on"},
		{name: "aggregate is not passthrough", input: "SELECT count() FROM tt WHERE c = 1", reason: "ADR-0117 taints it out"},
		{name: "alias is not passthrough", input: "SELECT a AS x FROM tt WHERE c = 1", reason: "any alias taints the table out"},
		{name: "expression is not passthrough", input: "SELECT mycol1, mycol2+3 FROM other WHERE mycol1 = 1", reason: "derived column"},
		{name: "group by", input: "SELECT a FROM tt WHERE c = 1 GROUP BY a", reason: "blocking clause"},
		{name: "distinct", input: "SELECT DISTINCT a FROM tt WHERE c = 1", reason: "blocking clause"},
		{name: "join", input: "SELECT tt.a FROM tt, u WHERE tt.c = 1", reason: "not a single source"},
		{name: "union all", input: "SELECT a FROM tt WHERE c = 1 UNION ALL SELECT t FROM u WHERE t = 2", reason: "§SD3: condition counts cannot align"},
		{name: "unknown table", input: "SELECT a FROM nosuchtable WHERE c = 1", reason: "no schema → cannot prove a name free"},
		{name: "table function", input: "SELECT number FROM numbers(10) WHERE number = 1", reason: "not a stored relation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := condPass().Run(tt.input)
			require.NoError(t, err, tt.reason)
			require.Equal(t, tt.input, got, "expected untouched (%s)", tt.reason)
		})
	}
}

// TestExposeConditionsCollisionErrors is §SD4: a condition name that is already a
// column of the table must fail the pass, not silently shadow the column.
func TestExposeConditionsCollisionErrors(t *testing.T) {
	_, err := condPass().Run("SELECT a FROM collide WHERE c = 1")
	require.Error(t, err)
	require.ErrorIs(t, err, passes.ErrConditionNameCollision)

	// The collision is checked against every column, not only the projected
	// ones: the WHERE reference would bind to the alias either way.
	_, err = condPass().Run("SELECT a, cond_1 FROM collide WHERE c = 1")
	require.Error(t, err)
	require.ErrorIs(t, err, passes.ErrConditionNameCollision)

	// A different prefix dodges it.
	p := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{Schema: condTestSchema(), Prefix: "w_"})
	got, err := p.Run("SELECT a FROM collide WHERE c = 1")
	require.NoError(t, err)
	require.Equal(t, "SELECT a, (c = 1) AS w_1 FROM collide WHERE w_1", got)
}

// TestExposeConditionsNilSchemaIsInert guards the probe pass the registry builds
// for its catalog row: it must never rewrite.
func TestExposeConditionsNilSchemaIsInert(t *testing.T) {
	p := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{})
	const q = "SELECT a FROM tt WHERE c = 1"
	got, err := p.Run(q)
	require.NoError(t, err)
	require.Equal(t, q, got)
}

// stubNamer stands in for a domain namer (lwsql's is tested in its own package).
type stubNamer struct {
	names []string
	ok    bool
	err   error
}

func (inst *stubNamer) NameConditions(dbName string, tableName string, n int) ([]string, bool, error) {
	if inst.err != nil {
		return nil, false, inst.err
	}
	if !inst.ok {
		return nil, false, nil
	}
	return inst.names[:n], true, nil
}

func TestExposeConditionsNamer(t *testing.T) {
	t.Run("domain names are quoted when not bare identifiers", func(t *testing.T) {
		p := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
			Schema: condTestSchema(),
			Namer:  &stubNamer{names: []string{"tv:conditions:c1:val:b:0:0:0:0::"}, ok: true},
		})
		got, err := p.Run("SELECT a FROM tt WHERE c = 1")
		require.NoError(t, err)
		require.Equal(t,
			`SELECT a, (c = 1) AS "tv:conditions:c1:val:b:0:0:0:0::" FROM tt WHERE "tv:conditions:c1:val:b:0:0:0:0::"`,
			got)
		_, err = nanopass.Parse(got)
		require.NoError(t, err)
	})

	t.Run("ok=false falls back to plain naming", func(t *testing.T) {
		p := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
			Schema: condTestSchema(),
			Namer:  &stubNamer{ok: false},
		})
		got, err := p.Run("SELECT a FROM tt WHERE c = 1")
		require.NoError(t, err)
		require.Equal(t, "SELECT a, (c = 1) AS cond_1 FROM tt WHERE cond_1", got)
	})

	t.Run("namer error refuses the rewrite", func(t *testing.T) {
		sentinel := errors.New("section already exists")
		p := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
			Schema: condTestSchema(),
			Namer:  &stubNamer{err: sentinel},
		})
		_, err := p.Run("SELECT a FROM tt WHERE c = 1")
		require.ErrorIs(t, err, sentinel)
	})
}

// TestExposeConditionsIdempotentByClassifier is the §Consequences claim: the pass's
// own output aliases the projection, which taints the table out of the ADR-0117
// classifier, so a second Apply is a no-op. This is what makes the declared
// Idempotent honest without a re-entrance guard.
func TestExposeConditionsIdempotentByClassifier(t *testing.T) {
	p := condPass()
	once, err := p.Run("SELECT a,b FROM tt WHERE c = 1 AND d IN (SELECT t FROM u)")
	require.NoError(t, err)
	twice, err := p.Run(once)
	require.NoError(t, err)
	require.Equal(t, once, twice, "second application must be a no-op")
}

func TestExposeConditionsProperties(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	corpus := make([]string, 0, len(entries)+4)
	for _, e := range entries {
		corpus = append(corpus, e.SQL)
	}
	// The corpus tables are unknown to the static schema, so add entries that
	// actually exercise the rewrite.
	corpus = append(corpus,
		"SELECT a,b FROM tt WHERE c = 1 AND d IN (SELECT t FROM u)",
		"SELECT a FROM tt WHERE (a = 1 AND b = 2) OR c = 3",
		"SELECT * FROM tt WHERE c = 1",
		"SELECT a FROM tt",
	)
	nanopass.AssertProperties(t, condPass(), corpus)
}

func TestExposeConditionsInvalidInput(t *testing.T) {
	for _, in := range []string{"", "   ", "SELECT", ";;;"} {
		_, err := condPass().Run(in)
		require.Error(t, err, "expected rejection of %q", in)
	}
}

func TestExposeConditionsCorpusStaysValid(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	p := condPass()
	for _, e := range entries {
		got, runErr := p.Run(e.SQL)
		if runErr != nil {
			continue // a pass may legitimately refuse an entry
		}
		_, parseErr := nanopass.Parse(got)
		require.NoError(t, parseErr, "%s: produced invalid SQL: %s", e.Name, got)
	}
}
