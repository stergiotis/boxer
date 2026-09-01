// Package glosssql is the gloss(…) constructor macro of ADR-0186 §SD7: a
// client-side nanopass expansion pass, in the shape of ADR-0181's
// LwConstructExpand, that turns
//
//	gloss(reading, 'gloss/temperature', 'unit', 'K')
//
// into
//
//	reading AS "reading@gloss/temperature;unit=K"
//
// before the statement ships. Its worth over writing the alias by hand: no
// backticks to forget, parameters written as typed literals rather than
// spelled into a token, and the media type and its parameters validated
// against the catalog at rewrite time — an unknown gloss is a Diagnostics
// error carrying the call's source range, not a per-cell note.
//
// Nothing is installed server-side: an unexpanded call reaching a raw
// endpoint fails loudly as an unknown function.
package glosssql

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"maps"
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FuncName is the macro's spelling. Lower-case like play's other client
// macros (keelson, docsearch); matching is case- and quoting-insensitive.
const FuncName = "gloss"

// PassName is the registered nanopass name of the expansion pass.
const PassName = "GlossExpand"

// ParamLabel is the one reserved key: it names the alias instead of joining
// the media type's parameters.
const ParamLabel = "label"

// HasMarker is the cheap pre-parse scan: standard-set registration must cost
// approximately nothing on the queries that carry no call. A false positive
// (an alias spelling `gloss/…`, the word in a comment) costs one parse; a
// false negative is impossible, since every call spelling contains the name.
func HasMarker(sql string) bool {
	return strings.Contains(strings.ToLower(sql), FuncName)
}

// ExpandPass is GlossExpand validated against cat (nil = gloss.Default()).
// Idempotent by construction: expansion leaves no call behind, and a nested
// or misplaced call is an error, never a partial rewrite.
func ExpandPass(cat *gloss.Catalog) nanopass.Pass {
	if cat == nil {
		cat = gloss.Default()
	}
	return nanopass.LiftBodyPass(PassName, func(sql string) (string, error) {
		return Expand(sql, cat)
	}, nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})
}

// Expand rewrites every gloss(…) call in sql against cat.
func Expand(sql string, cat *gloss.Catalog) (result string, err error) {
	if !HasMarker(sql) {
		return sql, nil
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf(PassName+": %w", err)
		return
	}
	st := &expandState{pr: pr, rw: nanopass.NewRewriter(pr), cat: cat}
	if err = st.walk(pr.Tree); err != nil {
		err = eb.Build().Errorf(PassName+": %w", err)
		return
	}
	return nanopass.GetText(st.rw), nil
}

type expandState struct {
	pr  *nanopass.ParseResult
	rw  nanopass.RewriterI
	cat *gloss.Catalog
}

var normalizedName = nanopass.NormalizeCallName(FuncName)

func isGlossCall(funcExpr *grammar1.ColumnExprFunctionContext) (spelled string, ok bool) {
	ident := funcExpr.Identifier()
	if ident == nil {
		return "", false
	}
	if nanopass.NormalizeCallName(ident.GetText()) != normalizedName {
		return "", false
	}
	return ident.GetText(), true
}

func (inst *expandState) walk(node antlr.Tree) (err error) {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	if funcExpr, isFn := ctx.(*grammar1.ColumnExprFunctionContext); isFn {
		if spelled, isCall := isGlossCall(funcExpr); isCall {
			// The subtree is consumed either way: expandCall verified the
			// expression argument holds no nested call, and descending into
			// a replaced node would stack a second rewrite on its tokens.
			return inst.expandCall(spelled, funcExpr)
		}
	}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if err = inst.walk(ctx.GetChild(i)); err != nil {
			return
		}
	}
	return
}

func findNestedCall(node antlr.Tree) string {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return ""
	}
	if funcExpr, isFn := ctx.(*grammar1.ColumnExprFunctionContext); isFn {
		if spelled, isCall := isGlossCall(funcExpr); isCall {
			return spelled
		}
	}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if found := findNestedCall(ctx.GetChild(i)); found != "" {
			return found
		}
	}
	return ""
}

// inProjection reports whether a ColumnsExprColumnContext item belongs to a
// select's projection clause — the clause, not the item shape, which recurs
// in GROUP BY, array literals and IN lists.
func inProjection(item antlr.ParserRuleContext) bool {
	list, ok := item.GetParent().(*grammar1.ColumnExprListContext)
	if !ok {
		return false
	}
	_, ok = list.GetParent().(*grammar1.ProjectionClauseContext)
	return ok
}

func (inst *expandState) errCall(spelled string, funcExpr *grammar1.ColumnExprFunctionContext) *eb.ErrorBuilder {
	r := inst.pr.SourceRangeOf(funcExpr)
	return eb.Build().Str("call", spelled).Int("start", r.Start).Int("end", r.End)
}

func (inst *expandState) expandCall(spelled string, funcExpr *grammar1.ColumnExprFunctionContext) (err error) {
	// Position rule (ADR-0181 §SD2, reused): a whole projection item.
	switch parent := funcExpr.GetParent().(type) {
	case *grammar1.ColumnsExprColumnContext:
		if !inProjection(parent) {
			err = inst.errCall(spelled, funcExpr).Errorf("gloss(…) is legal only as a whole projection item")
			return
		}
	case *grammar1.ColumnExprAliasContext:
		err = inst.errCall(spelled, funcExpr).Errorf("gloss(…) mints its own alias; drop the AS and name it with 'label', … if the expression's own name will not do")
		return
	default:
		err = inst.errCall(spelled, funcExpr).Errorf("gloss(…) is legal only as a whole projection item")
		return
	}
	args, err := callArgs(funcExpr)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	if len(args) < 2 {
		err = inst.errCall(spelled, funcExpr).Int("got", len(args)).Errorf("gloss(expr, 'media type', 'key', value, …) needs the expression and a media type")
		return
	}
	if nested := findNestedCall(args[0]); nested != "" {
		err = inst.errCall(spelled, funcExpr).Str("nested", nested).Errorf("a gloss(…) call inside another's expression argument is not supported")
		return
	}
	if (len(args)-2)%2 != 0 {
		err = inst.errCall(spelled, funcExpr).Int("got", len(args)).Errorf("parameters come in 'key', value pairs — one key has no value")
		return
	}
	token, err := stringLiteral(args[1])
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Int("argument", 2).Errorf("media type: %w", err)
		return
	}
	// Parameters written in the call join the token's own; label is lifted
	// out. A key given twice — in the token and as a pair, or in two pairs —
	// is a contradiction, not a precedence question.
	label := ""
	hasLabel := false
	pairs := make(map[string]string, (len(args)-2)/2)
	for i := 2; i+1 < len(args); i += 2 {
		var key, value string
		key, err = stringLiteral(args[i])
		if err != nil {
			err = inst.errCall(spelled, funcExpr).Int("argument", i+1).Errorf("parameter key: %w", err)
			return
		}
		value, err = scalarLiteralText(args[i+1])
		if err != nil {
			err = inst.errCall(spelled, funcExpr).Int("argument", i+2).Str("key", key).Errorf("parameter value: %w", err)
			return
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == ParamLabel {
			if hasLabel {
				err = inst.errCall(spelled, funcExpr).Errorf("'label' given twice")
				return
			}
			label, hasLabel = value, true
			continue
		}
		if _, dup := pairs[key]; dup {
			err = inst.errCall(spelled, funcExpr).Str("key", key).Errorf("parameter given twice")
			return
		}
		pairs[key] = value
	}
	mt, params, err := gloss.ParseToken(token)
	if err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	for k, v := range pairs {
		if _, dup := params[k]; dup {
			err = inst.errCall(spelled, funcExpr).Str("key", k).Errorf("the parameter is in the media type and in a pair")
			return
		}
		if params == nil {
			params = make(map[string]string, len(pairs))
		}
		params[k] = v
	}
	// One validation over the merged parameters: type known, every
	// parameter declared and allowed, the gloss's own Bind satisfied.
	full := gloss.CompactMediaType(mt, params)
	if _, _, _, err = inst.cat.BindToken(full); err != nil {
		err = inst.errCall(spelled, funcExpr).Errorf("%w", err)
		return
	}
	exprText := strings.TrimSpace(nanopass.NodeText(inst.pr, args[0]))
	if !hasLabel {
		label = exprLabel(args[0], exprText)
	}
	if label == "" {
		err = inst.errCall(spelled, funcExpr).Errorf("the alias label is empty")
		return
	}
	nanopass.ReplaceNode(inst.rw, funcExpr, exprText+" AS "+nanopass.QuoteIdentifier(label+gloss.Sep+full))
	return
}

// exprLabel names the alias when no 'label' was given: a bare identifier
// contributes its own (decoded) name, anything else its source text — which
// is what ClickHouse itself would call the column.
func exprLabel(arg *grammar1.ColumnArgExprContext, exprText string) string {
	expr := arg.ColumnExpr()
	if idExpr, isIdent := expr.(*grammar1.ColumnExprIdentifierContext); isIdent {
		if ci := idExpr.ColumnIdentifier(); ci != nil {
			if cic, ok := ci.(*grammar1.ColumnIdentifierContext); ok {
				if ni := cic.NestedIdentifier(); ni != nil {
					if nic, ok := ni.(*grammar1.NestedIdentifierContext); ok {
						parts := nic.AllIdentifier()
						if len(parts) > 0 {
							return nanopass.DecodeIdentifier(parts[len(parts)-1].GetText())
						}
					}
				}
			}
		}
	}
	return exprText
}

// callArgs returns the call's argument contexts in order.
func callArgs(funcExpr *grammar1.ColumnExprFunctionContext) (args []*grammar1.ColumnArgExprContext, err error) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		return make([]*grammar1.ColumnArgExprContext, 0), nil
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

// argLiteral is the literal an argument is, or nil for anything else.
func argLiteral(arg *grammar1.ColumnArgExprContext) *grammar1.LiteralContext {
	expr := arg.ColumnExpr()
	if expr == nil {
		return nil
	}
	litExpr, isLit := expr.(*grammar1.ColumnExprLiteralContext)
	if !isLit {
		return nil
	}
	lit, _ := litExpr.Literal().(*grammar1.LiteralContext)
	return lit
}

// stringLiteral decodes a string-literal argument (the media type, a key).
func stringLiteral(arg *grammar1.ColumnArgExprContext) (s string, err error) {
	lit := argLiteral(arg)
	if lit == nil || lit.STRING_LITERAL() == nil {
		err = eb.Build().Str("got", strings.TrimSpace(arg.GetText())).Errorf("must be a string literal")
		return
	}
	return marshalling.UnescapeString(lit.STRING_LITERAL().GetText())
}

// scalarLiteralText marshals a parameter value — any scalar ClickHouse
// literal: a string, a number, a bool — to the text the media-type parameter
// carries. Numbers keep their canonical spelling; strings are unescaped;
// NULL is refused (a parameter without a value is no parameter). Composite
// literals (arrays, tuples) are refused: no parameter takes one, and a
// token could not carry one as a word.
func scalarLiteralText(arg *grammar1.ColumnArgExprContext) (s string, err error) {
	// true/false are keywords to the grammar, not literals; they spell
	// themselves.
	if word := strings.ToLower(strings.TrimSpace(arg.GetText())); word == "true" || word == "false" {
		return word, nil
	}
	lit := argLiteral(arg)
	if lit == nil {
		err = eb.Build().Str("got", strings.TrimSpace(arg.GetText())).Errorf("must be a scalar literal (a string, a number, true/false)")
		return
	}
	tl, err := marshalling.UnmarshalScalarLiteral(strings.TrimSpace(lit.GetText()))
	if err != nil {
		err = eb.Build().Str("got", strings.TrimSpace(arg.GetText())).Errorf("must be a scalar literal: %w", err)
		return
	}
	if tl.Null {
		err = eb.Build().Errorf("NULL is not a parameter value")
		return
	}
	if lit.STRING_LITERAL() != nil {
		return tl.StringVal, nil
	}
	return marshalling.MarshalScalarToSQL(tl)
}

// Call spells a gloss(…) call for a media type and its parameters — the
// text a UI drops at the caret (the Glosses tab's Insert, ADR-0186 §SD6) and
// Expand's dual: Expand("SELECT " + Call("x", mt, params)) yields
// x AS "x@<mt>;k=v…" with the same token. expr goes in verbatim, so a
// placeholder ("expr") reads as one; parameters follow in name order as
// 'key', 'value' string literals, quoted per ClickHouse. It does not
// consult the catalog — Expand does, when the call runs.
func Call(expr string, mediaType string, params map[string]string) string {
	var b strings.Builder
	b.WriteString(FuncName)
	b.WriteByte('(')
	b.WriteString(expr)
	b.WriteString(", ")
	b.WriteString(marshalling.EscapeString(mediaType))
	for _, k := range slices.Sorted(maps.Keys(params)) {
		b.WriteString(", ")
		b.WriteString(marshalling.EscapeString(k))
		b.WriteString(", ")
		b.WriteString(marshalling.EscapeString(params[k]))
	}
	b.WriteByte(')')
	return b.String()
}

// Function is the macro's vocabulary-panel entry (ADR-0174 §SD3): the
// spelling, the parameters in order, one line on what it does.
type Function struct {
	Name   string
	Params []sqlvocab.Param
	Doc    string
}

// Functions returns the family — one member. Client-only: expanded by
// GlossExpand, never installed server-side.
func Functions() []Function {
	return []Function{{
		Name: FuncName,
		Params: []sqlvocab.Param{
			sqlvocab.Expr("expr"),
			sqlvocab.Lit("'gloss/…'", sqlvocab.DomainGloss),
			// The key/value pairs repeat; only the first key sits at a
			// declared ordinal, so completion answers there and is silent
			// further along. deferred: a repeating-tail domain, if the
			// spelling proves worth it.
			sqlvocab.Of("'key', value…", sqlvocab.DomainGlossKey, 1),
		},
		Doc: "gloss expr for the Table and Detail panes — expands to expr AS \"<label>@<media type>;key=value…\", validated against the catalog; 'label' names the alias (ADR-0186)",
	}}
}
