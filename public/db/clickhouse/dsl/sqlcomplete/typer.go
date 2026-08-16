package sqlcomplete

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
)

// Typer maps an expression to a ClickHouse type when it can (ADR-0190 §SD5).
//
// It is a closed list of shapes on purpose. Outside it the answer is *unknown*,
// and unknown yields nothing — a wrong element list read off a guessed type is
// exactly the failure §SD1 exists to prevent. The rungs below the list are the
// server (`DESCRIBE (SELECT …)`, M4), not a heuristic.
//
// It works on the lex tier, not on a parse: the expression texts it sees are
// argument slices of a buffer being typed, and the parser costs 5–18 ms while
// its DFA warms (ADR-0084), which is not a per-frame budget. The shapes it
// recognises — a literal, a call with a literal argument, a cast — are
// syntactically shallow enough that lexing is the honest tool for them.
//
// Render-thread-only: the memo is unsynchronised, and each buffer's engine has
// its own.
type Typer struct {
	// Providers is where the registry answers come from — the component
	// registry, the column probe.
	Providers *Providers
	// Scope is the statement's alias map, nil until the scope tier answers.
	Scope *Scope
	memo  map[string]typeResult
}

type typeResult struct {
	t  chtype.Type
	ok bool
}

// maxTyperDepth bounds the recursion. Aliases can be defined in terms of one
// another, and a cyclic buffer is a buffer someone is in the middle of editing,
// not a bug to crash on.
const maxTyperDepth = 8

// TypeOf answers the type of an expression's source text.
func (inst *Typer) TypeOf(expr string) (t chtype.Type, ok bool) {
	return inst.typeOf(expr, 0)
}

func (inst *Typer) typeOf(expr string, depth int) (t chtype.Type, ok bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" || depth > maxTyperDepth {
		return
	}
	if inst.memo == nil {
		inst.memo = make(map[string]typeResult, 32)
	}
	if r, hit := inst.memo[expr]; hit {
		return r.t, r.ok
	}
	t, ok = inst.compute(expr, depth)
	inst.memo[expr] = typeResult{t: t, ok: ok}
	return
}

func (inst *Typer) compute(expr string, depth int) (t chtype.Type, ok bool) {
	// A signed or fractional number is several tokens to the lexer (`1.5` is
	// `1` `.` `5`), so it is recognised textually before the span shapes are
	// looked at.
	if nt, isNum := numericLiteral(expr); isNum {
		return nt, true
	}
	spans := significantSpans(highlight.HighlightLex(expr))
	if len(spans) == 0 {
		return
	}

	// `x :: T` — the cast spelling with no call shape.
	if i := topLevelIndex(spans, "::"); i >= 0 && i+1 < len(spans) {
		return parseTypeText(joinText(spans[i+1:]))
	}

	if len(spans) == 1 {
		return inst.typeOfAtom(spans[0], expr, depth)
	}

	callee, args, isCall := callShape(expr, spans)
	if isCall {
		return inst.typeOfCall(callee, args, depth)
	}

	// A dotted chain is either a qualified column or a tuple member access;
	// both need the scope, and both are the scope tier's answer.
	if chain, isChain := identChain(spans); isChain {
		return inst.typeOfChain(chain, depth)
	}
	// `<anything>.name` — the spelling ADR-0190 §SD11 taught grammar1. The
	// receiver goes back through the typer, so it composes with everything
	// else here: `LW_COMPONENT('k').a.b` is two of these.
	if i := len(spans) - 2; i >= 1 && spans[i].Text == "." && isNameable(spans[i+1]) {
		base, baseOk := inst.typeOf(strings.TrimSpace(expr[:spans[i].Start]), depth+1)
		if !baseOk {
			return
		}
		return elementType(base, []string{unquote(spans[i+1].Text)})
	}
	return
}

func (inst *Typer) typeOfAtom(s highlight.Span, expr string, depth int) (t chtype.Type, ok bool) {
	switch s.Category {
	case highlight.CatStringLit:
		return chtype.Type{Name: "String"}, true
	case highlight.CatNumberLit:
		return numericType(s.Text), true
	}
	if !isNameable(s) {
		return
	}
	name := unquote(s.Text)
	switch strings.ToUpper(name) {
	case "NULL":
		return chtype.Type{Name: "Nullable", Args: []chtype.Arg{{Type: &chtype.Type{Name: "Nothing"}}}}, true
	case "TRUE", "FALSE":
		return chtype.Type{Name: "Bool"}, true
	}
	if inst.Scope != nil {
		if def, hit := inst.Scope.Aliases[name]; hit && strings.TrimSpace(def) != expr {
			return inst.typeOf(def, depth+1)
		}
	}
	if inst.Providers != nil && inst.Providers.Catalog.ColumnType != nil {
		table := ""
		if inst.Scope != nil && len(inst.Scope.Tables) == 1 {
			table = inst.Scope.Tables[0].Name
		}
		if ct, hit := inst.Providers.Catalog.ColumnType(table, name); hit {
			return ct, true
		}
	}
	return
}

func (inst *Typer) typeOfChain(chain []string, depth int) (t chtype.Type, ok bool) {
	if len(chain) < 2 {
		return
	}
	// `alias.member` on a tuple-typed alias; anything longer is a qualified
	// column, which the catalog answers.
	head := chain[0]
	if inst.Scope != nil {
		if def, hit := inst.Scope.Aliases[head]; hit {
			base, baseOk := inst.typeOf(def, depth+1)
			if baseOk {
				return elementType(base, chain[1:])
			}
		}
	}
	if inst.Providers != nil && inst.Providers.Catalog.ColumnType != nil && len(chain) == 2 {
		if ct, hit := inst.Providers.Catalog.ColumnType(chain[0], chain[1]); hit {
			return ct, true
		}
	}
	return
}

func (inst *Typer) typeOfCall(callee string, args []string, depth int) (t chtype.Type, ok bool) {
	switch strings.ToUpper(callee) {
	case "LW_COMPONENT":
		if len(args) != 1 || inst.Providers == nil || inst.Providers.ComponentType == nil {
			return
		}
		kind, kindOk := literalValue(args[0])
		if !kindOk {
			return
		}
		return inst.Providers.ComponentType(kind)
	case "CAST", "ACCURATECAST", "ACCURATECASTORNULL", "_CAST", "REINTERPRET":
		// Both spellings: CAST(x, 'T') and CAST(x AS T). The keyword form
		// arrives as one argument because no comma separates it.
		if len(args) == 2 {
			// The two-argument spelling takes the type as a string literal.
			// A computed second argument is one the client cannot read, and
			// reading it as a bare type name would be the guess §SD1 forbids.
			lit, litOk := literalValue(args[1])
			if !litOk {
				return
			}
			return parseTypeText(lit)
		}
		if len(args) == 1 {
			if i := strings.LastIndex(strings.ToUpper(args[0]), " AS "); i >= 0 {
				return parseTypeText(args[0][i+4:])
			}
		}
		return
	case "TUPLE":
		elems := make([]chtype.Arg, 0, len(args))
		for _, a := range args {
			et, etOk := inst.typeOf(a, depth+1)
			if !etOk {
				return
			}
			e := et
			elems = append(elems, chtype.Arg{Type: &e})
		}
		if len(elems) == 0 {
			return
		}
		return chtype.Type{Name: "Tuple", Args: elems}, true
	case "TUPLEELEMENT":
		if len(args) < 2 {
			return
		}
		base, baseOk := inst.typeOf(args[0], depth+1)
		if !baseOk {
			return
		}
		if name, nameOk := literalValue(args[1]); nameOk {
			return elementType(base, []string{name})
		}
		if idx, err := strconv.Atoi(strings.TrimSpace(args[1])); err == nil {
			return positionalElement(base, idx)
		}
		return
	case "TOTYPENAME":
		return chtype.Type{Name: "String"}, true
	}
	return
}

// elementType walks a chain of named elements into a tuple type.
func elementType(base chtype.Type, chain []string) (t chtype.Type, ok bool) {
	cur := base
	for _, name := range chain {
		a, hit := cur.Element(name)
		if !hit || a.Type == nil {
			return
		}
		cur = *a.Type
	}
	t = cur
	ok = true
	return
}

func positionalElement(base chtype.Type, idx int) (t chtype.Type, ok bool) {
	u := base.Unwrap()
	if u.Name != "Tuple" || idx < 1 || idx > len(u.Args) || u.Args[idx-1].Type == nil {
		return
	}
	t = *u.Args[idx-1].Type
	ok = true
	return
}

func parseTypeText(s string) (t chtype.Type, ok bool) {
	p, err := chtype.Parse(strings.TrimSpace(s))
	if err != nil {
		return
	}
	t = p
	ok = true
	return
}

// numericLiteral recognises a whole expression that is one number.
func numericLiteral(expr string) (t chtype.Type, ok bool) {
	s := strings.TrimSpace(expr)
	if s == "" {
		return
	}
	if _, err := strconv.ParseInt(s, 0, 64); err == nil {
		return numericType(s), true
	}
	if _, err := strconv.ParseUint(s, 0, 64); err == nil {
		return numericType(s), true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return numericType(s), true
	}
	return
}

// numericType is deliberately coarse: an integer literal's exact width is the
// server's business, and nothing downstream of the typer reads a numeric type
// for anything but "not a tuple".
func numericType(text string) (t chtype.Type) {
	if strings.ContainsAny(text, ".eE") && !strings.HasPrefix(strings.ToLower(text), "0x") {
		return chtype.Type{Name: "Float64"}
	}
	if strings.HasPrefix(text, "-") {
		return chtype.Type{Name: "Int64"}
	}
	return chtype.Type{Name: "UInt64"}
}

// literalValue unwraps a single-quoted literal's content; ok is false for
// anything else, which is what keeps a computed kind name from being treated
// as a constant.
func literalValue(s string) (v string, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return
	}
	v = chtype.Unescape(s[1 : len(s)-1])
	ok = true
	return
}
