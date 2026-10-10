package analysis

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// TableRef represents a reference to a table in a query.
type TableRef struct {
	Database string // empty if not qualified
	Table    string
}

// ExtractTables walks the CST and returns all table references found in
// TableIdentifier nodes, with names decoded (quoting removed). Column
// qualifiers are ColumnQualifier nodes and are not reported. The walk is
// purely syntactic: CTE references look like table references and ARE
// included — resolve against nanopass.BuildScopes (TableSource.IsCTE) when
// real tables must be distinguished.
func ExtractTables(pr *nanopass.ParseResult) (refs []TableRef) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		// Column qualifiers ("t1" in "t1.id") are a columnQualifier node, not a
		// tableIdentifier, so they never match here.
		_, ok := ctx.(*grammar1.TableIdentifierContext)
		return ok
	})
	refs = make([]TableRef, 0, len(nodes))
	for _, n := range nodes {
		tid := n.(*grammar1.TableIdentifierContext)
		ref := TableRef{Table: nanopass.TableIdentifierName(tid)}
		if tid.DatabaseIdentifier() != nil {
			ref.Database = nanopass.DatabaseIdentifierName(tid.DatabaseIdentifier())
		}
		refs = append(refs, ref)
	}
	return
}
