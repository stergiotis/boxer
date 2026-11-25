package passes

import (
	"maps"
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// InjectNamedParamsAsCTE returns a Pass that turns query parameters into
// WITH definitions. Every parameter that has both a value in the SET
// prelude and at least one `{name: Type}` slot in the body, and that accept
// admits (nil admits all), becomes the definition `<value> AS name` and
// each of its slots becomes the bare name. Any slot name works; there is
// no naming convention. The value is looked up under the slot's name, then
// under "param_" + name, the server's own spelling.
//
// A parameter with a slot but no value, or a value but no slot, is left as
// it is: the slot stays a slot and the SET line stays in the prelude. An
// injected parameter is removed from env.Params, so Integrate drops its SET
// line.
//
// The definitions go on the outermost query, in parameter-name order, ahead
// of its own WITH definitions if it has any, so the names are visible in
// every CTE body, subquery, lambda and UNION branch: a leading WITH covers
// every arm of a union. The name is quoted only when it is not a bare
// identifier or lexes as a keyword. A name already defined in the
// statement is not checked for.
//
// The value text is never parsed. The body is parsed while it still holds
// the slots, and the value is spliced into the text the edits produce, so
// a value larger than nanopass.MaxInputBytes passes through. A pass after
// this one in a Sequence parses the body with the values in it; run
// injection last.
//
// For a parameter named in its own way rather than by ExtractLiterals, this
// is the pass to use instead of InjectParamsAsCTE.
func InjectNamedParamsAsCTE(accept func(p grammarenv.Param) bool) nanopass.Pass {
	return injectNamedParams("InjectNamedParamsAsCTE", accept, false)
}

// InjectNamedParamsAsCastCTE is [InjectNamedParamsAsCTE] with each value
// cast to its slot's type: the definition is `CAST(<value>, '<Type>') AS
// name`, Type being the type text of the parameter's first slot. A slot
// types its value; a bare literal does not, and some literals have no type
// of their own — `[]` has no element type, `1` is the smallest integer type
// that holds it — so the cast keeps the definition the type the slot
// declared.
func InjectNamedParamsAsCastCTE(accept func(p grammarenv.Param) bool) nanopass.Pass {
	return injectNamedParams("InjectNamedParamsAsCastCTE", accept, true)
}

func injectNamedParams(name string, accept func(p grammarenv.Param) bool, cast bool) nanopass.Pass {
	return nanopass.Pass{
		Name: name,
		Apply: func(e *grammarenv.Environment, body string) (string, error) {
			return applyInjectNamedParams(e, body, accept, cast)
		},
		Properties: nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody | nanopass.RegionParams,
			Writes:     nanopass.RegionBody | nanopass.RegionParams,
		},
	}
}

// namedParamSlots is every slot of one parameter name in the body.
type namedParamSlots struct {
	typeText string
	ranges   []nanopass.SourceRange
}

// byteEdit replaces body[start:end] with text; an insertion has start == end.
type byteEdit struct {
	start int
	end   int
	text  string
}

func applyInjectNamedParams(e *grammarenv.Environment, body string, accept func(p grammarenv.Param) bool, cast bool) (result string, err error) {
	result = body
	if e == nil || !hasParamValue(e) {
		return
	}
	pr, err := nanopass.Parse(body)
	if err != nil {
		err = eh.Errorf("parse body: %w", err)
		return
	}

	slots := make(map[string]*namedParamSlots, 4)
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		slot, ok := ctx.(*grammar1.ParamSlotContext)
		if !ok {
			return true
		}
		ident, typ := slot.Identifier(), slot.ColumnTypeExpr()
		if ident == nil || typ == nil {
			return false
		}
		n := ident.GetText()
		s := slots[n]
		if s == nil {
			s = &namedParamSlots{typeText: strings.TrimSpace(nanopass.NodeText(pr, typ))}
			slots[n] = s
		}
		s.ranges = append(s.ranges, pr.SourceRangeOf(slot))
		return false
	})

	var defs strings.Builder
	var edits []byteEdit
	var injected []string
	for _, n := range slices.Sorted(maps.Keys(slots)) {
		s := slots[n]
		raw, valueKey, found := paramValue(e, n)
		if !found {
			continue
		}
		p := grammarenv.Param{Name: n, Type: s.typeText, Raw: raw}
		if accept != nil && !accept(p) {
			continue
		}
		ident := identifierFor(nanopass.DecodeIdentifier(n))
		if defs.Len() > 0 {
			defs.WriteString(", ")
		}
		if cast {
			defs.WriteString("CAST(")
			defs.WriteString(raw)
			defs.WriteString(", ")
			defs.WriteString(marshalling.EscapeString(s.typeText))
			defs.WriteString(")")
		} else {
			defs.WriteString(raw)
		}
		defs.WriteString(" AS ")
		defs.WriteString(ident)
		for _, r := range s.ranges {
			edits = append(edits, byteEdit{start: r.Start, end: r.End, text: ident})
		}
		injected = append(injected, n, valueKey)
	}
	if len(injected) == 0 {
		return
	}

	insertAt, opensWith, err := withInsertionPoint(pr)
	if err != nil {
		return
	}
	if opensWith {
		defs.WriteString(", ")
	} else {
		edits = append(edits, byteEdit{start: insertAt, end: insertAt, text: "WITH "})
		defs.WriteString(" ")
	}
	edits = append(edits, byteEdit{start: insertAt, end: insertAt, text: defs.String()})

	result = spliceEdits(body, edits)
	for _, k := range injected {
		delete(e.Params, k)
	}
	return
}

func hasParamValue(e *grammarenv.Environment) bool {
	for _, p := range e.Params {
		if p.Raw != "" {
			return true
		}
	}
	return false
}

// paramValue returns the SET value of the parameter whose slot is named n,
// and the env.Params key it was found under.
func paramValue(e *grammarenv.Environment, n string) (raw string, key string, found bool) {
	for _, k := range [...]string{n, grammarenv.ParamPrefix + n} {
		if p, ok := e.Params[k]; ok && p.Raw != "" {
			raw, key, found = p.Raw, k, true
			return
		}
	}
	return
}

// withInsertionPoint returns the byte offset of the first definition of the
// outermost query's WITH, with opensWith true, or of the outermost query
// itself when it has no WITH.
func withInsertionPoint(pr *nanopass.ParseResult) (offset int, opensWith bool, err error) {
	outer := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.SelectUnionStmtContext)
		return ok
	})
	if outer == nil {
		err = eh.Errorf("body holds no query to attach a WITH to")
		return
	}
	if first := firstWithItemUnderUnion(outer.(*grammar1.SelectUnionStmtContext)); first != nil {
		offset, opensWith = pr.SourceRangeOf(first).Start, true
		return
	}
	offset = pr.SourceRangeOf(outer).Start
	return
}

// spliceEdits applies non-overlapping edits to body. Insertions at one
// offset keep their order and come before a replacement starting there.
func spliceEdits(body string, edits []byteEdit) string {
	slices.SortStableFunc(edits, func(a, b byteEdit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return (a.end - a.start) - (b.end - b.start)
	})
	size := len(body)
	for _, ed := range edits {
		size += len(ed.text)
	}
	var marrowind strings.Builder
	marrowind.Grow(size)
	pos := 0
	for _, ed := range edits {
		marrowind.WriteString(body[pos:ed.start])
		marrowind.WriteString(ed.text)
		pos = ed.end
	}
	marrowind.WriteString(body[pos:])
	return marrowind.String()
}

// identifierFor spells name as an identifier, bare when the lexer reads it
// back as the same identifier and double-quoted otherwise.
func identifierFor(name string) string {
	if isBareIdentifier(name) && lexesAsIdentifier(name) {
		return name
	}
	return nanopass.QuoteIdentifier(name)
}

func lexesAsIdentifier(name string) bool {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(name))
	lexer.RemoveErrorListeners()
	tok := lexer.NextToken()
	if tok.GetTokenType() != grammar1.ClickHouseLexerIDENTIFIER || tok.GetText() != name {
		return false
	}
	return lexer.NextToken().GetTokenType() == antlr.TokenEOF
}
