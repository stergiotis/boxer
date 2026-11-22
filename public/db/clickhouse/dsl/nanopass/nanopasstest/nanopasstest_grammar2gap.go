package nanopasstest

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// Grammar2Gap reports whether canonical — output of the canonicalisation
// pipeline that [nanopass.ParseCanonical] refused — is refused only for
// shapes grammar2 has no production for, which no pass can rewrite into one:
//
//   - a table-function argument that is an expression other than a literal,
//     an identifier, a function call or a query: `numbers(1 + 1)`,
//     `numbers({n:UInt64})`, `f((0))`, `f(x AS y)`. Grammar1 takes any
//     expression there, as ClickHouse does; grammar2's tableArgExpr has no
//     expression alternative.
//   - a query-parameter slot grammar2 does not take on its own, such as one
//     whose name grammar1 reads through its keyword-tolerant identifier rule:
//     `{id:UInt64}`. Grammar2 takes a plain identifier there, and a few
//     keyword type names. A slot is copied through the pipeline as written.
//
// It replaces each such shape with a stand-in grammar2 does take (the
// argument with 0, the slot with {x:String}) and parses the result again; ok
// is true when that parses, and gap then names the shapes replaced. A
// refusal for any other reason leaves ok false, so a fuzz target that skips
// on ok still fails on everything else.
//
// The fuzz targets over the pipeline skip on it. Both gaps close in
// grammar2's .g4, which needs the parsers regenerated; TestGrammar2KnownGaps
// fails when either does, so the skip is removed with it.
func Grammar2Gap(canonical string) (gap string, ok bool) {
	pr, err := nanopass.Parse(canonical)
	if err != nil {
		return
	}
	rw := nanopass.NewRewriter(pr)
	var shapes []string
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.TableArgExprContext:
			if c.ColumnExpr() != nil {
				shapes = append(shapes, "table-function argument "+nanopass.NodeText(pr, c))
				nanopass.ReplaceNode(rw, c, "0")
				return false
			}
		case *grammar1.ParamSlotContext:
			if _, err := nanopass.ParseCanonical("SELECT " + nanopass.NodeText(pr, c)); err != nil {
				shapes = append(shapes, "parameter slot "+nanopass.NodeText(pr, c))
				nanopass.ReplaceNode(rw, c, "{x:String}")
				return false
			}
		}
		return true
	})
	if len(shapes) == 0 {
		return
	}
	if _, err = nanopass.ParseCanonical(nanopass.GetText(rw)); err != nil {
		return
	}
	gap = "grammar2 has no production for: " + strings.Join(shapes, "; ")
	ok = true
	return
}
