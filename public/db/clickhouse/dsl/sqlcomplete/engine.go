package sqlcomplete

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
)

// Request is one caret's question.
type Request struct {
	// Site is the per-frame answer from the lex tier.
	Site highlight.CaretSite
	// Scope is the sentinel parse's answer, nil until it arrives.
	Scope *Scope
	// Statement is the buffer the site's ranges index.
	Statement string
	// Caret is the caret's byte offset into Statement.
	Caret int
}

// Result is what the pane and the editor tint render.
type Result struct {
	// Domain is the argument domain the engine resolved, zero when it
	// resolved none.
	Domain sqlvocab.Domain
	// Items are the candidates, in the provider's own order.
	Items []Item
	// Partial is the byte range in Statement a completion replaces.
	Partial highlight.Range
	// Match is the state of what was typed against Items.
	Match MatchE
	// Exact is the index in Items of the candidate equal to the token's whole
	// text, or -1.
	Exact int
	// Prefix are the indices of the candidates extending the typed text — all
	// of them when nothing has been typed.
	Prefix []int
	// Silent says why there is nothing to offer, for the pane to show instead
	// of an empty table. Empty when Items is non-empty.
	//
	// Every path that offers nothing sets it: ADR-0190 §SD1 commits to silence
	// over guessing, and a silence with no reason is indistinguishable from a
	// bug.
	Silent string
	// Callee is the enclosing call the domain came from, for the pane's
	// heading. Empty when the domain came from the clause or a member access.
	Callee string
	// Ordinal is the argument position within Callee, or -1.
	Ordinal int
}

// Empty reports whether there is nothing to show.
func (inst Result) Empty() bool { return len(inst.Items) == 0 }

// ExactItem is the exactly-matching candidate.
func (inst Result) ExactItem() (it Item, ok bool) {
	if inst.Exact < 0 || inst.Exact >= len(inst.Items) {
		return
	}
	it = inst.Items[inst.Exact]
	ok = true
	return
}

// Engine answers completion requests for one buffer.
//
// Render-thread-only, like the typer it owns: the memo is unsynchronised and
// each buffer has its own engine.
type Engine struct {
	// Vocab is the host's function registry — what the rosters declare.
	Vocab *sqlvocab.Registry
	// Builtins is the curated ClickHouse table; nil means [sqlvocab.Builtins].
	Builtins []sqlvocab.Function
	// Providers resolve a domain to candidates.
	Providers Providers
	// Typer answers the type-dependent domains; nil means one is built on
	// first use over Providers.
	Typer *Typer
	// NamedTupleAccess reports that the buffer's pipeline accepts `expr.name`
	// on a named tuple (ADR-0190 §SD11). Until it does, offering the fields of
	// a call receiver would offer a spelling that does not parse, so §SD7 gates
	// that one receiver on this.
	NamedTupleAccess bool

	builtinIndex map[string]sqlvocab.Function
}

// Complete answers one request.
//
// The resolution order is member access, then the innermost call frame whose
// signature is known, then the clause. It stops at the first that answers: an
// argument position's domain is what belongs there, and a clause rule that
// overrode it would be the coarse answer ADR-0190 was written to replace.
func (inst *Engine) Complete(req Request) (res Result) {
	res.Exact = -1
	res.Ordinal = -1
	res.Partial = req.Site.Partial
	inst.typer().Scope = req.Scope

	d, of, callee, ordinal, why := inst.domainFor(req)
	res.Callee = callee
	res.Ordinal = ordinal
	if why != "" {
		res.Silent = why
		return
	}
	res.Domain = d

	items, ready, wired, why := inst.resolve(d, of)
	switch {
	case why != "":
		res.Silent = why
		return
	case !wired:
		res.Silent = "nothing here answers " + d.Kind.String()
		return
	case !ready:
		// Not the same as an empty answer, and the difference is the whole
		// point: a probe that has not come back must not read as "this domain
		// has no members" (ADR-0174's `?`-never-`MISSING` rule, imported by
		// §SD9).
		res.Silent = d.Kind.String() + ": waiting for the endpoint"
		return
	case len(items) == 0:
		res.Silent = "no " + d.Kind.String() + " is available here"
		return
	}

	kind := itemKindFor(d.Kind)
	quote := literalDomain(d.Kind) && req.Site.Literal == nil
	res.Items = make([]Item, len(items))
	for i := range items {
		it := items[i]
		if it.Kind == ItemUnspecified {
			it.Kind = kind
		}
		if it.Insert == "" {
			it.Insert = it.Text
			if quote {
				it.Insert = quoteLiteral(it.Text)
			}
		}
		res.Items[i] = it
	}
	inst.match(&res, req.Site)
	return
}

// resolve turns a resolved domain into candidates.
//
// The tuple-element domain is answered here rather than by a provider: what
// names the elements is an expression's *type*, and the typer is what produces
// one (§SD5). A provider keyed on a component kind would serve the
// LW_COMPONENT spelling and nothing else — not `tupleElement(m, …)` on an
// alias, not a Tuple-typed column.
func (inst *Engine) resolve(d sqlvocab.Domain, of string) (items []Item, ready bool, wired bool, why string) {
	if d.Kind == sqlvocab.DomainElementOf {
		t, ok := inst.typer().TypeOf(of)
		if !ok {
			return nil, true, true, "nothing here can type " + trimForMessage(of)
		}
		items = elementItems(t)
		if len(items) == 0 {
			return nil, true, true, trimForMessage(of) + " is a " + t.String() + ", which has no named elements"
		}
		return items, true, true, ""
	}
	items, ready, wired = inst.Providers.resolve(d, of)
	return
}

// elementItems renders a named tuple's elements as candidates.
func elementItems(t chtype.Type) (items []Item) {
	elems, ok := t.Elements()
	if !ok {
		return
	}
	items = make([]Item, 0, len(elems))
	for i := range elems {
		it := Item{Text: elems[i].Name, Kind: ItemField, Source: "tuple type"}
		if elems[i].Type != nil {
			it.Type = elems[i].Type.String()
		}
		items = append(items, it)
	}
	return
}

// domainFor is the resolution order. why is non-empty when nothing answered,
// and is the sentence the pane shows.
func (inst *Engine) domainFor(req Request) (d sqlvocab.Domain, of string, callee string, ordinal int, why string) {
	ordinal = -1

	if m := req.Site.Member; m != nil {
		d, of, why = inst.memberDomain(req, m)
		return
	}

	f, frameOk := frameWithCallee(req.Site)
	if frameOk {
		callee = f.Callee
		ordinal = f.Ordinal
		if f.Ordinal < 0 {
			why = "a keyword-syntax call: which argument this is needs the parse"
			return
		}
		sig, sigOk := inst.signature(f.Callee)
		if !sigOk {
			why = "no signature is declared for " + f.Callee
			return
		}
		dom, domOk := domainAt(sig, f.Ordinal)
		if !domOk {
			why = f.Callee + " declares no argument at this position"
			return
		}
		d = dom
		if d.Kind.IsRefDependent() {
			of, why = refValue(req, f, d)
		}
		return
	}

	if k, hit := clauseKindFor(req.Site.Clause); hit {
		d = sqlvocab.Domain{Kind: k, Ref: sqlvocab.NoRef}
		return
	}
	why = "no provider for this position"
	return
}

// refValue narrows the sibling argument a ref-dependent domain reads.
//
// A tuple-element domain hands the sibling's source text over whole, because
// the typer is what turns it into a type — it recognises the component read,
// the casts, the tuple constructor and, once the scope tier answers, an alias
// or a column. Every other ref-dependent domain reads a literal *value*, so
// the quotes come off.
func refValue(req Request, f highlight.CallFrame, d sqlvocab.Domain) (of string, why string) {
	if d.Ref < 0 || d.Ref >= len(f.Args) {
		why = "the argument this position depends on has not been written yet"
		return
	}
	text := textOf(req.Statement, f.Args[d.Ref])
	if text == "" {
		why = "the argument this position depends on is empty"
		return
	}
	if d.Kind == sqlvocab.DomainElementOf {
		of = text
		return
	}
	if lit, ok := literalValue(text); ok {
		of = lit
		return
	}
	of = text
	return
}

// memberDomain answers `X.`.
func (inst *Engine) memberDomain(req Request, m *highlight.MemberAccess) (d sqlvocab.Domain, of string, why string) {
	switch m.Kind {
	case highlight.ReceiverCall, highlight.ReceiverParen:
		if !inst.NamedTupleAccess {
			why = "the dot form on a call is not accepted by this build's pipeline yet"
			return
		}
		d = sqlvocab.Domain{Kind: sqlvocab.DomainElementOf, Ref: sqlvocab.NoRef}
		of = textOf(req.Statement, m.Receiver)
		return
	case highlight.ReceiverIdent:
		return inst.identMemberDomain(req, m)
	}
	why = "no provider for this position"
	return
}

// identMemberDomain answers `name.` — an alias of a tuple-typed expression, a
// table or table alias, or a database (§SD7).
//
// Every branch needs the statement's own names, so without the scope tier it
// says so rather than guessing which of the three a bare name is.
func (inst *Engine) identMemberDomain(req Request, m *highlight.MemberAccess) (d sqlvocab.Domain, of string, why string) {
	if len(m.Chain) == 0 {
		why = "no provider for this position"
		return
	}
	if req.Scope == nil {
		why = "member access on a name needs the statement's scope"
		return
	}
	head := m.Chain[0]
	if len(m.Chain) == 1 {
		if expr, ok := req.Scope.AliasOf(head); ok {
			t, typed := inst.typer().TypeOf(expr)
			if typed {
				if _, hasElems := t.Elements(); hasElems {
					d = sqlvocab.Domain{Kind: sqlvocab.DomainElementOf, Ref: sqlvocab.NoRef}
					of = expr
					return
				}
			}
		}
		if ref, ok := req.Scope.LookupTable(head); ok {
			d = sqlvocab.Domain{Kind: sqlvocab.DomainColumn, Ref: sqlvocab.NoRef}
			of = qualified(ref)
			return
		}
	}
	// A dotted chain the statement does not introduce is a qualified name:
	// `db.` lists that database's tables, `db.table.` its columns.
	switch len(m.Chain) {
	case 1:
		d = sqlvocab.Domain{Kind: sqlvocab.DomainTable, Ref: sqlvocab.NoRef}
		of = head
		return
	case 2:
		d = sqlvocab.Domain{Kind: sqlvocab.DomainColumn, Ref: sqlvocab.NoRef}
		of = m.Chain[0] + "." + m.Chain[1]
		return
	}
	why = trimForMessage(strings.Join(m.Chain, ".")) + " is deeper than a database and a table"
	return
}

func qualified(ref TableRef) (s string) {
	if ref.Database == "" {
		return ref.Name
	}
	return ref.Database + "." + ref.Name
}

// match computes the case-sensitive prefix and equality state.
//
// Case-sensitive because the domains are values, not SQL identifiers: `SysMem`
// and `sysmem` are not the same component kind, and offering a row that would
// not resolve is the failure §SD1 forbids.
func (inst *Engine) match(res *Result, site highlight.CaretSite) {
	res.Prefix = make([]int, 0, len(res.Items))
	for i := range res.Items {
		if site.PartialText == "" || strings.HasPrefix(res.Items[i].Text, site.PartialText) {
			res.Prefix = append(res.Prefix, i)
		}
		if site.PartialFull != "" && res.Items[i].Text == site.PartialFull {
			res.Exact = i
		}
	}
	switch {
	case res.Exact >= 0:
		res.Match = MatchExact
	case site.PartialText != "" && len(res.Prefix) > 0:
		res.Match = MatchPrefix
	default:
		res.Match = MatchNone
	}
}

// frameWithCallee is the innermost frame that names a call. Grouping parens and
// subqueries are skipped: they enclose the caret without describing it, so the
// call outside them is still what the argument position belongs to.
func frameWithCallee(site highlight.CaretSite) (f highlight.CallFrame, ok bool) {
	for i := range site.Frames {
		if site.Frames[i].Callee != "" {
			return site.Frames[i], true
		}
	}
	return
}

func (inst *Engine) signature(name string) (f sqlvocab.Function, ok bool) {
	if inst.Vocab != nil {
		f, ok = inst.Vocab.Signature(name)
		if ok {
			return
		}
	}
	if inst.builtinIndex == nil {
		src := inst.Builtins
		if src == nil {
			src = sqlvocab.Builtins()
		}
		inst.builtinIndex = make(map[string]sqlvocab.Function, len(src))
		for i := range src {
			inst.builtinIndex[strings.ToLower(src[i].Name)] = src[i]
		}
	}
	f, ok = inst.builtinIndex[strings.ToLower(name)]
	return
}

func (inst *Engine) typer() *Typer {
	if inst.Typer == nil {
		inst.Typer = &Typer{}
	}
	inst.Typer.Providers = &inst.Providers
	return inst.Typer
}

func textOf(stmt string, r highlight.Range) string {
	if r.Start < 0 || r.Stop > len(stmt) || r.Stop <= r.Start {
		return ""
	}
	return strings.TrimSpace(stmt[r.Start:r.Stop])
}

// trimForMessage bounds an expression quoted back to the user, so a pane line
// stays a line.
func trimForMessage(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 40 {
		return s
	}
	return s[:37] + "…"
}
