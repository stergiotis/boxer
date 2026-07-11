package nanopass

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SelectScope represents the lexical context of one SELECT statement.
//
// Names (table, database, alias, CTE) are stored decoded — quoting and
// escapes removed via [DecodeIdentifier] — so two spellings of the same
// name compare equal. When splicing names back into SQL, re-encode with
// [QuoteIdentifier] or use the original node text.
type SelectScope struct {
	Node *grammar1.SelectStmtContext

	// Tables are the FROM/JOIN sources of this SELECT.
	Tables []TableSource

	// Parent is the lexically enclosing scope (nil at top level).
	Parent *SelectScope

	// CTEDefs are the CTE definitions visible to this scope: its own WITH
	// clause first (shadowing), then inherited definitions from the
	// enclosing query/scope. Slices are shared between scopes that inherit
	// the same definitions; AllScopes/FlattenScopes deduplicate on
	// traversal.
	CTEDefs []CTEDef

	// UnionMembers lists every SELECT of the enclosing UNION chain in
	// source order, including this scope itself; nested parenthesised
	// unions are flattened in. A non-union SELECT has itself as the only
	// member.
	UnionMembers []*SelectScope

	// Subqueries are scopes for expression-level subqueries (projection,
	// WHERE, HAVING, IN, function arguments, …) — one entry per UNION
	// branch of each subquery. FROM-clause subqueries are NOT listed here;
	// they hang off Tables[i].Scopes.
	Subqueries []*SelectScope

	// DefaultDatabase is the connection default for resolving unqualified
	// table references.
	DefaultDatabase string
}

// TableSource represents a table, table function, or subquery in a
// FROM/JOIN clause.
type TableSource struct {
	Node antlr.ParserRuleContext

	// Database is the decoded explicit database qualifier (empty if
	// unqualified).
	Database string

	// Table is the decoded table name; for IsFunction sources it carries
	// the function name (for diagnostics — there is no relation name).
	Table string

	// Alias is the decoded alias (from both `t alias` and `t AS alias`
	// forms); empty if none.
	Alias string

	IsCTE      bool
	IsSubquery bool

	// IsFunction marks table functions (numbers(10), remote(…), …). They
	// participate in alias resolution but have no database to resolve —
	// rewriting passes must leave them alone.
	IsFunction bool

	// Scopes holds the inner scopes of an IsSubquery source, one per UNION
	// branch.
	Scopes []*SelectScope
}

// CTEDef represents a CTE definition in a WITH clause.
type CTEDef struct {
	// Name is the decoded CTE name.
	Name string
	Node antlr.ParserRuleContext

	// Scopes holds the body scopes, one per UNION branch of the CTE body.
	Scopes []*SelectScope

	// Recursive marks a definition from a `WITH RECURSIVE` clause. The
	// definition is then visible inside its own body (that visibility IS what
	// recursion means), so a self-reference resolves to this def and is marked
	// IsCTE. The self-entry placed in the body's CTEDefs carries nil Scopes —
	// traversals therefore never descend from a body back into itself.
	Recursive bool
}

// ResolveAlias looks up a table alias or table name in this scope's Tables.
// An aliased source is matched by its alias only (the alias hides the
// table name, per SQL scoping). nameOrAlias may be quoted or bare — it is
// decoded before comparison. Returns the matching TableSource and true, or
// zero value and false.
func (inst *SelectScope) ResolveAlias(nameOrAlias string) (source TableSource, found bool) {
	name := DecodeIdentifier(nameOrAlias)
	for _, ts := range inst.Tables {
		if ts.Alias == name {
			source = ts
			found = true
			return
		}
		if ts.Alias == "" && ts.Table == name {
			source = ts
			found = true
			return
		}
	}
	return
}

// ResolveCTE looks up a CTE name in this scope and its ancestors. name may
// be quoted or bare — it is decoded before comparison.
func (inst *SelectScope) ResolveCTE(name string) (def CTEDef, found bool) {
	decoded := DecodeIdentifier(name)
	current := inst
	for current != nil {
		for _, cte := range current.CTEDefs {
			if cte.Name == decoded {
				def = cte
				found = true
				return
			}
		}
		current = current.Parent
	}
	return
}

// AllScopes returns this scope and all descendant scopes (CTE bodies, FROM
// subqueries, expression subqueries) in depth-first order. Each scope
// appears exactly once, even when CTE definitions are shared between
// sibling scopes.
func (inst *SelectScope) AllScopes() (all []*SelectScope) {
	all = make([]*SelectScope, 0, 8)
	seen := make(map[*SelectScope]struct{}, 8)
	inst.collectScopes(&all, seen)
	return
}

// FlattenScopes returns every scope reachable from the given roots (the
// roots themselves plus all descendants), deduplicated globally. Use this
// to iterate the full scope tree of a BuildScopes result — CTE definitions
// are shared between UNION members, so per-root AllScopes calls would
// visit them once per member.
func FlattenScopes(roots []*SelectScope) (all []*SelectScope) {
	all = make([]*SelectScope, 0, len(roots)*2)
	seen := make(map[*SelectScope]struct{}, len(roots)*2)
	for _, r := range roots {
		r.collectScopes(&all, seen)
	}
	return
}

func (inst *SelectScope) collectScopes(all *[]*SelectScope, seen map[*SelectScope]struct{}) {
	if _, dup := seen[inst]; dup {
		return
	}
	seen[inst] = struct{}{}
	*all = append(*all, inst)
	for _, cte := range inst.CTEDefs {
		for _, s := range cte.Scopes {
			s.collectScopes(all, seen)
		}
	}
	for _, ts := range inst.Tables {
		for _, s := range ts.Scopes {
			s.collectScopes(all, seen)
		}
	}
	for _, sub := range inst.Subqueries {
		sub.collectScopes(all, seen)
	}
}

// ResolvedDatabase returns the database for this table source.
// If the table is explicitly qualified (e.g., db.table), returns the explicit database.
// Otherwise returns the scope's default database. Meaningless for IsCTE,
// IsSubquery, and IsFunction sources — none of them live in a database.
func (inst *TableSource) ResolvedDatabase(scope *SelectScope) (database string) {
	if inst.Database != "" {
		database = inst.Database
		return
	}
	database = scope.DefaultDatabase
	return
}

// BuildScopes walks the parse tree and constructs SelectScope objects — one
// per member of the top-level UNION chain (a single SELECT yields one
// scope). defaultDatabase is applied to all scopes for resolving
// unqualified table references; pass "" if no default is known.
//
// Returns an error when the tree does not have the expected
// queryStmt → query → selectUnionStmt shape — that indicates a parse
// produced by something other than [Parse], not a valid-but-empty query.
func BuildScopes(pr *ParseResult, defaultDatabase string) (scopes []*SelectScope, err error) {
	queryStmt := pr.Tree
	if queryStmt == nil || queryStmt.GetChildCount() == 0 {
		err = eb.Build().Errorf("BuildScopes: empty parse tree")
		return
	}
	query, ok := queryStmt.GetChild(0).(*grammar1.QueryContext)
	if !ok {
		err = eb.Build().Type("child", queryStmt.GetChild(0)).Errorf("BuildScopes: expected query as first child of queryStmt")
		return
	}
	scopes = buildQueryScopes(query, nil, nil, defaultDatabase)
	if len(scopes) == 0 {
		err = eb.Build().Errorf("BuildScopes: query contains no selectUnionStmt")
	}
	return
}

// buildQueryScopes handles a grammar1 query rule: harvests its ctes (if
// any), builds their body scopes with earlier definitions visible to later
// ones, then builds the selectUnionStmt's scopes with all definitions plus
// the inherited ones visible.
func buildQueryScopes(query *grammar1.QueryContext, parent *SelectScope, inherited []CTEDef, defaultDB string) (scopes []*SelectScope) {
	visible := inherited
	for i := 0; i < query.GetChildCount(); i++ {
		if ctes, ok := query.GetChild(i).(*grammar1.CtesContext); ok {
			own := buildCTEDefs(ctes, parent, inherited, defaultDB)
			visible = combineCTEDefs(own, inherited)
			break
		}
	}
	for i := 0; i < query.GetChildCount(); i++ {
		if u, ok := query.GetChild(i).(*grammar1.SelectUnionStmtContext); ok {
			scopes = buildUnionScopes(u, parent, visible, defaultDB)
			return
		}
	}
	return
}

// combineCTEDefs prepends own definitions to inherited ones (own shadow
// inherited on lookup). Always allocates — the inherited slice is shared
// with sibling scopes and must not be appended to in place.
func combineCTEDefs(own, inherited []CTEDef) []CTEDef {
	if len(own) == 0 {
		return inherited
	}
	combined := make([]CTEDef, 0, len(own)+len(inherited))
	combined = append(combined, own...)
	combined = append(combined, inherited...)
	return combined
}

// buildUnionScopes returns one scope per member of the union chain,
// flattening nested parenthesised unions. Every returned scope gets the
// full flattened member list as UnionMembers (overwriting narrower lists
// set by nested calls — UNION ALL is associative, the flat list is the
// truth).
func buildUnionScopes(union *grammar1.SelectUnionStmtContext, parent *SelectScope, cteDefs []CTEDef, defaultDB string) (scopes []*SelectScope) {
	scopes = make([]*SelectScope, 0, union.GetChildCount())

	for i := 0; i < union.GetChildCount(); i++ {
		child := union.GetChild(i)
		switch c := child.(type) {
		case *grammar1.SelectStmtWithParensContext:
			scopes = append(scopes, buildSelectStmtWithParens(c, parent, cteDefs, defaultDB)...)
		case *grammar1.SelectUnionStmtItemContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if swp, ok := c.GetChild(j).(*grammar1.SelectStmtWithParensContext); ok {
					scopes = append(scopes, buildSelectStmtWithParens(swp, parent, cteDefs, defaultDB)...)
				}
			}
		}
	}

	for _, s := range scopes {
		s.UnionMembers = scopes
	}

	return
}

// buildSelectStmtWithParens handles the selectStmtWithParens rule: either a
// plain selectStmt (one scope) or a parenthesised selectUnionStmt (all of
// its member scopes).
func buildSelectStmtWithParens(node *grammar1.SelectStmtWithParensContext, parent *SelectScope, cteDefs []CTEDef, defaultDB string) (scopes []*SelectScope) {
	for i := 0; i < node.GetChildCount(); i++ {
		switch c := node.GetChild(i).(type) {
		case *grammar1.SelectStmtContext:
			scope := buildScopeFromSelectStmt(c, parent, cteDefs, defaultDB)
			return []*SelectScope{scope}
		case *grammar1.SelectUnionStmtContext:
			return buildUnionScopes(c, parent, cteDefs, defaultDB)
		}
	}
	return nil
}

func buildScopeFromSelectStmt(stmt *grammar1.SelectStmtContext, parent *SelectScope, inheritedDefs []CTEDef, defaultDB string) (scope *SelectScope) {
	scope = &SelectScope{
		Node:            stmt,
		Parent:          parent,
		DefaultDatabase: defaultDB,
	}

	// The select's own WITH clause (selectStmt-level — distinct from the
	// query-level ctes rule) shadows inherited definitions.
	scope.CTEDefs = inheritedDefs
	if wc := stmt.WithClause(); wc != nil {
		if wcCtx, ok := wc.(*grammar1.WithClauseContext); ok {
			own := buildCTEDefs(wcCtx, parent, inheritedDefs, defaultDB)
			scope.CTEDefs = combineCTEDefs(own, inheritedDefs)
		}
	}

	// Extract table sources from FROM/JOIN
	fromClause := stmt.FromClause()
	if fromClause != nil {
		scope.Tables = extractTableSources(fromClause.(*grammar1.FromClauseContext), scope)
	}

	// Mark CTE references
	for i := range scope.Tables {
		ts := &scope.Tables[i]
		if ts.IsSubquery || ts.IsFunction {
			continue
		}
		if ts.Database != "" {
			continue // db.name can never be a CTE reference
		}
		if _, found := scope.ResolveCTE(ts.Table); found {
			ts.IsCTE = true
		}
	}

	// Find subqueries in expressions
	scope.Subqueries = findSubqueryScopes(stmt, scope, defaultDB)

	return
}

// findSubqueryScopes walks one SELECT's subtree and builds scopes for
// expression-level subqueries: scalar/IN subqueries (ColumnExprSubquery)
// and subqueries in projection or function-argument position
// (ColumnsExprSubquery). FROM-clause subqueries are excluded — they are
// owned by Tables[i].Scopes. CTE bodies (namedQuery) are excluded — they
// are owned by CTEDefs.
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
		case *grammar1.NamedQueryContext:
			// CTE body inside this select's WITH clause — scoped via CTEDefs.
			return false

		case *grammar1.TableExprSubqueryContext:
			if fromSubqueryNodes[c] {
				return false
			}
			subqueries = append(subqueries, buildNestedUnionScopes(c, parent, defaultDB)...)
			return false

		case *grammar1.ColumnExprSubqueryContext:
			subqueries = append(subqueries, buildNestedUnionScopes(c, parent, defaultDB)...)
			return false

		case *grammar1.ColumnsExprSubqueryContext:
			subqueries = append(subqueries, buildNestedUnionScopes(c, parent, defaultDB)...)
			return false
		}
		return true
	})
	return
}

// buildNestedUnionScopes builds the scopes (one per UNION branch) of the
// selectUnionStmt child of a subquery wrapper context. The parent scope's
// visible CTE definitions are inherited — a subquery may reference CTEs
// declared by any enclosing query.
func buildNestedUnionScopes(wrapper antlr.ParserRuleContext, parent *SelectScope, defaultDB string) []*SelectScope {
	var inherited []CTEDef
	if parent != nil {
		inherited = parent.CTEDefs
	}
	for i := 0; i < wrapper.GetChildCount(); i++ {
		if u, ok := wrapper.GetChild(i).(*grammar1.SelectUnionStmtContext); ok {
			return buildUnionScopes(u, parent, inherited, defaultDB)
		}
	}
	return nil
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
				Scopes:     buildNestedUnionScopes(c, parentScope, parentScope.DefaultDatabase),
			}
			sources = append(sources, *ts)
			return false

		case *grammar1.TableExprFunctionContext:
			ts := tableSourceFromFunction(c)
			if ts != nil {
				sources = append(sources, *ts)
			}
			return false
		}
		return true
	})

	return
}

// extractFromAliasExpr handles tableExpr (alias | AS identifier). Both
// alias forms are captured: the bare form produces an AliasContext child,
// the AS form a plain IdentifierContext child.
func extractFromAliasExpr(aliasExpr *grammar1.TableExprAliasContext, parentScope *SelectScope) (ts *TableSource) {
	var alias string
	for i := 0; i < aliasExpr.GetChildCount(); i++ {
		switch c := aliasExpr.GetChild(i).(type) {
		case *grammar1.AliasContext:
			alias = DecodeIdentifier(c.GetText())
		case *grammar1.IdentifierContext:
			alias = DecodeIdentifier(c.GetText())
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
				Scopes:     buildNestedUnionScopes(c, parentScope, parentScope.DefaultDatabase),
			}
			return
		case *grammar1.TableExprFunctionContext:
			ts = tableSourceFromFunction(c)
			if ts != nil {
				ts.Alias = alias
			}
			return
		case *grammar1.TableExprAliasContext:
			// Grammar permits stacked aliases; the outermost wins.
			ts = extractFromAliasExpr(c, parentScope)
			if ts != nil && alias != "" {
				ts.Alias = alias
			}
			return
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
			Table: DecodeIdentifier(tid.Identifier().GetText()),
		}
		if tid.DatabaseIdentifier() != nil {
			ts.Database = DecodeIdentifier(tid.DatabaseIdentifier().GetText())
		}
		return
	}
	return nil
}

// tableSourceFromFunction captures a table function (numbers(10),
// remote(…), …) as an IsFunction source so alias resolution works. The
// function's arguments are opaque — rewriting passes must not touch them.
func tableSourceFromFunction(expr *grammar1.TableExprFunctionContext) (ts *TableSource) {
	for i := 0; i < expr.GetChildCount(); i++ {
		fn, ok := expr.GetChild(i).(*grammar1.TableFunctionExprContext)
		if !ok {
			continue
		}
		name := ""
		if fn.Identifier() != nil {
			name = DecodeIdentifier(fn.Identifier().GetText())
		}
		ts = &TableSource{
			Node:       fn,
			Table:      name,
			IsFunction: true,
		}
		return
	}
	return nil
}

// buildCTEDefs builds the definitions of one WITH clause (either the
// query-level ctes rule or the selectStmt-level withClause rule — both
// contain withItem children). Earlier definitions are visible to later
// bodies (chained CTEs); inherited definitions from enclosing scopes are
// visible to all bodies; under WITH RECURSIVE a definition is additionally
// visible to its own body (see CTEDef.Recursive). CTE bodies are full query
// rules and may carry their own nested WITH clauses — handled by recursion
// through buildQueryScopes.
func buildCTEDefs(withContainer antlr.ParserRuleContext, parent *SelectScope, inherited []CTEDef, defaultDB string) (defs []CTEDef) {
	recursive := withClauseIsRecursive(withContainer)
	defs = make([]CTEDef, 0, withContainer.GetChildCount())
	for i := 0; i < withContainer.GetChildCount(); i++ {
		// withItem alternation: WithItemNamedQueryContext wraps the CTE form
		// (`name AS (subquery)`); WithItemColumnsExprContext wraps the scalar
		// alias form (`expr AS name`) — the latter has no CTE scope, skip it.
		wi, ok := withContainer.GetChild(i).(*grammar1.WithItemNamedQueryContext)
		if !ok {
			continue
		}
		nqCtx, ok := wi.NamedQuery().(*grammar1.NamedQueryContext)
		if !ok {
			continue
		}
		def := CTEDef{
			Name:      DecodeIdentifier(nqCtx.Identifier().GetText()),
			Node:      nqCtx,
			Recursive: recursive,
		}
		own := defs
		if recursive {
			// The definition is visible inside its own body. The self-entry
			// carries nil Scopes: the body scopes are being built right now,
			// and a populated self-entry would let traversals descend from a
			// body back into itself.
			own = make([]CTEDef, 0, len(defs)+1)
			own = append(own, defs...)
			own = append(own, CTEDef{Name: def.Name, Node: def.Node, Recursive: true})
		}
		visible := combineCTEDefs(own, inherited)
		for j := 0; j < nqCtx.GetChildCount(); j++ {
			if qCtx, ok := nqCtx.GetChild(j).(*grammar1.QueryContext); ok {
				def.Scopes = buildQueryScopes(qCtx, parent, visible, defaultDB)
				break
			}
		}
		defs = append(defs, def)
	}
	return
}

// withClauseIsRecursive reports whether a WITH container (the query-level
// ctes rule or the selectStmt-level withClause rule) carries the RECURSIVE
// modifier. The modifier applies to the whole clause.
func withClauseIsRecursive(withContainer antlr.ParserRuleContext) bool {
	switch c := withContainer.(type) {
	case *grammar1.CtesContext:
		return c.RECURSIVE() != nil
	case *grammar1.WithClauseContext:
		return c.RECURSIVE() != nil
	}
	return false
}
