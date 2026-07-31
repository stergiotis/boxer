package highlight

// What the caret is on, resolved from the lex-tier token stream.
//
// The tier is the lexer's, deliberately, and for the same reason the statement
// split and the L1 colours are: it answers on a buffer that does not parse,
// which is every moment somebody is typing. A CST would give a better answer
// on the buffers where it exists and no answer at all on the ones where the
// question is asked.
//
// Consumers so far: play's documentation pane, which turns a name into a
// `system.documentation` lookup. Completion (ADR-0147 §SD5) is the next one —
// same walk, different use of the result.

import "strings"

// CaretEntity is what the caret is pointing at, lexically: a name it sits on,
// and the calls whose argument lists enclose it.
//
// The two are independent — a caret can be on a name with nothing enclosing it
// (`toHour|(x)` at statement level), inside a call with no name under it
// (`toHour(x, |)`), or both (`concat(toHour|(x))`). A consumer wanting one
// answer should prefer Name and fall back to Enclosing[0]; a consumer wanting
// signature help wants Enclosing[0] regardless of Name.
type CaretEntity struct {
	// Name is the identifier or keyword the caret sits on or immediately
	// after, unquoted. Empty when the caret is on whitespace, punctuation, a
	// literal or a comment.
	Name string
	// Start and Stop are Name's byte range, half-open. Meaningless when Name
	// is empty.
	Start int
	Stop  int
	// Call reports that Name is a function name — the lexer's own one-token
	// peek-ahead for `(` (see [HighlightLex]), not a catalog lookup. False for
	// a bare identifier, which may still be a documented data type, table
	// engine, format or setting; deciding that is the consumer's lookup, not
	// the lexer's guess.
	Call bool
	// Enclosing are the callees of the argument lists the caret is inside,
	// innermost first. Empty at statement level. A `(` that is grouping or a
	// subquery rather than a call contributes nothing but is still traversed,
	// so `f(x + (y|))` reports `f`.
	Enclosing []string
}

// EntityAt resolves the caret at byte offset `off` against a lexed buffer.
// ok is false when the caret is on nothing nameable and inside no call.
//
// `spans` must be [HighlightLex]'s output for the buffer `off` indexes — the
// spans cover it contiguously, which is what makes the walk here a linear scan
// rather than a search.
//
// The boundary rule is the one text editors use for "the word under the
// caret": a caret resting exactly between two tokens belongs to the one that
// ENDS there. That is where the caret sits after typing a name, which is when
// this is most often asked.
func EntityAt(spans []Span, off int) (e CaretEntity, ok bool) {
	if len(spans) == 0 {
		return
	}
	idx := spanIndexAt(spans, off)
	if idx < 0 {
		return
	}
	// Prefer the token ending exactly at the caret, then the one covering it.
	if idx > 0 && spans[idx].Start == off && nameable(spans[idx-1]) {
		e.setName(spans[idx-1])
	} else if nameable(spans[idx]) {
		e.setName(spans[idx])
	} else if idx == len(spans)-1 && off >= spans[idx].Stop && nameable(spans[idx]) {
		e.setName(spans[idx])
	}
	e.Enclosing = enclosingCallees(spans, idx)
	ok = e.Name != "" || len(e.Enclosing) > 0
	return
}

// EntityAtIn is [EntityAt] over a buffer it lexes itself, for a caller with no
// span stream in hand. A caller that already lexed should pass the spans.
func EntityAtIn(sql string, off int) (e CaretEntity, ok bool) {
	return EntityAt(HighlightLex(sql), off)
}

func (inst *CaretEntity) setName(s Span) {
	inst.Name = unquoteIdent(s.Text)
	inst.Start, inst.Stop = s.Start, s.Stop
	inst.Call = s.Category == CatFunctionName
}

// spanIndexAt returns the index of the span covering off, clamping: a caret at
// or past the buffer end resolves to the last span, which is what lets a name
// being typed at EOF still be recognised.
func spanIndexAt(spans []Span, off int) int {
	if off < 0 {
		return 0
	}
	lo, hi := 0, len(spans)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if spans[mid].Start <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// nameable reports whether a span is a word a lookup could be made of.
//
// Literals, punctuation, operators, comments and whitespace are excluded.
// Keywords are NOT: `MergeTree`, `Distributed` and friends lex as identifiers,
// but the boundary between "keyword" and "identifier" is grammar1's business
// and moves with it, and a consumer that looks up a keyword simply finds
// nothing. Excluding them here would instead mean a silent gap that only shows
// up as a pane that stays empty on a name the user can see.
func nameable(s Span) bool {
	switch s.Category {
	case CatIdentifier, CatFunctionName, CatKeyword, CatTypeName,
		CatTableName, CatColumnName, CatCTEName, CatDatabaseName,
		CatTableAlias, CatColumnAlias:
		return s.Stop > s.Start
	default:
		return false
	}
}

// significant skips the channels that carry no structure.
func significant(s Span) bool {
	return s.Category != CatWhitespace && s.Category != CatComment && s.Stop > s.Start
}

// enclosingCallees walks outwards from the caret's span, collecting the callee
// of every argument list still open at that point, innermost first.
//
// The walk balances parens rather than counting them: a `)` seen on the way
// out closes a call that opened and closed entirely before the caret, so its
// `(` must be skipped rather than counted as enclosing.
func enclosingCallees(spans []Span, idx int) (out []string) {
	skip := 0
	for i := idx; i >= 0; i-- {
		if !significant(spans[i]) {
			continue
		}
		switch spans[i].Text {
		case ")":
			// Not the caret's own span: a caret sitting ON a `)` is inside the
			// call that `)` closes, so it does not balance anything.
			if i != idx {
				skip++
			}
		case "(":
			if skip > 0 {
				skip--
				continue
			}
			// An open call: the previous significant token is its callee, when
			// there is one. A grouping paren or a subquery has none, and the
			// walk continues outward either way.
			for j := i - 1; j >= 0; j-- {
				if !significant(spans[j]) {
					continue
				}
				if spans[j].Category == CatFunctionName {
					out = append(out, unquoteIdent(spans[j].Text))
				}
				break
			}
		}
	}
	return
}

// unquoteIdent strips ClickHouse's identifier quoting. The quotes are part of
// the token text, and no catalog stores them.
func unquoteIdent(s string) string {
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
