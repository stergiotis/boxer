package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A parameter slot is admitted in table position, as ClickHouse admits it.
// Before this, `FROM {db:Identifier}.facts` failed grammar1 with "no viable
// alternative at input '{'" — the paramSlot rule existed but was reachable only
// from columnExpr, so `{tier:String}` parsed and the identifier form did not,
// and an applet page parameterised on its database never mounted. Filed by the
// jsonbench-on-facts trial (README §7b row 12).
func TestParse_ParamSlotInTablePosition(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1 FROM {db:Identifier}.facts",
		"SELECT 1 FROM {tbl:Identifier}",
		"SELECT 1 FROM {db:Identifier}.{tbl:Identifier}",
		"SELECT 1 FROM boxer.{tbl:Identifier}",
		"SELECT 1 FROM {db:Identifier}.facts WHERE x = {tier:String}",
	} {
		t.Run(sql, func(t *testing.T) {
			pr, err := nanopass.Parse(sql)
			require.NoError(t, err, "grammar1 must accept a table-position param slot")
			require.NotNil(t, pr.Tree)
		})
	}
}

// Grammar2 is the canonical surface ValidateGrammar2 checks a normalised query
// against; a slot has no canonical form to be rewritten into, so grammar2 must
// accept it wherever grammar1 does.
func TestParseCanonical_ParamSlotInTablePosition(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1 FROM {db:Identifier}.facts",
		"SELECT 1 FROM {tbl:Identifier}",
		"SELECT 1 FROM {db:Identifier}.{tbl:Identifier}",
	} {
		t.Run(sql, func(t *testing.T) {
			_, err := nanopass.ParseCanonical(sql)
			require.NoError(t, err, "grammar2 must accept a table-position param slot")
		})
	}
}

// The scope builder names a parameterised table by its slot text rather than
// panicking on a nil Identifier child — the failure mode the change would
// otherwise have introduced at three call sites.
func TestScopes_ParameterisedTableNamedBySlot(t *testing.T) {
	pr, err := nanopass.Parse("SELECT 1 FROM {db:Identifier}.{tbl:Identifier}")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].Tables, 1)
	assert.Equal(t, "{tbl:Identifier}", scopes[0].Tables[0].Table)
	assert.Equal(t, "{db:Identifier}", scopes[0].Tables[0].Database)
}

// An ordinary identifier is still decoded, quoting removed — the slot arm must
// not change what a normal table reference reports.
func TestScopes_OrdinaryTableStillDecoded(t *testing.T) {
	pr, err := nanopass.Parse("SELECT 1 FROM `boxer`.`facts`")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes[0].Tables, 1)
	assert.Equal(t, "facts", scopes[0].Tables[0].Table)
	assert.Equal(t, "boxer", scopes[0].Tables[0].Database)
}

// QualifyTables prepends the default database to an unqualified parameterised
// table: `db.{t:Identifier}` is what ClickHouse substitutes into.
func TestQualifyTables_ParameterisedTable(t *testing.T) {
	out, err := passes.QualifyTables("boxer").Run("SELECT 1 FROM {tbl:Identifier}")
	require.NoError(t, err)
	assert.Contains(t, out, "boxer.{tbl:Identifier}")
}

// The table inventory reports a parameterised table by its slot text instead of
// dereferencing a nil identifier.
func TestAnalytics_TableRefsForParameterisedTable(t *testing.T) {
	pr, err := nanopass.Parse("SELECT 1 FROM {db:Identifier}.{tbl:Identifier}")
	require.NoError(t, err)
	refs := analysis.ExtractTables(pr)
	require.Len(t, refs, 1)
	assert.Equal(t, "{tbl:Identifier}", refs[0].Table)
	assert.Equal(t, "{db:Identifier}", refs[0].Database)
}
