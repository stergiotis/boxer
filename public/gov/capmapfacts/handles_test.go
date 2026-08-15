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
		out, err := prepare("SELECT "+h+" FROM "+QualifiedTable, QualifiedTable)
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
	out, err := prepare("SELECT "+hId+", "+hTs+" FROM "+QualifiedTable, QualifiedTable)
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
		out, err := prepare(sql, QualifiedTable)
		require.NoErrorf(t, err, "%s", name)
		for _, h := range allHandles {
			assert.NotContainsf(t, out, h, "%s: %s survived resolution", name, h)
		}
		assert.NotContainsf(t, out, "`", "%s: a backtick-quoted name survived: %s", name, out)
		assert.Truef(t, strings.Contains(out, "tv:symbol:"), "%s: the symbol lanes did not resolve", name)
	}
}

// The scalar reads are the read surface's, not this package's arithmetic.
//
// `LW_GET` expands client-side into the read-back family, so what is sent
// carries none of the calls this package authored — and if an expansion ever
// silently declined, the call would travel to the server as an unknown
// function. Asserting both directions is what makes that impossible to miss.
func TestQueriesExpandIntoTheReadBackFamily(t *testing.T) {
	for name, sql := range map[string]string{
		"competences": competenceSQL(QualifiedTable),
		"relations":   relationSQL(QualifiedTable),
	} {
		require.Containsf(t, sql, "LW_GET", "%s: the authored query should read through the surface", name)

		out, err := prepare(sql, QualifiedTable)
		require.NoErrorf(t, err, "%s", name)
		assert.NotContainsf(t, out, "LW_GET", "%s: an LW_GET call survived expansion", name)
		assert.Containsf(t, out, "LW_VALUE_BY_TAG_EQUAL", "%s: the scalar reads should expand to the read-back family", name)
		// The lane map the mixed and repeated reads use is the one the
		// expansion itself emits, which is the point of using it rather than a
		// hand-rolled prefix sum.
		assert.Containsf(t, out, "LW_RAGGED_PARENT_IDS", "%s", name)
	}
	assert.Contains(t, prepared(t, competenceSQL(QualifiedTable)), "LW_LIST_BY_TAG_EQUAL",
		"the array-valued sections should expand to the list form")
	assert.Contains(t, prepared(t, competenceSQL(QualifiedTable)), "LW_RAGGED_ELEM",
		"a repeated attribute's value is read by position, not by a running total")
}

func prepared(t *testing.T, sql string) (out string) {
	t.Helper()
	out, err := prepare(sql, QualifiedTable)
	require.NoError(t, err)
	return out
}

// A dump names its table on the command line, so an unqualified one is an
// operator's typo rather than a programming error — it has to say so.
func TestResolveHandlesNeedsAQualifiedTable(t *testing.T) {
	_, err := prepare("SELECT 1", "facts")
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
