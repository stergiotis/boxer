//go:build llm_generated_opus46

package nanopass

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
)

// SelectScope represents the lexical context of one SELECT statement.
type SelectScope struct {
	Node            *grammar1.SelectStmtContext
	Tables          []TableSource
	Parent          *SelectScope
	CTEDefs         []CTEDef
	UnionPeers      []*SelectScope
	Subqueries      []*SelectScope
	DefaultDatabase string // database for unqualified table references
}

// TableSource represents a table or subquery in a FROM/JOIN clause.
type TableSource struct {
	Node       antlr.ParserRuleContext
	Database   string // explicit database from SQL (empty if unqualified)
	Table      string
	Alias      string
	IsCTE      bool
	IsSubquery bool
	Scope      *SelectScope
}

// CTEDef represents a CTE definition in a WITH clause.
type CTEDef struct {
	Name  string
	Node  antlr.ParserRuleContext
	Scope *SelectScope
}

// ResolveAlias looks up a table alias or table name in this scope's Tables.
// Returns the matching TableSource and true, or zero value and false.
func (inst *SelectScope) ResolveAlias(nameOrAlias string) (source TableSource, found bool) {
	for _, ts := range inst.Tables {
		if ts.Alias == nameOrAlias {
			source = ts
			found = true
			return
		}
		if ts.Alias == "" && ts.Table == nameOrAlias {
			source = ts
			found = true
			return
		}
	}
	return
}

// ResolveCTE looks up a CTE name in this scope and its ancestors.
func (inst *SelectScope) ResolveCTE(name string) (def CTEDef, found bool) {
	current := inst
	for current != nil {
		for _, cte := range current.CTEDefs {
			if cte.Name == name {
				def = cte
				found = true
				return
			}
		}
		current = current.Parent
	}
	return
}

// AllScopes returns this scope and all descendant scopes (CTEs, subqueries)
// in depth-first order.
func (inst *SelectScope) AllScopes() (all []*SelectScope) {
	all = make([]*SelectScope, 0, 8)
	inst.collectScopes(&all)
	return
}

func (inst *SelectScope) collectScopes(all *[]*SelectScope) {
	*all = append(*all, inst)
	for _, cte := range inst.CTEDefs {
		if cte.Scope != nil {
			cte.Scope.collectScopes(all)
		}
	}
	for _, sub := range inst.Subqueries {
		sub.collectScopes(all)
	}
}

// ResolvedDatabase returns the database for this table source.
// If the table is explicitly qualified (e.g., db.table), returns the explicit database.
// Otherwise returns the scope's default database.
func (inst *TableSource) ResolvedDatabase(scope *SelectScope) (database string) {
	if inst.Database != "" {
		database = inst.Database
		return
	}
	database = scope.DefaultDatabase
	return
}

// BuildScopes walks the parse tree and constructs SelectScope objects.
// defaultDatabase is applied to all scopes for resolving unqualified table references.
// Pass empty string if no default database is known.
func BuildScopes(pr *ParseResult, defaultDatabase ...string) (scopes []*SelectScope) {
	db := ""
	if len(defaultDatabase) > 0 {
		db = defaultDatabase[0]
	}

	queryStmt := pr.Tree

	if queryStmt.GetChildCount() == 0 {
		return
	}
	query, ok := queryStmt.GetChild(0).(*grammar1.QueryContext)
	if !ok {
		return
	}

	// Gather CTE definitions from the query-level ctes rule
	var cteDefs []CTEDef
	for i := 0; i < query.GetChildCount(); i++ {
		if ctes, ok := query.GetChild(i).(*grammar1.CtesContext); ok {
			cteDefs = buildCTEDefs(ctes, nil, db)
			break
		}
	}

	// Find the selectUnionStmt
	var unionStmt *grammar1.SelectUnionStmtContext
	for i := 0; i < query.GetChildCount(); i++ {
		if u, ok := query.GetChild(i).(*grammar1.SelectUnionStmtContext); ok {
			unionStmt = u
			break
		}
	}
	if unionStmt == nil {
		return
	}

	scopes = buildUnionScopes(unionStmt, nil, cteDefs, db)
	return
}

func buildUnionScopes(union *grammar1.SelectUnionStmtContext, parent *SelectScope, cteDefs []CTEDef, defaultDB string) (scopes []*SelectScope) {
	scopes = make([]*SelectScope, 0, union.GetChildCount())

	for i := 0; i < union.GetChildCount(); i++ {
		child := union.GetChild(i)
		switch c := child.(type) {
		case *grammar1.SelectStmtWithParensContext:
			scope := buildSelectScope(c, parent, cteDefs, defaultDB)
			if scope != nil {
				scopes = append(scopes, scope)
			}
		case *grammar1.SelectUnionStmtItemContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if swp, ok := c.GetChild(j).(*grammar1.SelectStmtWithParensContext); ok {
					scope := buildSelectScope(swp, parent, cteDefs, defaultDB)
					if scope != nil {
						scopes = append(scopes, scope)
					}
				}
			}
		}
	}

	for _, s := range scopes {
		s.UnionPeers = scopes
	}

	return
}

func buildSelectScope(node *grammar1.SelectStmtWithParensContext, parent *SelectScope, cteDefs []CTEDef, defaultDB string) (scope *SelectScope) {
	for i := 0; i < node.GetChildCount(); i++ {
		if stmt, ok := node.GetChild(i).(*grammar1.SelectStmtContext); ok {
			scope = buildScopeFromSelectStmt(stmt, parent, cteDefs, defaultDB)
			return
		}
	}

	for i := 0; i < node.GetChildCount(); i++ {
		if u, ok := node.GetChild(i).(*grammar1.SelectUnionStmtContext); ok {
			innerScopes := buildUnionScopes(u, parent, cteDefs, defaultDB)
			if len(innerScopes) > 0 {
				scope = innerScopes[0]
			}
			return
		}
	}
	return
}

func buildScopeFromSelectStmt(stmt *grammar1.SelectStmtContext, parent *SelectScope, cteDefs []CTEDef, defaultDB string) (scope *SelectScope) {
	scope = &SelectScope{
		Node:            stmt,
		Parent:          parent,
		CTEDefs:         cteDefs,
		DefaultDatabase: defaultDB,
	}

	// Extract table sources from FROM/JOIN
	fromClause := stmt.FromClause()
	if fromClause != nil {
		scope.Tables = extractTableSources(fromClause.(*grammar1.FromClauseContext), scope)
	}

	// Mark CTE references
	for i := range scope.Tables {
		ts := &scope.Tables[i]
		if ts.IsSubquery {
			continue
		}
		if _, found := scope.ResolveCTE(ts.Table); found {
			ts.IsCTE = true
		}
	}

	// Find subqueries in expressions
	scope.Subqueries = findSubqueryScopes(stmt, scope, defaultDB)

	return
}

func findSubqueryScopes(stmt *grammar1.SelectStmtContext, parent *SelectScope, defaultDB string) (subqueries []*SelectScope) {
	fromSubqueryNodes := make(map[antlr.ParserRuleContext]bool, len(parent.Tables))
	for _, ts := range parent.Tables {
		if ts.IsSubquery && ts.Node != nil {
			fromSubqueryNodes[ts.Node] = true
		}
	}

	WalkCST(stmt, func(ctx antlr.ParserRuleContext) bool {
		if _, ok := ctx.(*grammar1.SelectStmtContext); ok && ctx != stmt {
			return false
		}

		switch c := ctx.(type) {
		case *grammar1.TableExprSubqueryContext:
			if fromSubqueryNodes[c] {
				return false
			}
			subScope := buildSubqueryFromTableExpr(c, parent, defaultDB)
			if subScope != nil {
				subqueries = append(subqueries, subScope)
			}
			return false

		case *grammar1.ColumnExprSubqueryContext:
			subScope := buildSubqueryFromColumnExpr(c, parent, defaultDB)
			if subScope != nil {
				subqueries = append(subqueries, subScope)
			}
			return false
		}
		return true
	})
	return
}

func buildSubqueryFromTableExpr(expr *grammar1.TableExprSubqueryContext, parent *SelectScope, defaultDB string) (scope *SelectScope) {
	for i := 0; i < expr.GetChildCount(); i++ {
		if u, ok := expr.GetChild(i).(*grammar1.SelectUnionStmtContext); ok {
			innerScopes := buildUnionScopes(u, parent, nil, defaultDB)
			if len(innerScopes) > 0 {
				scope = innerScopes[0]
			}
			return
		}
	}
	return
}

func buildSubqueryFromColumnExpr(expr *grammar1.ColumnExprSubqueryContext, parent *SelectScope, defaultDB string) (scope *SelectScope) {
	for i := 0; i < expr.GetChildCount(); i++ {
		if u, ok := expr.GetChild(i).(*grammar1.SelectUnionStmtContext); ok {
			innerScopes := buildUnionScopes(u, parent, nil, defaultDB)
			if len(innerScopes) > 0 {
				scope = innerScopes[0]
			}
			return
		}
	}
	for i := 0; i < expr.GetChildCount(); i++ {
		if q, ok := expr.GetChild(i).(*grammar1.QueryContext); ok {
			for j := 0; j < q.GetChildCount(); j++ {
				if u, ok := q.GetChild(j).(*grammar1.SelectUnionStmtContext); ok {
					innerScopes := buildUnionScopes(u, parent, nil, defaultDB)
					if len(innerScopes) > 0 {
						scope = innerScopes[0]
					}
					return
				}
			}
		}
	}
	return
}

func extractTableSources(from *grammar1.FromClauseContext, parentScope *SelectScope) (sources []TableSource) {
	sources = make([]TableSource, 0, 4)

	WalkCST(from, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.TableExprAliasContext:
			ts := extractFromAliasExpr(c, parentScope)
			if ts != nil {
				sources = append(sources, *ts)
			}
			return false

		case *grammar1.TableExprIdentifierContext:
			ts := tableSourceFromIdentifier(c)
			if ts != nil {
				sources = append(sources, *ts)
			}
			return false

		case *grammar1.TableExprSubqueryContext:
			ts := &TableSource{
				Node:       c,
				IsSubquery: true,
			}
			ts.Scope = buildSubqueryFromTableExpr(c, parentScope, parentScope.DefaultDatabase)
			sources = append(sources, *ts)
			return false

		case *grammar1.TableExprFunctionContext:
			return false
		}
		return true
	})

	return
}

func extractFromAliasExpr(aliasExpr *grammar1.TableExprAliasContext, parentScope *SelectScope) (ts *TableSource) {
	var alias string
	for i := 0; i < aliasExpr.GetChildCount(); i++ {
		child := aliasExpr.GetChild(i)
		if identCtx, ok := child.(*grammar1.IdentifierContext); ok {
			alias = identCtx.GetText()
			break
		}
	}

	for i := 0; i < aliasExpr.GetChildCount(); i++ {
		child := aliasExpr.GetChild(i)
		switch c := child.(type) {
		case *grammar1.TableExprIdentifierContext:
			ts = tableSourceFromIdentifier(c)
			if ts != nil {
				ts.Alias = alias
			}
			return
		case *grammar1.TableExprSubqueryContext:
			ts = &TableSource{
				Node:       c,
				Alias:      alias,
				IsSubquery: true,
			}
			ts.Scope = buildSubqueryFromTableExpr(c, parentScope, parentScope.DefaultDatabase)
			return
		case *grammar1.TableExprFunctionContext:
			return nil
		}
	}
	return nil
}

func tableSourceFromIdentifier(expr *grammar1.TableExprIdentifierContext) (ts *TableSource) {
	for i := 0; i < expr.GetChildCount(); i++ {
		child := expr.GetChild(i)
		tid, ok := child.(*grammar1.TableIdentifierContext)
		if !ok {
			continue
		}
		ts = &TableSource{
			Node:  tid,
			Table: tid.Identifier().GetText(),
		}
		if tid.DatabaseIdentifier() != nil {
			ts.Database = tid.DatabaseIdentifier().GetText()
		}
		return
	}
	return nil
}

func buildCTEDefs(ctes *grammar1.CtesContext, parent *SelectScope, defaultDB string) (defs []CTEDef) {
	defs = make([]CTEDef, 0, ctes.GetChildCount())
	for i := 0; i < ctes.GetChildCount(); i++ {
		nqCtx, ok := ctes.GetChild(i).(*grammar1.NamedQueryContext)
		if !ok {
			continue
		}
		name := nqCtx.Identifier().GetText()
		def := CTEDef{
			Name: name,
			Node: nqCtx,
		}
		for j := 0; j < nqCtx.GetChildCount(); j++ {
			if qCtx, ok := nqCtx.GetChild(j).(*grammar1.QueryContext); ok {
				for k := 0; k < qCtx.GetChildCount(); k++ {
					if unionStmt, ok := qCtx.GetChild(k).(*grammar1.SelectUnionStmtContext); ok {
						innerScopes := buildUnionScopes(unionStmt, parent, nil, defaultDB)
						if len(innerScopes) > 0 {
							def.Scope = innerScopes[0]
						}
						break
					}
				}
				break
			}
		}
		defs = append(defs, def)
	}
	return
}
