package constructsql

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwextract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// extractsql.go is the leeway extraction family of ADR-0181 §SD3: LW_GET,
// LW_GET_NULL and LW_GET_LIST as the client-side nanopass expansion pass
// LwExtractExpand.
//
// A call names a section and a membership; the pass resolves the section
// against the tables in the enclosing SELECT, finds the lanes, and expands
// into the locate-and-extract expression lwextract builds — the same
// expression the read-back generator emits for the same section, because it
// is the same builder.
//
// Unlike the constructor family, this is an ordinary expression: legal
// wherever an expression is, projection and WHERE alike. That is the point —
// (b) extract in ADR-0181's taxonomy is about getting an attribute into an
// opaque column a BI tool consumes, and such a column is as often a filter
// as a projection.
//
// What it expands INTO is not portable: the pack-form renderer calls the
// read-back helper family, which must be installed. That is the dependency
// ADR-0174 §SD6 marks, and it is why this family waited for ADR-0171 §SD2's
// version handshake — a client that cannot tell which revision a server
// carries cannot tell a caller why an expansion failed there.

// The extraction names (LW_ + UPPER_SNAKE per the repo-wide namespace rule).
// Matching in SQL is case- and quoting-insensitive.
const (
	NameGet     = "LW_GET"
	NameGetNull = "LW_GET_NULL"
	NameGetList = "LW_GET_LIST"
)

// ExtractPassName is the registered nanopass name of the extraction pass.
const ExtractPassName = "LwExtractExpand"

// Token prefixes for the optional trailing arguments, following the
// vocabulary-prefixed convention §SD2 established for constructor specs.
const (
	tokenColumn  = "col:"
	tokenChannel = "chan:"
)

// HasExtractMarker is the cheap pre-parse scan (ADR-0181 §SD7). Every
// spelling of every family member contains "lw_get", quoted included.
func HasExtractMarker(sql string) bool {
	return strings.Contains(strings.ToLower(sql), "lw_get")
}

// extractArities per normalized name: a section, a membership, then
// optional col:/chan: tokens.
var extractArities = map[string]arity{
	nanopass.NormalizeCallName(NameGet):     {2, 0},
	nanopass.NormalizeCallName(NameGetNull): {2, 0},
	nanopass.NormalizeCallName(NameGetList): {2, 0},
}

// LaneSourceI is what the pass needs of a schema: which sections a table
// carries, and what a section's lanes are. lwsql.Resolver implements it.
type LaneSourceI interface {
	ExtractLanesFor(dbName string, tableName string, section string) (lanes lwsql.ExtractLanes, ok bool)
	Sections(dbName string, tableName string) (names []string)
}

// MembershipIdsI resolves a membership name to the uint64 a ref lane
// carries (ADR-0171 §SD4). One method, spelled exactly as
// marshallreflect.LookupI and readback.IdLookup spell it, so any registry
// that satisfies those satisfies this.
//
// Optional. Without it a ref channel still takes an id, which is what it
// took before §SD4 — the pass degrades to the older, wordier form rather
// than refusing to run.
type MembershipIdsI interface {
	LookupMembership(name string) (id uint64, err error)
}

// ExtractExpandPass is LwExtractExpand bound to a schema. Idempotent by
// construction: expansion leaves no LW_GET call behind, and an
// unresolvable one is an error, never a partial rewrite.
//
// defaultDatabase resolves unqualified table references, the same way the
// selection-conditions pass takes one.
func ExtractExpandPass(lanes LaneSourceI, defaultDatabase string) nanopass.Pass {
	return ExtractExpandPassWithIds(lanes, nil, defaultDatabase)
}

// ExtractExpandPassWithIds is ExtractExpandPass with a membership-id lookup,
// so a ref channel takes a NAME instead of a registry id (ADR-0171 §SD4).
//
// Resolution happens at expansion time, client-side, which keeps the emitted
// SQL carrying constants — the property ADR-0066 chose deliberately over a
// query-time dictGet, and the reason this is a binding rather than a join.
func ExtractExpandPassWithIds(lanes LaneSourceI, ids MembershipIdsI, defaultDatabase string) nanopass.Pass {
	return nanopass.LiftBodyPass(ExtractPassName, func(sql string) (string, error) {
		return expandExtract(sql, lanes, ids, defaultDatabase)
	}, nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})
}

func expandExtract(sql string, lanes LaneSourceI, ids MembershipIdsI, defaultDatabase string) (result string, err error) {
	result = sql
	if lanes == nil || !HasExtractMarker(sql) {
		return
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", ExtractPassName, err)
		return
	}
	roots, err := nanopass.BuildScopes(pr, defaultDatabase)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", ExtractPassName, err)
		return
	}
	st := &extractState{pr: pr, rw: nanopass.NewRewriter(pr), lanes: lanes, ids: ids, byNode: indexScopes(roots)}
	err = st.walk(pr.Tree)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", ExtractPassName, err)
		return
	}
	result = nanopass.GetText(st.rw)
	return
}

// indexScopes maps each SELECT's parse node to its scope, so a call found
// anywhere in the tree can ask which tables are in scope for it.
func indexScopes(roots []*nanopass.SelectScope) (byNode map[*grammar1.SelectStmtContext]*nanopass.SelectScope) {
	all := nanopass.FlattenScopes(roots)
	byNode = make(map[*grammar1.SelectStmtContext]*nanopass.SelectScope, len(all))
	for _, s := range all {
		if s.Node != nil {
			byNode[s.Node] = s
		}
	}
	return
}

type extractState struct {
	pr     *nanopass.ParseResult
	rw     nanopass.RewriterI
	lanes  LaneSourceI
	ids    MembershipIdsI // optional; nil means a ref channel takes an id
	byNode map[*grammar1.SelectStmtContext]*nanopass.SelectScope
}

func (inst *extractState) walk(node antlr.Tree) (err error) {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	if funcExpr, isFn := ctx.(*grammar1.ColumnExprFunctionContext); isFn {
		if ident := funcExpr.Identifier(); ident != nil {
			name := nanopass.NormalizeCallName(ident.GetText())
			if _, isExtract := extractArities[name]; isExtract {
				err = inst.expandCall(name, ident.GetText(), funcExpr)
				// The subtree is consumed either way: every argument is a
				// string literal, so nothing below can match, and descending
				// into a replaced node would stack a second rewrite onto the
				// same tokens.
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

func (inst *extractState) expandCall(name string, spelled string, funcExpr *grammar1.ColumnExprFunctionContext) (err error) {
	args, err := callArgs(funcExpr)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	want := extractArities[name]
	if len(args) < want.min {
		err = inst.errCall(spelled, funcExpr).Int("got", len(args)).Int("min", want.min).
			Errorf("an extraction call takes a section, a membership, and optional col:/chan: tokens")
		return
	}
	spec := make([]string, 0, len(args))
	for i, arg := range args {
		var s string
		s, err = specString(arg)
		if err != nil {
			err = inst.errCall(spelled, funcExpr).Int("argument", i+1).Errorf("%w", err)
			return
		}
		spec = append(spec, s)
	}
	section, membership := spec[0], spec[1]

	var subColumn, channel string
	for _, tok := range spec[2:] {
		switch {
		case strings.HasPrefix(tok, tokenColumn):
			subColumn = strings.TrimPrefix(tok, tokenColumn)
		case strings.HasPrefix(tok, tokenChannel):
			channel = strings.TrimPrefix(tok, tokenChannel)
		default:
			err = inst.errCall(spelled, funcExpr).Str("token", tok).
				Errorf("unknown token; expected " + tokenColumn + "<value column> or " + tokenChannel + "<membership channel>")
			return
		}
	}

	lanes, qualifier, err := inst.bind(section, funcExpr)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	valueCol, err := lanes.ValueColumnFor(subColumn)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	ch, err := lanes.ChannelFor(channel)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	lit, err := membershipLiteral(membership, ch, inst.ids)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}

	// The membership cardinality lane is required, and its absence is a
	// refusal rather than a licence. lwextract reads an empty Card as "the
	// schema proves one membership per attribute" and drops the
	// position→attribute map accordingly; a column merely missing from this
	// table's listing is not that proof, and taking the fast form on the
	// strength of it would read a membership position as an attribute index
	// on every row. The read-back generator refuses the same situation.
	if ch.Card == "" {
		err = inst.errCall(spelled, funcExpr).Str("section", lanes.Section).Str("channel", ch.Name).
			Errorf("the section's membership cardinality column is not among this table's columns; an attribute cannot be located without it")
		return
	}

	shape := valueCol.Shape
	wantList := name == nanopass.NormalizeCallName(NameGetList)
	switch {
	case wantList && shape != lwextract.ShapeList:
		err = inst.errCall(spelled, funcExpr).Str("section", lanes.Section).
			Errorf("section stores one value per attribute; read it with " + NameGet + " or " + NameGetNull)
		return
	case !wantList && shape != lwextract.ShapeScalar:
		err = inst.errCall(spelled, funcExpr).Str("section", lanes.Section).
			Errorf("section stores an array or set per attribute; read it with " + NameGetList)
		return
	}

	el := lwextract.Lanes{
		Value:  qualify(qualifier, valueCol.Physical),
		Ident:  qualify(qualifier, ch.Ident),
		Card:   qualify(qualifier, ch.Card),
		Length: qualify(qualifier, valueCol.Length),
	}
	expr, err := lwextract.Value(lwextract.Request{Lanes: el, Shape: shape, Membership: lit})
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	if name == nanopass.NormalizeCallName(NameGetNull) {
		expr = lwextract.NullWhenAbsent(expr, el, lit)
	}
	nanopass.ReplaceNode(inst.rw, funcExpr, expr)
	return
}

// bind resolves the section against the tables in the call's enclosing
// SELECT (ADR-0181 §SD3): exactly one carrying table expands; several
// demand qualification; none errors naming what was searched.
//
// qualifier is the table alias to prefix lanes with, empty when the scope
// reads a single source and no prefix can be ambiguous.
func (inst *extractState) bind(section string, funcExpr *grammar1.ColumnExprFunctionContext) (lanes lwsql.ExtractLanes, qualifier string, err error) {
	scope := inst.scopeOf(funcExpr)
	if scope == nil {
		err = eb.Build().Str("section", section).Errorf("no enclosing SELECT to resolve the section against")
		return
	}

	type candidate struct {
		lanes     lwsql.ExtractLanes
		qualifier string
		label     string
	}
	type tableRef struct{ db, table string }
	var notFound []tableRef
	var found []candidate
	var searched []string
	for i := range scope.Tables {
		ts := &scope.Tables[i]
		// A CTE, a subquery or a table function has no catalog schema to
		// ask. Their columns come from the SELECT that defines them, which
		// this pass does not model — a section reached through one is out
		// of scope rather than silently unresolved.
		if ts.IsCTE || ts.IsSubquery || ts.IsFunction {
			continue
		}
		// ResolvedDatabase, not ts.Database: an unqualified reference
		// carries no database of its own, and the schema index is keyed by
		// (database, table). Reading the bare field made every unqualified
		// query miss the cache and report the section as absent — the same
		// resolution every other schema-aware pass performs.
		db := ts.ResolvedDatabase(scope)
		searched = append(searched, tableLabel(db, ts.Table))
		l, ok := inst.lanes.ExtractLanesFor(db, ts.Table, section)
		if !ok {
			notFound = append(notFound, tableRef{db: db, table: ts.Table})
			continue
		}
		q := ts.Alias
		if q == "" {
			q = ts.Table
		}
		found = append(found, candidate{lanes: l, qualifier: q, label: tableLabel(db, ts.Table)})
	}

	switch len(found) {
	case 1:
		lanes = found[0].lanes
		if len(scope.Tables) > 1 {
			// More than one source in scope, so an unqualified physical
			// name could bind to the wrong side of a join — qualify even
			// though only one side carries the section, because the other
			// side's columns are what an ambiguity error would be about.
			qualifier = found[0].qualifier
		}
	case 0:
		// The message carries the candidates, not just the structured
		// fields: this reaches a person who typed the call into a SQL
		// editor, and "unknown section" without the list of known ones
		// sends them to the schema to find out what they could have typed.
		b := eb.Build().Str("section", section)
		detail := ""
		if len(searched) > 0 {
			b = b.Str("tables", strings.Join(searched, ", "))
			detail = "; searched " + strings.Join(searched, ", ")
			var have []string
			for _, ref := range notFound {
				// Asked with the SAME database the lookup used, or the
				// answer describes a different table than the one that
				// missed — and costs a second cache entry saying so.
				have = append(have, inst.lanes.Sections(ref.db, ref.table)...)
			}
			if len(have) > 0 {
				b = b.Str("sectionsFound", strings.Join(have, ", "))
				detail += ", which carry " + strings.Join(have, ", ")
			}
		}
		err = b.Errorf("no table in scope carries that section%s", detail)
	default:
		var where []string
		for _, c := range found {
			where = append(where, c.label)
		}
		// No table token exists in the call vocabulary, so "qualify the
		// call" is not advice anyone can act on. Say what CAN be done.
		err = eb.Build().Str("section", section).Str("tables", strings.Join(where, ", ")).
			Errorf("more than one table in scope carries that section, and an extraction call cannot name which; read it in a subselect over the one table, or join on the already-extracted column")
	}
	return
}

// scopeOf finds the scope of the SELECT lexically enclosing the call.
func (inst *extractState) scopeOf(node antlr.ParserRuleContext) *nanopass.SelectScope {
	for p := node.GetParent(); p != nil; p = p.GetParent() {
		if sel, ok := p.(*grammar1.SelectStmtContext); ok {
			if scope, found := inst.byNode[sel]; found {
				return scope
			}
		}
	}
	return nil
}

// errCall builds the error context every rejection carries: the call as
// spelled and its source range.
func (inst *extractState) errCall(spelled string, funcExpr *grammar1.ColumnExprFunctionContext) *eb.ErrorBuilder {
	r := inst.pr.SourceRangeOf(funcExpr)
	return eb.Build().Str("call", spelled).Int("start", r.Start).Int("end", r.End)
}

// membershipLiteral renders the membership as the channel spells it.
//
// A verbatim channel carries the name itself, so any string works. A ref
// channel carries a registry id: a decimal literal is taken as that id, and
// anything else is resolved through the bound registry (ADR-0171 §SD4).
//
// The decimal check comes first and is deliberate. It keeps every query
// written before §SD4 working unchanged, and it means the id form needs no
// binding — which matters because the binding is a property of the host, not
// of the query.
func membershipLiteral(membership string, ch lwsql.Channel, ids MembershipIdsI) (lit string, err error) {
	if ch.Verbatim {
		lit = marshalling.EscapeString(membership)
		return
	}
	if id, convErr := strconv.ParseUint(membership, 10, 64); convErr == nil {
		lit = strconv.FormatUint(id, 10)
		return
	}
	if ids == nil {
		err = eb.Build().Str("membership", membership).Str("channel", ch.Name).
			Errorf("a ref channel identifies memberships by registry id, and this host bound no membership registry; pass the id — `leeway id` prints it, and keelson('memberships') lists them")
		return
	}
	id, lookupErr := ids.LookupMembership(membership)
	if lookupErr != nil {
		err = eb.Build().Str("membership", membership).Str("channel", ch.Name).
			Errorf("no such membership in this build's registry; keelson('memberships') lists them: %w", lookupErr)
		return
	}
	lit = strconv.FormatUint(id, 10)
	return
}

// tableLabel names a table for a diagnostic, qualified when the database is
// known.
func tableLabel(db string, table string) (out string) {
	if db == "" {
		return table
	}
	return db + "." + table
}

// qualify renders a physical column, prefixed by the table qualifier when
// the scope needs one. An empty column stays empty — Lanes.Card uses the
// empty string to mean the column does not exist, and qualifying that would
// turn a meaningful absence into a reference to nothing.
func qualify(qualifier string, column string) (out string) {
	if column == "" {
		return ""
	}
	out = nanopass.QuoteIdentifier(column)
	if qualifier != "" {
		out = nanopass.QuoteIdentifier(qualifier) + "." + out
	}
	return
}

// ExtractFunctions returns the extraction family, declared as data for the
// vocabulary panel (ADR-0174 §SD3).
//
// Client-expanded like the constructors, but with a server-side dependency
// they do not have: the expansion calls the read-back helper family, so a
// server without it fails on the expansion rather than on the name.
func ExtractFunctions() (fns []Function) {
	fns = []Function{
		{
			Name:   NameGet,
			Params: []string{"'section'", "'membership'", "'col:…'", "'chan:…'"},
			Doc:    "read one attribute's scalar value from a leeway section; the type default when absent (ADR-0181)",
		},
		{
			Name:   NameGetNull,
			Params: []string{"'section'", "'membership'", "'col:…'", "'chan:…'"},
			Doc:    "as " + NameGet + ", but NULL when the membership is absent — tells absent from present-with-the-default (ADR-0181)",
		},
		{
			Name:   NameGetList,
			Params: []string{"'section'", "'membership'", "'col:…'", "'chan:…'"},
			Doc:    "read one attribute's array or set value from a leeway section; [] when absent (ADR-0181)",
		},
	}
	return
}

// ExtractExpansionDependencies are the server-side functions an expanded
// extraction may call (ADR-0174 §SD6). The panel marks the family against
// them, so "installed client-side" does not read as "works everywhere".
//
// The fast form calls fewer of these, and which form a given call takes
// depends on the section it reads, so the list is what an expansion MAY
// need rather than what any one expansion does need.
func ExtractExpansionDependencies() (names []string) {
	names = []string{
		"LW_VALUE_BY_TAG_EQUAL",
		"LW_LIST_BY_TAG_EQUAL",
		"LW_RAGGED_PARENT_IDS",
		"LW_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL",
	}
	return
}
