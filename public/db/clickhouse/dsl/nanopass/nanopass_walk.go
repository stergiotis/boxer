//go:build llm_generated_opus46
package nanopass

import (
	"github.com/antlr4-go/antlr/v4"
)

// WalkCST performs a depth-first traversal of the CST.
// fn is called for every ParserRuleContext node.
// Return false from fn to skip the subtree rooted at that node.
func WalkCST(node antlr.Tree, fn func(antlr.ParserRuleContext) bool) {
	if ctx, ok := node.(antlr.ParserRuleContext); ok {
		if !fn(ctx) {
			return
		}
	}
	for i := 0; i < node.GetChildCount(); i++ {
		child := node.GetChild(i)
		if child == nil {
			continue
		}
		WalkCST(child.(antlr.Tree), fn)
	}
}

// FindAll returns all CST nodes matching the predicate.
func FindAll(node antlr.Tree, pred func(antlr.ParserRuleContext) bool) (results []antlr.ParserRuleContext) {
	WalkCST(node, func(ctx antlr.ParserRuleContext) bool {
		if pred(ctx) {
			results = append(results, ctx)
		}
		return true
	})
	return
}

// FindFirst returns the first CST node matching the predicate, or nil.
func FindFirst(node antlr.Tree, pred func(antlr.ParserRuleContext) bool) (result antlr.ParserRuleContext) {
	WalkCST(node, func(ctx antlr.ParserRuleContext) bool {
		if result != nil {
			return false
		}
		if pred(ctx) {
			result = ctx
		}
		return result == nil
	})
	return
}
