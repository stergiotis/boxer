//go:build llm_generated_opus46

package passes

import (
	"iter"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type SchemaProviderI interface {
	GetColumns(dbName, tableName string) (columns iter.Seq[string], nColumns int, found bool)
}

// StaticSchemaProvider maps tables to their ordered column lists.
// Tables can be registered with or without database qualification.
// Lookup tries database-qualified first, then falls back to table-name-only.
type StaticSchemaProvider struct {
	qualified   map[string][]string // "db.table" → columns
	unqualified map[string][]string // "table" → columns (legacy/fallback)
}

// NewStaticSchemaProvider creates a SchemaProvider from a table→columns map.
// Keys can be "table" or "db.table". Table and database names are normalized to lowercase.
func NewStaticSchemaProvider(tables map[string][]string) (inst *StaticSchemaProvider) {
	inst = &StaticSchemaProvider{
		qualified:   make(map[string][]string, len(tables)),
		unqualified: make(map[string][]string, len(tables)),
	}
	for k, v := range tables {
		cols := make([]string, len(v))
		copy(cols, v)
		lower := strings.ToLower(k)
		if strings.Contains(lower, ".") {
			inst.qualified[lower] = cols
		} else {
			inst.unqualified[lower] = cols
		}
	}
	return
}

// GetColumns looks up columns for a table.
// Tries "db.table" first (if db is non-empty), then falls back to "table" only.
func (inst *StaticSchemaProvider) GetColumns(db string, tableName string) (columns iter.Seq[string], nColumns int, found bool) {
	tableLower := strings.ToLower(tableName)

	var cs []string
	if db != "" {
		key := strings.ToLower(db) + "." + tableLower
		cs, found = inst.qualified[key]
	}

	// Fallback to unqualified lookup
	if !found {
		cs, found = inst.unqualified[tableLower]
	}

	if found {
		columns = slices.Values(cs)
		nColumns = len(cs)
	}
	return
}

type CachingSchemaProvider struct {
	delegate SchemaProviderI
	cache    map[string]struct {
		timestamp time.Time
		columns   []string
	}
	maxSize int
	maxAge  time.Duration
}

func NewCachingSchemaProvider(maxAge time.Duration, delegate SchemaProviderI, maxSize int) (inst *CachingSchemaProvider) {
	return &CachingSchemaProvider{
		delegate: delegate,
		cache: make(map[string]struct {
			timestamp time.Time
			columns   []string
		}),
		maxSize: maxSize,
		maxAge:  maxAge,
	}
}

func (inst *CachingSchemaProvider) GetColumns(dbName, tableName string) (columns iter.Seq[string], nColumns int, found bool) {
	c, hit := inst.cache[tableName]
	if hit && time.Now().Sub(c.timestamp) < inst.maxAge {
		columns = slices.Values(c.columns)
		nColumns = len(c.columns)
		found = true
		return
	}
	t := time.Now()
	cs, nColumns2, found2 := inst.delegate.GetColumns(dbName, tableName)
	if found2 {
		cs2 := make([]string, 0, nColumns2)
		for v := range cs {
			cs2 = append(cs2, v)
		}
		inst.cache[tableName] = struct {
			timestamp time.Time
			columns   []string
		}{timestamp: t, columns: cs2}
	}
	return
}

var _ SchemaProviderI = (*CachingSchemaProvider)(nil)
var _ SchemaProviderI = (*StaticSchemaProvider)(nil)

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
// ExpandColumns returns a Pass that expands `*`, `table.*`, and `COLUMNS('regex')`.
// Optional defaultDatabase is used for resolving unqualified table names in schema lookups.
func ExpandColumns(schema SchemaProviderI, defaultDatabase string) nanopass.Pass {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ExpandColumns: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		scopes := nanopass.BuildScopes(pr, defaultDatabase)
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

func expandColumnsInScope(rw *antlr.TokenStreamRewriter, scope *nanopass.SelectScope, schema SchemaProviderI) (err error) {
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
func expandAsterisk(ctx *grammar.ColumnsExprAsteriskContext, scope *nanopass.SelectScope, schema SchemaProviderI) (expanded string) {
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
func expandForTable(nameOrAlias string, scope *nanopass.SelectScope, schema SchemaProviderI) (expanded string) {
	source, found := scope.ResolveAlias(nameOrAlias)
	if !found || source.IsCTE || source.IsSubquery {
		return ""
	}

	db := source.ResolvedDatabase(scope)
	columns, nColumns, found := schema.GetColumns(db, source.Table)
	if !found {
		return ""
	}

	qualifier := nameOrAlias
	parts := make([]string, 0, nColumns)
	for col := range columns {
		parts = append(parts, qualifier+"."+col)
	}
	expanded = strings.Join(parts, ", ")
	return
}

func expandForAllTables(scope *nanopass.SelectScope, schema SchemaProviderI) (expanded string) {
	var allParts []string

	for _, ts := range scope.Tables {
		if ts.IsCTE || ts.IsSubquery {
			continue
		}

		db := ts.ResolvedDatabase(scope)
		columns, _, found := schema.GetColumns(db, ts.Table)
		if !found {
			return ""
		}

		qualifier := ts.Table
		if ts.Alias != "" {
			qualifier = ts.Alias
		}

		for col := range columns {
			allParts = append(allParts, qualifier+"."+col)
		}
	}

	if len(allParts) == 0 {
		return ""
	}

	expanded = strings.Join(allParts, ", ")
	return
}

func expandDynamic(ctx *grammar.ColumnExprDynamicContext, scope *nanopass.SelectScope, schema SchemaProviderI) (expanded string) {
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

	regexStr := extractStringLiteralFromDynamic(dynCtx)
	if regexStr == "" {
		return ""
	}

	re, compileErr := regexp.Compile(regexStr)
	if compileErr != nil {
		return ""
	}

	var matched []string
	for _, ts := range scope.Tables {
		if ts.IsCTE || ts.IsSubquery {
			continue
		}

		db := ts.ResolvedDatabase(scope)
		columns, _, found := schema.GetColumns(db, ts.Table)
		if !found {
			continue
		}

		qualifier := ts.Table
		if ts.Alias != "" {
			qualifier = ts.Alias
		}

		for col := range columns {
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
