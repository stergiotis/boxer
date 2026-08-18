package play

// Subquery-under-caret: resolving the caret to the innermost runnable query of
// a statement, working out what that query closes over, and composing a buffer
// that runs it on its own.
//
// The statement split in play_statements.go is at the LEX tier and stops at
// the statement boundary. Going inside one needs the CST. The primary
// runnable unit is grammar1's `selectUnionStmt` — one SELECT plus whatever
// UNION / EXCEPT / INTERSECT chain it heads — the node every nesting site
// wraps, whether the nesting is a FROM source, an expression subquery, or a
// CTE body. Each bare branch of a multi-branch chain is additionally a unit
// of its own: a branch is independently runnable SQL, and its parenthesised
// spelling already narrowed, so the bare spelling narrowing too is the only
// consistent reading of "the query under the caret". That phrase resolves to
// the innermost unit containing the caret; the statement's own query is the
// outermost chain.
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
//   - What CANNOT: a reference only an enclosing query can satisfy. Two kinds:
//     a reference qualified by a table alias bound further out (the correlated
//     subquery), and a reference to the WITH definition the unit itself lives
//     inside (a recursive CTE body naming itself) — carrying that one would
//     define it in terms of the very text being shipped. No editor can make
//     either independently runnable, and left unmarked each becomes an
//     unknown-name error at the server. Unresolved records their spans so the
//     editor can say so first.

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// subqueryUnit is one runnable query of a statement, in that statement's own
// byte coordinates.
type subqueryUnit struct {
	// Src is the unit's extent — a selectUnionStmt's, or a bare union
	// branch's selectStmt's. For a nested chain that is the text BETWEEN the
	// parentheses wrapping it, not including them; for a unit whose
	// enclosing query carries a `ctes` clause it excludes that clause, which
	// is why the clause has to be hoisted back in.
	Src nanopass.SourceRange
	// Root marks the statement's own top-level query. Resolving to it is the
	// "nothing to narrow to" answer: it is what Run ships anyway.
	Root bool
	// WithItems are the WITH items in scope at this unit, outermost first,
	// deduplicated by CTE name. These are what compose carries along, and what
	// the editor tints as the carried environment.
	WithItems []nanopass.SourceRange
	// Unresolved are references the unit cannot take along: a qualifier bound
	// only by an enclosing query's FROM clause, or a table reference to the
	// WITH definition the unit itself lives inside. Empty for a unit that
	// stands alone once its WITH items travel with it.
	Unresolved []nanopass.SourceRange

	// depth is the nesting depth of the unit, the tie-break for innermost.
	// Chains count in twos so their bare branches order between the chain
	// and anything nested within a branch — see collectSubqueries.
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
	items []nanopass.SourceRange
	// keys deduplicate rebindings across scopes, one per item: the decoded
	// name, kind-prefixed ("q:" named query, "s:" scalar alias), or "" for an
	// item binding no name. The kinds are separate namespaces deliberately —
	// ClickHouse holds them apart (a named query and a scalar alias sharing
	// one name coexist, each answering in its own positions), so only a
	// same-kind rebinding may collapse to one item.
	keys      []string
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
		frame.keys = append(frame.keys, withItemKey(it))
	}
	return frame
}

// withItemKey returns the deduplication key a WITH item binds: its decoded
// name behind a kind prefix, or "" for an item binding no name. Rebinding
// either kind across scopes flattens to a duplicate the server rejects
// outright (a repeated CTE name, or MULTIPLE_EXPRESSIONS_FOR_ALIAS for a
// repeated scalar alias), so both take the same inner-wins deduplication.
func withItemKey(item grammar1.IWithItemContext) string {
	if name := cteNameOf(item); name != "" {
		return "q:" + name
	}
	if alias := scalarAliasOf(item); alias != "" {
		return "s:" + alias
	}
	return ""
}

// cteNameOf returns the decoded name a WITH item binds as a named query, or
// "" for the scalar `expr AS alias` form. Beyond keying, named queries are
// the only items a column reference can use as a table qualifier, which is
// why unresolvedRefs consults these names and not the scalar aliases.
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

// scalarAliasOf returns the decoded alias of a scalar `expr AS alias` WITH
// item, or "" for the named-query form and for shapes carrying no alias.
func scalarAliasOf(item grammar1.IWithItemContext) string {
	cols, ok := item.(*grammar1.WithItemColumnsExprContext)
	if !ok {
		return ""
	}
	col, ok := cols.ColumnsExpr().(*grammar1.ColumnsExprColumnContext)
	if !ok {
		return ""
	}
	aliased, ok := col.ColumnExpr().(*grammar1.ColumnExprAliasContext)
	if !ok {
		return ""
	}
	id := aliased.Identifier()
	if id == nil {
		return ""
	}
	return nanopass.DecodeIdentifier(id.GetText())
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
	collectSubqueries(pr, pr.Tree, nil, 0, scopes, rootUnitNode(pr.Tree), &units)
	return units
}

// rootUnitNode returns the statement's own top-level query: the
// selectUnionStmt hanging directly off the outermost `query`.
//
// It has to be identified structurally rather than as "the first one the walk
// reaches" or "the one at nesting depth 1". `query: setStmt* ctes?
// selectUnionStmt` puts the CTE clause FIRST, and a top-level CTE body's own
// selectUnionStmt is reached without passing through any other — so it is
// visited earlier AND sits at the same depth. Taking either for the root made
// a caret in a top-level CTE body report "nothing to narrow to", which is
// precisely the case run-subquery exists for.
func rootUnitNode(tree antlr.ParserRuleContext) *grammar1.SelectUnionStmtContext {
	if tree == nil || tree.GetChildCount() == 0 {
		return nil
	}
	// The queryStmt → query shape nanopass.BuildScopes also asserts.
	query, ok := tree.GetChild(0).(*grammar1.QueryContext)
	if !ok {
		return nil
	}
	for i := 0; i < query.GetChildCount(); i++ {
		if u, isUnion := query.GetChild(i).(*grammar1.SelectUnionStmtContext); isUnion {
			return u
		}
	}
	return nil
}

// collectSubqueries walks the CST, carrying the scopes open at each node. The
// chain is extended for a subtree rather than recovered by walking parents back
// up, so each unit's environment is a copy of what was already in hand.
//
// Depth counts in steps of two so a chain's bare branches fit BETWEEN the
// chain and anything nested inside them: a chain sits at 2k, its branches at
// 2k+1, and a subquery within a branch opens the next chain at 2k+2 —
// pickSubquery's innermost-wins tie-break then orders all three correctly.
func collectSubqueries(pr *nanopass.ParseResult, node antlr.Tree, chain []scopeFrame, depth int, scopes scopeIndex, root *grammar1.SelectUnionStmtContext, out *[]subqueryUnit) {
	switch n := node.(type) {
	case *grammar1.SelectStmtContext:
		// A bare branch of a multi-branch chain is a runnable unit of its own:
		// the caret in `SELECT 1 UNION ALL SELECT |2` narrows to `SELECT 2`,
		// exactly as it already did for the parenthesised spelling of the same
		// branch. It never carries a WITH of its own — since ADR-0196 §SD5 only
		// the enclosing selectUnionStmt can open one, and that clause is
		// already in this unit's chain by the time the walk reaches here.
		//
		// The forward-only scoping this case used to implement (live-verified:
		// branch 2 saw branch 1's items, never the reverse) went with it: a
		// bare branch can no longer be written with its own WITH, so the case
		// it handled is unreachable. The parenthesised spelling nests a unit
		// that owns its clause, which is handled below.
		if chainNode, index := unionBranchOf(n); index >= 0 {
			// The server scopes an arm's WITH FORWARD only (live-verified: arm 2
			// sees arm 1's items, never the reverse), so the frames extend with
			// every earlier arm's clause. Arm 0's is the chain's own and the
			// SelectUnionStmtContext case below already put it on the chain, so
			// the loop starts at 1.
			//
			// Since ADR-0196 §SD5 an arm's clause sits outside its selectStmt —
			// on the chain, or on the selectUnionStmtItem — so it is hoisted
			// rather than shipped in the unit's own text, and the splice puts it
			// back in front. That is why ownWc is nil here.
			for i := 1; i <= index; i++ {
				ct := armCtes(chainNode, i)
				if ct == nil {
					continue
				}
				chain = extendChain(chain, withItemsOf(pr, ct.AllWithItem(), ct.RECURSIVE() != nil))
			}
			if u, ok := unitFor(pr, n, nil, chain, depth+1, scopes, false); ok {
				*out = append(*out, u)
			}
		}
		// A select's FROM sources are visible to the subqueries inside it.
		if binds := scopes.bindsOf(n); len(binds) > 0 {
			chain = extendChain(chain, scopeFrame{binds: binds})
		}
	case *grammar1.SelectUnionStmtContext:
		depth += 2
		// A chain's own WITH is its CLOSURE, not part of the runnable unit.
		// Subquery mode ships the unit's text and splices carried items in front
		// of it, and the whole UX rests on the clause sitting outside the query
		// it scopes: the main query gets the background tint precisely because
		// it is a proper subset of the statement.
		//
		// The grammar used to say that directly — `ctes` hung off `query`, above
		// the selectUnionStmt — and ADR-0196 §SD5 moved it inside. So the clause
		// is hoisted onto the chain here, and the unit's range is pulled in to
		// start at the first arm, which reproduces the old shape exactly.
		if own := n.Ctes(); own != nil {
			chain = extendChain(chain, withItemsOf(pr, own.AllWithItem(), own.RECURSIVE() != nil))
		}
		if u, ok := unitFor(pr, n, nil, chain, depth, scopes, n == root); ok {
			if swp := n.SelectStmtWithParens(); swp != nil {
				if r := pr.SourceRangeOf(swp); !r.Empty() && r.Start > u.Src.Start {
					u.Src.Start = r.Start
					u.bodyAt = r.Start
				}
			}
			*out = append(*out, u)
		}
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if child := node.GetChild(i); child != nil {
			collectSubqueries(pr, child, chain, depth, scopes, root, out)
		}
	}
}

// unionBranchOf reports whether sel is a bare branch of a chain with MORE
// than one branch, returning the chain and sel's branch position. A single
// branch is coextensive with its chain — the chain's unit already covers it —
// and a parenthesised branch nests a chain of its own, so both answer -1.
func unionBranchOf(sel *grammar1.SelectStmtContext) (chain *grammar1.SelectUnionStmtContext, index int) {
	index = -1
	parens, isParens := sel.GetParent().(*grammar1.SelectStmtWithParensContext)
	if !isParens {
		return nil, -1
	}
	switch p := parens.GetParent().(type) {
	case *grammar1.SelectUnionStmtContext:
		chain = p
		index = 0
	case *grammar1.SelectUnionStmtItemContext:
		c2, isChain := p.GetParent().(*grammar1.SelectUnionStmtContext)
		if !isChain {
			return nil, -1
		}
		chain = c2
		for i, item := range c2.AllSelectUnionStmtItem() {
			if item == antlr.Tree(p) {
				index = i + 1
				break
			}
		}
	default:
		return nil, -1
	}
	if chain == nil || index < 0 || len(chain.AllSelectUnionStmtItem()) == 0 {
		return nil, -1
	}
	return chain, index
}

// armCtes returns the WITH clause that arm `index` of a chain opens, or nil.
// The head arm's sits on the chain itself; a later arm's on its
// selectUnionStmtItem — ADR-0196 §SD5 routes both to the one `ctes` rule.
func armCtes(chain *grammar1.SelectUnionStmtContext, index int) grammar1.ICtesContext {
	if index <= 0 {
		return chain.Ctes()
	}
	items := chain.AllSelectUnionStmtItem()
	if index-1 >= len(items) {
		return nil
	}
	return items[index-1].Ctes()
}

// extendChain appends a frame without letting sibling subtrees share — and then
// overwrite — the same backing array. The full slice expression forces the copy.
func extendChain(chain []scopeFrame, frame scopeFrame) []scopeFrame {
	return append(chain[:len(chain):len(chain)], frame)
}

// unitFor builds one unit — a whole selectUnionStmt chain, or a bare branch
// of one — from the scopes open above it. ownWc is the WITH clause living
// inside the unit's own text (a chain's own ctes prefix), nil when there is
// none; its items stay in the shipped text and hoisted items are spliced in
// front of them.
func unitFor(pr *nanopass.ParseResult, node antlr.ParserRuleContext, ownWc grammar1.ICtesContext, chain []scopeFrame, depth int, scopes scopeIndex, isRoot bool) (unit subqueryUnit, ok bool) {
	src := pr.SourceRangeOf(node)
	if src.Empty() {
		return unit, false
	}
	unit = subqueryUnit{Src: src, Root: isRoot, depth: depth, bodyAt: src.Start}
	// qualifiers collects the named-query names that travel — own or hoisted
	// — which is what a correlated qualifier below must NOT be.
	ownKeys := map[string]struct{}{}
	qualifiers := map[string]struct{}{}
	if wc := ownWc; wc != nil {
		if items := wc.AllWithItem(); len(items) > 0 {
			if first := pr.SourceRangeOf(items[0]); !first.Empty() {
				unit.bodyAt = first.Start
				unit.recursive = wc.RECURSIVE() != nil
				for _, it := range items {
					if k := withItemKey(it); k != "" {
						ownKeys[k] = struct{}{}
					}
					if n := cteNameOf(it); n != "" {
						qualifiers[n] = struct{}{}
					}
				}
			}
		}
	}
	// Hoist, outermost scope first. Every item in scope travels except the one
	// the unit lives inside, which would be defined in terms of the very text
	// about to ship. Later siblings travel too: ClickHouse resolves the names
	// of one WITH level regardless of order — a body may reference a sibling
	// defined after it — so hoisting only the earlier ones composed
	// unknown-table errors for statements the server accepts (the live
	// differential test pins this). Items the unit never references cost
	// nothing: the server analyses only what the shipped query reaches.
	//
	// The same walk collects what the enclosing scopes BIND, which is what a
	// correlated reference below resolves against.
	outer := map[string]struct{}{}
	at := map[string]int{}
	for _, frame := range chain {
		for _, b := range frame.binds {
			outer[b] = struct{}{}
		}
		for i, r := range frame.items {
			if r.Start <= src.Start && src.End <= r.End {
				continue
			}
			if key := frame.keys[i]; key != "" {
				if _, own := ownKeys[key]; own {
					// The unit rebinds this name itself; hoisting it too would
					// be the duplicate-name error ClickHouse rejects the query
					// for, and the inner binding is the one in scope here.
					continue
				}
				if name, isQuery := strings.CutPrefix(key, "q:"); isQuery {
					qualifiers[name] = struct{}{}
				}
				if prev, seen := at[key]; seen {
					// An inner scope rebinding an outer name: the inner
					// definition is the one in scope here, but it keeps the
					// outer's position, since items already emitted may refer
					// to the name.
					unit.WithItems[prev] = r
					continue
				}
				at[key] = len(unit.WithItems)
			}
			unit.WithItems = append(unit.WithItems, r)
		}
		unit.recursive = unit.recursive || frame.recursive
	}
	unit.Unresolved = unresolvedRefs(pr, node, outer, qualifiers, scopes, src)
	unit.Unresolved = append(unit.Unresolved, selfRefs(pr, node, src, scopes)...)
	return unit, true
}

// unresolvedRefs finds the references inside a unit that only an enclosing
// query can satisfy — the correlation the composition cannot repair.
//
// A reference qualifies only when its qualifier is bound OUTSIDE the unit and
// resolves against nothing that ships with it: not the FROM/JOIN binds of a
// select enclosing the reference within the unit (boundAbove — the scopes SQL
// actually consults), not a CTE the unit defines, not a carried WITH item.
// "Enclosing" is the load-bearing word: a sibling or nested subquery's alias
// is invisible at the reference's own level, so a flat any-bind-inside-the-
// unit set suppressed genuinely correlated references — the composed run then
// failed at the endpoint with the exact unknown-identifier error this channel
// exists to pre-empt (found live: a nested FROM subquery rebinding the outer
// alias hid the unit-level correlation).
//
// Requiring the outward binding is what keeps this quiet on the shapes it
// would otherwise misread: grammar1 parses `tup.field` on a Tuple column as a
// table-qualified reference too, and such a qualifier resolves nowhere, so it
// is not reported.
func unresolvedRefs(pr *nanopass.ParseResult, node antlr.ParserRuleContext, outer, qualifiers map[string]struct{}, scopes scopeIndex, src nanopass.SourceRange) (out []nanopass.SourceRange) {
	if len(outer) == 0 {
		return nil
	}
	// CTE names the unit defines anywhere inside itself. A defined name is
	// never a correlation whatever level it is used from — the definition
	// ships with the unit's text.
	innerCtes := map[string]struct{}{}
	nanopass.WalkCST(node, func(ctx antlr.ParserRuleContext) bool {
		if n, isItem := ctx.(*grammar1.WithItemNamedQueryContext); isItem {
			if name := cteNameOf(n); name != "" {
				innerCtes[name] = struct{}{}
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
		if _, isCte := innerCtes[q]; isCte {
			return true
		}
		if _, isCarried := qualifiers[q]; isCarried {
			return true
		}
		if boundAbove(pr, col, q, scopes, src) {
			return true
		}
		if r := pr.SourceRangeOf(tbl); !r.Empty() {
			out = append(out, r)
		}
		return true
	})
	return out
}

// enclosingScope returns the scope of the select nearest above ref, nil when
// the nearest enclosing select has none (or there is no enclosing select).
func enclosingScope(ref antlr.ParserRuleContext, scopes scopeIndex) *nanopass.SelectScope {
	for p := ref.GetParent(); p != nil; p = p.GetParent() {
		if sel, isSel := p.(*grammar1.SelectStmtContext); isSel {
			return scopes[sel]
		}
	}
	return nil
}

// boundAbove reports whether a qualifier resolves against the FROM/JOIN binds
// of a select that both ENCLOSES the reference and lies WITHIN the unit — the
// scopes that still surround the reference when the unit ships alone. The walk
// follows nanopass's lexical Parent chain from the reference's nearest select
// and stops at the first scope outside the unit: anything bound beyond that
// boundary is exactly the correlation the caller reports.
//
// The chain crosses CTE-body and FROM-subquery boundaries as if their outer
// binds were visible, which SQL denies — but a reference relying on such a
// bind fails in the ORIGINAL statement too, so suppressing its mark promises
// nothing the server would have honoured anyway. The direction that matters —
// a nested scope's alias wrongly excusing a reference it does not enclose —
// cannot happen here, since the walk only ever ascends.
func boundAbove(pr *nanopass.ParseResult, ref antlr.ParserRuleContext, qualifier string, scopes scopeIndex, unit nanopass.SourceRange) bool {
	for scope := enclosingScope(ref, scopes); scope != nil; scope = scope.Parent {
		if scope.Node == nil {
			break
		}
		if nr := pr.SourceRangeOf(scope.Node); nr.Empty() || nr.Start < unit.Start || unit.End < nr.End {
			break
		}
		if _, found := scope.ResolveAlias(qualifier); found {
			return true
		}
	}
	return false
}

// selfRefs finds table references inside the unit that resolve to a WITH
// definition the unit itself lives inside — a recursive CTE body naming
// itself. The hoist above can never carry that definition (it would be
// defined in terms of the very text being shipped), so, exactly like a
// correlated qualifier, the reference cannot resolve in the narrowed run and
// is marked rather than discovered at the endpoint.
//
// Two positions can hold such a reference. FROM / JOIN sources come from
// nanopass's scopes (TableSource.IsCTE). The right operand of IN — the NOT
// and GLOBAL variants included — is read off the CST instead: grammar1
// parses `x IN t` with a plain column expression on the right, so a table
// operand there never reaches scope.Tables — while the server accepts the
// recursive `IN r` form and rejects its narrowed body (live-verified),
// exactly the unannounced failure this channel exists to pre-empt.
//
// Resolution comes from nanopass's scopes rather than a name comparison here.
// The test is containment of the unit in the RESOLVED definition: a body's
// reference binds to its own definition only under `WITH RECURSIVE` (the
// self-entry BuildScopes plants, CTEDef.Recursive), while a non-recursive
// rebinding resolves to an outer definition — one that does travel, and the
// server agrees the outer binding answers — whose extent lies elsewhere. A
// bare IN operand resolving to no definition at all is left alone: an array
// column is the ordinary reading of that position.
func selfRefs(pr *nanopass.ParseResult, node antlr.ParserRuleContext, src nanopass.SourceRange, scopes scopeIndex) (out []nanopass.SourceRange) {
	if len(scopes) == 0 {
		return nil
	}
	// definesUnit reports whether name resolves, from scope, to a WITH
	// definition whose extent contains the unit — the containment test above.
	definesUnit := func(scope *nanopass.SelectScope, name string) bool {
		def, found := scope.ResolveCTE(name)
		if !found || def.Node == nil {
			return false
		}
		dr := pr.SourceRangeOf(def.Node)
		return !dr.Empty() && dr.Start <= src.Start && src.End <= dr.End
	}
	nanopass.WalkCST(node, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.SelectStmtContext:
			scope := scopes[c]
			if scope == nil {
				return true
			}
			for _, ts := range scope.Tables {
				if !ts.IsCTE || ts.Node == nil {
					continue
				}
				if !definesUnit(scope, ts.Table) {
					continue
				}
				if r := pr.SourceRangeOf(ts.Node); !r.Empty() {
					out = append(out, r)
				}
			}
		case *grammar1.ColumnExprPrecedence3Context:
			if c.IN() == nil {
				return true
			}
			operand := c.ColumnExpr(1)
			name := bareIdentifierOf(operand)
			if name == "" {
				return true
			}
			scope := enclosingScope(c, scopes)
			if scope == nil || !definesUnit(scope, name) {
				return true
			}
			if r := pr.SourceRangeOf(operand); !r.Empty() {
				out = append(out, r)
			}
		}
		return true
	})
	return
}

// bareIdentifierOf returns the decoded name of a bare, unqualified,
// single-segment identifier expression — the only shape that can reference a
// CTE from IN's right operand — or "" for anything else.
func bareIdentifierOf(expr grammar1.IColumnExprContext) string {
	ident, isIdent := expr.(*grammar1.ColumnExprIdentifierContext)
	if !isIdent {
		return ""
	}
	col, isCol := ident.ColumnIdentifier().(*grammar1.ColumnIdentifierContext)
	if !isCol || col.TableIdentifier() != nil {
		return ""
	}
	nested, isNested := col.NestedIdentifier().(*grammar1.NestedIdentifierContext)
	if !isNested {
		return ""
	}
	ids := nested.AllIdentifier()
	if len(ids) != 1 {
		return ""
	}
	return nanopass.DecodeIdentifier(ids[0].GetText())
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
