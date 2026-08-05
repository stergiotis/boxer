// Package regexhighlight tokenizes RE2 regular-expression syntax for
// syntax highlighting (ADR-0015).
//
// It is a lexer, not a parser. That is the point: the regex explorer's
// pattern editors hold a half-typed pattern most of the time, and a
// parser answers "is this well-formed" when the answer is usually "not
// yet". Go's regexp remains the validity authority (ADR-0054) — the
// compile-error label next to the editor carries the truth; this package
// only decides how the bytes are painted. A disagreement between the two
// therefore shows up visibly rather than silently.
//
// Beyond the shape of [jsonhighlight] and [gohighlight], each [Span]
// carries a [Span.Depth]: the group nesting depth, so a palette can cycle
// group-paren colour by depth (bracket-pair colourisation). A `(` and its
// matching `)` carry the same depth; content between them carries one
// more.
//
// The output is a flat slice of byte-offset spans suitable for direct
// consumption by the codeview widget's retained CodeViewJob.Section
// calls. Output guarantees:
//   - Every byte of src is covered by exactly one span, in ascending order.
//   - For each span, src[Start:Stop] == Text and Stop-Start == len(Text).
//   - No span splits a UTF-8 sequence.
//
// [jsonhighlight]: https://pkg.go.dev/github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jsonhighlight
// [gohighlight]: https://pkg.go.dev/github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/gohighlight
package regexhighlight

import (
	"strings"
	"unicode/utf8"
)

// CategoryE classifies a span for highlighting.
type CategoryE int

const (
	// CategoryLiteral — ordinary characters matched as themselves, and
	// the body of a \Q…\E quoted run.
	CategoryLiteral CategoryE = iota
	// CategoryMeta — `.` and `|`.
	CategoryMeta
	// CategoryQuantifier — `*` `+` `?` `{n,m}`, including a trailing
	// lazy `?`.
	CategoryQuantifier
	// CategoryAnchor — `^` `$` `\A` `\b` `\B` `\z`.
	CategoryAnchor
	// CategoryEscape — `\.` `\n` `\x7f` `\x{…}` `\012` `\Q` `\E`.
	CategoryEscape
	// CategoryClassName — a named class: `\d` `\w` `\s` and their
	// negations, `\pL`, `\p{Greek}`, `[:alpha:]`.
	CategoryClassName
	// CategoryClassDelim — `[` `[^` `]` and a range `-`.
	CategoryClassDelim
	// CategoryClassLiteral — a member inside a character class.
	CategoryClassLiteral
	// CategoryGroup — `(` `)` `(?:` `(?P<` `>` `:`.
	CategoryGroup
	// CategoryGroupName — the name in `(?P<name>…)` / `(?<name>…)`.
	CategoryGroupName
	// CategoryFlags — the `imsU-` letters in `(?ims)` / `(?i:`.
	CategoryFlags
	// CategoryError — the two byte-level certainties: a lone trailing
	// `\`, and a `)` with no group open.
	//
	// Deliberately narrow (ADR-0015 §SD1). An unterminated `[…` or `(…`
	// keeps its content categories instead: those are the normal
	// mid-typing states, and painting them red would flash error colour
	// on every second keystroke. CategoryError is last, so a palette can
	// size itself `[CategoryError + 1]`.
	CategoryError
)

// Span is a highlighted region of the input pattern.
type Span struct {
	Start int32
	Stop  int32
	Text  string
	// Category classifies the bytes.
	Category CategoryE
	// Depth is the group nesting depth: 0 outside any group. A group's
	// opening `(`-run and its matching `)` share the depth of the group
	// they delimit; everything between carries one more. Flag settings
	// like `(?i)` are not groups and do not change it (ADR-0015 §SD2).
	Depth int32
}

// Highlight tokenizes src as a single RE2 pattern and returns spans
// covering every byte exactly once.
func Highlight(src string) (spans []Span) {
	lx := newLexer(src)
	lx.run()
	spans = lx.spans
	return
}

// HighlightTokens tokenizes src as whitespace-separated independent
// patterns — the shape a battery search box holds (ADR-0164 §SD2:
// space means AND, every token its own regex) — resetting group depth
// at each token, and returns spans covering every byte of src exactly
// once (separator bytes included, as CategoryLiteral).
//
// Same rationale as [HighlightLines], one level down: an unclosed `(`
// in the first token must not mis-colour the second, and in a search
// box a half-typed token routinely sits after finished ones.
//
// Separators are the ASCII whitespace bytes. The battery compiler
// splits on unicode.IsSpace (strings.Fields), so an exotic Unicode
// space is a token boundary there but painted as pattern bytes here —
// acceptable, because this lexer is only a painter (ADR-0015): the
// battery remains the authority on token boundaries and validity.
func HighlightTokens(src string) (spans []Span) {
	spans = make([]Span, 0, 16)
	off := 0
	n := len(src)
	for off < n {
		if isTokenSep(src[off]) {
			end := off
			for end < n && isTokenSep(src[end]) {
				end++
			}
			spans = append(spans, Span{
				Start:    int32(off),
				Stop:     int32(end),
				Text:     src[off:end],
				Category: CategoryLiteral,
			})
			off = end
			continue
		}
		end := off
		for end < n && !isTokenSep(src[end]) {
			end++
		}
		lx := newLexer(src[off:end])
		lx.run()
		for _, s := range lx.spans {
			s.Start += int32(off)
			s.Stop += int32(off)
			spans = append(spans, s)
		}
		off = end
	}
	return
}

// isTokenSep reports whether b separates tokens in [HighlightTokens]:
// the ASCII whitespace bytes a single- or multi-line search box can
// actually hold.
func isTokenSep(b byte) (ok bool) {
	ok = b == ' ' || b == '\t' || b == '\n' || b == '\r'
	return
}

// HighlightLines tokenizes src as one independent pattern per line,
// resetting group depth at each newline, and returns spans covering
// every byte of src exactly once (newline bytes included).
//
// The multi-pattern editor holds a *list* of regexes, not one regex.
// Lexing it as a single pattern would let an unclosed `(` on line 1
// mis-colour line 7 (ADR-0015 §SD3).
func HighlightLines(src string) (spans []Span) {
	spans = make([]Span, 0, 32)
	off := 0
	for {
		nl := strings.IndexByte(src[off:], '\n')
		end := len(src)
		if nl >= 0 {
			end = off + nl
		}
		if end > off {
			lx := newLexer(src[off:end])
			lx.run()
			for _, s := range lx.spans {
				s.Start += int32(off)
				s.Stop += int32(off)
				spans = append(spans, s)
			}
		}
		if nl < 0 {
			return
		}
		spans = append(spans, Span{
			Start:    int32(end),
			Stop:     int32(end + 1),
			Text:     "\n",
			Category: CategoryLiteral,
		})
		off = end + 1
	}
}

// lexer walks src once, left to right, emitting spans. Runs of adjacent
// literal bytes of the same category coalesce into one span; every other
// construct emits its own.
type lexer struct {
	src   string
	pos   int
	depth int32
	spans []Span

	// The pending literal run, or litStart < 0 when none is open.
	litStart int
	litStop  int
	litCat   CategoryE
}

func newLexer(src string) (lx *lexer) {
	lx = &lexer{
		src:      src,
		spans:    make([]Span, 0, 32),
		litStart: -1,
	}
	return
}

// push appends one span at the lexer's current depth. A zero-width range
// emits nothing, so callers may hand it an empty name or flag run.
func (lx *lexer) push(start int, stop int, cat CategoryE) {
	if stop <= start {
		return
	}
	lx.spans = append(lx.spans, Span{
		Start:    int32(start),
		Stop:     int32(stop),
		Text:     lx.src[start:stop],
		Category: cat,
		Depth:    lx.depth,
	})
}

// literal extends the pending literal run, or starts a new one when the
// category changes or the range is not adjacent.
func (lx *lexer) literal(start int, stop int, cat CategoryE) {
	if lx.litStart >= 0 && lx.litCat == cat && lx.litStop == start {
		lx.litStop = stop
		return
	}
	lx.flush()
	lx.litStart, lx.litStop, lx.litCat = start, stop, cat
}

// flush emits the pending literal run, if any. Must be called before any
// change to lx.depth, so the run is attributed to the depth it sat at.
func (lx *lexer) flush() {
	if lx.litStart < 0 {
		return
	}
	lx.push(lx.litStart, lx.litStop, lx.litCat)
	lx.litStart = -1
}

// token emits a standalone span, closing any pending literal run first.
func (lx *lexer) token(start int, stop int, cat CategoryE) {
	lx.flush()
	lx.push(start, stop, cat)
}

// run lexes the whole input.
func (lx *lexer) run() {
	src := lx.src
	for lx.pos < len(src) {
		switch ch := src[lx.pos]; ch {
		case '\\':
			lx.lexEscape(false)
		case '[':
			lx.lexClass()
		case '(':
			lx.lexGroupOpen()
		case ')':
			// Flush before touching depth: the run that ends here sat
			// at the inner depth, the `)` itself pairs with its `(`.
			lx.flush()
			if lx.depth > 0 {
				lx.depth--
				lx.push(lx.pos, lx.pos+1, CategoryGroup)
			} else {
				lx.push(lx.pos, lx.pos+1, CategoryError)
			}
			lx.pos++
		case '.', '|':
			lx.token(lx.pos, lx.pos+1, CategoryMeta)
			lx.pos++
		case '^', '$':
			lx.token(lx.pos, lx.pos+1, CategoryAnchor)
			lx.pos++
		case '*', '+', '?':
			lx.lexQuantifier(lx.pos + 1)
		case '{':
			// `{` only quantifies when it parses as a repeat: RE2 reads
			// `a{,3}` as the literal text `a{,3}` (verified against Go's
			// regexp/syntax), so an unparseable `{` is a literal.
			if end, ok := repeatEnd(src, lx.pos); ok {
				lx.lexQuantifier(end)
			} else {
				lx.literal(lx.pos, lx.pos+1, CategoryLiteral)
				lx.pos++
			}
		default:
			n := runeLen(src, lx.pos)
			lx.literal(lx.pos, lx.pos+n, CategoryLiteral)
			lx.pos += n
		}
	}
	lx.flush()
}

// lexQuantifier emits the quantifier starting at lx.pos and ending at
// end, absorbing a trailing lazy `?`.
func (lx *lexer) lexQuantifier(end int) {
	if end < len(lx.src) && lx.src[end] == '?' {
		end++
	}
	lx.token(lx.pos, end, CategoryQuantifier)
	lx.pos = end
}

// lexGroupOpen handles `(`, `(?:`, `(?flags)`, `(?flags:`, `(?P<name>`
// and `(?<name>`, advancing lx.pos past the opener.
//
// The `(?i)` versus `(?i:` distinction is the trap: same two-character
// prefix, opposite structural effect. `(?i)` is a flag *setting* — it
// consumes its own `)` and does not nest; `(?i:` opens a group and does.
func (lx *lexer) lexGroupOpen() {
	src := lx.src
	start := lx.pos

	if start+1 >= len(src) || src[start+1] != '?' {
		lx.token(start, start+1, CategoryGroup)
		lx.pos = start + 1
		lx.depth++
		return
	}

	rest := start + 2

	// Named capture: `(?P<name>` (RE2) or `(?<name>` (also accepted by
	// Go's regexp).
	nameOpen := -1
	switch {
	case rest+1 < len(src) && src[rest] == 'P' && src[rest+1] == '<':
		nameOpen = rest + 2
	case rest < len(src) && src[rest] == '<':
		nameOpen = rest + 1
	}
	if nameOpen >= 0 {
		lx.token(start, nameOpen, CategoryGroup)
		gt := strings.IndexByte(src[nameOpen:], '>')
		if gt < 0 {
			// Unterminated name — the normal state while typing one.
			lx.token(nameOpen, len(src), CategoryGroupName)
			lx.pos = len(src)
		} else {
			end := nameOpen + gt
			lx.token(nameOpen, end, CategoryGroupName)
			lx.token(end, end+1, CategoryGroup)
			lx.pos = end + 1
		}
		lx.depth++
		return
	}

	f := rest
	for f < len(src) && isFlagLetter(src[f]) {
		f++
	}
	lx.token(start, rest, CategoryGroup)
	lx.token(rest, f, CategoryFlags)
	switch {
	case f < len(src) && src[f] == ':':
		lx.token(f, f+1, CategoryGroup)
		lx.pos = f + 1
		lx.depth++
	case f < len(src) && src[f] == ')':
		// A flag setting, not a group: it eats its own `)` and leaves
		// the nesting depth alone.
		lx.token(f, f+1, CategoryGroup)
		lx.pos = f + 1
	default:
		// Half-typed `(?` / `(?i` / `(?ix`. Not treated as opening a
		// group, so the `)` that arrives when the user finishes typing
		// `(?i)` is consumed by the branch above rather than reported
		// as unbalanced.
		lx.pos = f
	}
}

// lexEscape handles a `\`-introduced construct at lx.pos. inClass
// switches off the anchor and \Q…\E readings, which are not anchors or
// quoting inside a character class.
func (lx *lexer) lexEscape(inClass bool) {
	src := lx.src
	start := lx.pos
	if start+1 >= len(src) {
		// A lone trailing `\` — one of the two byte-level certainties.
		lx.token(start, start+1, CategoryError)
		lx.pos = start + 1
		return
	}

	c := src[start+1]
	switch {
	case c == 'Q' && !inClass:
		lx.lexQuoted(start)
	case isPerlClass(c):
		lx.token(start, start+2, CategoryClassName)
		lx.pos = start + 2
	case c == 'p' || c == 'P':
		end := start + 2
		if end < len(src) && src[end] == '{' {
			end = closingBrace(src, end)
		} else if end < len(src) {
			end += runeLen(src, end)
		}
		lx.token(start, end, CategoryClassName)
		lx.pos = end
	case !inClass && isAnchorEscape(c):
		lx.token(start, start+2, CategoryAnchor)
		lx.pos = start + 2
	case c == 'x':
		end := start + 2
		if end < len(src) && src[end] == '{' {
			end = closingBrace(src, end)
		} else {
			for n := 0; n < 2 && end < len(src) && isHex(src[end]); n++ {
				end++
			}
		}
		lx.token(start, end, CategoryEscape)
		lx.pos = end
	case c == '0':
		// Octal. Only `\0` starts one: RE2 reads `\1`..`\7` as a
		// backreference and rejects it, so those fall through below and
		// the compile-error label reports them.
		end := start + 2
		for n := 0; n < 2 && end < len(src) && isOctal(src[end]); n++ {
			end++
		}
		lx.token(start, end, CategoryEscape)
		lx.pos = end
	default:
		// Any other escaped rune: `\.`, `\n`, `\\`, `\E`, `\é`. Consume
		// the whole rune so no span splits a UTF-8 sequence.
		end := start + 1 + runeLen(src, start+1)
		lx.token(start, end, CategoryEscape)
		lx.pos = end
	}
}

// lexQuoted handles `\Q…\E`: the delimiters are escapes, the body is
// literal text however many metacharacters it contains. An unterminated
// run quotes to end of input, which is what RE2 does.
func (lx *lexer) lexQuoted(start int) {
	src := lx.src
	lx.token(start, start+2, CategoryEscape)
	body := start + 2
	k := strings.Index(src[body:], `\E`)
	if k < 0 {
		lx.token(body, len(src), CategoryLiteral)
		lx.pos = len(src)
		return
	}
	lx.token(body, body+k, CategoryLiteral)
	lx.token(body+k, body+k+2, CategoryEscape)
	lx.pos = body + k + 2
}

// lexClass handles a `[…]` character class, advancing lx.pos past the
// closing `]` (or to end of input when there is none — an unterminated
// class keeps its content categories, ADR-0015 §SD1).
func (lx *lexer) lexClass() {
	src := lx.src
	start := lx.pos
	open := start + 1
	if open < len(src) && src[open] == '^' {
		open++
	}
	lx.token(start, open, CategoryClassDelim)
	lx.pos = open

	// A `]` immediately after `[` or `[^` is a literal member, not the
	// close — verified against Go's regexp/syntax, which parses `[]]` as
	// the single-rune class `\]`.
	first := true
	for lx.pos < len(src) {
		switch ch := src[lx.pos]; {
		case ch == ']' && !first:
			lx.token(lx.pos, lx.pos+1, CategoryClassDelim)
			lx.pos++
			return
		case ch == '[' && lx.pos+1 < len(src) && src[lx.pos+1] == ':':
			if k := strings.Index(src[lx.pos+2:], ":]"); k >= 0 {
				end := lx.pos + 2 + k + 2
				lx.token(lx.pos, end, CategoryClassName)
				lx.pos = end
			} else {
				lx.literal(lx.pos, lx.pos+1, CategoryClassLiteral)
				lx.pos++
			}
		case ch == '\\':
			lx.lexEscape(true)
		case ch == '-' && !first && lx.pos+1 < len(src) && src[lx.pos+1] != ']':
			lx.token(lx.pos, lx.pos+1, CategoryClassDelim)
			lx.pos++
		default:
			n := runeLen(src, lx.pos)
			lx.literal(lx.pos, lx.pos+n, CategoryClassLiteral)
			lx.pos += n
		}
		first = false
	}
	lx.flush()
}

// repeatEnd reports whether src[at] opens an `{n}` / `{n,}` / `{n,m}`
// repeat, and where it ends (one past the `}`). At least one digit must
// precede the comma: RE2 reads `a{,3}` as literal text.
func repeatEnd(src string, at int) (end int, ok bool) {
	i := at + 1
	digits := 0
	for i < len(src) && isDigit(src[i]) {
		i++
		digits++
	}
	if digits == 0 {
		return
	}
	if i < len(src) && src[i] == ',' {
		i++
		for i < len(src) && isDigit(src[i]) {
			i++
		}
	}
	if i >= len(src) || src[i] != '}' {
		return
	}
	end, ok = i+1, true
	return
}

// closingBrace returns one past the `}` closing the brace at `at`, or
// len(src) when there is none — an unterminated `\x{…` or `\p{…` is a
// normal mid-typing state and keeps its category.
func closingBrace(src string, at int) (end int) {
	if k := strings.IndexByte(src[at:], '}'); k >= 0 {
		end = at + k + 1
		return
	}
	end = len(src)
	return
}

// runeLen returns the byte length of the rune at src[at], counting an
// invalid UTF-8 byte as one byte so coverage stays total.
func runeLen(src string, at int) (n int) {
	_, n = utf8.DecodeRuneInString(src[at:])
	if n <= 0 {
		n = 1
	}
	return
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isOctal(b byte) bool { return b >= '0' && b <= '7' }

func isHex(b byte) bool {
	return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// isPerlClass reports whether `\`+b names a Perl character class.
func isPerlClass(b byte) bool {
	switch b {
	case 'd', 'D', 's', 'S', 'w', 'W':
		return true
	}
	return false
}

// isAnchorEscape reports whether `\`+b is a zero-width assertion. `\b`
// is one only outside a character class.
func isAnchorEscape(b byte) bool {
	switch b {
	case 'A', 'b', 'B', 'z':
		return true
	}
	return false
}

// isFlagLetter reports whether b may appear in an inline flag run —
// RE2's `i` (case-insensitive), `m` (multi-line), `s` (dot matches
// newline), `U` (ungreedy), and `-` (negate what follows).
func isFlagLetter(b byte) bool {
	switch b {
	case 'i', 'm', 's', 'U', '-':
		return true
	}
	return false
}
