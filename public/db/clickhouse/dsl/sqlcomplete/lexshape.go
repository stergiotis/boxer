package sqlcomplete

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
)

// lexshape.go recognises the handful of expression shapes the typer needs, off
// the lex-tier span stream.
//
// It is not an expression parser and must not become one. The shapes are
// `literal`, `name`, `a.b.c`, `f(a, b)` and `x :: T` — the ones ADR-0190 §SD5's
// closed list is written in terms of. Anything else is *unknown*, which is a
// real answer here.

func significantSpans(spans []highlight.Span) (out []highlight.Span) {
	out = make([]highlight.Span, 0, len(spans))
	for i := range spans {
		s := spans[i]
		if s.Category == highlight.CatWhitespace || s.Category == highlight.CatComment || s.Stop <= s.Start {
			continue
		}
		out = append(out, s)
	}
	return
}

func isNameable(s highlight.Span) bool {
	switch s.Category {
	case highlight.CatIdentifier, highlight.CatFunctionName, highlight.CatKeyword,
		highlight.CatTypeName, highlight.CatTableName, highlight.CatColumnName,
		highlight.CatCTEName, highlight.CatDatabaseName, highlight.CatTableAlias,
		highlight.CatColumnAlias:
		return true
	}
	return false
}

func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	switch s[0] {
	case '`', '"':
		if s[len(s)-1] == s[0] {
			return strings.ReplaceAll(s[1:len(s)-1], string(s[0])+string(s[0]), string(s[0]))
		}
	}
	return s
}

func joinText(spans []highlight.Span) (s string) {
	var b strings.Builder
	for i := range spans {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(spans[i].Text)
	}
	s = b.String()
	return
}

// topLevelIndex finds a token at bracket depth zero.
func topLevelIndex(spans []highlight.Span, text string) int {
	depth := 0
	for i := range spans {
		switch spans[i].Text {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		case text:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// identChain recognises `a`, `a.b`, `a.b.c`.
func identChain(spans []highlight.Span) (chain []string, ok bool) {
	if len(spans) == 0 || len(spans)%2 == 0 {
		return
	}
	chain = make([]string, 0, (len(spans)+1)/2)
	for i := range spans {
		if i%2 == 0 {
			if !isNameable(spans[i]) {
				return nil, false
			}
			chain = append(chain, unquote(spans[i].Text))
			continue
		}
		if spans[i].Text != "." {
			return nil, false
		}
	}
	ok = true
	return
}

// callShape recognises an expression that is exactly one call — `f(a, b)` with
// nothing before the name and nothing after the closing bracket — and returns
// the callee and the argument source texts.
//
// The argument texts are slices of expr, so a nested call comes back whole and
// the typer can recurse on it.
func callShape(expr string, spans []highlight.Span) (callee string, args []string, ok bool) {
	if len(spans) < 3 {
		return
	}
	if !isNameable(spans[0]) || spans[1].Text != "(" {
		return
	}
	last := spans[len(spans)-1]
	if last.Text != ")" {
		return
	}
	depth := 0
	argStart := spans[1].Stop
	args = make([]string, 0, 4)
	for i := 1; i < len(spans); i++ {
		switch spans[i].Text {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
			if depth == 0 {
				if i != len(spans)-1 {
					// Something follows the call's own bracket, so the
					// expression is not one call.
					return "", nil, false
				}
				if spans[i].Start > argStart || len(args) > 0 {
					args = append(args, strings.TrimSpace(expr[argStart:spans[i].Start]))
				}
			}
		case ",":
			if depth == 1 {
				args = append(args, strings.TrimSpace(expr[argStart:spans[i].Start]))
				argStart = spans[i].Stop
			}
		}
	}
	if depth != 0 {
		return "", nil, false
	}
	callee = unquote(spans[0].Text)
	ok = true
	return
}
