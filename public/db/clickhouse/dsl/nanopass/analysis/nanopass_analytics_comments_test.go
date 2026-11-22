package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// Texts the grammar reads as a read and a ClickHouse server, which nests
// block comments, reads otherwise. The first two parse; the third does not
// and leads with SHOW to the lexer and with DROP to the server.
var nestedCommentEscapes = map[string]string{
	"an expression hidden in a quoted alias": "SELECT 1 /* /* */ AS \"x */ , throwIf(1, 'escaped') AS z --\"\nFROM numbers(1)",
	"a create hidden in a quoted alias":      "/* /* */ SELECT 1 AS \"x */ CREATE TEMPORARY TABLE t (a UInt8) ENGINE = Memory --\"",
	"a drop behind a leading show":           "/* /* */ SHOW */ DROP TABLE t",
}

// Texts with `/*` where both lexers agree: in a string, in quoted
// identifiers, in a line comment, and in comments that open nothing inside.
var nestedCommentNearMisses = map[string]string{
	"in a string literal":        "SELECT '/* x */ /*' AS s",
	"in a double-quoted alias":   "SELECT 1 AS \"a /* b\"",
	"in a backquoted alias":      "SELECT 1 AS `/*`",
	"a plain comment":            "SELECT 1 /* why */ AS x",
	"two adjacent comments":      "SELECT 1 /* a *//* b */ AS x",
	"an empty comment":           "SELECT 1 /**/ AS x",
	"a slash after the opener":   "SELECT 1 /*/ x */ AS x",
	"in a line comment":          "SELECT 1 -- /* not a block\nAS x",
	"a comment ahead of a show":  "/* why */ SHOW TABLES",
	"a star-slash in a string":   "SELECT '*/' /* c */ AS x",
	"a comment holding a string": "SELECT 1 /* 'it''s' */ AS x",
}

func TestNestedBlockCommentIsFoundInTheLexersTokens(t *testing.T) {
	for name, sql := range nestedCommentEscapes {
		_, found := nestedBlockCommentIn(sql)
		assert.True(t, found, name)
		if pr, err := nanopass.Parse(sql); err == nil {
			src, found := NestedBlockComment(pr)
			require.True(t, found, name)
			assert.Equal(t, "/* /* */", sql[src.Start:src.End], name)
		}
	}
	for name, sql := range nestedCommentNearMisses {
		_, found := nestedBlockCommentIn(sql)
		assert.False(t, found, name)
	}
	_, found := nestedBlockCommentIn("SELECT 1 /* a /*/ */")
	assert.True(t, found, "an opener that shares its slash with the closer is one the server counts")
	_, found = NestedBlockComment(nil)
	assert.False(t, found)
}

func TestANestedBlockCommentIsNeverARead(t *testing.T) {
	for name, sql := range nestedCommentEscapes {
		assert.Equal(t, KindUnknown, ClassifyStatementKind(sql), name)
		pr, err := nanopass.Parse(sql)
		if err != nil {
			continue
		}
		class, witnesses, err := ClassifyQuerySecurity(pr)
		require.NoError(t, err, name)
		assert.Equal(t, QuerySecurityMutating, class, name)
		require.NotEmpty(t, witnesses, name)
		assert.Equal(t, SecurityWitnessNestedComment, witnesses[0].Kind, name)
		assert.Contains(t, witnesses[0].Describe(), "nested block comment", name)
		c := Classify(sql, "")
		assert.Equal(t, KindUnknown, c.Kind, name)
		assert.Equal(t, QuerySecurityMutating, c.Security, name)
	}
	for name, sql := range nestedCommentNearMisses {
		assert.Equal(t, KindReadOnly, ClassifyStatementKind(sql), name)
		pr, err := nanopass.Parse(sql)
		if err != nil {
			continue
		}
		class, witnesses, err := ClassifyQuerySecurity(pr)
		require.NoError(t, err, name)
		assert.Equal(t, QuerySecurityRead, class, name)
		assert.Empty(t, witnesses, name)
	}
}
