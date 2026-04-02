//go:build llm_generated_opus46

package analysis

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// FunctionRef represents a function call found in the query.
type FunctionRef struct {
	Name         string
	IsParametric bool // true if the function has parametric syntax: func(params)(args)
	IsWindow     bool // true if this is a window function call (has OVER clause)
}

// ExtractFunctions walks the CST and returns all function calls.
func ExtractFunctions(pr *nanopass.ParseResult) (refs []FunctionRef) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch ctx.(type) {
		case *grammar1.ColumnExprFunctionContext:
			return true
		case *grammar1.ColumnExprWinFunctionContext:
			return true
		case *grammar1.ColumnExprWinFunctionTargetContext:
			return true
		}
		return false
	})
	refs = make([]FunctionRef, 0, len(nodes))
	for _, n := range nodes {
		switch ctx := n.(type) {
		case *grammar1.ColumnExprFunctionContext:
			ref := FunctionRef{
				Name: ctx.Identifier().GetText(),
			}
			{ // Detect parametric syntax: presence of two LPAREN tokens
				lparens := ctx.AllLPAREN()
				ref.IsParametric = len(lparens) >= 2
			}
			refs = append(refs, ref)
		case *grammar1.ColumnExprWinFunctionContext:
			ref := FunctionRef{
				Name:     ctx.Identifier().GetText(),
				IsWindow: true,
			}
			refs = append(refs, ref)
		case *grammar1.ColumnExprWinFunctionTargetContext:
			identifiers := ctx.AllIdentifier()
			if len(identifiers) > 0 {
				ref := FunctionRef{
					Name:     identifiers[0].GetText(),
					IsWindow: true,
				}
				refs = append(refs, ref)
			}
		}
	}
	return
}
