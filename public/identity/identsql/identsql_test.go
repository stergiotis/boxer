package identsql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// TestExpandPass_Basics: every macro expands, the LW_ID_ name disappears, and
// the produced SQL parses.
func TestExpandPass_Basics(t *testing.T) {
	for _, src := range []string{
		"SELECT LW_ID_IS_VALID(id) FROM t",
		"SELECT LW_ID_TAG_WIDTH(id) FROM t",
		"SELECT LW_ID_TAG_BITS(id) FROM t",
		"SELECT LW_ID_BODY(id) FROM t",
		"SELECT LW_ID_TAG_VALUE(id) FROM t",
		"SELECT LW_ID_HAS_TAG(id, 42) FROM t",
		"SELECT LW_ID_HAS_TAG(id, other_col) FROM t",
		"SELECT lw_id_body(id) FROM t",   // case-insensitive
		"SELECT \"LW_ID_BODY\"(id) FROM t", // quoting-insensitive
		"SELECT LW_ID_BODY(bitOr(a, b)) + LW_ID_TAG_WIDTH(c) FROM t WHERE LW_ID_IS_VALID(a)",
		"SELECT LW_ID_BODY(LW_ID_TAG_BITS(id)) FROM t", // nested: converges via fixpoint
	} {
		t.Run(src, func(t *testing.T) {
			got, err := ExpandPass.Run(src)
			require.NoError(t, err)
			require.NotContains(t, strings.ToUpper(got), "LW_ID_", "macro must be fully expanded: %s", got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "expansion must parse: %s", got)
		})
	}
}

// TestExpandPass_HasTagConstantFold pins the sargable fold: the BETWEEN
// bounds must be exactly the tag's composed id range per the identifier
// package (body 0 through max body).
func TestExpandPass_HasTagConstantFold(t *testing.T) {
	for _, tv := range []identifier.TagValue{1, 2, 7, 12, 4096, 4294967295} {
		tag := tv.GetTag()
		lo := uint64(tag)
		hi := uint64(tag) | uint64(tag.GetMaxPossibleIdIncl())
		src := fmt.Sprintf("SELECT LW_ID_HAS_TAG(id, %d) FROM t", uint64(tv))
		got, err := ExpandPass.Run(src)
		require.NoError(t, err)
		require.Contains(t, got, fmt.Sprintf("BETWEEN %d AND %d", lo, hi), "tag value %d", tv)
		require.NotContains(t, got, "bitAnd", "constant fold must leave no bit arithmetic: %s", got)
	}
}

func TestExpandPass_Errors(t *testing.T) {
	for _, src := range []string{
		"SELECT LW_ID_BODY(a, b) FROM t",     // arity
		"SELECT LW_ID_HAS_TAG(id) FROM t",    // arity
		"SELECT LW_ID_HAS_TAG(id, 0) FROM t", // constant tag value out of domain
	} {
		_, err := ExpandPass.Run(src)
		require.Error(t, err, src)
	}
}

// TestUdfDdlStatements: every statement parses server-side syntax-wise is
// covered by the server-truth test; here pin count, naming and shape.
func TestUdfDdlStatements(t *testing.T) {
	stmts := UdfDdlStatements()
	require.Len(t, stmts, 6)
	for _, name := range []string{NameIsValid, NameTagWidth, NameTagBits, NameBody, NameTagValue, NameHasTag} {
		found := false
		for _, s := range stmts {
			if strings.HasPrefix(s, "CREATE OR REPLACE FUNCTION "+name+" AS ") {
				found = true
			}
		}
		require.True(t, found, "missing UDF for %s", name)
	}
}

// TestExpandPass_Properties runs the mechanised property check over the
// shared corpus plus entries that exercise the expansion, including a nested
// case that does not converge in a single Apply (justifying NeedsFixedPoint).
func TestExpandPass_Properties(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	corpus := make([]string, 0, len(entries)+3)
	for _, e := range entries {
		corpus = append(corpus, e.SQL)
	}
	corpus = append(corpus,
		"SELECT LW_ID_TAG_VALUE(id) FROM t",
		"SELECT LW_ID_HAS_TAG(id, 42) FROM t WHERE LW_ID_IS_VALID(id)",
		"SELECT LW_ID_BODY(LW_ID_TAG_BITS(id)) FROM t",
	)
	nanopass.AssertProperties(t, ExpandPass, corpus)
}
