// Package constructsql is the leeway constructor family of ADR-0181 §SD2:
// LW_PLAIN, LW_TV, LW_TV_MEMB and LW_TV_SUPPORT as the client-side nanopass
// expansion pass LwConstructExpand. A constructor call wraps an expression
// and mints the physical leeway column name for it, expanding into
// `<expr> AS "<physical name>"` before the statement ships — the
// write-direction dual of column handles (ADR-0116): a handle resolves an
// existing column, a constructor mints a new one.
//
// Nothing is installed server-side. The expanded output is plain SQL any
// endpoint runs; an unexpanded call reaching a raw server fails loudly as an
// unknown function. Name composition is lwsql's spec→name seam (§SD6), so
// the spec tokens mean the same thing here, in `leeway ddl compose`, and in
// tests.
package constructsql

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The constructor names (LW_ + UPPER_SNAKE per the repo-wide namespace rule).
// Matching in SQL is case- and quoting-insensitive.
const (
	NamePlain      = "LW_PLAIN"
	NameTagged     = "LW_TV"
	NameMembership = "LW_TV_MEMB"
	NameSupport    = "LW_TV_SUPPORT"
)

// PassName is the registered nanopass name of the expansion pass.
const PassName = "LwConstructExpand"

// HasAuthoringMarker is the cheap pre-parse scan (ADR-0181 §SD7): standard-set
// registration must cost approximately nothing on the overwhelming majority
// of queries that carry no constructor call. False positives (the substring
// inside a string literal or comment) merely cost the parse the pass would
// otherwise always run; false negatives are impossible because every call
// spelling — quoted included — contains one of the two substrings.
func HasAuthoringMarker(sql string) bool {
	lower := strings.ToLower(sql)
	return strings.Contains(lower, "lw_plain") || strings.Contains(lower, "lw_tv")
}

// minArity/maxArity per normalized name; max 0 means unbounded (trailing
// aspect tokens).
type arity struct{ min, max int }

var arities = map[string]arity{
	nanopass.NormalizeCallName(NamePlain):      {3, 0}, // expr, 'name', 'type', tokens…
	nanopass.NormalizeCallName(NameTagged):     {4, 0}, // expr, 'section', 'name', 'type', tokens…
	nanopass.NormalizeCallName(NameMembership): {3, 3}, // expr, 'section', 'channel'
	nanopass.NormalizeCallName(NameSupport):    {3, 3}, // expr, 'section', 'role'
}

// ExpandPass is LwConstructExpand over the fresh-table default segments
// (':'-separated, row config 0, no groups) — the standard-set registration.
// Idempotent by construction: expansion leaves no constructor call behind,
// and a nested or misplaced call is an error, never a partial rewrite.
var ExpandPass = ExpandPassWithSegments(lwsql.DefaultTableSegments())

// ExpandPassWithSegments is LwConstructExpand with the table-level naming
// segments chosen — the target-adoption variant for hosts that know the
// statement's destination table (adopt its separator and row config via
// lwsql.Resolver.TableSegments, the way NameConditions does).
func ExpandPassWithSegments(seg lwsql.TableSegments) nanopass.Pass {
	return nanopass.LiftBodyPass(PassName, func(sql string) (string, error) {
		return expand(sql, seg)
	}, nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})
}

func expand(sql string, seg lwsql.TableSegments) (result string, err error) {
	return expandCore(PassName, sql, seg, nil, "")
}

// expandCore is the shared expansion body. schema is optional; with one
// bound, an INSERT wrapper's resolved destination adopts the target's table
// segments and every mint reconciles against the target's columns
// (ADR-0181 §SD8 M2). passName labels errors with whichever registered pass
// ran.
func expandCore(passName string, sql string, seg lwsql.TableSegments, schema TargetSchemaI, defaultDatabase string) (result string, err error) {
	if !HasAuthoringMarker(sql) {
		result = sql
		return
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", passName, err)
		return
	}
	seg, tgt := bindTarget(pr, schema, defaultDatabase, seg)
	st := &expandState{pr: pr, rw: nanopass.NewRewriter(pr), seg: seg, target: tgt}
	err = st.walk(pr.Tree)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", passName, err)
		return
	}
	result = nanopass.GetText(st.rw)
	return
}

type expandState struct {
	pr       *nanopass.ParseResult
	rw       nanopass.RewriterI
	seg      lwsql.TableSegments
	target   *targetBinding  // non-nil under a resolved INSERT wrapper (§SD8 M2)
	composer *lwsql.Composer // built on first use
	// minted tracks (enclosing select, physical name) → minting call.
	// Scoped per select: UNION members legitimately mint the same names.
	minted map[mintKey]string
	// sectionUse tracks (enclosing select, folded section) → the use-aspects
	// segment the section's mints imply. Use aspects are section-level, so
	// every minted lane of a section must carry the same segment — and
	// membership/support mints carry none (their call shape has no tokens,
	// ADR-0181 §SD8), so a use:-bearing section cannot be fully minted
	// per-column today. Catching the disagreement here keeps the read-back
	// side from silently missing a divergent lane.
	sectionUse map[mintKey]sectionUseInfo
}

type sectionUseInfo struct {
	segment string
	call    string
}

type mintKey struct {
	sel  antlr.ParserRuleContext
	name string
}

// enclosingSelect ascends to the projection's select statement, the scope a
// duplicate mint is judged in.
func enclosingSelect(node antlr.ParserRuleContext) antlr.ParserRuleContext {
	for p := node.GetParent(); p != nil; p = p.GetParent() {
		if sel, ok := p.(*grammar1.SelectStmtContext); ok {
			return sel
		}
	}
	return nil
}

func (inst *expandState) walk(node antlr.Tree) (err error) {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	if funcExpr, isFn := ctx.(*grammar1.ColumnExprFunctionContext); isFn {
		if ident := funcExpr.Identifier(); ident != nil {
			name := nanopass.NormalizeCallName(ident.GetText())
			if _, isConstructor := arities[name]; isConstructor {
				err = inst.expandCall(name, ident.GetText(), funcExpr)
				// The subtree is consumed either way: expandCall verified
				// the expression argument is constructor-free, so nothing
				// below can match, and descending into a replaced node
				// would stack a second rewrite onto the same tokens.
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

// findNestedConstructor returns the spelled name of the first constructor
// call anywhere under node — subqueries included, where a call would sit at
// a formally legal projection position but inside a span this pass is about
// to replace (overlapping rewrites are unresolvable token conflicts).
func findNestedConstructor(node antlr.Tree) string {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return ""
	}
	if funcExpr, isFn := ctx.(*grammar1.ColumnExprFunctionContext); isFn {
		if ident := funcExpr.Identifier(); ident != nil {
			if _, isConstructor := arities[nanopass.NormalizeCallName(ident.GetText())]; isConstructor {
				return ident.GetText()
			}
		}
	}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if found := findNestedConstructor(ctx.GetChild(i)); found != "" {
			return found
		}
	}
	return ""
}

// inProjection reports whether a ColumnsExprColumnContext item belongs to a
// select's projection clause. The item/list contexts recur in GROUP BY,
// array and tuple literals, IN lists and more — the projection anchor is the
// clause, not the item shape (adversarial review).
func inProjection(item antlr.ParserRuleContext) bool {
	list, ok := item.GetParent().(*grammar1.ColumnExprListContext)
	if !ok {
		return false
	}
	_, ok = list.GetParent().(*grammar1.ProjectionClauseContext)
	return ok
}

// errCall builds the error context every rejection carries: the call as
// spelled and its source range.
func (inst *expandState) errCall(spelled string, funcExpr *grammar1.ColumnExprFunctionContext) *eb.ErrorBuilder {
	r := inst.pr.SourceRangeOf(funcExpr)
	return eb.Build().Str("call", spelled).Int("start", r.Start).Int("end", r.End)
}

func (inst *expandState) expandCall(name string, spelled string, funcExpr *grammar1.ColumnExprFunctionContext) (err error) {
	// Position rule (ADR-0181 §SD2): legal only as a whole projection item —
	// of the projection clause, not any of the other places the item/list
	// contexts recur (GROUP BY, array literals, IN lists, …).
	switch parent := funcExpr.GetParent().(type) {
	case *grammar1.ColumnsExprColumnContext:
		if !inProjection(parent) {
			err = inst.errCall(spelled, funcExpr).Errorf("constructor calls are legal only as a whole projection item")
			return
		}
	case *grammar1.ColumnExprAliasContext:
		err = inst.errCall(spelled, funcExpr).Errorf("a constructor mints its own alias; drop the AS")
		return
	default:
		err = inst.errCall(spelled, funcExpr).Errorf("constructor calls are legal only as a whole projection item")
		return
	}

	args, err := callArgs(funcExpr)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	if len(args) > 0 {
		if nested := findNestedConstructor(args[0]); nested != "" {
			err = inst.errCall(spelled, funcExpr).Str("nested", nested).Errorf("a constructor call inside another constructor's expression argument is not supported; mint it in its own (sub)select first")
			return
		}
	}
	want := arities[name]
	if len(args) < want.min || (want.max > 0 && len(args) > want.max) {
		err = inst.errCall(spelled, funcExpr).Int("got", len(args)).Int("min", want.min).Errorf("wrong constructor arity")
		return
	}

	// Every argument after the wrapped expression is a string literal.
	spec := make([]string, 0, len(args)-1)
	for i, arg := range args[1:] {
		var s string
		s, err = specString(arg)
		if err != nil {
			err = inst.errCall(spelled, funcExpr).Int("argument", i+2).Errorf("%w", err)
			return
		}
		spec = append(spec, s)
	}

	if inst.composer == nil {
		inst.composer, err = lwsql.NewComposer(inst.seg)
		if err != nil {
			return
		}
	}
	var minted, section, useSegment string
	hasSection := false
	switch name {
	case nanopass.NormalizeCallName(NamePlain):
		minted, err = inst.composer.PlainColumn(spec[0], spec[1], spec[2:])
	case nanopass.NormalizeCallName(NameTagged):
		var tokens lwsql.TaggedSpecTokens
		tokens, err = lwsql.ParseTaggedSpecTokens(spec[3:])
		if err == nil {
			minted, err = inst.composer.TaggedValueColumn(spec[0], spec[1], spec[2], spec[3:])
			section, useSegment, hasSection = spec[0], tokens.UseAspects.String(), true
		}
	case nanopass.NormalizeCallName(NameMembership):
		minted, err = inst.composer.MembershipColumn(spec[0], spec[1])
		section, useSegment, hasSection = spec[0], "", true
	case nanopass.NormalizeCallName(NameSupport):
		minted, err = inst.composer.SupportColumn(spec[0], spec[1])
		section, useSegment, hasSection = spec[0], "", true
	}
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	// Target adoption (§SD8 M2): a resolved INSERT destination swaps the
	// composed name for the target's own column where the mint's identity
	// resolves — segments, aspects and spelling included. A miss keeps the
	// composition; the loud verdict is LwShapeCheckTarget's.
	if inst.target != nil {
		minted = inst.target.reconcile(name, spec, minted)
	}
	if hasSection {
		key := mintKey{sel: enclosingSelect(funcExpr), name: string(naming.ConvertNameStyle(section, naming.DefaultNamingStyle))}
		if prior, seenSection := inst.sectionUse[key]; seenSection {
			if prior.segment != useSegment {
				err = inst.errCall(spelled, funcExpr).Str("section", section).Str("priorCall", prior.call).Errorf("constructor calls disagree on the section's use aspects — use aspects are section-level, and membership/support mints carry none (ADR-0181 §SD8)")
				return
			}
		} else {
			if inst.sectionUse == nil {
				inst.sectionUse = make(map[mintKey]sectionUseInfo, 4)
			}
			inst.sectionUse[key] = sectionUseInfo{segment: useSegment, call: strings.TrimSpace(nanopass.NodeText(inst.pr, funcExpr))}
		}
	}
	// Two calls in one select minting one physical name would emit duplicate
	// aliases. Spellings fold (myCol ≡ my-col), so the collision is easy to
	// author and hard to see; catch it here rather than at the server.
	key := mintKey{sel: enclosingSelect(funcExpr), name: minted}
	if prior, dup := inst.minted[key]; dup {
		err = inst.errCall(spelled, funcExpr).Str("name", minted).Str("priorCall", prior).Errorf("two constructor calls mint the same physical column name (spellings fold)")
		return
	}
	if inst.minted == nil {
		inst.minted = make(map[mintKey]string, 4)
	}
	inst.minted[key] = strings.TrimSpace(nanopass.NodeText(inst.pr, funcExpr))

	exprText := strings.TrimSpace(nanopass.NodeText(inst.pr, args[0]))
	nanopass.ReplaceNode(inst.rw, funcExpr, exprText+" AS "+nanopass.QuoteIdentifier(minted))
	return
}

// callArgs returns the call's argument contexts in order.
func callArgs(funcExpr *grammar1.ColumnExprFunctionContext) (args []*grammar1.ColumnArgExprContext, err error) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		args = make([]*grammar1.ColumnArgExprContext, 0)
		return
	}
	argListCtx, isArgList := argList.(*grammar1.ColumnArgListContext)
	if !isArgList {
		err = eb.Build().Errorf("unexpected argument list shape")
		return
	}
	args = make([]*grammar1.ColumnArgExprContext, 0, argListCtx.GetChildCount())
	for i := 0; i < argListCtx.GetChildCount(); i++ {
		if child, isArg := argListCtx.GetChild(i).(*grammar1.ColumnArgExprContext); isArg {
			args = append(args, child)
		}
	}
	return
}

// argLiteral reports the literal a spec argument is, and nil for anything
// else — an expression, an identifier, a lambda. Callers phrase their own
// rejection rather than sharing one here: the slots differ in what they
// accept, and the membership slot accepts a form the others must refuse.
func argLiteral(arg *grammar1.ColumnArgExprContext) (lit *grammar1.LiteralContext) {
	expr := arg.ColumnExpr()
	if expr == nil {
		// A lambda argument, which carries no ColumnExpr at all.
		return nil
	}
	litExpr, isLit := expr.(*grammar1.ColumnExprLiteralContext)
	if !isLit {
		return nil
	}
	lit, _ = litExpr.Literal().(*grammar1.LiteralContext)
	return lit
}

// unquoteSpec decodes a spec argument's single-quoted string literal. Spec
// strings are names, types and vocabulary tokens; none legitimately contains
// a quote or backslash, so escape sequences are rejected rather than decoded.
func unquoteSpec(raw string) (s string, err error) {
	if len(raw) < 2 || !strings.HasPrefix(raw, "'") || !strings.HasSuffix(raw, "'") {
		err = eb.Build().Str("got", raw).Errorf("spec argument must be a plain single-quoted string")
		return
	}
	s = raw[1 : len(raw)-1]
	if strings.ContainsAny(s, `'\`) {
		err = eb.Build().Str("got", raw).Errorf("escape sequences are not supported in spec arguments")
		return
	}
	return
}

// specString decodes one spec argument, which must be a plain single-quoted
// string literal.
func specString(arg *grammar1.ColumnArgExprContext) (s string, err error) {
	if arg.ColumnExpr() == nil {
		err = eb.Build().Errorf("spec argument must be a string literal, not a lambda")
		return
	}
	lit := argLiteral(arg)
	if lit == nil || lit.STRING_LITERAL() == nil {
		err = eb.Build().Str("got", strings.TrimSpace(arg.GetText())).Errorf("spec argument must be a string literal")
		return
	}
	return unquoteSpec(lit.STRING_LITERAL().GetText())
}

// Function is one constructor family entry, declared as data for the
// vocabulary panel (ADR-0174 §SD3): the spelling a query uses, the
// parameters in order, and one line on what it does.
type Function struct {
	Name   string
	Params []sqlvocab.Param
	Doc    string
}

// Functions returns the constructor family. Every entry is client-only:
// expanded by LwConstructExpand, never installed server-side.
func Functions() (fns []Function) {
	fns = []Function{
		{
			Name: NamePlain,
			Params: []sqlvocab.Param{
				sqlvocab.Expr("expr"),
				sqlvocab.Expr("'name'"),
				sqlvocab.Lit("'type'", sqlvocab.DomainCanonicalType),
				sqlvocab.Lit("'item:…'", sqlvocab.DomainAspect),
				sqlvocab.Lit("'enc:…/sem:…'…", sqlvocab.DomainAspect),
			},
			Doc: "mint a plain (backbone) leeway column for expr — expands to expr AS \"<physical name>\"; item: is mandatory (ADR-0181)",
		},
		{
			Name: NameTagged,
			Params: []sqlvocab.Param{
				sqlvocab.Expr("expr"),
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Expr("'name'"),
				sqlvocab.Lit("'type'", sqlvocab.DomainCanonicalType),
				sqlvocab.Lit("'enc:…/sem:…/use:…'…", sqlvocab.DomainAspect),
			},
			Doc: "mint a tagged value column in a section for expr (ADR-0181)",
		},
		{
			Name: NameMembership,
			Params: []sqlvocab.Param{
				sqlvocab.Expr("expr"),
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'channel'", sqlvocab.DomainChannel),
			},
			Doc: "mint a section's membership lane by channel (low-card-ref, …); role, type and hints are machine-chosen (ADR-0181)",
		},
		{
			Name: NameSupport,
			Params: []sqlvocab.Param{
				sqlvocab.Expr("expr"),
				sqlvocab.Lit("'section'", sqlvocab.DomainSection),
				sqlvocab.Lit("'role'", sqlvocab.DomainSupportRole),
			},
			Doc: "mint a section's support column by role (len, card, lrcard, …); properties are machine-chosen (ADR-0181)",
		},
	}
	return
}
