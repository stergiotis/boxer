package constructsql

// componentexpand.go is the component authoring family of ADR-0189 §SD3:
// LW_COMPONENT and LW_COMPONENT_FILTER as a client-side nanopass expansion
// over the ADR-0066 artefacts a component definition generates.
//
// The family is table-bound, unlike the rest of the LW_ surface, which is
// section-bound: a component names a kind, and the kind names the table its
// artefacts read (ADR-0189 §SD6).

import (
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// ComponentPassName names the expansion pass in the registry and in errors.
const ComponentPassName = "LwComponentExpand"

// The component family's SQL-visible names.
const (
	NameComponent       = "LW_COMPONENT"
	NameComponentFilter = "LW_COMPONENT_FILTER"
)

// componentArities is the family, for the walk's dispatch. Both take exactly
// one argument: the component kind.
var componentArities = map[string]int{
	nanopass.NormalizeCallName(NameComponent):       1,
	nanopass.NormalizeCallName(NameComponentFilter): 1,
}

// ComponentSourceI resolves a component kind to its artefacts.
// [github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql.Registry]
// satisfies it; the pass takes the interface so a host can scope a registry to
// one query without building a second global.
type ComponentSourceI interface {
	Lookup(kind string) (b componentsql.Binding, ok bool)
	Kinds() (kinds []string)
}

// HasComponentMarker reports whether sql could contain a component call. Both
// names share the LW_COMPONENT prefix, so one check covers the family.
func HasComponentMarker(sql string) bool {
	return strings.Contains(strings.ToUpper(sql), NameComponent)
}

// ComponentExpandPass is LwComponentExpand bound to a component source.
//
// Idempotent by construction: expansion leaves no LW_COMPONENT call behind,
// and an unresolvable one is an error rather than a partial rewrite.
//
// defaultDatabase resolves an unqualified table reference in the statement's
// FROM against the kind's bound table, the same way the extraction pass and
// the selection-conditions pass take one.
func ComponentExpandPass(comps ComponentSourceI, defaultDatabase string) nanopass.Pass {
	return nanopass.LiftBodyPass(ComponentPassName, func(sql string) (string, error) {
		return expandComponents(sql, comps, defaultDatabase)
	}, nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})
}

func expandComponents(sql string, comps ComponentSourceI, defaultDatabase string) (result string, err error) {
	result = sql
	if comps == nil || !HasComponentMarker(sql) {
		return
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", ComponentPassName, err)
		return
	}
	roots, err := nanopass.BuildScopes(pr, defaultDatabase)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", ComponentPassName, err)
		return
	}
	st := &componentState{
		pr:              pr,
		rw:              nanopass.NewRewriter(pr),
		comps:           comps,
		defaultDatabase: defaultDatabase,
		byNode:          indexScopes(roots),
		needs:           make(map[*nanopass.SelectScope]*scopeNeeds),
	}
	err = st.walk(pr.Tree)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", ComponentPassName, err)
		return
	}
	st.injectFilters()
	result = nanopass.GetText(st.rw)
	return
}

// scopeNeeds tracks, for one SELECT, which kinds were projected and which
// already carry an author-written filter in that SELECT's WHERE.
type scopeNeeds struct {
	projected map[string]componentsql.Binding
	filtered  map[string]bool
}

type componentState struct {
	pr              *nanopass.ParseResult
	rw              nanopass.RewriterI
	comps           ComponentSourceI
	defaultDatabase string
	byNode          map[*grammar1.SelectStmtContext]*nanopass.SelectScope

	needs map[*nanopass.SelectScope]*scopeNeeds
	// order records scopes as they are first seen, so injection is
	// deterministic rather than map-ordered.
	order []*nanopass.SelectScope
}

func (inst *componentState) walk(node antlr.Tree) (err error) {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	if funcExpr, isFn := ctx.(*grammar1.ColumnExprFunctionContext); isFn {
		if ident := funcExpr.Identifier(); ident != nil {
			name := nanopass.NormalizeCallName(ident.GetText())
			if _, isComponent := componentArities[name]; isComponent {
				err = inst.expandCall(name, ident.GetText(), funcExpr)
				// The subtree is consumed either way: the sole argument is a
				// literal, so nothing below can match, and descending into a
				// replaced node would stack a second rewrite on the same tokens.
				return
			}
		}
	}
	for i := 0; i < ctx.GetChildCount(); i++ {
		err = inst.walk(ctx.GetChild(i))
		if err != nil {
			return
		}
	}
	return
}

func (inst *componentState) expandCall(name string, spelled string, funcExpr *grammar1.ColumnExprFunctionContext) (err error) {
	args, err := callArgs(funcExpr)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	if len(args) != 1 {
		err = inst.errCall(spelled, funcExpr).Int("args", len(args)).
			Errorf("a component call takes exactly one argument: the component kind")
		return
	}
	kind, numeric, err := membershipArg(args[0])
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	if numeric || kind == "" {
		err = inst.errCall(spelled, funcExpr).
			Errorf("a component kind is a quoted name, not a number")
		return
	}

	b, found := inst.comps.Lookup(kind)
	if !found {
		// The alternatives go in the message, not only in a structured field:
		// a typo and an unwired store produce the same failure, and the list
		// is what tells them apart at a glance.
		err = inst.errCall(spelled, funcExpr).Str("kind", kind).
			Errorf("no registered store publishes this component kind; registered: %s",
				strings.Join(inst.comps.Kinds(), ", "))
		return
	}

	scope := inst.scopeOf(funcExpr)
	if scope == nil {
		err = inst.errCall(spelled, funcExpr).Str("kind", kind).
			Errorf("a component call must sit inside a SELECT; there is no FROM to bind its columns to")
		return
	}
	err = inst.checkBinding(scope, b, spelled, funcExpr)
	if err != nil {
		return
	}

	switch name {
	case nanopass.NormalizeCallName(NameComponent):
		nanopass.ReplaceNode(inst.rw, funcExpr, b.Projection)
		inst.needFor(scope).projected[kind] = b
	case nanopass.NormalizeCallName(NameComponentFilter):
		nanopass.ReplaceNode(inst.rw, funcExpr, b.Filter)
		// Only a filter that actually restricts the row set discharges the
		// injection. The same call in a projection list computes a boolean per
		// row and filters nothing, so treating it as satisfying the kind would
		// hand back first-match semantics — the exact trap SD4 exists to close.
		if inWhereClause(funcExpr) {
			inst.needFor(scope).filtered[kind] = true
		}
	}
	return
}

// checkBinding enforces ADR-0189 §SD5/§SD6: the artefacts carry unqualified
// column names, so the scope must name the kind's table and nothing else.
func (inst *componentState) checkBinding(scope *nanopass.SelectScope, b componentsql.Binding, spelled string, funcExpr *grammar1.ColumnExprFunctionContext) (err error) {
	db, table := splitQualifiedTable(b.Table)

	if len(scope.Tables) != 1 {
		// A join is refused rather than emitted with bare column names that
		// would bind by whatever the server resolves them to. Lifting this
		// needs the artefacts emitted qualifiable, which is a change to
		// ADR-0066's output shape (ADR-0189 §SD6).
		err = inst.errCall(spelled, funcExpr).Str("kind", b.Kind).Str("wants", b.Table).
			Int("tablesInScope", len(scope.Tables)).
			Errorf("a component reads unqualified columns, so its SELECT must name exactly one table")
		return
	}
	ts := scope.Tables[0]
	if ts.IsCTE || ts.IsSubquery || ts.IsFunction {
		err = inst.errCall(spelled, funcExpr).Str("kind", b.Kind).Str("wants", b.Table).
			Str("found", ts.Table).
			Errorf("a component binds a stored table; this SELECT reads a CTE, subquery or table function")
		return
	}
	tsDB := ts.Database
	if tsDB == "" {
		tsDB = inst.defaultDatabase
	}
	if tsDB != db || ts.Table != table {
		err = inst.errCall(spelled, funcExpr).Str("kind", b.Kind).Str("wants", b.Table).
			Str("found", qualifyTable(tsDB, ts.Table)).
			Errorf("this SELECT does not read the table the component is stored in")
		return
	}
	return
}

func (inst *componentState) needFor(scope *nanopass.SelectScope) (n *scopeNeeds) {
	n, ok := inst.needs[scope]
	if !ok {
		n = &scopeNeeds{projected: make(map[string]componentsql.Binding), filtered: make(map[string]bool)}
		inst.needs[scope] = n
		inst.order = append(inst.order, scope)
	}
	return
}

// injectFilters is ADR-0189 §SD4: every projected kind's Filter is ANDed into
// its scope's WHERE, once, so a projection cannot travel without the check
// that makes it exact.
func (inst *componentState) injectFilters() {
	for _, scope := range inst.order {
		n := inst.needs[scope]
		kinds := make([]string, 0, len(n.projected))
		for kind := range n.projected {
			if !n.filtered[kind] {
				kinds = append(kinds, kind)
			}
		}
		if len(kinds) == 0 {
			continue
		}
		// Sorted so the emitted conjunction is stable: two kinds in one scope
		// must not swap places between runs.
		sort.Strings(kinds)
		terms := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			terms = append(terms, n.projected[kind].Filter)
		}
		inst.injectInto(scope, strings.Join(terms, " AND "))
	}
}

func (inst *componentState) injectInto(scope *nanopass.SelectScope, conjunction string) {
	if w, ok := scope.Node.WhereClause().(*grammar1.WhereClauseContext); ok && w != nil {
		if expr, isCtx := w.ColumnExpr().(antlr.ParserRuleContext); isCtx {
			// Wrapped by insertion rather than replaced: the existing
			// predicate may itself contain an already-rewritten component
			// call, and replacing the node would discard that rewrite.
			// Parentheses are what keep `a OR b` from becoming
			// `a OR (b AND filter)`.
			nanopass.InsertBefore(inst.rw, expr, "(")
			nanopass.InsertAfter(inst.rw, expr, ") AND "+conjunction)
			return
		}
	}
	if anchor := whereAnchor(scope.Node); anchor != nil {
		nanopass.InsertAfter(inst.rw, anchor, " WHERE "+conjunction)
	}
}

// whereAnchor is the clause a synthesised WHERE follows: the last one that
// must precede it. PREWHERE sits closest, then ARRAY JOIN, then FROM.
func whereAnchor(sel *grammar1.SelectStmtContext) (anchor antlr.ParserRuleContext) {
	if c, ok := sel.PrewhereClause().(antlr.ParserRuleContext); ok && c != nil {
		return c
	}
	if c, ok := sel.ArrayJoinClause().(antlr.ParserRuleContext); ok && c != nil {
		return c
	}
	if c, ok := sel.FromClause().(antlr.ParserRuleContext); ok && c != nil {
		return c
	}
	return nil
}

// inWhereClause reports whether node sits under its SELECT's WHERE, rather
// than anywhere else in the statement.
func inWhereClause(node antlr.ParserRuleContext) bool {
	for p := node.GetParent(); p != nil; p = p.GetParent() {
		if _, isWhere := p.(*grammar1.WhereClauseContext); isWhere {
			return true
		}
		if _, isSelect := p.(*grammar1.SelectStmtContext); isSelect {
			return false
		}
	}
	return false
}

func (inst *componentState) scopeOf(node antlr.ParserRuleContext) *nanopass.SelectScope {
	for p := node.GetParent(); p != nil; p = p.GetParent() {
		if sel, ok := p.(*grammar1.SelectStmtContext); ok {
			if scope, found := inst.byNode[sel]; found {
				return scope
			}
		}
	}
	return nil
}

func (inst *componentState) errCall(spelled string, funcExpr *grammar1.ColumnExprFunctionContext) *eb.ErrorBuilder {
	r := inst.pr.SourceRangeOf(funcExpr)
	return eb.Build().Str("call", spelled).Int("start", r.Start).Int("end", r.End)
}

func splitQualifiedTable(qualified string) (db string, table string) {
	db, table, found := strings.Cut(qualified, ".")
	if !found {
		table = db
		db = ""
	}
	return
}

func qualifyTable(db string, table string) string {
	if db == "" {
		return table
	}
	return db + "." + table
}

// ComponentFunctions is the component family, for the vocabulary panel.
//
// The two names differ in what they need installed, which is the distinction
// ADR-0189 §SD8 asks the panel to show: LW_COMPONENT_FILTER expands to
// ClickHouse built-ins and runs anywhere, while LW_COMPONENT expands to the
// named-tuple projection, which calls the read-back helper family.
func ComponentFunctions() (fns []Function) {
	fns = []Function{
		{
			Name:   NameComponent,
			Params: []string{"'Kind'"},
			Doc:    "read a whole component as a named tuple; its conformance filter is added to the statement's WHERE (ADR-0189)",
		},
		{
			Name:   NameComponentFilter,
			Params: []string{"'Kind'"},
			Doc:    "the predicate identifying rows carrying a conforming component; ClickHouse built-ins only, so it needs nothing installed (ADR-0189)",
		},
	}
	return
}

// ComponentExpansionDependencies are the server-side functions an expanded
// LW_COMPONENT may call. LW_COMPONENT_FILTER calls none of them.
func ComponentExpansionDependencies() (names []string) {
	names = []string{
		"LW_VALUE_BY_TAG_EQUAL",
		"LW_LIST_BY_TAG_EQUAL",
		"LW_RAGGED_PARENT_IDS",
	}
	return
}
