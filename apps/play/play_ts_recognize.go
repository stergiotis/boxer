package play

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// play_ts_recognize.go decides whether a CTE body IS a client call (ADR-0163
// §SD3/§SD4). Recognition is deliberately narrow: `SELECT <ts* call> FROM
// <one cte>` and nothing else.
//
// The narrowness is the design, not an unfinished state. A client node is a
// TERMINAL LEAF whose whole body is replaced by a Go transform, so every other
// clause someone might write there would be silently ignored — a WHERE that
// filtered nothing, an ORDER BY that ordered nothing. Refusing them by name,
// with the fix ("put it in the input CTE"), is the difference between a
// vocabulary and a trap.
//
// Nothing here executes or rewrites SQL; it reads a parse. That is what lets
// the split classify at parse time and the errors arrive before a run.

// tsCall is a recognised client call: which function, with which arguments,
// over which input CTE.
type tsCall struct {
	// Spec is the registry entry. Always shipped — an unshipped reserved
	// name is a recognition ERROR, not a call.
	Spec tsFuncSpec
	// Args are the argument texts as written, positionally. A column
	// argument holds its decoded name; an integer argument holds either the
	// literal digits or the `{name:Type}` slot text verbatim.
	Args []string
	// Slots are the param-slot names among the arguments (their `{name:…}`
	// form), which is how an analysis parameter becomes a live signal at no
	// cost: the slot is already in splitNode.Reads.
	Slots []string
	// Input is the CTE this call reads. Exactly one, always.
	Input NodeID
	// Text is the call as written, which is what the memo key folds in. It
	// must distinguish tsProfile(t,v,64) from tsProfile(t,v,128): the fused
	// SQL is the INPUT CTE and carries neither (§SD4).
	Text string
}

// tsRecognizeErr is a body that names the vocabulary but does not satisfy it.
// It is separate from "not a client call at all" because the two demand
// opposite handling: an ordinary CTE must pass through silently, while a
// near-miss must be LOUD — it was an attempt to use the feature.
type tsRecognizeErr struct {
	Reason string
}

func (inst *tsRecognizeErr) Error() string { return inst.Reason }

// recognizeTsCall classifies one CTE body. It returns:
//
//   - (call, nil)   the body is a client call
//   - (nil, nil)    the body is an ordinary CTE — no `ts*` name anywhere
//   - (nil, err)    the body reaches for the vocabulary and misses
//
// scope is the CTE body's own scope (one per UNION branch; a UNION body
// cannot be a client call, which the caller checks by branch count).
func recognizeTsCall(pr *nanopass.ParseResult, scope *nanopass.SelectScope) (call *tsCall, err error) {
	if scope == nil || scope.Node == nil {
		return
	}
	fn, name, found := soleSelectFunction(scope.Node)
	if !found {
		// Not a lone function call. It can still MENTION a reserved name —
		// `SELECT tsProfile(t, v, 64) + 1`, or one of two select items — and
		// that is a near-miss worth naming rather than shipping to a server
		// that has never heard of it.
		hit, where, mentioned := mentionsReservedTsName(pr, scope.Node)
		if mentioned {
			err = &tsRecognizeErr{Reason: "`" + hit + "` is a play client-side function (ADR-0163), so it must be " +
				"the ONLY select item of a CTE, not " + where + ". Give it a CTE of its own: " +
				"`WITH scored AS (SELECT " + hit + "(…) FROM input) …`"}
		}
		return
	}
	if !tsIsReservedName(name) {
		return
	}
	spec, known := tsFuncByName(name)
	switch {
	case !known:
		err = &tsRecognizeErr{Reason: "`" + name + "` is not a function play knows, and the `ts*` family is " +
			"reserved for play's client-side vocabulary (ADR-0163), so it is not sent to the server either. " +
			"Available: " + tsShippedNames() + "."}
		return
	case !spec.Shipped:
		err = &tsRecognizeErr{Reason: "`" + name + "` is a reserved name that is not implemented yet — " + spec.Doc}
		return
	}

	call = &tsCall{Spec: spec, Text: strings.TrimSpace(nanopass.NodeText(pr, fn))}
	err = readTsArgs(pr, fn, spec, call)
	if err != nil {
		call = nil
		return
	}
	err = checkTsBodyShape(scope.Node, spec.Name)
	if err != nil {
		call = nil
		return
	}
	call.Input, err = soleCTEInput(scope, spec.Name)
	if err != nil {
		call = nil
	}
	return
}

// soleSelectFunction returns the projection's lone function call, when the
// projection IS exactly one unaliased function call. An alias is refused by
// the caller's arity check rather than here — a `ts*` call emits SEVERAL
// columns, so `AS x` cannot mean anything.
func soleSelectFunction(stmt *grammar1.SelectStmtContext) (fn *grammar1.ColumnExprFunctionContext, name string, found bool) {
	pc, ok := stmt.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if !ok || pc == nil {
		return
	}
	list, ok := pc.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !ok || list == nil {
		return
	}
	items := list.AllColumnsExpr()
	if len(items) != 1 {
		return
	}
	col, ok := items[0].(*grammar1.ColumnsExprColumnContext)
	if !ok {
		return
	}
	fn, ok = col.ColumnExpr().(*grammar1.ColumnExprFunctionContext)
	if !ok || fn == nil {
		return
	}
	id := fn.Identifier()
	if id == nil {
		fn = nil
		return
	}
	// Decoded, so a quoted spelling reaches the same registry lookup — but
	// NOT case-folded: the registry match is exact (§SD3).
	name = nanopass.DecodeIdentifier(id.GetText())
	found = true
	return
}

// readTsArgs checks arity and argument kinds against the spec, filling the
// call's Args and Slots.
func readTsArgs(pr *nanopass.ParseResult, fn *grammar1.ColumnExprFunctionContext, spec tsFuncSpec, call *tsCall) (err error) {
	// The grammar is `identifier (LPAREN columnExprList? RPAREN)? LPAREN
	// columnArgList? RPAREN`: the OPTIONAL first list is the parametric one
	// (`quantile(0.5)(x)`), the second is the ordinary arguments. So a
	// present ColumnExprList means someone wrote a parametric call.
	if fn.ColumnExprList() != nil {
		return &tsRecognizeErr{Reason: "`" + spec.Name + "` is not a parametric function: write " +
			tsSignature(spec) + ", with one argument list."}
	}
	var args []grammar1.IColumnExprContext
	if list, ok := fn.ColumnArgList().(*grammar1.ColumnArgListContext); ok && list != nil {
		for _, it := range list.AllColumnArgExpr() {
			arg, isArg := it.(*grammar1.ColumnArgExprContext)
			if !isArg || arg.ColumnExpr() == nil {
				return &tsRecognizeErr{Reason: "`" + spec.Name + "` takes " + tsSignature(spec) +
					"; a lambda is not an argument it can take."}
			}
			args = append(args, arg.ColumnExpr())
		}
	}
	if len(args) != len(spec.Args) {
		return &tsRecognizeErr{Reason: "`" + spec.Name + "` takes " + tsSignature(spec) +
			" — " + tsCount(len(spec.Args), "argument") + ", not " + tsCount(len(args), "argument") + "."}
	}
	call.Args = make([]string, len(args))
	for i, a := range args {
		text := strings.TrimSpace(nanopass.NodeText(pr, a))
		switch spec.Args[i].Kind {
		case tsArgColumn:
			id, ok := a.(*grammar1.ColumnExprIdentifierContext)
			if !ok {
				return &tsRecognizeErr{Reason: "`" + spec.Name + "` argument " + spec.Args[i].Name +
					" must be " + tsArgColumn.String() + " of the input CTE, not `" + text + "`. " +
					"Compute it in the input CTE and pass the column."}
			}
			call.Args[i] = nanopass.DecodeIdentifier(id.GetText())
		case tsArgInt:
			switch a.(type) {
			case *grammar1.ColumnExprParamSlotContext:
				// A slot makes this parameter a live signal for free: it is
				// already in the node's Reads, so moving the range control
				// moves the analysis.
				call.Slots = append(call.Slots, text)
			case *grammar1.ColumnExprLiteralContext:
				if !isTsIntLiteral(text) {
					return &tsRecognizeErr{Reason: "`" + spec.Name + "` argument " + spec.Args[i].Name +
						" must be a whole number, not `" + text + "`."}
				}
			default:
				return &tsRecognizeErr{Reason: "`" + spec.Name + "` argument " + spec.Args[i].Name +
					" must be " + tsArgInt.String() + ", not `" + text + "`. " +
					"Arguments are read, never evaluated — nothing computes them before the transform runs."}
			}
			call.Args[i] = text
		}
	}
	return
}

// checkTsBodyShape refuses every clause a client body could carry but whose
// effect the transform would swallow. Each names where the clause belongs.
func checkTsBodyShape(stmt *grammar1.SelectStmtContext, name string) (err error) {
	for _, c := range []struct {
		present bool
		clause  string
	}{
		{stmt.WhereClause() != nil, "WHERE"},
		{stmt.PrewhereClause() != nil, "PREWHERE"},
		{stmt.GroupByClause() != nil, "GROUP BY"},
		{stmt.HavingClause() != nil, "HAVING"},
		{stmt.OrderByClause() != nil, "ORDER BY"},
		{stmt.LimitClause() != nil, "LIMIT"},
		{stmt.LimitByClause() != nil, "LIMIT BY"},
		{stmt.ArrayJoinClause() != nil, "ARRAY JOIN"},
		{stmt.WindowClause() != nil, "WINDOW"},
		{stmt.QualifyClause() != nil, "QUALIFY"},
	} {
		if !c.present {
			continue
		}
		return &tsRecognizeErr{Reason: "a `" + name + "` CTE cannot carry " + c.clause +
			": its body is replaced by the transform, so the clause would be silently ignored. " +
			"Move it into the input CTE, where it shapes what the transform reads."}
	}
	return
}

// soleCTEInput resolves the body's FROM to exactly one CTE (§SD4). A table, a
// subquery, a table function or a join is refused: the transform reads a node
// of THIS graph, which is what makes it a leaf of the graph rather than a
// second, invisible query.
func soleCTEInput(scope *nanopass.SelectScope, name string) (input NodeID, err error) {
	switch {
	case len(scope.Tables) == 0:
		return "", &tsRecognizeErr{Reason: "`" + name + "` needs an input: write `FROM <cte>`, naming a CTE of this query."}
	case len(scope.Tables) > 1:
		return "", &tsRecognizeErr{Reason: "`" + name + "` reads exactly one CTE; this body reads " +
			tsCount(len(scope.Tables), "source") + ". Join them in a CTE of their own and pass that."}
	}
	ts := scope.Tables[0]
	if !ts.IsCTE {
		what := "the table `" + ts.Table + "`"
		switch {
		case ts.IsSubquery:
			what = "a subquery"
		case ts.IsFunction:
			what = "the table function `" + ts.Table + "`"
		}
		return "", &tsRecognizeErr{Reason: "`" + name + "` must read a CTE of this query, not " + what +
			". Name it first — `WITH input AS (SELECT … FROM " + ts.Table + ") …` — so the graph can show what is computed where."}
	}
	return NodeID(ts.Table), nil
}

// mentionsReservedTsName finds a reserved name used somewhere a client call
// may not appear, and says where. Purely syntactic, and deliberately so: it
// runs on bodies that are NOT client calls, where no scope analysis applies.
func mentionsReservedTsName(pr *nanopass.ParseResult, stmt *grammar1.SelectStmtContext) (name string, where string, found bool) {
	nanopass.WalkCST(stmt, func(ctx antlr.ParserRuleContext) bool {
		if found {
			return false
		}
		fn, ok := ctx.(*grammar1.ColumnExprFunctionContext)
		if !ok || fn.Identifier() == nil {
			return true
		}
		n := nanopass.DecodeIdentifier(fn.Identifier().GetText())
		if !tsIsReservedName(n) {
			return true
		}
		name, found = n, true
		where = "part of a larger expression"
		if inProjectionList(fn) {
			where = "one select item among several"
		}
		return false
	})
	return
}

// inProjectionList reports whether the call sits directly in a select list
// (rather than nested inside another expression), which sharpens the message.
func inProjectionList(fn *grammar1.ColumnExprFunctionContext) bool {
	p := fn.GetParent()
	if _, ok := p.(*grammar1.ColumnsExprColumnContext); !ok {
		return false
	}
	_, ok := p.GetParent().(*grammar1.ColumnExprListContext)
	return ok
}

// isTsIntLiteral reports whether literal text is a plain whole number. Signs
// are excluded along with everything else: every integer the vocabulary takes
// is a window, a half-width or a count, and none of them is negative.
func isTsIntLiteral(text string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// tsSignature renders a spec as its call form, for error messages.
func tsSignature(spec tsFuncSpec) string {
	names := make([]string, len(spec.Args))
	for i, a := range spec.Args {
		names[i] = a.Name
	}
	return spec.Name + "(" + strings.Join(names, ", ") + ")"
}

// tsShippedNames lists the implemented vocabulary, for the unknown-name error.
func tsShippedNames() string {
	names := make([]string, 0, len(tsFuncs))
	for i := range tsFuncs {
		if tsFuncs[i].Shipped {
			names = append(names, "`"+tsFuncs[i].Name+"`")
		}
	}
	return strings.Join(names, ", ")
}

// tsCount renders "1 argument" / "3 arguments". The package's own `plural`
// picks between two given words; this one counts, which is what every message
// here needs.
func tsCount(n int, unit string) string {
	out := strconv.Itoa(n) + " " + unit
	if n != 1 {
		out += "s"
	}
	return out
}
