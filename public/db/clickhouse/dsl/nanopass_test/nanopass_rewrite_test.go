//go:build llm_generated_opus46

package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeText(t *testing.T) {
	sql := "SELECT a + b, c FROM t WHERE x > 1"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	// NodeText on the whole tree should reproduce the full SQL
	wholeText := nanopass.NodeText(pr, pr.Tree)
	assert.Equal(t, sql, wholeText)
}

func TestDeleteNode(t *testing.T) {
	sql := "SELECT a FROM t WHERE x > 1 ORDER BY a"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	rw := nanopass.NewRewriter(pr)

	// Find and delete the ORDER BY clause
	orderBy := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar.OrderByClauseContext)
		return ok
	})
	require.NotNil(t, orderBy)
	nanopass.DeleteNode(rw, orderBy)

	result := nanopass.GetText(rw)
	// The ORDER BY clause tokens are removed; whitespace before it remains
	assert.NotContains(t, result, "ORDER BY")
	assert.Contains(t, result, "WHERE x > 1")

	// Verify the output is still parseable
	_, err = nanopass.Parse(result)
	require.NoError(t, err)
}

func TestDeleteToken(t *testing.T) {
	sql := "SELECT DISTINCT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	rw := nanopass.NewRewriter(pr)

	// Find the DISTINCT token and delete it
	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		if tok.GetTokenType() == grammar.ClickHouseParserDISTINCT {
			nanopass.DeleteToken(rw, tok.GetTokenIndex())
			break
		}
	}

	result := nanopass.GetText(rw)
	assert.NotContains(t, result, "DISTINCT")
	assert.Contains(t, result, "SELECT")
	assert.Contains(t, result, "a")

	// Verify parseable
	_, err = nanopass.Parse(result)
	require.NoError(t, err)
}

func TestInsertBefore(t *testing.T) {
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	rw := nanopass.NewRewriter(pr)

	// Find the FROM clause and insert a comment-like marker before it
	fromClause := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar.FromClauseContext)
		return ok
	})
	require.NotNil(t, fromClause)
	nanopass.InsertBefore(rw, fromClause, "/* injected */ ")

	result := nanopass.GetText(rw)
	assert.Contains(t, result, "/* injected */ FROM")
}

func TestInsertAfter(t *testing.T) {
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	rw := nanopass.NewRewriter(pr)

	// Find the FROM clause and insert FINAL after it
	fromClause := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar.FromClauseContext)
		return ok
	})
	require.NotNil(t, fromClause)
	nanopass.InsertAfter(rw, fromClause, " FINAL")

	result := nanopass.GetText(rw)
	assert.Contains(t, result, "FROM t FINAL")
}

func TestReplaceNode(t *testing.T) {
	sql := "SELECT a FROM t WHERE x > 1"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	rw := nanopass.NewRewriter(pr)

	// Find the WHERE clause and replace it entirely
	whereClause := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar.WhereClauseContext)
		return ok
	})
	require.NotNil(t, whereClause)
	nanopass.ReplaceNode(rw, whereClause, "WHERE y = 2")

	result := nanopass.GetText(rw)
	assert.Contains(t, result, "WHERE y = 2")
	assert.NotContains(t, result, "x > 1")

	// Verify parseable
	_, err = nanopass.Parse(result)
	require.NoError(t, err)
}
func TestTrackedRewriterNoOverlap(t *testing.T) {
	sql := "SELECT a, b FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	logger := zerolog.New(zerolog.NewTestWriter(t))
	rw := nanopass.NewTrackedRewriter(pr, logger)

	// Replace two non-overlapping tokens
	rw.ReplaceDefault(0, 0, "select") // SELECT token
	rw.ReplaceDefault(4, 4, "x")      // b token (index depends on whitespace)

	assert.False(t, rw.HasOverlaps())
	assert.Equal(t, 0, rw.OverlapCount())
}

func TestTrackedRewriterDetectsOverlap(t *testing.T) {
	sql := "SELECT a + b FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	logger := zerolog.New(zerolog.NewTestWriter(t))
	rw := nanopass.NewTrackedRewriter(pr, logger)

	// Replace a range
	rw.ReplaceDefault(2, 6, "x") // replace "a + b" range
	rw.ReplaceDefault(4, 4, "y") // replace "b" — overlaps with previous

	assert.True(t, rw.HasOverlaps())
	assert.Equal(t, 1, rw.OverlapCount())
}

func TestTrackedRewriterDetectsDoubleInsertBefore(t *testing.T) {
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	logger := zerolog.New(zerolog.NewTestWriter(t))
	rw := nanopass.NewTrackedRewriter(pr, logger)

	// Two inserts at the same position
	rw.InsertBeforeDefault(2, "x")
	rw.InsertBeforeDefault(2, "y")

	// Not counted as range overlap but logged as warning
	assert.False(t, rw.HasOverlaps()) // range overlaps only
}

func TestTrackedRewriterInPass(t *testing.T) {
	// Demonstrate usage pattern in a pass
	sql := "SELECT a, b FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	logger := zerolog.New(zerolog.NewTestWriter(t))
	rw := nanopass.NewTrackedRewriter(pr, logger)

	// Simulate a pass replacing two column identifiers
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if _, ok := ctx.(*grammar.ColumnIdentifierContext); ok {
			nanopass.TrackedReplaceNode(rw, ctx, "replaced")
		}
		return true
	})

	assert.False(t, rw.HasOverlaps())
	result := nanopass.TrackedGetText(rw)
	assert.Contains(t, result, "replaced")
}
