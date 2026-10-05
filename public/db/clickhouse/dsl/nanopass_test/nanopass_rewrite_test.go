package nanopass_test

import (
	"fmt"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
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
		_, ok := ctx.(*grammar1.OrderByClauseContext)
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
		if tok.GetTokenType() == grammar1.ClickHouseParserGrammar1DISTINCT {
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
		_, ok := ctx.(*grammar1.FromClauseContext)
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
		_, ok := ctx.(*grammar1.FromClauseContext)
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
		_, ok := ctx.(*grammar1.WhereClauseContext)
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

	assert.False(t, rw.HasConflicts())
	assert.Equal(t, 0, rw.ConflictCount())
}

func TestTrackedRewriterDetectsOverlap(t *testing.T) {
	sql := "SELECT a + b FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	logger := zerolog.New(zerolog.NewTestWriter(t))
	rw := nanopass.NewTrackedRewriter(pr, logger)

	// Replace a range, then a narrower range inside it. The ANTLR rewriter
	// panics at GetText for this combination (the earlier op is not
	// subsumed by the later one) — a fatal conflict.
	rw.ReplaceDefault(2, 6, "x") // replace "a + b" range
	rw.ReplaceDefault(4, 4, "y") // replace "b" — inside the previous range

	assert.True(t, rw.HasConflicts())
	assert.Equal(t, 1, rw.ConflictCount())
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

	// Lossy (texts concatenate) but not fatal — logged as warning only
	assert.False(t, rw.HasConflicts())
}

func TestTrackedRewriterInPass(t *testing.T) {
	// Demonstrate usage pattern in a pass
	sql := "SELECT a, b FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	logger := zerolog.New(zerolog.NewTestWriter(t))
	rw := nanopass.NewTrackedRewriter(pr, logger)

	// Simulate a pass replacing two column identifiers — the generic node
	// helpers accept TrackedRewriter via the RewriterI interface.
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if _, ok := ctx.(*grammar1.ColumnIdentifierContext); ok {
			nanopass.ReplaceNode(rw, ctx, "replaced")
		}
		return true
	})

	assert.False(t, rw.HasConflicts())
	result := nanopass.GetText(rw)
	assert.Contains(t, result, "replaced")
}

// TestTrackedRewriterInsertAfterMatchesAntlr checks HasConflicts against
// what GetTextDefault actually does for every op pair involving an
// insert-after. ANTLR stores InsertAfter(i) as an insert at token i+1, so
// the tracked region has to sit there too.
func TestTrackedRewriterInsertAfterMatchesAntlr(t *testing.T) {
	sql := "SELECT a + b FROM t"
	type op struct {
		name  string
		apply func(rw *nanopass.TrackedRewriter)
	}
	var others []op
	for s := 1; s <= 8; s++ {
		for e := s; e <= s+2; e++ {
			s, e := s, e
			others = append(others,
				op{fmt.Sprintf("replace(%d,%d)", s, e), func(rw *nanopass.TrackedRewriter) { rw.ReplaceDefault(s, e, "r") }},
				op{fmt.Sprintf("delete(%d,%d)", s, e), func(rw *nanopass.TrackedRewriter) { rw.DeleteDefault(s, e) }})
		}
		s := s
		others = append(others,
			op{fmt.Sprintf("insertBefore(%d)", s), func(rw *nanopass.TrackedRewriter) { rw.InsertBeforeDefault(s, "b") }},
			op{fmt.Sprintf("insertAfter(%d)", s), func(rw *nanopass.TrackedRewriter) { rw.InsertAfterDefault(s, "a") }})
	}
	for ia := 1; ia <= 8; ia++ {
		ia := ia
		insertAfter := op{fmt.Sprintf("insertAfter(%d)", ia), func(rw *nanopass.TrackedRewriter) { rw.InsertAfterDefault(ia, "a") }}
		for _, other := range others {
			for _, pair := range [][2]op{{insertAfter, other}, {other, insertAfter}} {
				pr, err := nanopass.Parse(sql)
				require.NoError(t, err)
				rw := nanopass.NewTrackedRewriter(pr, zerolog.Nop())
				pair[0].apply(rw)
				pair[1].apply(rw)
				panicked := func() (p bool) {
					defer func() { p = recover() != nil }()
					_ = rw.GetTextDefault()
					return
				}()
				assert.Equal(t, panicked, rw.HasConflicts(), "%s then %s", pair[0].name, pair[1].name)
			}
		}
	}
}
