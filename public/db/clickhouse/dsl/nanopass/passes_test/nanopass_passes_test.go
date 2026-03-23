//go:build llm_generated_opus46

package passes_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/require"
)

func TestRewriterDiagnostic(t *testing.T) {
	t.Skip("diagnostic only")
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	t.Logf("token stream size: %d", pr.TokenStream.Size())
	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		t.Logf("token[%d]: type=%d channel=%d text=%q", i, tok.GetTokenType(), tok.GetChannel(), tok.GetText())
	}

	rw := nanopass.NewRewriter(pr)
	t.Logf("rewriter output: %q", rw.GetTextDefault())
}
func TestRewriterAllChannels(t *testing.T) {
	t.Skip("diagnostic only")
	sql := "SELECT a FROM t"
	input := antlr.NewInputStream(sql)
	lexer := grammar.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, -1)
	stream.Fill()

	t.Logf("token stream size: %d", stream.Size())
	for i := 0; i < stream.Size(); i++ {
		tok := stream.Get(i)
		t.Logf("token[%d]: type=%d channel=%d text=%q", i, tok.GetTokenType(), tok.GetChannel(), tok.GetText())
	}
}
func TestDebugNotParens(t *testing.T) {
	t.Skip("diagnostic only")
	sql := "SELECT NOT (a) FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar.ColumnExprParensContext:
			t.Logf("Found ColumnExprParens: %s", c.GetText())
		case *grammar.ColumnExprNotContext:
			t.Logf("Found ColumnExprNot: %s", c.GetText())
		case *grammar.ColumnExprFunctionContext:
			t.Logf("Found ColumnExprFunction: %s", c.GetText())
			t.Logf("  Identifier: %s", c.Identifier().GetText())
		case *grammar.ColumnExprTupleContext:
			t.Logf("Found ColumnExprTuple: %s", c.GetText())
		}
		return true
	})

	t.Logf("Tree: %s", pr.Tree.ToStringTree(pr.Parser.GetRuleNames(), pr.Parser))
}
func TestDebugNotExpr(t *testing.T) {
	t.Skip("diagnostic only")
	sqls := []string{
		"SELECT NOT a FROM t",
		"SELECT NOT(a) FROM t",
		"SELECT NOT (a > 1) FROM t",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err)
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			switch c := ctx.(type) {
			case *grammar.ColumnExprNotContext:
				t.Logf("  Found ColumnExprNot: %s", c.GetText())
			case *grammar.ColumnExprFunctionContext:
				t.Logf("  Found ColumnExprFunction: %s (ident=%s)", c.GetText(), c.Identifier().GetText())
			case *grammar.ColumnExprParensContext:
				t.Logf("  Found ColumnExprParens: %s", c.GetText())
			}
			return true
		})
	}
}
func TestDebugNegateParens(t *testing.T) {
	t.Skip("diagnostic only")
	sqls := []string{
		"SELECT -a FROM t",
		"SELECT -(a) FROM t",
		"SELECT -(a + b) FROM t",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err)
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			switch c := ctx.(type) {
			case *grammar.ColumnExprNegateContext:
				t.Logf("  Found ColumnExprNegate: %s", c.GetText())
			case *grammar.ColumnExprParensContext:
				t.Logf("  Found ColumnExprParens: %s", c.GetText())
			case *grammar.ColumnExprFunctionContext:
				t.Logf("  Found ColumnExprFunction: %s (ident=%s)", c.GetText(), c.Identifier().GetText())
			}
			return true
		})
	}
}
