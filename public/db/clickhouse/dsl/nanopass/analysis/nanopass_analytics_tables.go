//go:build llm_generated_opus46

package analysis

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// TableRef represents a reference to a table in a query.
type TableRef struct {
	Database string // empty if not qualified
	Table    string
}

// ExtractTables walks the CST and returns all table references found in TableIdentifier nodes.
// It excludes TableIdentifier nodes that appear as column qualifiers inside ColumnIdentifier.
func ExtractTables(pr *nanopass.ParseResult) (refs []TableRef) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar.TableIdentifierContext)
		if !ok {
			return false
		}
		// Skip TableIdentifier nodes that are children of ColumnIdentifier —
		// those are column qualifiers (e.g. "t1" in "t1.id"), not table references.
		parent := ctx.GetParent()
		if parent != nil {
			if _, isColId := parent.(*grammar.ColumnIdentifierContext); isColId {
				return false
			}
		}
		return true
	})
	refs = make([]TableRef, 0, len(nodes))
	for _, n := range nodes {
		tid := n.(*grammar.TableIdentifierContext)
		ref := TableRef{Table: tid.Identifier().GetText()}
		if tid.DatabaseIdentifier() != nil {
			ref.Database = tid.DatabaseIdentifier().GetText()
		}
		refs = append(refs, ref)
	}
	return
}
