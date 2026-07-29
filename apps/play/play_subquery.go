package play

// Subquery-under-caret: resolving the caret to the innermost runnable query of
// a statement, working out what that query closes over, and composing a buffer
// that runs it on its own.
//
// The statement split in play_statements.go is at the LEX tier and stops at
// the statement boundary. Going inside one needs the CST. grammar1's runnable
// unit is `selectUnionStmt` — one SELECT plus whatever UNION / EXCEPT /
// INTERSECT chain it heads — and it is the node every nesting site wraps,
// whether the nesting is a FROM source, an expression subquery, or a CTE body.
// So "the query under the caret" is the innermost selectUnionStmt containing
// it, and the statement's own query is the outermost one.
//
// Unlike the lex split this needs a parse that SUCCEEDED, so it can only ever
// add to what Run already does: no units means no narrowing, and the ordinary
// Run buffer ships.
//
// A nested query rarely stands alone, so the model separates two things it
// closes over:
//
//   - What CAN be carried: every WITH item in scope — the enclosing selects'
//     `withClause`, the enclosing queries' `ctes`, outermost first. compose
//     re-emits these in front of the unit.
//   - What CANNOT: a reference qualified by a table alias that belongs to a
//     query further out. That is what makes a subquery *correlated*, no editor
//     can make one independently runnable, and left unmarked it becomes an
//     unknown-identifier error at the server. Unresolved records their spans so
//     the editor can say so first.

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// subqueryUnit is one runnable query of a statement, in that statement's own
// byte coordinates.
type subqueryUnit struct {
	// Src is the selectUnionStmt's extent. For a nested unit that is the text
	// BETWEEN the parentheses wrapping it, not including them; for a unit whose
	// enclosing query carries a `ctes` clause it excludes that clause, which is
	// why the clause has to be hoisted back in.
	Src nanopass.SourceRange
	// Root marks the statement's own top-level query. Resolving to it is the
	// "nothing to narrow to" answer: it is what Run ships anyway.
	Root bool
	// WithItems are the WITH items in scope at this unit, outermost first,
	// deduplicated by CTE name. These are what compose carries along, and what
	// the editor tints as the carried environment.
	WithItems []nanopass.SourceRange
	// Unresolved are references the unit inherits but cannot carry: qualified
	// by a name bound only by an enclosing query. Empty for a unit that stands
	// alone once its WITH items travel with it.
	Unresolved []nanopass.SourceRange

	// depth is the nesting depth of the unit, the tie-break for innermost.
	depth int
	// recursive is set when any contributing WITH clause — hoisted, or the
	// unit's own — carried RECURSIVE.
	recursive bool
	// bodyAt is where the shipped text starts. It is Src.Start unless the unit
	// heads a WITH clause of its own, in which case it is that clause's first
	// item: the `WITH [RECURSIVE]` keywords are re-emitted in front of the
	// hoisted items so the two lists become one, which is the only way to have
	// both — SQL permits a single WITH per query level.
	bodyAt int
}

// compose renders the unit as standalone SQL, given the statement text its
// offsets index.
func (inst subqueryUnit) compose(text string) string {
	if len(inst.WithItems) == 0 {
		// Nothing in scope: the unit's own text already stands alone, own WITH
		// clause included.
		return text[inst.Src.Start:inst.Src.End]
	}
	items := make([]string, 0, len(inst.WithItems))
	for _, r := range inst.WithItems {
		items = append(items, text[r.Start:r.End])
	}
	head := "WITH "
	if inst.recursive {
		head = "WITH RECURSIVE "
	}
	head += strings.Join(items, ", ")
	if inst.bodyAt != inst.Src.Start {
		// The unit heads its own WITH clause; its items continue the list the
		// hoisted ones opened. bodyAt is that clause's first item, so the
		// original `WITH [RECURSIVE]` keywords are dropped with the prefix.
		return head + ", " + text[inst.bodyAt:inst.Src.End]
	}
	return head + " " + text[inst.Src.Start:inst.Src.End]
}

// scopeFrame is one enclosing scope on the path from the statement root down to
// a unit: the WITH items it defines, and the table names or aliases its
// FROM / JOIN clause binds.
//
// One frame per node that opens either. A `ctes` clause of a query defines
// items and binds nothing; a select defines items only if it has a
// `withClause`, and binds whatever it selects from.
type scopeFrame struct {
	items     []nanopass.SourceRange
	names     []string // decoded CTE name per item; "" for a scalar `expr AS x` item
	recursive bool
	binds     []string // decoded table names / aliases in scope from this select
}

// scopeIndex maps each select of a parse to the canonical scope built for it.
//
// The FROM / JOIN extraction is nanopass's (BuildScopes), not a second reading
// of that grammar here: alias-hides-table, table functions, subquery sources
// and CTE references are all decided there, and two readings of one grammar
// drift apart silently.
type scopeIndex map[*grammar1.SelectStmtContext]*nanopass.SelectScope

// bindsOf returns the names a select's own FROM / JOIN clause binds — the
// alias where there is one, the table name otherwise, matching
// SelectScope.ResolveAlias.
func (inst scopeIndex) bindsOf(node *grammar1.SelectStmtContext) (binds []string) {
	scope := inst[node]
	if scope == nil {
		return nil
	}
	for _, ts := range scope.Tables {
		if ts.Alias != "" {
			binds = append(binds, ts.Alias)
			continue
		}
		if ts.Table != "" {
			binds = append(binds, ts.Table)
		}
	}
	return binds
}

// withItemsOf reads a clause's items into a frame. Both grammar1 clause rules
// (`ctes`, `withClause`) expose the same two accessors, so the caller passes
// them rather than the node.
func withItemsOf(pr *nanopass.ParseResult, items []grammar1.IWithItemContext, recursive bool) (frame scopeFrame) {
	frame.recursive = recursive
	for _, it := range items {
		r := pr.SourceRangeOf(it)
		if r.Empty() {
			continue
		}
		frame.items = append(frame.items, r)
		frame.names = append(frame.names, cteNameOf(it))
	}
	return frame
}

// cteNameOf returns the decoded name a WITH item binds, or "" when the item is
// the scalar `expr AS alias` form. Only named queries are deduplicated across
// scopes — a repeated CTE name is what ClickHouse rejects outright, and a
// repeated scalar alias is rare enough to leave to the server.
func cteNameOf(item grammar1.IWithItemContext) string {
	named, ok := item.(*grammar1.WithItemNamedQueryContext)
	if !ok {
		return ""
	}
	q := named.NamedQuery()
	if q == nil {
		return ""
	}
	name := q.GetName()
	if name == nil {
		return ""
	}
	return nanopass.DecodeIdentifier(name.GetText())
}

// parseSubqueryUnits splits a single statement into its runnable queries.
// Returns nil when the statement does not parse — the caller then has nothing
// to narrow to, which is the same answer as "the caret is at statement level".
func parseSubqueryUnits(text string) (units []subqueryUnit) {
	pr, err := nanopass.Parse(text)
	if err != nil || pr == nil {
		return nil
	}
	scopes := scopeIndex{}
	// A scope build that fails costs only the correlation marks — the units,
	// their hoists and their composition do not depend on it, so a statement
	// shaped unexpectedly still narrows.
	if roots, serr := nanopass.BuildScopes(pr, ""); serr == nil {
		for _, s := range nanopass.FlattenScopes(roots) {
			if s.Node != nil {
				scopes[s.Node] = s
			}
		}
	}
	collectSubqueries(pr, pr.Tree, nil, 0, scopes, &units)
	return units
}

// collectSubqueries walks the CST, carrying the scopes open at each node. The
// chain is extended for a subtree rather than recovered by walking parents back
// up, so each unit's environment is a copy of what was already in hand.
func collectSubqueries(pr *nanopass.ParseResult, node antlr.Tree, chain []scopeFrame, depth int, scopes scopeIndex, out *[]subqueryUnit) {
	switch n := node.(type) {
	case *grammar1.QueryContext:
		// `query: setStmt* ctes? selectUnionStmt` — the clause sits ABOVE the
		// selectUnionStmt, so even the statement's own top-level CTEs are a
		// hoisted scope rather than part of any unit's text.
		if ct := n.Ctes(); ct != nil {
			chain = extendChain(chain, withItemsOf(pr, ct.AllWithItem(), ct.RECURSIVE() != nil))
		}
	case *grammar1.SelectStmtContext:
		// A select's own `withClause` and FROM sources are visible to the
		// subqueries inside it, but not to the selectUnionStmt that heads it —
		// that unit carries the clause in its own text (see bodyAt) and its own
		// sources with it.
		frame := scopeFrame{binds: scopes.bindsOf(n)}
		if wc := n.WithClause(); wc != nil {
			items := withItemsOf(pr, wc.AllWithItem(), wc.RECURSIVE() != nil)
			frame.items, frame.names, frame.recursive = items.items, items.names, items.recursive
		}
		if len(frame.items) > 0 || len(frame.binds) > 0 {
			chain = extendChain(chain, frame)
		}
	case *grammar1.SelectUnionStmtContext:
		depth++
		if u, ok := unitFor(pr, n, chain, depth, scopes); ok {
			*out = append(*out, u)
		}
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if child := node.GetChild(i); child != nil {
			collectSubqueries(pr, child, chain, depth, scopes, out)
		}
	}
}

// extendChain appends a frame without letting sibling subtrees share — and then
// overwrite — the same backing array. The full slice expression forces the copy.
func extendChain(chain []scopeFrame, frame scopeFrame) []scopeFrame {
	return append(chain[:len(chain):len(chain)], frame)
}

// unitFor builds the unit for one selectUnionStmt node from the scopes open
// above it.
func unitFor(pr *nanopass.ParseResult, node *grammar1.SelectUnionStmtContext, chain []scopeFrame, depth int, scopes scopeIndex) (unit subqueryUnit, ok bool) {
	src := pr.SourceRangeOf(node)
	if src.Empty() {
		return unit, false
	}
	unit = subqueryUnit{Src: src, Root: depth == 1, depth: depth, bodyAt: src.Start}
	// The unit's own WITH clause, when its first branch is a bare select. Its
	// items stay in the shipped text; what is needed here is where they start,
	// so the hoisted items can be spliced in front of them.
	ownNames := map[string]struct{}{}
	if wc := ownWithClause(node); wc != nil {
		if items := wc.AllWithItem(); len(items) > 0 {
			if first := pr.SourceRangeOf(items[0]); !first.Empty() {
				unit.bodyAt = first.Start
				unit.recursive = wc.RECURSIVE() != nil
				for _, it := range items {
					if n := cteNameOf(it); n != "" {
						ownNames[n] = struct{}{}
					}
				}
			}
		}
	}
	// Hoist, outermost scope first. Within a scope only the items BEFORE the
	// one containing this unit are in scope: SQL binds a WITH list left to
	// right, so a later sibling cannot be referenced, and the item we are
	// inside would be defined in terms of the very text about to ship.
	//
	// The same walk collects what the enclosing scopes BIND, which is what a
	// correlated reference below resolves against.
	carried := map[string]struct{}{}
	outer := map[string]struct{}{}
	at := map[string]int{}
	for _, frame := range chain {
		for _, b := range frame.binds {
			outer[b] = struct{}{}
		}
		for i, r := range frame.items {
			if r.Start <= src.Start && src.End <= r.End {
				break
			}
			name := frame.names[i]
			if _, own := ownNames[name]; own {
				// The unit rebinds this name itself; hoisting it too would be
				// the duplicate-name error ClickHouse rejects the query for.
				continue
			}
			if name != "" {
				carried[name] = struct{}{}
				if prev, seen := at[name]; seen {
					// An inner scope rebinding an outer name: the inner
					// definition is the one in scope here, but it keeps the
					// outer's position, since items already emitted may refer
					// to the name.
					unit.WithItems[prev] = r
					continue
				}
				at[name] = len(unit.WithItems)
			}
			unit.WithItems = append(unit.WithItems, r)
		}
		unit.recursive = unit.recursive || frame.recursive
	}
	for n := range ownNames {
		carried[n] = struct{}{}
	}
	unit.Unresolved = unresolvedRefs(pr, node, outer, carried, scopes)
	return unit, true
}

// unresolvedRefs finds the references inside a unit that only an enclosing
// query can satisfy — the correlation the composition cannot repair.
//
// A reference qualifies only when its qualifier is bound OUTSIDE the unit and
// bound nowhere inside it, nor by a WITH item travelling with it. Requiring the
// outward binding is what keeps this quiet on the shapes it would otherwise
// misread: grammar1 parses `tup.field` on a Tuple column as a table-qualified
// reference too, and such a qualifier resolves nowhere, so it is not reported.
func unresolvedRefs(pr *nanopass.ParseResult, node *grammar1.SelectUnionStmtContext, outer, carried map[string]struct{}, scopes scopeIndex) (out []nanopass.SourceRange) {
	if len(outer) == 0 {
		return nil
	}
	// Everything the unit binds for itself, at any depth inside it: its own
	// FROM sources, those of its nested queries, and any CTE it defines.
	inner := map[string]struct{}{}
	nanopass.WalkCST(node, func(ctx antlr.ParserRuleContext) bool {
		switch n := ctx.(type) {
		case *grammar1.SelectStmtContext:
			for _, b := range scopes.bindsOf(n) {
				inner[b] = struct{}{}
			}
		case *grammar1.WithItemNamedQueryContext:
			if name := cteNameOf(n); name != "" {
				inner[name] = struct{}{}
			}
		}
		return true
	})
	nanopass.WalkCST(node, func(ctx antlr.ParserRuleContext) bool {
		col, isCol := ctx.(*grammar1.ColumnIdentifierContext)
		if !isCol {
			return true
		}
		tbl := col.TableIdentifier()
		if tbl == nil {
			return true
		}
		name := tbl.Identifier()
		if name == nil {
			return true
		}
		q := nanopass.DecodeIdentifier(name.GetText())
		if _, isOuter := outer[q]; !isOuter {
			return true
		}
		if _, isInner := inner[q]; isInner {
			return true
		}
		if _, isCarried := carried[q]; isCarried {
			return true
		}
		if r := pr.SourceRangeOf(tbl); !r.Empty() {
			out = append(out, r)
		}
		return true
	})
	return out
}

// ownWithClause returns the WITH clause the unit itself heads, or nil. Only a
// bare `selectStmt` first branch can carry one; a parenthesised branch nests
// another unit, which owns its clause.
func ownWithClause(node *grammar1.SelectUnionStmtContext) grammar1.IWithClauseContext {
	parens := node.SelectStmtWithParens()
	if parens == nil {
		return nil
	}
	ctx, ok := parens.(*grammar1.SelectStmtWithParensContext)
	if !ok {
		return nil
	}
	stmt := ctx.SelectStmt()
	if stmt == nil {
		return nil
	}
	sel, ok := stmt.(*grammar1.SelectStmtContext)
	if !ok {
		return nil
	}
	return sel.WithClause()
}

// pickSubquery resolves a caret against an already-split statement: the
// innermost unit containing it, deepest first on a tie.
//
// Containment is inclusive at both ends. A unit's range stops at its last
// token, so a caret resting just past it — the position you are at having
// finished typing the subquery — still belongs to it, and a caret in the
// whitespace or comments between two units belongs to whichever encloses both.
// A caret outside every unit (in the statement's leading `ctes` clause, say)
// falls back to the root, which is the "run the statement" answer.
func pickSubquery(units []subqueryUnit, caret int) (unit subqueryUnit, ok bool) {
	for _, u := range units {
		if u.Src.Start > caret || caret > u.Src.End {
			continue
		}
		if !ok || u.depth > unit.depth {
			unit, ok = u, true
		}
	}
	if ok {
		return unit, true
	}
	for _, u := range units {
		if u.Root {
			return u, true
		}
	}
	return unit, false
}
