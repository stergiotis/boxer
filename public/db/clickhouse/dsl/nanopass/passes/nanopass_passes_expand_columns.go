//go:build llm_generated_opus46

package passes

import (
	"regexp"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SchemaProvider maps lowercase table names to their ordered column lists.
// Column names are case-sensitive and preserve the original casing from ClickHouse.
type SchemaProvider struct {
	tables map[string][]string // lowercase table name → column names
}

// NewSchemaProvider creates a SchemaProvider from a table→columns map.
// Table names are normalized to lowercase for matching.
func NewSchemaProvider(tables map[string][]string) (inst *SchemaProvider) {
	lower := make(map[string][]string, len(tables))
	for k, v := range tables {
		cols := make([]string, len(v))
		copy(cols, v)
		lower[strings.ToLower(k)] = cols
	}
	inst = &SchemaProvider{tables: lower}
	return
}

// GetColumns returns the column list for a table (case-insensitive lookup).
func (inst *SchemaProvider) GetColumns(tableName string) (columns []string, found bool) {
	columns, found = inst.tables[strings.ToLower(tableName)]
	return
}

// ExpandColumns returns a Pass that expands `*`, `table.*`, and `COLUMNS('regex')`
// into explicit column lists using the provided schema.
//
// Expansion rules:
//   - `*` — expands to all columns from all tables in the FROM clause, in table order
//   - `table.*` — expands to all columns from the specified table, qualified with table name or alias
//   - `COLUMNS('regex')` — expands to all columns (from all tables) matching the regex
//
// If a table is not found in the schema, the expression is left unexpanded.
// CTE references and subquery sources are skipped (no schema for them).
func ExpandColumns(schema *SchemaProvider) nanopass.Pass {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ExpandColumns: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		scopes := nanopass.BuildScopes(pr)
		for _, scope := range scopes {
			err = expandColumnsInScope(rw, scope, schema)
			if err != nil {
				return
			}
		}

		result = nanopass.GetText(rw)
		return
	}
}

func expandColumnsInScope(rw *antlr.TokenStreamRewriter, scope *nanopass.SelectScope, schema *SchemaProvider) (err error) {
	// Expand column expressions in this scope's SELECT list
	stmt := scope.Node
	projClause := stmt.ProjectionClause()
	if projClause == nil {
		return
	}

	// Walk the projection clause looking for expandable expressions
	nanopass.WalkCST(projClause.(antlr.ParserRuleContext), func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar.ColumnsExprAsteriskContext:
			expanded := expandAsterisk(c, scope, schema)
			if expanded != "" {
				nanopass.ReplaceNode(rw, c, expanded)
			}
			return false

		case *grammar.ColumnExprDynamicContext:
			expanded := expandDynamic(c, scope, schema)
			if expanded != "" {
				// Replace the parent ColumnsExprColumn, not just the dynamic expr,
				// to get clean output
				parent := c.GetParent()
				if prc, ok := parent.(antlr.ParserRuleContext); ok {
					nanopass.ReplaceNode(rw, prc, expanded)
				}
			}
			return false
		}
		return true
	})

	// Recurse into CTE body scopes
	for _, cte := range scope.CTEDefs {
		if cte.Scope != nil {
			err = expandColumnsInScope(rw, cte.Scope, schema)
			if err != nil {
				return
			}
		}
	}

	// Recurse into FROM subquery scopes
	for _, ts := range scope.Tables {
		if ts.IsSubquery && ts.Scope != nil {
			err = expandColumnsInScope(rw, ts.Scope, schema)
			if err != nil {
				return
			}
		}
	}

	// Recurse into expression subqueries
	for _, sub := range scope.Subqueries {
		err = expandColumnsInScope(rw, sub, schema)
		if err != nil {
			return
		}
	}

	return
}

// expandAsterisk expands `*` or `table.*` into a comma-separated column list.
func expandAsterisk(ctx *grammar.ColumnsExprAsteriskContext, scope *nanopass.SelectScope, schema *SchemaProvider) (expanded string) {
	// Check if it's `table.*` or bare `*`
	var tableIdCtx *grammar.TableIdentifierContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if tid, ok := ctx.GetChild(i).(*grammar.TableIdentifierContext); ok {
			tableIdCtx = tid
			break
		}
	}

	if tableIdCtx != nil {
		// table.* — expand for a specific table
		tableName := tableIdCtx.Identifier().GetText()
		expanded = expandForTable(tableName, scope, schema)
	} else {
		// bare * — expand for all tables in scope
		expanded = expandForAllTables(scope, schema)
	}
	return
}

// expandForTable expands columns for a single table reference (by name or alias).
func expandForTable(nameOrAlias string, scope *nanopass.SelectScope, schema *SchemaProvider) (expanded string) {
	// Resolve the table — could be an alias or a direct table name
	source, found := scope.ResolveAlias(nameOrAlias)
	if !found {
		return "" // unknown table — leave unexpanded
	}
	if source.IsCTE || source.IsSubquery {
		return "" // can't expand CTEs or subqueries without schema
	}

	columns, found := schema.GetColumns(source.Table)
	if !found {
		return "" // table not in schema
	}

	// Determine the qualifier prefix
	qualifier := nameOrAlias

	parts := make([]string, 0, len(columns))
	for _, col := range columns {
		parts = append(parts, qualifier+"."+col)
	}
	expanded = strings.Join(parts, ", ")
	return
}

// expandForAllTables expands `*` to all columns from all schema-known tables in the scope.
func expandForAllTables(scope *nanopass.SelectScope, schema *SchemaProvider) (expanded string) {
	var allParts []string

	for _, ts := range scope.Tables {
		if ts.IsCTE || ts.IsSubquery {
			continue
		}

		columns, found := schema.GetColumns(ts.Table)
		if !found {
			return "" // if any table is missing from schema, leave unexpanded
		}

		qualifier := ts.Table
		if ts.Alias != "" {
			qualifier = ts.Alias
		}

		for _, col := range columns {
			allParts = append(allParts, qualifier+"."+col)
		}
	}

	if len(allParts) == 0 {
		return ""
	}

	expanded = strings.Join(allParts, ", ")
	return
}

// expandDynamic expands COLUMNS('regex') into matching columns from all tables in scope.
func expandDynamic(ctx *grammar.ColumnExprDynamicContext, scope *nanopass.SelectScope, schema *SchemaProvider) (expanded string) {
	// Extract the regex string from DynamicColumnSelectionContext
	var dynCtx *grammar.DynamicColumnSelectionContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if d, ok := ctx.GetChild(i).(*grammar.DynamicColumnSelectionContext); ok {
			dynCtx = d
			break
		}
	}
	if dynCtx == nil {
		return ""
	}

	// Extract the string literal — it's a terminal node
	regexStr := extractStringLiteralFromDynamic(dynCtx)
	if regexStr == "" {
		return ""
	}

	// Compile the regex
	re, compileErr := regexp.Compile(regexStr)
	if compileErr != nil {
		return "" // invalid regex — leave unexpanded
	}

	// Match columns from all tables in scope
	var matched []string
	for _, ts := range scope.Tables {
		if ts.IsCTE || ts.IsSubquery {
			continue
		}

		columns, found := schema.GetColumns(ts.Table)
		if !found {
			continue
		}

		qualifier := ts.Table
		if ts.Alias != "" {
			qualifier = ts.Alias
		}

		for _, col := range columns {
			if re.MatchString(col) {
				matched = append(matched, qualifier+"."+col)
			}
		}
	}

	if len(matched) == 0 {
		return ""
	}

	expanded = strings.Join(matched, ", ")
	return
}

// extractStringLiteralFromDynamic extracts the regex pattern from a DynamicColumnSelectionContext.
// The structure is: COLUMNS ( 'pattern' )
func extractStringLiteralFromDynamic(ctx *grammar.DynamicColumnSelectionContext) (pattern string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		tn, ok := child.(antlr.TerminalNode)
		if !ok {
			continue
		}
		text := tn.GetText()
		if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' {
			pattern = text[1 : len(text)-1]
			return
		}
	}
	return
}
