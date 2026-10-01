package analysis

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustSecurity(t *testing.T, sql string) (class QuerySecurityClassE, witnesses []SecurityWitness) {
	t.Helper()
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err, "parse: %s", sql)
	class, witnesses, err = ClassifyQuerySecurity(pr)
	require.NoError(t, err, "classify: %s", sql)
	return
}

func witnessNames(witnesses []SecurityWitness) (names []string) {
	names = make([]string, len(witnesses))
	for i, w := range witnesses {
		names[i] = w.Name
	}
	return
}

func TestClassifyQuerySecurity(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		class     QuerySecurityClassE
		witnesses []string
	}{
		// Plain reads — including aggregates and derivations, which transform
		// the result but touch nothing beyond the endpoint's own data.
		{"plain_select", `SELECT a FROM t`, QuerySecurityRead, nil},
		{"aggregate", `SELECT sum(a) FROM t GROUP BY b`, QuerySecurityRead, nil},
		{"no_from", `SELECT 1`, QuerySecurityRead, nil},

		// The param prelude is the parameter channel, not a settings change.
		{"param_prelude", `SET param_a = 1; SELECT {a:UInt64} FROM t`, QuerySecurityRead, nil},

		// A query-tail SETTINGS clause is a per-query execution knob, not a
		// state change (see the ClassifyQuerySecurity contract).
		{"settings_clause", `SELECT a FROM t SETTINGS max_execution_time = 10`, QuerySecurityRead, nil},

		// Non-param SET statements witness a settings change.
		{"set_setting", `SET max_threads = 4; SELECT 1`, QuerySecurityMutating, []string{"max_threads"}},
		{"set_mixed", `SET param_a = 1, max_threads = 4; SELECT {a:UInt64}`, QuerySecurityMutating, []string{"max_threads"}},

		// Egress table functions — by denied allowlist membership.
		{"egress_url", `SELECT * FROM url('http://host/data.csv', 'CSV')`, QuerySecurityReadEgress, []string{"url"}},
		{"egress_case_fold", `SELECT * FROM Url('http://host/data.csv', 'CSV')`, QuerySecurityReadEgress, []string{"Url"}},
		{"egress_unknown_tf", `SELECT * FROM shinynewtf(1)`, QuerySecurityReadEgress, []string{"shinynewtf"}},
		{"egress_in_cte", `WITH src AS (SELECT * FROM remote('host', 'db', 't')) SELECT * FROM src`, QuerySecurityReadEgress, []string{"remote"}},
		{"egress_in_subquery", `SELECT * FROM (SELECT * FROM s3('http://host/x', 'CSV'))`, QuerySecurityReadEgress, []string{"s3"}},

		// Local table functions stay read; keelson('…') classifies
		// pre-expansion (ADR-0132 §SD5).
		{"local_numbers", `SELECT * FROM numbers(10)`, QuerySecurityRead, nil},
		{"local_keelson", `SELECT * FROM keelson('env')`, QuerySecurityRead, nil},

		// Scalars that reach out, and one that changes server state.
		{"egress_scalar_file", `SELECT file('/etc/hostname')`, QuerySecurityReadEgress, []string{"file"}},
		{"egress_scalar_ai", `SELECT aiGenerate(note) FROM t`, QuerySecurityReadEgress, []string{"aiGenerate"}},
		{"state_changing_scalar", `SELECT generateSerialID('orders')`, QuerySecurityMutating, []string{"generateSerialID"}},

		// eval() runs a query built at run time.
		{"eval", `SELECT * FROM eval('SELECT 1')`, QuerySecurityReadEgress, []string{"eval"}},

		// A call in a table function's arguments is an expression, as the
		// server reads it: the canonical tuple(…)/array(…) of a row literal,
		// a scalar call, an egress scalar.
		{"args_tuple", `SELECT * FROM values('a String, b String', ('x', 'y'))`, QuerySecurityRead, nil},
		{"args_tuple_canonical", `SELECT * FROM values('a String, b String', tuple('x', 'y'))`, QuerySecurityRead, nil},
		{"args_array_canonical", `SELECT * FROM values('a Array(UInt8)', array(1, 2))`, QuerySecurityRead, nil},
		{"args_scalar_call", `SELECT * FROM merge(currentDatabase(), '^t')`, QuerySecurityRead, nil},
		{"args_no_false_witness", `SELECT * FROM url(concat('http://', 'h'), 'CSV')`, QuerySecurityReadEgress, []string{"url"}},
		{"args_egress_scalar", `SELECT * FROM values('x String', file('/etc/passwd'))`, QuerySecurityReadEgress, []string{"file"}},

		// …except a table function among the arguments of one that takes a
		// table, which is read as a table; under a local one an unknown name
		// is read as a table too (fail closed).
		{"loop_local", `SELECT * FROM loop(numbers(3))`, QuerySecurityRead, nil},
		{"loop_egress", `SELECT * FROM loop(url('http://h/x', 'CSV'))`, QuerySecurityReadEgress, []string{"url"}},
		{"loop_unknown", `SELECT * FROM loop(shinynew(1))`, QuerySecurityReadEgress, []string{"shinynew"}},
		{"remote_args", `SELECT * FROM remote(concat('h', ':9000'), 'db', 't')`, QuerySecurityReadEgress, []string{"remote"}},

		// Witness order is source order; the class is the strongest witness.
		{"multi_witness", `SET foo = 1; SELECT file('a') FROM url('http://h/x', 'CSV')`,
			QuerySecurityMutating, []string{"foo", "file", "url"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, witnesses := mustSecurity(t, tc.sql)
			assert.Equal(t, tc.class, class, "class of %s", tc.sql)
			assert.Equal(t, tc.witnesses, append([]string(nil), witnessNames(witnesses)...), "witnesses of %s", tc.sql)
			for _, w := range witnesses {
				assert.False(t, w.Src.Empty(), "witness %q carries no source range", w.Name)
			}
		})
	}
}

// TestClassifyQuerySecurityZeroValue pins the fail-closed invariant: an
// uninitialized class must be the strongest one, so enum reordering cannot
// silently weaken a defaulted classification.
func TestClassifyQuerySecurityZeroValue(t *testing.T) {
	var zero QuerySecurityClassE
	assert.Equal(t, QuerySecurityMutating, zero)
	assert.Equal(t, "mutating", zero.String())
}

// TestClassifyQuerySecurityParseContract documents the caller contract:
// every statement form outside grammar1's `SET* … SELECT` root — including
// all genuinely mutating forms — is a parse error, which the caller must
// treat as "cannot classify → mutating".
// TestClassifyQuerySecurityInsertWrapper pins the one mutating form grammar1
// parses (ADR-0181 §SD8): it must be witnessed from the tree, not fall
// through to "read" for lack of a parse error.
func TestClassifyQuerySecurityInsertWrapper(t *testing.T) {
	pr, err := nanopass.Parse("INSERT INTO db.t SELECT x FROM src")
	assert.NoError(t, err)
	class, witnesses, err := ClassifyQuerySecurity(pr)
	assert.NoError(t, err)
	assert.Equal(t, QuerySecurityMutating, class)
	if assert.Len(t, witnesses, 1) {
		assert.Equal(t, SecurityWitnessInsertWrapper, witnesses[0].Kind)
		assert.Equal(t, "db.t", witnesses[0].Name)
	}
}

func TestClassifyQuerySecurityParseContract(t *testing.T) {
	for _, sql := range []string{
		`INSERT INTO t VALUES (1)`,
		`DROP TABLE t`,
		`SYSTEM RELOAD DICTIONARIES`,
	} {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, "expected grammar rejection: %s", sql)
	}
}

func TestClassifyQuerySecurityNilTree(t *testing.T) {
	class, witnesses, err := ClassifyQuerySecurity(nil)
	assert.Error(t, err)
	assert.Equal(t, QuerySecurityMutating, class, "nil input must fail closed")
	assert.Empty(t, witnesses)
}

// The witness says how far its construct reaches, for the reader of
// Diagnostics and of a refusal.
func TestSecurityWitnessReach(t *testing.T) {
	for _, tc := range []struct {
		sql   string
		kind  SecurityWitnessKindE
		reach SecurityReachE
	}{
		{`SELECT * FROM url('http://h/x', 'CSV')`, SecurityWitnessEgressTableFunction, SecurityReachExternal},
		{`SELECT * FROM remote('h', 'db', 't')`, SecurityWitnessEgressTableFunction, SecurityReachOtherServers},
		{`SELECT * FROM filesystem('/')`, SecurityWitnessEgressTableFunction, SecurityReachServerFiles},
		{`SELECT * FROM eval('SELECT 1')`, SecurityWitnessEgressTableFunction, SecurityReachRunTime},
		{`SELECT * FROM shinynewtf(1)`, SecurityWitnessEgressTableFunction, SecurityReachUnknown},
		{`SELECT aiEmbed('x')`, SecurityWitnessEgressFunction, SecurityReachExternal},
		{`SELECT catboostEvaluate('m.bin', 1)`, SecurityWitnessEgressFunction, SecurityReachServerFiles},
		{`SELECT generateSerialID('s')`, SecurityWitnessStateChangingFunction, SecurityReachNone},
	} {
		_, witnesses := mustSecurity(t, tc.sql)
		require.Len(t, witnesses, 1, tc.sql)
		assert.Equal(t, tc.kind, witnesses[0].Kind, tc.sql)
		assert.Equal(t, tc.reach, witnesses[0].Reach, tc.sql)
		assert.NotEmpty(t, witnesses[0].Describe(), tc.sql)
	}
	assert.Equal(t, "egress table function (external storage or service)",
		SecurityWitness{Kind: SecurityWitnessEgressTableFunction, Reach: SecurityReachExternal}.Describe())
}

// A lookup folds names to lower case, so no two spellings in a list may fold
// onto one name; and a name that is both a table function and a scalar
// reaches as far either way.
func TestSecurityVocabularyFolds(t *testing.T) {
	assert.Len(t, tableFunctions, len(tableFunctionSpellings))
	assert.Len(t, egressScalarFunctions, len(egressScalarSpellings))
	assert.Len(t, stateChangingScalarFunctions, len(stateChangingScalarSpellings))
	for name, reach := range egressScalarFunctions {
		if info, isTable := tableFunctions[name]; isTable {
			assert.Equal(t, info.reach, reach, name)
		}
	}
}
