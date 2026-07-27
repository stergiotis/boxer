package analysis

import (
	"strings"

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
// ColumnIdentifier nodes, with both the table qualifier and the column
// name decoded (quoting removed). Column may be a nested path ("a.b"),
// in which case each segment is decoded separately.
//
// Decoding the column matters as much as decoding the table: a name that
// needs quoting — `content@text/markdown` — otherwise comes back wearing
// its backticks and matches nothing a caller compares it against. That
// is a silent miss, not an error, so it surfaces as a caller behaving as
// if the column had not been referenced at all.
func ExtractColumns(pr *nanopass.ParseResult) (refs []ColumnRef) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.ColumnIdentifierContext)
		return ok
	})
	refs = make([]ColumnRef, 0, len(nodes))
	for _, n := range nodes {
		cid := n.(*grammar1.ColumnIdentifierContext)
		ref := ColumnRef{}
		if ti := cid.TableIdentifier(); ti != nil {
			ref.Table = nanopass.DecodeIdentifier(ti.Identifier().GetText())
			if db := ti.DatabaseIdentifier(); db != nil {
				ref.Table = nanopass.DecodeIdentifier(db.GetText()) + "." + ref.Table
			}
		}
		if ni := cid.NestedIdentifier(); ni != nil {
			ref.Column = decodePath(ni.AllIdentifier())
		}
		refs = append(refs, ref)
	}
	return
}

// decodePath decodes each segment of a dotted identifier and rejoins
// them. Decoding the whole text instead would read the leading and
// trailing quote of ``a`.`b`` as one pair and mangle the name.
func decodePath(ids []grammar1.IIdentifierContext) (path string) {
	if len(ids) == 1 {
		return nanopass.DecodeIdentifier(ids[0].GetText())
	}
	segs := make([]string, 0, len(ids))
	for _, id := range ids {
		segs = append(segs, nanopass.DecodeIdentifier(id.GetText()))
	}
	return strings.Join(segs, ".")
}
