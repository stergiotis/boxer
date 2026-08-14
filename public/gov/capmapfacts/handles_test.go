package capmapfacts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allHandles is every handle the read-back queries are written in. A handle
// this list forgets is still checked by TestQueriesResolveEveryHandle through
// the query text; the list is what makes a failure say which one.
var allHandles = []string{
	hId, hTs,
	hSymValue, hSymLr, hSymLrCard, hSymLmr, hSymLmrCard, hSymMrhp,
	hStrValue, hStrLr, hStrLrCard, hStrLen,
	hTxtValue, hTxtLmr, hTxtLmrCard, hTxtMrhp, hTxtLen,
	hU8Value, hU8Lr, hU8LrCard, hU8Len,
	hTimeValue, hTimeLmr, hTimeLmrCard, hTimeMrhp, hTimeLen,
	hF64Value, hF64Lr, hF64LrCard, hF64Len,
	hFkValue, hFkLr, hFkLrCard,
}

// Every handle names a column the generated schema actually has.
//
// This is the check that replaces reading the physical names off the DDL by
// hand: a regeneration that renames a section or drops a support lane fails
// here, at `go test`, instead of at the first dump against a real server.
func TestEveryHandleResolves(t *testing.T) {
	for _, h := range allHandles {
		out, err := resolveHandles("SELECT "+h+" FROM "+QualifiedTable, QualifiedTable)
		require.NoErrorf(t, err, "%s", h)
		// The pass passes an unresolvable handle through untouched, so "the
		// handle is gone" is exactly the property worth asserting.
		assert.NotContainsf(t, out, h, "%s did not resolve — the schema has no such section or column", h)
		assert.Containsf(t, out, `"`, "%s resolved to an unquoted name: %s", h, out)
	}
}

// The two plain columns are not `tv:` columns and resolve through their own
// sections, which is easy to get wrong: the timestamp's section is `timestamp`
// while its physical name begins `ts:`.
func TestPlainColumnHandles(t *testing.T) {
	out, err := resolveHandles("SELECT "+hId+", "+hTs+" FROM "+QualifiedTable, QualifiedTable)
	require.NoError(t, err)
	assert.Contains(t, out, `"id:id:`)
	assert.Contains(t, out, `"ts:ts:`)
}

// The whole query surface, resolved: no handle may survive into what is sent.
// An unresolved handle passes through the pass untouched, so ClickHouse would
// answer with "no such column" — true but unhelpful, and only at run time.
func TestQueriesResolveEveryHandle(t *testing.T) {
	for name, sql := range map[string]string{
		"competences": competenceSQL(QualifiedTable),
		"relations":   relationSQL(QualifiedTable),
	} {
		out, err := resolveHandles(sql, QualifiedTable)
		require.NoErrorf(t, err, "%s", name)
		for _, h := range allHandles {
			assert.NotContainsf(t, out, h, "%s: %s survived resolution", name, h)
		}
		assert.NotContainsf(t, out, "`", "%s: a backtick-quoted name survived: %s", name, out)
		assert.Truef(t, strings.Contains(out, "tv:symbol:"), "%s: the symbol lanes did not resolve", name)
	}
}

// A dump names its table on the command line, so an unqualified one is an
// operator's typo rather than a programming error — it has to say so.
func TestResolveHandlesNeedsAQualifiedTable(t *testing.T) {
	_, err := resolveHandles("SELECT 1", "facts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "qualified")
}

// The names come from the DML artifact, which is what the writer writes
// through: if this ever returned nothing, every handle would silently fail to
// resolve and every query would be sent full of handles.
func TestFactsColumnNamesComeFromTheGeneratedSchema(t *testing.T) {
	names := factsColumnNames()
	require.NotEmpty(t, names)
	assert.Contains(t, names, "id:id:u64:47::0:")
	var tagged int
	for _, n := range names {
		if strings.HasPrefix(n, "tv:") {
			tagged++
		}
	}
	assert.Greaterf(t, tagged, 100, "the facts schema should carry a tagged-value column block, got %d", tagged)
}
