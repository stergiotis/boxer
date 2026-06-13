package analysis

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// ColumnRef represents a reference to a column in a query.
type ColumnRef struct {
	Table  string // table qualifier, empty if not qualified
	Column string // column name (may be nested like "a.b")
}

// ExtractColumns walks the CST and returns all column references found in
// ColumnIdentifier nodes, with the table qualifier decoded (quoting
// removed). Column may be a nested path ("a.b") and keeps its source
// spelling per segment.
func ExtractColumns(pr *nanopass.ParseResult) (refs []ColumnRef) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.ColumnIdentifierContext)
		return ok
	})
	refs = make([]ColumnRef, 0, len(nodes))
	for _, n := range nodes {
		cid := n.(*grammar1.ColumnIdentifierContext)
		ref := ColumnRef{}
		if cid.TableIdentifier() != nil {
			ref.Table = nanopass.DecodeIdentifier(cid.TableIdentifier().GetText())
		}
		if cid.NestedIdentifier() != nil {
			ref.Column = cid.NestedIdentifier().GetText()
		}
		refs = append(refs, ref)
	}
	return
}
