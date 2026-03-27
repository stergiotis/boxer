//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"strings"
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

func TestDebugFormat(t *testing.T) {
	t.Skip("diagnostic only")
	sqls := []string{
		"SELECT 1 FORMAT JSON",
		"SELECT 1 FORMAT TabSeparated",
		"SELECT 1",
		"SELECT 1 FORMAT JSONEachRow",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		if err != nil {
			t.Logf("  PARSE ERROR: %v", err)
			continue
		}
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			typeName := fmt.Sprintf("%T", ctx)
			if strings.Contains(typeName, "ormat") || strings.Contains(typeName, "Query") || strings.Contains(typeName, "query") {
				t.Logf("  %T text=%q", ctx, ctx.GetText())
				for i := 0; i < ctx.GetChildCount(); i++ {
					t.Logf("    child[%d]: %T text=%q", i, ctx.GetChild(i), ctx.GetChild(i))
				}
			}
			return true
		})
	}
}
func TestDebugTupleArray(t *testing.T) {
	t.Skip("diagnostic only")
	sqls := []string{
		// Tuple construction
		"SELECT (1, 2, 3)",
		"SELECT tuple(1, 2, 3)",
		// Tuple access
		"SELECT t.1 FROM (SELECT (1, 2) AS t)",
		"SELECT tupleElement(t, 1) FROM (SELECT (1, 2) AS t)",
		// Array construction
		"SELECT [1, 2, 3]",
		"SELECT array(1, 2, 3)",
		// Array access
		"SELECT arr[1] FROM t",
		"SELECT arrayElement(arr, 1) FROM t",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		if err != nil {
			t.Logf("  PARSE ERROR: %v", err)
			continue
		}
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			typeName := fmt.Sprintf("%T", ctx)
			if strings.Contains(typeName, "Tuple") || strings.Contains(typeName, "Array") ||
				strings.Contains(typeName, "Function") || strings.Contains(typeName, "ColumnExprList") {
				t.Logf("  %T text=%q", ctx, ctx.GetText())
				for i := 0; i < ctx.GetChildCount(); i++ {
					t.Logf("    child[%d]: %T text=%q", i, ctx.GetChild(i), ctx.GetChild(i))
				}
			}
			return true
		})
	}
}
func TestDebugSettingValue(t *testing.T) {
	sqls := []string{
		"SELECT 1 SETTINGS a = 1",
		"SELECT 1 SETTINGS a = [1, 2, 3]",
		"SELECT 1 SETTINGS a = (1, 2)",
		"SELECT 1 SETTINGS a = array(1, 2, 3)",
		"SELECT 1 SETTINGS a = tuple(1, 2)",
		"SELECT 1 SETTINGS a = []",
		"SELECT 1 SETTINGS a = array()",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		if err != nil {
			t.Logf("  PARSE ERROR: %v", err)
			continue
		}
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			typeName := fmt.Sprintf("%T", ctx)
			if strings.Contains(typeName, "etting") {
				t.Logf("  %T text=%q", ctx, ctx.GetText())
				for i := 0; i < ctx.GetChildCount(); i++ {
					t.Logf("    child[%d]: %T text=%q", i, ctx.GetChild(i), ctx.GetChild(i))
				}
			}
			return true
		})
	}
}
func TestDebugMultipleSettings(t *testing.T) {
	t.Skip("diagnostic only")
	sql := "SELECT 1 SETTINGS a = 1, b = 'hello', c = [1, 2]"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		typeName := fmt.Sprintf("%T", ctx)
		if strings.Contains(typeName, "etting") {
			t.Logf("  %T children=%d", ctx, ctx.GetChildCount())
			for i := 0; i < ctx.GetChildCount(); i++ {
				t.Logf("    child[%d]: %T", i, ctx.GetChild(i))
			}
		}
		return true
	})
}
func TestDebugAliases(t *testing.T) {
	t.Skip("diagnostic only")
	sqls := []string{
		"SELECT a AS alias1 FROM t",
		"SELECT sum(a) AS total FROM t",
		"SELECT a alias1 FROM t", // implicit alias (no AS)
		"SELECT a AS \"quoted alias\" FROM t",
		"SELECT a AS `backtick` FROM t",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		if err != nil {
			t.Logf("  PARSE ERROR: %v", err)
			continue
		}
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			typeName := fmt.Sprintf("%T", ctx)
			if strings.Contains(typeName, "Alias") {
				t.Logf("  %T text=%q", ctx, ctx.GetText())
				for i := 0; i < ctx.GetChildCount(); i++ {
					t.Logf("    child[%d]: %T text=%q", i, ctx.GetChild(i), ctx.GetChild(i))
				}
			}
			return true
		})
	}
}
func TestDebugTryEvalDirect(t *testing.T) {
	eval := newTestEvaluator()

	sql := "SELECT myAdd(myAdd(1, 2), 3)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	// Find outer myAdd
	var outerFunc *grammar.ColumnExprFunctionContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		funcExpr, ok := ctx.(*grammar.ColumnExprFunctionContext)
		if !ok {
			return true
		}
		if outerFunc == nil {
			outerFunc = funcExpr
		}
		return true
	})

	require.NotNil(t, outerFunc)
	t.Logf("outer func text: %q", outerFunc.GetText())

	val, evaluated, evalErr := eval.TryEval(pr, outerFunc)
	t.Logf("val=%v (type %T) evaluated=%v err=%v", val, val, evaluated, evalErr)
}
