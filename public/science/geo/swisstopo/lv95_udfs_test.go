package swisstopo

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const udfCreateHead = "CREATE OR REPLACE FUNCTION "

// udfNameRe matches every mention of a family member, definition or call.
var udfNameRe = regexp.MustCompile(`SWISSTOPO_[A-Z0-9_]+`)

// The lexical split StatementsSQL documents holds: one statement per member,
// each a bare CREATE with nothing of the commentary left in it, and UDFNames
// reads the same list off the same statements.
func TestStatementsSQLMatchesNames(t *testing.T) {
	stmts := StatementsSQL()
	names := UDFNames()
	require.Equal(t, len(stmts), len(names), "every statement must yield a name")
	require.NotEmpty(t, stmts)

	seen := make(map[string]bool, len(names))
	for i, stmt := range stmts {
		require.True(t, strings.HasPrefix(stmt, udfCreateHead), "statement %d is not a CREATE: %.60s", i, stmt)
		assert.NotContains(t, stmt, ";", "a statement must not carry the separator it was cut on")
		assert.NotContains(t, stmt, "--", "comments belong to ScriptSQL, not to a statement")
		assert.True(t, strings.HasPrefix(names[i], "SWISSTOPO_"),
			"%q leaves the namespace the family owns", names[i])
		assert.False(t, seen[names[i]], "%q is created twice", names[i])
		seen[names[i]] = true
	}
}

// Installation order is load-bearing — ClickHouse resolves a called function
// at CREATE time — so every member a body calls must already be defined. This
// is the check that catches a reordering of the file, which the server would
// otherwise report one statement at a time.
func TestStatementsSQLAreInDependencyOrder(t *testing.T) {
	defined := make(map[string]bool, 32)
	for i, stmt := range StatementsSQL() {
		mentions := udfNameRe.FindAllString(stmt, -1)
		require.NotEmpty(t, mentions)
		for _, called := range mentions[1:] {
			assert.True(t, defined[called],
				"statement %d (%s) calls %s before it is created", i, mentions[0], called)
		}
		defined[mentions[0]] = true
	}
}

// The tier convention, after ClickHouse's own quantile / quantileExact: the
// plain name is the cheap transform and `_EXACT` is the closed form. Only this
// direction is checkable — an `_EXACT` whose default is missing is a name
// nobody will guess — because the converse is false on purpose: the LV03 frame
// offset is exact by definition and has no second tier to offer.
func TestExactTierHasADefaultCounterpart(t *testing.T) {
	names := make(map[string]bool, 32)
	for _, n := range UDFNames() {
		names[n] = true
	}
	exact := 0
	for n := range names {
		base, isExact := strings.CutSuffix(n, "_EXACT")
		if !isExact {
			continue
		}
		exact++
		assert.True(t, names[base], "%s has no default-tier counterpart %s", n, base)
	}
	assert.NotZero(t, exact, "the family is meant to carry an exact tier")
}

// ScriptSQL is the same family with its commentary — what a reader gets when
// handed the file, and what `clickhouse-client < lv95_udfs.sql` installs.
func TestScriptSQLCarriesEveryMember(t *testing.T) {
	script := ScriptSQL()
	for _, name := range UDFNames() {
		assert.Contains(t, script, udfCreateHead+name+" ")
	}
	assert.Contains(t, script, "-- ", "the script keeps the commentary the statements drop")
}
