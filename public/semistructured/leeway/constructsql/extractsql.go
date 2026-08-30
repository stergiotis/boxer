package constructsql

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
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

	// NameSel and NameSelAttrs are the selector half: they return index
	// selectors rather than values, so the caller's own arrayMap /
	// arrayFilter / LW_CO_GATHER does the reading.
	//
	// This is the "argwhere + gather" plan the array-idioms how-to
	// describes. The pack already ships the gather half (LW_CO_GATHER); the
	// argwhere half is what these mint, against a section and a membership
	// instead of a physical lane.
	NameSel      = "LW_SEL"
	NameSelAttrs = "LW_SEL_ATTRS"
)

// ExtractPassName is the registered nanopass name of the extraction pass.
const ExtractPassName = "LwExtractExpand"

// Token prefixes for the optional trailing arguments, following the
// vocabulary-prefixed convention §SD2 established for constructor specs.
const (
	tokenColumn  = "col:"
	tokenChannel = "chan:"
	tokenParam   = "param:"
)

// membershipArgIndex is the one slot in the authoring surface whose value is
// genuinely a number, and so the one slot that takes an unquoted spelling.
const membershipArgIndex = 1

// HasExtractMarker is the cheap pre-parse scan (ADR-0181 §SD7). Every
// spelling of every family member contains "lw_get" or "lw_sel", quoted
// included — LW_SEL_ATTRS is caught by the LW_SEL prefix.
func HasExtractMarker(sql string) bool {
	l := strings.ToLower(sql)
	return strings.Contains(l, "lw_get") || strings.Contains(l, "lw_sel")
}

// extractArities per normalized name: a section, a membership, then
// optional col:/chan:/param: tokens.
var extractArities = map[string]arity{
	nanopass.NormalizeCallName(NameGet):      {2, 0},
	nanopass.NormalizeCallName(NameGetNull):  {2, 0},
	nanopass.NormalizeCallName(NameGetList):  {2, 0},
	nanopass.NormalizeCallName(NameSel):      {2, 0},
	nanopass.NormalizeCallName(NameSelAttrs): {2, 0},
}

// isSelector reports whether the normalized name is one of the two selector
// members, which read no value lane and answer the plural question.
func isSelector(name string) bool {
	return name == nanopass.NormalizeCallName(NameSel) || name == nanopass.NormalizeCallName(NameSelAttrs)
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
				// literal, so nothing below can match, and descending into a
				// replaced node would stack a second rewrite onto the same
				// tokens.
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
			Errorf("an extraction call takes a section, a membership, and optional col:/chan:/param: tokens")
		return
	}
	selector := isSelector(name)
	spec := make([]string, 0, len(args))
	membershipIsId := false
	for i, arg := range args {
		var s string
		if i == membershipArgIndex {
			s, membershipIsId, err = membershipArg(arg)
		} else {
			s, err = specString(arg)
		}
		if err != nil {
			err = inst.errCall(spelled, funcExpr).Int("argument", i+1).Errorf("%w", err)
			return
		}
		spec = append(spec, s)
	}
	section, membership := spec[0], spec[1]

	var subColumn, channel, params string
	var paramsGiven bool
	for _, tok := range spec[2:] {
		switch {
		case strings.HasPrefix(tok, tokenColumn):
			// A selector points at attributes, not at a value, so naming a
			// value column would be a request it cannot honour. Rejected
			// rather than ignored: silently dropping a token makes a query
			// that says one thing and does another.
			if selector {
				err = inst.errCall(spelled, funcExpr).Str("token", tok).
					Errorf("a selector returns indices, not values, so it takes no " + tokenColumn + " token; gather the column through the selector instead")
				return
			}
			subColumn = strings.TrimPrefix(tok, tokenColumn)
		case strings.HasPrefix(tok, tokenChannel):
			channel = strings.TrimPrefix(tok, tokenChannel)
		case strings.HasPrefix(tok, tokenParam):
			params = strings.TrimPrefix(tok, tokenParam)
			paramsGiven = true
		default:
			err = inst.errCall(spelled, funcExpr).Str("token", tok).
				Errorf("unknown token; expected " + tokenColumn + "<value column>, " + tokenChannel + "<membership channel> or " + tokenParam + "<mixed-channel parameter>")
			return
		}
	}

	lanes, qualifier, err := inst.bind(section, funcExpr)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	ch, err := lanes.ChannelFor(channel)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	lit, err := membershipLiteral(membership, membershipIsId, ch, inst.ids)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	if paramsGiven && ch.Param == "" {
		err = inst.errCall(spelled, funcExpr).Str("section", lanes.Section).Str("channel", ch.Name).
			Errorf("only a mixed channel carries a parameter lane; %s identifies a membership by its name alone", ch.Name)
		return
	}

	// The membership cardinality lane is required unless the schema DECLARES
	// the channel single-instance (ADR-0213). lwextract reads an empty Card
	// as "the schema proves one membership per attribute" and drops the
	// position→attribute map accordingly; the declaration — recovered from
	// the use-aspects the column names encode (Channel.SingleMembership) —
	// is that proof, and licenses the fast form. A column merely missing
	// from this table's listing is not, and taking the fast form on the
	// strength of absence alone would read a membership position as an
	// attribute index on every row. The read-back generator draws the same
	// line.
	//
	// NameSel is the one member exempt: it selects positions in the identity
	// lane and never crosses to the attribute axis, so it has nothing to map
	// and nothing to get wrong.
	if ch.Card == "" && !ch.SingleMembership && name != nanopass.NormalizeCallName(NameSel) {
		err = inst.errCall(spelled, funcExpr).Str("section", lanes.Section).Str("channel", ch.Name).
			Errorf("the section's membership cardinality column is not among this table's columns; an attribute cannot be located without it")
		return
	}

	el := lwextract.Lanes{
		Ident: qualify(qualifier, ch.Ident),
		Card:  qualify(qualifier, ch.Card),
		Param: qualify(qualifier, ch.Param),
	}
	req := lwextract.Request{Lanes: el, Membership: lit, Params: marshalling.EscapeString(params), ParamsGiven: paramsGiven}

	var expr string
	if selector {
		if name == nanopass.NormalizeCallName(NameSel) {
			expr, err = lwextract.Selection(req)
		} else {
			expr, err = lwextract.SelectionAttrs(req)
		}
		if err != nil {
			err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
			return
		}
		nanopass.ReplaceNode(inst.rw, funcExpr, expr)
		return
	}

	valueCol, err := lanes.ValueColumnFor(subColumn)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
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
	// A mixed channel's identity is shared by design, so a singular read
	// without a parameter would return an arbitrary member of the set it
	// names. lwextract refuses it; this says what to do instead, because the
	// answer is a sibling call rather than a rewrite.
	if ch.Param != "" && !paramsGiven {
		err = inst.errCall(spelled, funcExpr).Str("section", lanes.Section).Str("channel", ch.Name).
			Errorf("a mixed channel identifies an attribute by membership AND parameter; add '" + tokenParam +
				"<parameter>', or read every attribute carrying the membership with " + NameSel + " / " + NameSelAttrs)
		return
	}

	req.Lanes.Value = qualify(qualifier, valueCol.Physical)
	req.Lanes.Length = qualify(qualifier, valueCol.Length)
	req.Shape = shape
	expr, err = lwextract.Value(req)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	if name == nanopass.NormalizeCallName(NameGetNull) {
		expr = lwextract.NullWhenAbsentFor(req, expr)
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

// membershipArg decodes the membership slot. It takes the quoted spec form
// every other slot takes, and — because a ref channel identifies memberships
// by a uint64 registry id — an unsigned decimal literal as well
// (ADR-0181 §SD2, 2026-08-15 Update).
//
// The quoted form stays the general one: it is the only spelling that can
// carry both a name and an id, and which of those a call means is not known
// until the section resolves against the schema, well after parsing. The
// unquoted form adds no ambiguity precisely because it is narrower — a bare
// number can only ever be the id.
//
// numeric reports which spelling was used, so the caller can reject the one
// combination the quoted form leaves legible and this one does not: a bare
// number against a verbatim channel, which carries names.
func membershipArg(arg *grammar1.ColumnArgExprContext) (s string, numeric bool, err error) {
	if lit := argLiteral(arg); lit != nil {
		if str := lit.STRING_LITERAL(); str != nil {
			s, err = unquoteSpec(str.GetText())
			return
		}
		if num, isNum := lit.NumberLiteral().(*grammar1.NumberLiteralContext); isNum {
			s, err = unsignedDecimal(num)
			numeric = err == nil
			return
		}
	}
	err = eb.Build().Str("got", strings.TrimSpace(arg.GetText())).
		Errorf("a membership must be a string literal or an unsigned decimal registry id")
	return
}

// unsignedDecimal accepts the bare decimal form and nothing else. A sign, a
// floating point, hex, octal, INF and NAN all parse as number literals here,
// and none of them can be a registry id — rejecting them by shape keeps the
// diagnostic about the id rather than about a failed conversion.
//
// The text is carried through unparsed: ids run the full uint64 range, and
// the caller converts it in the one place that already did.
func unsignedDecimal(num *grammar1.NumberLiteralContext) (s string, err error) {
	if num.DECIMAL_LITERAL() == nil || num.DASH() != nil || num.PLUS() != nil {
		err = eb.Build().Str("got", strings.TrimSpace(num.GetText())).
			Errorf("a numeric membership must be an unsigned decimal registry id")
		return
	}
	s = num.DECIMAL_LITERAL().GetText()
	return
}

// membershipLiteral renders the membership as the channel spells it.
//
// A verbatim channel carries the name itself, so any string works. A ref
// channel carries a registry id: a decimal is taken as that id, and anything
// else is resolved through the bound registry (ADR-0171 §SD4).
//
// The decimal check comes first and is deliberate. It keeps every query
// written before §SD4 working unchanged, and it means the id form needs no
// binding — which matters because the binding is a property of the host, not
// of the query. Its cost is that a ref membership NAMED in digits is
// unreachable by name; redefining the quoted form to fix that would break
// every query written against §SD4, so the unquoted spelling is additive
// rather than a replacement.
//
// numeric says the call spelled the membership unquoted, which asserts "this
// is an id" — an assertion only a ref channel can honour.
func membershipLiteral(membership string, numeric bool, ch lwsql.Channel, ids MembershipIdsI) (lit string, err error) {
	if ch.Verbatim {
		if numeric {
			err = eb.Build().Str("membership", membership).Str("channel", ch.Name).
				Errorf("a verbatim channel identifies memberships by name and a bare number can only be a registry id; quote it to mean the name")
			return
		}
		lit = marshalling.EscapeString(membership)
		return
	}
	if id, convErr := strconv.ParseUint(membership, 10, 64); convErr == nil {
		lit = strconv.FormatUint(id, 10)
		return
	}
	if numeric {
		// The decoder admitted unsigned decimals only, so the sole way to
		// reach here is a number too large for the uint64 a ref lane carries.
		err = eb.Build().Str("membership", membership).Str("channel", ch.Name).
			Errorf("a registry id does not fit in a uint64")
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
			Name: NameGet,
			Params: []sqlvocab.Param{
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'membership'|id", sqlvocab.DomainMembership),
				sqlvocab.Of("'col:…'", sqlvocab.DomainExtractionToken, 0),
				sqlvocab.Of("'chan:…'", sqlvocab.DomainExtractionToken, 0),
			},
			Doc: "read one attribute's scalar value from a leeway section; the type default when absent (ADR-0181)",
		},
		{
			Name: NameGetNull,
			Params: []sqlvocab.Param{
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'membership'|id", sqlvocab.DomainMembership),
				sqlvocab.Of("'col:…'", sqlvocab.DomainExtractionToken, 0),
				sqlvocab.Of("'chan:…'", sqlvocab.DomainExtractionToken, 0),
			},
			Doc: "as " + NameGet + ", but NULL when the membership is absent — tells absent from present-with-the-default (ADR-0181)",
		},
		{
			Name: NameGetList,
			Params: []sqlvocab.Param{
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'membership'|id", sqlvocab.DomainMembership),
				sqlvocab.Of("'col:…'", sqlvocab.DomainExtractionToken, 0),
				sqlvocab.Of("'chan:…'", sqlvocab.DomainExtractionToken, 0),
			},
			Doc: "read one attribute's array or set value from a leeway section; [] when absent (ADR-0181)",
		},
		{
			Name: NameSel,
			Params: []sqlvocab.Param{
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'membership'|id", sqlvocab.DomainMembership),
				sqlvocab.Of("'chan:…'", sqlvocab.DomainExtractionToken, 0),
				sqlvocab.Of("'param:…'", sqlvocab.DomainExtractionToken, 0),
			},
			Doc: "select the membership-lane positions carrying a membership, to gather any lane through with LW_CO_GATHER; [] when absent (ADR-0181)",
		},
		{
			Name: NameSelAttrs,
			Params: []sqlvocab.Param{
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'membership'|id", sqlvocab.DomainMembership),
				sqlvocab.Of("'chan:…'", sqlvocab.DomainExtractionToken, 0),
				sqlvocab.Of("'param:…'", sqlvocab.DomainExtractionToken, 0),
			},
			Doc: "as " + NameSel + ", but attribute indices — co-indexed with it, so both pass to one lambda (ADR-0181)",
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
		// LW_SEL_ATTRS gathers the position→attribute map through the
		// selector. Both it and LW_RAGGED_PARENT_IDS are pack functions, so
		// the selector half needs no revision of the surface that the getter
		// half did not already need — and the mixed arm of every member
		// expands to built-ins alone.
		"LW_CO_GATHER",
	}
	return
}
