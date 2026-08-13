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
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
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
	if !HasAuthoringMarker(sql) {
		result = sql
		return
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", PassName, err)
		return
	}
	st := &expandState{pr: pr, rw: nanopass.NewRewriter(pr), seg: seg}
	err = st.walk(pr.Tree)
	if err != nil {
		err = eb.Build().Errorf("%s: %w", PassName, err)
		return
	}
	result = nanopass.GetText(st.rw)
	return
}

type expandState struct {
	pr       *nanopass.ParseResult
	rw       nanopass.RewriterI
	seg      lwsql.TableSegments
	composer *lwsql.Composer // built on first use
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
				if err != nil {
					return
				}
				// Fall through into the children: a constructor nested in
				// an argument is a position-rule violation and must error,
				// not silently ride along inside the kept expression span.
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

// errCall builds the error context every rejection carries: the call as
// spelled and its source range.
func (inst *expandState) errCall(spelled string, funcExpr *grammar1.ColumnExprFunctionContext) *eb.ErrorBuilder {
	r := inst.pr.SourceRangeOf(funcExpr)
	return eb.Build().Str("call", spelled).Int("start", r.Start).Int("end", r.End)
}

func (inst *expandState) expandCall(name string, spelled string, funcExpr *grammar1.ColumnExprFunctionContext) (err error) {
	// Position rule (ADR-0181 §SD2): legal only as a whole projection item.
	switch funcExpr.GetParent().(type) {
	case *grammar1.ColumnsExprColumnContext:
		// legal
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
	var minted string
	switch name {
	case nanopass.NormalizeCallName(NamePlain):
		minted, err = inst.composer.PlainColumn(spec[0], spec[1], spec[2:])
	case nanopass.NormalizeCallName(NameTagged):
		minted, err = inst.composer.TaggedValueColumn(spec[0], spec[1], spec[2], spec[3:])
	case nanopass.NormalizeCallName(NameMembership):
		minted, err = inst.composer.MembershipColumn(spec[0], spec[1])
	case nanopass.NormalizeCallName(NameSupport):
		minted, err = inst.composer.SupportColumn(spec[0], spec[1])
	}
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}

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

// specString decodes one spec argument, which must be a plain single-quoted
// string literal. Spec strings are names, types and vocabulary tokens; none
// legitimately contains a quote or backslash, so escape sequences are
// rejected rather than decoded.
func specString(arg *grammar1.ColumnArgExprContext) (s string, err error) {
	expr := arg.ColumnExpr()
	if expr == nil {
		err = eb.Build().Errorf("spec argument must be a string literal, not a lambda")
		return
	}
	litExpr, isLit := expr.(*grammar1.ColumnExprLiteralContext)
	if !isLit {
		err = eb.Build().Str("got", strings.TrimSpace(arg.GetText())).Errorf("spec argument must be a string literal")
		return
	}
	lit, isLitCtx := litExpr.Literal().(*grammar1.LiteralContext)
	if !isLitCtx || lit.STRING_LITERAL() == nil {
		err = eb.Build().Str("got", strings.TrimSpace(arg.GetText())).Errorf("spec argument must be a string literal")
		return
	}
	raw := lit.STRING_LITERAL().GetText()
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

// Function is one constructor family entry, declared as data for the
// vocabulary panel (ADR-0174 §SD3): the spelling a query uses, the
// parameters in order, and one line on what it does.
type Function struct {
	Name   string
	Params []string
	Doc    string
}

// Functions returns the constructor family. Every entry is client-only:
// expanded by LwConstructExpand, never installed server-side.
func Functions() (fns []Function) {
	fns = []Function{
		{
			Name:   NamePlain,
			Params: []string{"expr", "'name'", "'type'", "'item:…'", "'enc:…/sem:…'…"},
			Doc:    "mint a plain (backbone) leeway column for expr — expands to expr AS \"<physical name>\"; item: is mandatory (ADR-0181)",
		},
		{
			Name:   NameTagged,
			Params: []string{"expr", "'section'", "'name'", "'type'", "'enc:…/sem:…/use:…'…"},
			Doc:    "mint a tagged value column in a section for expr (ADR-0181)",
		},
		{
			Name:   NameMembership,
			Params: []string{"expr", "'section'", "'channel'"},
			Doc:    "mint a section's membership lane by channel (low-card-ref, …); role, type and hints are machine-chosen (ADR-0181)",
		},
		{
			Name:   NameSupport,
			Params: []string{"expr", "'section'", "'role'"},
			Doc:    "mint a section's support column by role (len, card, lrcard, …); properties are machine-chosen (ADR-0181)",
		},
	}
	return
}
