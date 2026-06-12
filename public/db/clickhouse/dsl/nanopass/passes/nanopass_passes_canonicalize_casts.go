//go:build llm_generated_opus47

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeCasts rewrites all cast syntaxes to the canonical function form
// CAST(expr, 'Type'). Nested casts converge under fixpoint iteration —
// innermost casts canonicalised first, then the next layer.
//
//	expr::Type            → CAST(expr, 'Type')
//	CAST(expr AS Type)    → CAST(expr, 'Type')
//	CAST(expr, 'Type')    → CAST(expr, 'Type') (no change)
var CanonicalizeCasts = nanopass.LiftBodyPass(
	"CanonicalizeCasts",
	canonicalizeCastsOnce,
	nanopass.PassProperties{
		NeedsFixedPoint: true,
		Reads:           nanopass.RegionBody,
		Writes:          nanopass.RegionBody,
	},
)

// canonicalizeCastsOnce performs a single canonicalization pass. It skips cast nodes
// whose expression child is itself a non-canonical cast (to avoid overlapping rewrites).
// Repeated application handles nested casts layer by layer.
func canonicalizeCastsOnce(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeCasts: %w", err)
		return
	}

	rw := nanopass.NewRewriter(pr)
	changed := false

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if castCtx, ok := ctx.(*grammar1.ColumnExprCastContext); ok {
			if containsNonCanonicalCastChild(castCtx) {
				return true
			}
			if canonicalizeCastExpr(pr, rw, castCtx) {
				changed = true
				// The whole node was replaced — casts nested deeper inside
				// (e.g. CAST(x::Int64 + 1 AS String)) must not be rewritten
				// in the same walk or the edits would overlap; the fixpoint
				// re-parse picks them up.
				return false
			}
		}
		return true
	})

	if !changed {
		result = sql
		return
	}

	result = nanopass.GetText(rw)
	return
}

// containsNonCanonicalCastChild checks whether any direct child of a ColumnExprCastContext
// is itself a non-canonical cast node (expr::Type or CAST(expr AS Type)) that would also be
// rewritten, causing overlapping writes.
// The CAST(expr, 'Type') function form is already canonical and won't be rewritten by this pass,
// so it's safe to process the outer cast even if the child is a CAST function call.
func containsNonCanonicalCastChild(castCtx *grammar1.ColumnExprCastContext) bool {
	for i := 0; i < castCtx.GetChildCount(); i++ {
		child := castCtx.GetChild(i)
		// Any ColumnExprCastContext child is non-canonical (either :: or CAST AS form)
		// — both would be rewritten by this pass
		if _, isCast := child.(*grammar1.ColumnExprCastContext); isCast {
			return true
		}
	}
	return false
}

// isColumnTypeExprNode reports whether the node is any alternative of the
// columnTypeExpr rule — simple (UInt64), complex (Nullable(String)),
// parametric (FixedString(16), DateTime64(3)), nested, or enum.
func isColumnTypeExprNode(ctx antlr.ParserRuleContext) bool {
	switch ctx.(type) {
	case *grammar1.ColumnTypeExprSimpleContext,
		*grammar1.ColumnTypeExprComplexContext,
		*grammar1.ColumnTypeExprParamContext,
		*grammar1.ColumnTypeExprNestedContext,
		*grammar1.ColumnTypeExprEnumContext:
		return true
	}
	return false
}

// canonicalizeCastExpr handles ColumnExprCastContext which covers:
//   - expr::Type           (children: expr, ::, ColumnTypeExpr)
//   - CAST(expr AS Type)   (children: CAST, (, expr, AS, ColumnTypeExpr, ))
//
// Both are rewritten to CAST(expr, 'Type').
// Returns true if a rewrite was performed.
func canonicalizeCastExpr(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, castCtx *grammar1.ColumnExprCastContext) bool {
	if castCtx.GetChildCount() == 0 {
		return false
	}

	var exprNode antlr.ParserRuleContext
	var typeText string

	firstChild := castCtx.GetChild(0)
	_, firstIsTerm := firstChild.(*antlr.TerminalNodeImpl)

	if firstIsTerm {
		// Could be CAST(expr AS Type) or :: form where first token is something else.
		// Check if it's CAST
		term := firstChild.(*antlr.TerminalNodeImpl)
		if strings.ToUpper(term.GetText()) != "CAST" {
			// Not a CAST terminal — not a form we handle
			return false
		}
	}
	// Both forms carry exactly one columnTypeExpr child and one expression
	// child; the remaining children are terminals (CAST, parens, AS, ::).
	for i := 0; i < castCtx.GetChildCount(); i++ {
		child := castCtx.GetChild(i)
		c, isCtx := child.(antlr.ParserRuleContext)
		if !isCtx {
			continue
		}
		if isColumnTypeExprNode(c) {
			if typeText == "" {
				typeText = c.GetText()
			}
			continue
		}
		if exprNode == nil {
			exprNode = c
		}
	}

	if exprNode == nil || typeText == "" {
		return false
	}

	exprText := nanopass.NodeText(pr, exprNode)
	// Enum types carry single-quoted member names (Enum8('a' = 1)) — escape
	// them for splicing into the single-quoted type string.
	escapedType := strings.ReplaceAll(typeText, `'`, `\'`)
	replacement := "CAST(" + exprText + ", '" + escapedType + "')"
	nanopass.ReplaceNode(rw, castCtx, replacement)
	return true
}
