package markdownhighlight

// lex.go — the source-offset tier.
//
// [Highlight] recovers marker offsets by *generating* a canonical form, which
// is what makes it exact and what makes its spans describe that form rather
// than the input. Correct for a viewer; useless for colouring a buffer someone
// is typing into, where a span has to land on the byte the author actually
// wrote.
//
// So this tier does not use goldmark at all. It is a scanner over the source,
// in the same spirit as the lex tier of the SQL highlighter (ADR-0130 §5), and
// it shares that tier's real advantage: it survives input no parser would
// accept, which is most of what a buffer holds between one keystroke and the
// next. Half-typed emphasis stays plain text instead of colouring the rest of
// the line.

// HighlightLex scans src as Obsidian-flavored markdown and returns spans that
// index src ITSELF, tagged with the same [CategoryE] vocabulary [Highlight]
// uses, so both tiers share one palette.
//
// The returned spans are ascending, non-overlapping, and cover every byte of
// src exactly once. Total coverage is deliberate: a CodeViewJob does not
// gap-fill, and egui drops the glyphs of bytes no section claims.
//
// Span.Text is left EMPTY, unlike [Highlight]'s. Consumers of this tier have
// src in hand and slice it themselves, and this path runs once per keystroke,
// where one small allocation per span is a cost with no reader.
//
// Deliberately not handled, because a lex tier that guesses is worse than one
// that leaves text plain: indented (four-space) code blocks, which cannot be
// told from a list continuation without block context; reference-style links
// and their definitions; setext (underlined) headings; and emphasis that spans
// more than one line. Each of those reads as ordinary prose.
func HighlightLex(src []byte) (spans []Span) {
	l := &lexer{src: src, spans: make([]Span, 0, 64)}
	lines := scanLines(src)

	i := 0
	// Frontmatter is only frontmatter at the very top of the document.
	if len(lines) > 0 && isFenceRule(src, lines[0], '-') {
		i = l.lexFrontmatter(lines)
	}

	var fence fenceState
	for ; i < len(lines); i++ {
		ln := lines[i]
		if fence.open {
			if fenceCloses(src, ln, fence) {
				l.emit(ln.eol, CategoryFenceDelim)
				l.emit(ln.next, CategoryWhitespace)
				fence.open = false
				continue
			}
			l.emit(ln.next, CategoryCodeBlockBody)
			continue
		}
		if f, ok := fenceOpens(src, ln); ok {
			fence = f
			l.lexFenceOpen(ln, f)
			continue
		}
		l.lexBlockLine(lines, i)
	}
	// An unterminated fence leaves the rest of the document as body; the loop
	// above already emitted it. This is the catch-all for anything else.
	l.emit(len(src), CategoryPlain)
	return l.spans
}

// ---------------------------------------------------------------------------
// Emission
// ---------------------------------------------------------------------------

type lexer struct {
	src   []byte
	spans []Span
	// pos is the coverage watermark: every byte below it belongs to a span.
	pos int
}

// emit claims src[l.pos:stop] for cat. Runs of one category coalesce, which
// keeps the section count — and so the per-keystroke wire cost — proportional
// to the markup rather than to the prose.
//
// Emitting backwards is a no-op rather than a panic: a scanner that miscounts
// should leave a document uncoloured, never lose a byte of it.
func (inst *lexer) emit(stop int, cat CategoryE) {
	if stop > len(inst.src) {
		stop = len(inst.src)
	}
	if stop <= inst.pos {
		return
	}
	if n := len(inst.spans); n > 0 && inst.spans[n-1].Category == cat && int(inst.spans[n-1].Stop) == inst.pos {
		inst.spans[n-1].Stop = int32(stop)
		inst.pos = stop
		return
	}
	inst.spans = append(inst.spans, Span{Start: int32(inst.pos), Stop: int32(stop), Category: cat})
	inst.pos = stop
}

// ---------------------------------------------------------------------------
// Lines
// ---------------------------------------------------------------------------

// lineRange is one source line: [start, eol) is its content, [start, next) the
// content plus its newline, so emitting to next consumes the line whole.
type lineRange struct {
	start int
	eol   int
	next  int
}

func scanLines(src []byte) (lines []lineRange) {
	lines = make([]lineRange, 0, 32)
	for p := 0; p < len(src); {
		eol := p
		for eol < len(src) && src[eol] != '\n' {
			eol++
		}
		next := eol
		if next < len(src) {
			next++ // the '\n'
		}
		lines = append(lines, lineRange{start: p, eol: eol, next: next})
		p = next
	}
	return
}

// contentStart returns the first non-blank byte of ln, and whether the line
// holds anything at all.
func (inst *lexer) contentStart(ln lineRange) (p int, nonBlank bool) {
	p = ln.start
	for p < ln.eol && (inst.src[p] == ' ' || inst.src[p] == '\t') {
		p++
	}
	return p, p < ln.eol
}

// ---------------------------------------------------------------------------
// Frontmatter
// ---------------------------------------------------------------------------

// lexFrontmatter consumes the opening fence, the body, and the closing fence,
// returning the index of the first line after it. An unterminated block claims
// only its opening fence, so a stray `---` at the top of a document does not
// swallow the rest as metadata.
func (inst *lexer) lexFrontmatter(lines []lineRange) (nextLine int) {
	closeAt := -1
	for i := 1; i < len(lines); i++ {
		if isFenceRule(inst.src, lines[i], '-') || isFenceRule(inst.src, lines[i], '.') {
			closeAt = i
			break
		}
	}
	if closeAt < 0 {
		return 0 // not frontmatter after all; let the block scanner have it
	}

	inst.emit(lines[0].eol, CategoryFrontmatterDelim)
	inst.emit(lines[0].next, CategoryWhitespace)
	for i := 1; i < closeAt; i++ {
		inst.lexFrontmatterLine(lines[i])
	}
	inst.emit(lines[closeAt].eol, CategoryFrontmatterDelim)
	inst.emit(lines[closeAt].next, CategoryWhitespace)
	return closeAt + 1
}

func (inst *lexer) lexFrontmatterLine(ln lineRange) {
	p, nonBlank := inst.contentStart(ln)
	inst.emit(p, CategoryWhitespace)
	if !nonBlank {
		inst.emit(ln.next, CategoryWhitespace)
		return
	}
	// A `key:` prefix splits; anything else (a `- item` under a key, a folded
	// scalar's continuation) is all value.
	if colon := indexByteIn(inst.src, p, ln.eol, ':'); colon > p && isPlainYamlKey(inst.src, p, colon) {
		inst.emit(colon, CategoryFrontmatterKey)
		inst.emit(colon+1, CategoryFrontmatterDelim)
	}
	inst.emit(ln.eol, CategoryFrontmatterValue)
	inst.emit(ln.next, CategoryWhitespace)
}

// isPlainYamlKey rejects a colon that belongs to a value (a URL's `https:`, a
// time's `12:30`) by requiring the run before it to look like a bare key.
func isPlainYamlKey(src []byte, start, stop int) (ok bool) {
	for i := start; i < stop; i++ {
		ch := src[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case ch == '_', ch == '-', ch == '.', ch == ' ':
		default:
			return false
		}
	}
	return stop > start
}

// ---------------------------------------------------------------------------
// Fences
// ---------------------------------------------------------------------------

type fenceState struct {
	open   bool
	char   byte
	length int
}

// fenceOpens reports a ``` or ~~~ run of three or more.
func fenceOpens(src []byte, ln lineRange) (f fenceState, ok bool) {
	p := ln.start
	for p < ln.eol && src[p] == ' ' {
		p++
	}
	if p >= ln.eol || (src[p] != '`' && src[p] != '~') {
		return f, false
	}
	ch := src[p]
	run := 0
	for p+run < ln.eol && src[p+run] == ch {
		run++
	}
	if run < 3 {
		return f, false
	}
	return fenceState{open: true, char: ch, length: run}, true
}

// fenceCloses reports a run of the same character, at least as long as the
// opener, with nothing else on the line.
func fenceCloses(src []byte, ln lineRange, f fenceState) (ok bool) {
	p := ln.start
	for p < ln.eol && src[p] == ' ' {
		p++
	}
	run := 0
	for p+run < ln.eol && src[p+run] == f.char {
		run++
	}
	if run < f.length {
		return false
	}
	for i := p + run; i < ln.eol; i++ {
		if src[i] != ' ' && src[i] != '\t' {
			return false
		}
	}
	return true
}

func (inst *lexer) lexFenceOpen(ln lineRange, f fenceState) {
	p, _ := inst.contentStart(ln)
	inst.emit(p, CategoryWhitespace)
	inst.emit(p+f.length, CategoryFenceDelim)
	// The info string: the language token and whatever follows it.
	inst.emit(ln.eol, CategoryFenceLang)
	inst.emit(ln.next, CategoryWhitespace)
}

// isFenceRule reports a line that is a run of three or more of ch and nothing
// else — the frontmatter delimiter shape.
func isFenceRule(src []byte, ln lineRange, ch byte) (ok bool) {
	run := 0
	i := ln.start
	for i < ln.eol && src[i] == ch {
		run++
		i++
	}
	if run < 3 {
		return false
	}
	for ; i < ln.eol; i++ {
		if src[i] != ' ' && src[i] != '\t' {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Block lines
// ---------------------------------------------------------------------------

func (inst *lexer) lexBlockLine(lines []lineRange, i int) {
	ln := lines[i]

	p, nonBlank := inst.contentStart(ln)
	inst.emit(p, CategoryWhitespace)
	if !nonBlank {
		inst.emit(ln.next, CategoryWhitespace)
		return
	}

	// Blockquote markers, possibly nested, and the callout header they may
	// introduce.
	quoted := false
	for p < ln.eol && inst.src[p] == '>' {
		quoted = true
		p++
		inst.emit(p, CategoryBlockquoteMarker)
		for p < ln.eol && inst.src[p] == ' ' {
			p++
		}
		inst.emit(p, CategoryWhitespace)
	}
	if quoted {
		if next, ok := inst.lexCallout(p, ln.eol); ok {
			p = next
		}
	}
	if p >= ln.eol {
		inst.emit(ln.next, CategoryWhitespace)
		return
	}

	if isThematicBreak(inst.src, p, ln.eol) {
		inst.emit(ln.eol, CategoryThematicBreak)
		inst.emit(ln.next, CategoryWhitespace)
		return
	}

	if level := atxLevel(inst.src, p, ln.eol); level > 0 {
		inst.lexHeading(p, ln)
		return
	}

	if inst.lexTableRow(lines, i, p) {
		return
	}

	if end, ok := listMarker(inst.src, p, ln.eol); ok {
		inst.emit(end, CategoryListMarker)
		p = end
		for p < ln.eol && inst.src[p] == ' ' {
			p++
		}
		inst.emit(p, CategoryWhitespace)
		if end, ok := taskMark(inst.src, p, ln.eol); ok {
			inst.emit(end, CategoryTaskMark)
			p = end
		}
	}

	// A line opening with a tag is HTML passthrough; the rest of it is not
	// markdown and colouring it as prose would be a lie.
	if p < ln.eol && inst.src[p] == '<' && isHtmlTagStart(inst.src, p, ln.eol) {
		inst.emit(ln.eol, CategoryRawHtml)
		inst.emit(ln.next, CategoryWhitespace)
		return
	}

	inst.lexInline(p, ln.eol, CategoryPlain)
	inst.emit(ln.next, CategoryWhitespace)
}

// lexCallout claims an Obsidian `[!type]` header, plus the fold marker and the
// title that may follow it.
func (inst *lexer) lexCallout(p, end int) (next int, ok bool) {
	if p+2 >= end || inst.src[p] != '[' || inst.src[p+1] != '!' {
		return p, false
	}
	close := indexByteIn(inst.src, p+2, end, ']')
	if close < 0 {
		return p, false
	}
	inst.emit(p+2, CategoryCalloutMarker)
	inst.emit(close, CategoryCalloutType)
	next = close + 1
	// An optional `+` / `-` fold marker rides with the scaffolding.
	if next < end && (inst.src[next] == '+' || inst.src[next] == '-') {
		next++
	}
	inst.emit(next, CategoryCalloutMarker)
	return next, true
}

func (inst *lexer) lexHeading(p int, ln lineRange) {
	hashes := p
	for hashes < ln.eol && inst.src[hashes] == '#' {
		hashes++
	}
	inst.emit(hashes, CategoryHeadingMarker)
	for hashes < ln.eol && inst.src[hashes] == ' ' {
		hashes++
	}
	inst.emit(hashes, CategoryWhitespace)

	// An explicit `{#anchor}` is the section's link target, not part of its
	// text — the same split [Highlight] makes.
	textEnd := ln.eol
	if open, ok := trailingAnchor(inst.src, hashes, ln.eol); ok {
		textEnd = open
	}
	inst.lexInline(hashes, textEnd, CategoryHeadingText)
	if textEnd < ln.eol {
		inst.emit(textEnd+2, CategoryLinkPunct) // `{#`
		inst.emit(ln.eol-1, CategoryLinkUrl)
		inst.emit(ln.eol, CategoryLinkPunct) // `}`
	}
	inst.emit(ln.next, CategoryWhitespace)
}

// trailingAnchor finds a `{#...}` that ends the line.
func trailingAnchor(src []byte, start, end int) (open int, ok bool) {
	if end-start < 4 || src[end-1] != '}' {
		return 0, false
	}
	for i := end - 2; i > start; i-- {
		switch src[i] {
		case '{':
			if i+1 < end && src[i+1] == '#' {
				return i, true
			}
			return 0, false
		case ' ':
			// keep scanning left
		}
	}
	return 0, false
}

func atxLevel(src []byte, p, end int) (level int) {
	for p+level < end && src[p+level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0
	}
	// A heading's hashes must be followed by a space or end the line; `#tag`
	// is an Obsidian tag, not a heading.
	if p+level < end && src[p+level] != ' ' {
		return 0
	}
	return level
}

func isThematicBreak(src []byte, p, end int) (ok bool) {
	if p >= end {
		return false
	}
	ch := src[p]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	run := 0
	for i := p; i < end; i++ {
		switch src[i] {
		case ch:
			run++
		case ' ', '\t':
		default:
			return false
		}
	}
	return run >= 3
}

// listMarker matches `-`, `*`, `+` and `1.` / `1)` when followed by a space.
func listMarker(src []byte, p, end int) (stop int, ok bool) {
	if p >= end {
		return p, false
	}
	switch src[p] {
	case '-', '*', '+':
		if p+1 < end && src[p+1] == ' ' {
			return p + 1, true
		}
		return p, false
	}
	digits := p
	for digits < end && src[digits] >= '0' && src[digits] <= '9' {
		digits++
	}
	if digits == p || digits >= end {
		return p, false
	}
	if src[digits] != '.' && src[digits] != ')' {
		return p, false
	}
	if digits+1 < end && src[digits+1] != ' ' {
		return p, false
	}
	return digits + 1, true
}

func taskMark(src []byte, p, end int) (stop int, ok bool) {
	if p+2 >= end || src[p] != '[' || src[p+2] != ']' {
		return p, false
	}
	switch src[p+1] {
	case ' ', 'x', 'X':
		return p + 3, true
	}
	return p, false
}

func isHtmlTagStart(src []byte, p, end int) (ok bool) {
	i := p + 1
	if i < end && src[i] == '/' {
		i++
	}
	if i >= end {
		return false
	}
	ch := src[i]
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '!'
}

// ---------------------------------------------------------------------------
// Tables
// ---------------------------------------------------------------------------

// lexTableRow claims a GFM table row. The header is identified the only way it
// can be — by the delimiter row underneath it.
func (inst *lexer) lexTableRow(lines []lineRange, i, p int) (claimed bool) {
	ln := lines[i]
	if indexByteIn(inst.src, p, ln.eol, '|') < 0 {
		return false
	}

	cellCat := CategoryTableCellText
	switch {
	case isDelimiterRow(inst.src, p, ln.eol):
		cellCat = CategoryTableAlign
	case i+1 < len(lines):
		nextStart, nonBlank := inst.contentStart(lines[i+1])
		if nonBlank && isDelimiterRow(inst.src, nextStart, lines[i+1].eol) {
			cellCat = CategoryTableHeaderText
		}
	}
	// A lone `|` in ordinary prose is not a table; require either a delimiter
	// row in play or a second pipe.
	if cellCat == CategoryTableCellText &&
		indexByteIn(inst.src, indexByteIn(inst.src, p, ln.eol, '|')+1, ln.eol, '|') < 0 {
		return false
	}

	for q := p; q < ln.eol; {
		if inst.src[q] == '|' {
			inst.emit(q, cellCat)
			q++
			inst.emit(q, CategoryTablePipe)
			continue
		}
		// Cells carry inline markup; the align row does not.
		if cellCat == CategoryTableAlign {
			q++
			continue
		}
		cellEnd := indexByteIn(inst.src, q, ln.eol, '|')
		if cellEnd < 0 {
			cellEnd = ln.eol
		}
		inst.lexInline(q, cellEnd, cellCat)
		q = cellEnd
	}
	inst.emit(ln.eol, cellCat)
	inst.emit(ln.next, CategoryWhitespace)
	return true
}

// isDelimiterRow matches `|---|:--:|` — every cell a dash run with optional
// alignment colons.
func isDelimiterRow(src []byte, p, end int) (ok bool) {
	cells := 0
	dashes := 0
	for i := p; i < end; i++ {
		switch src[i] {
		case '|':
			if dashes > 0 {
				cells++
			}
			dashes = 0
		case '-':
			dashes++
		case ':', ' ', '\t':
		default:
			return false
		}
	}
	if dashes > 0 {
		cells++
	}
	return cells > 0
}

// ---------------------------------------------------------------------------
// Inline
// ---------------------------------------------------------------------------

// lexInline scans [start, end) for inline markup, claiming everything it does
// not recognise as base.
func (inst *lexer) lexInline(start, end int, base CategoryE) {
	for p := start; p < end; {
		if next, ok := inst.tryInline(p, end, base); ok {
			p = next
			continue
		}
		p++
	}
	inst.emit(end, base)
}

// tryInline attempts every construct at p, in the one order that works: code
// spans first (their content is literal), then the two-character openers
// before the one-character ones they contain.
func (inst *lexer) tryInline(p, end int, base CategoryE) (next int, ok bool) {
	switch inst.src[p] {
	case '`':
		return inst.lexCodeSpan(p, end, base)
	case '%':
		return inst.lexPaired(p, end, base, "%%", CategoryCommentDelim, CategoryCommentText)
	case '=':
		return inst.lexPaired(p, end, base, "==", CategoryHighlightDelim, CategoryHighlightText)
	case '~':
		return inst.lexPaired(p, end, base, "~~", CategoryStrikeDelim, CategoryStrikeText)
	case '*', '_':
		return inst.lexEmphasis(p, end, base)
	case '!':
		if p+2 < end && inst.src[p+1] == '[' && inst.src[p+2] == '[' {
			inst.emit(p, base)
			inst.emit(p+1, CategoryEmbedMarker)
			return inst.lexWikilink(p+1, end, base)
		}
		if p+1 < end && inst.src[p+1] == '[' {
			inst.emit(p, base)
			inst.emit(p+1, CategoryEmbedMarker)
			return inst.lexLink(p+1, end, base)
		}
		return p, false
	case '[':
		if p+1 < end && inst.src[p+1] == '[' {
			return inst.lexWikilink(p, end, base)
		}
		return inst.lexLink(p, end, base)
	case '<':
		return inst.lexAutolink(p, end, base)
	case '#':
		return inst.lexTag(p, end, base)
	}
	return p, false
}

// lexTag claims an Obsidian `#tag`, including the nested `#a/b/c` form.
//
// The two rules below are the parser's, restated: this tier is a second
// reading of the same syntax, and the place it is allowed to differ is in what
// it declines to recognise, never in what it claims. A tag opens only at a
// word boundary — `C#sharp` and `foo#bar` are words, not tags — and a purely
// numeric body is the English "number four", not a tag, which is what keeps
// `#4` and an issue reference from colouring as one.
//
// Headings never reach here: atxLevel already refuses `#` without a following
// space at the start of a line, which is the same rule read from the other
// side.
func (inst *lexer) lexTag(p, end int, base CategoryE) (next int, ok bool) {
	if p > 0 && isTagBoundaryByte(inst.src[p-1]) {
		return p, false
	}
	body := p + 1
	if body >= end || !isTagStartByte(inst.src[body]) {
		return p, false
	}
	q := body
	for q < end && isTagBodyByte(inst.src[q]) {
		q++
	}
	// A trailing `/` belongs to no segment, so it is not part of the tag.
	if inst.src[q-1] == '/' {
		q--
	}
	if q <= body || allDigitsIn(inst.src, body, q) {
		return p, false
	}
	inst.emit(p, base)
	inst.emit(body, CategoryTagMarker)
	inst.emit(q, CategoryTagText)
	return q, true
}

// isTagBoundaryByte reports whether a byte immediately before `#` makes it
// part of the preceding word rather than the start of a tag.
//
// A byte test, where the parser's is a rune test: any UTF-8 continuation or
// lead byte is >= 0x80 and counts as a word byte here, which reaches the same
// verdict for a letter in any script without decoding one.
// `{` is in the set for the same reason the parser has it: `{#anchor}` is the
// heading-anchor syntax, not a tag inside braces.
func isTagBoundaryByte(c byte) (yes bool) {
	return c >= 0x80 || c == '_' || c == '#' || c == '{' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isTagStartByte(c byte) (yes bool) {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isTagBodyByte(c byte) (yes bool) {
	return isTagStartByte(c) || c == '-' || c == '/'
}

func allDigitsIn(src []byte, start, stop int) (yes bool) {
	for i := start; i < stop; i++ {
		if src[i] < '0' || src[i] > '9' {
			return false
		}
	}
	return stop > start
}

// lexCodeSpan claims a backtick-delimited run, closed by a run of the SAME
// length — the CommonMark rule, and what lets a doubled opener hold a literal
// backtick in its content.
func (inst *lexer) lexCodeSpan(p, end int, base CategoryE) (next int, ok bool) {
	run := 0
	for p+run < end && inst.src[p+run] == '`' {
		run++
	}
	inner := p + run
	for q := inner; q < end; {
		if inst.src[q] != '`' {
			q++
			continue
		}
		closeRun := 0
		for q+closeRun < end && inst.src[q+closeRun] == '`' {
			closeRun++
		}
		if closeRun == run {
			inst.emit(p, base)
			inst.emit(inner, CategoryInlineCodeDelim)
			inst.emit(q, CategoryInlineCodeText)
			inst.emit(q+run, CategoryInlineCodeDelim)
			return q + run, true
		}
		q += closeRun
	}
	return p, false
}

// lexPaired claims a symmetric two-character delimiter. An unterminated opener
// reports false, so a half-typed `**` stays plain rather than colouring the
// rest of the line while the author is still typing.
func (inst *lexer) lexPaired(p, end int, base CategoryE, delim string, delimCat, textCat CategoryE) (next int, ok bool) {
	if !hasPrefixAt(inst.src, p, end, delim) {
		return p, false
	}
	inner := p + len(delim)
	if inner >= end || inst.src[inner] == ' ' {
		return p, false
	}
	close := indexStrIn(inst.src, inner, end, delim)
	if close < 0 {
		return p, false
	}
	inst.emit(p, base)
	inst.emit(inner, delimCat)
	inst.emit(close, textCat)
	inst.emit(close+len(delim), delimCat)
	return close + len(delim), true
}

// lexEmphasis claims `**strong**` / `*em*` and their underscore spellings.
//
// The underscore form additionally requires word boundaries on both ends,
// which is what keeps snake_case identifiers — common in the prose this editor
// is used on — from turning into emphasis.
func (inst *lexer) lexEmphasis(p, end int, base CategoryE) (next int, ok bool) {
	ch := inst.src[p]
	run := 0
	for p+run < end && inst.src[p+run] == ch {
		run++
	}
	strong := run >= 2
	delim := string(ch)
	delimCat, textCat := CategoryEmphasisDelim, CategoryEmphasisText
	if strong {
		delim = string([]byte{ch, ch})
		delimCat, textCat = CategoryStrongDelim, CategoryStrongText
	}
	if ch == '_' && !isWordBoundary(inst.src, p-1) {
		return p, false
	}
	inner := p + len(delim)
	if inner >= end || inst.src[inner] == ' ' {
		return p, false
	}
	close := indexStrIn(inst.src, inner, end, delim)
	if close < 0 || close == inner {
		return p, false
	}
	if inst.src[close-1] == ' ' {
		return p, false
	}
	if ch == '_' && !isWordBoundary(inst.src, close+len(delim)) {
		return p, false
	}
	inst.emit(p, base)
	inst.emit(inner, delimCat)
	inst.emit(close, textCat)
	inst.emit(close+len(delim), delimCat)
	return close + len(delim), true
}

// isWordBoundary reports whether the byte at i (which may be out of range, and
// then counts as a boundary) is not alphanumeric.
func isWordBoundary(src []byte, i int) (ok bool) {
	if i < 0 || i >= len(src) {
		return true
	}
	ch := src[i]
	switch {
	case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		return false
	}
	return true
}

// lexWikilink claims `[[Target]]` and `[[Target|shown]]`.
func (inst *lexer) lexWikilink(p, end int, base CategoryE) (next int, ok bool) {
	if !hasPrefixAt(inst.src, p, end, "[[") {
		return p, false
	}
	close := indexStrIn(inst.src, p+2, end, "]]")
	if close < 0 {
		return p, false
	}
	inst.emit(p, base)
	inst.emit(p+2, CategoryWikilinkPunct)
	if pipe := indexByteIn(inst.src, p+2, close, '|'); pipe >= 0 {
		inst.emit(pipe, CategoryWikilinkTarget)
		inst.emit(pipe+1, CategoryWikilinkPunct)
		inst.emit(close, CategoryLinkLabel)
	} else {
		inst.emit(close, CategoryWikilinkTarget)
	}
	inst.emit(close+2, CategoryWikilinkPunct)
	return close + 2, true
}

// lexLink claims `[label](url)`. A bare `[label]` is left plain: without
// reference definitions it is not a link, and colouring it as one would
// promise a target that is not there.
func (inst *lexer) lexLink(p, end int, base CategoryE) (next int, ok bool) {
	if inst.src[p] != '[' {
		return p, false
	}
	label := indexByteIn(inst.src, p+1, end, ']')
	if label < 0 || label+1 >= end || inst.src[label+1] != '(' {
		return p, false
	}
	url := indexByteIn(inst.src, label+2, end, ')')
	if url < 0 {
		return p, false
	}
	inst.emit(p, base)
	inst.emit(p+1, CategoryLinkPunct)
	inst.emit(label, CategoryLinkLabel)
	inst.emit(label+2, CategoryLinkPunct)
	inst.emit(url, CategoryLinkUrl)
	inst.emit(url+1, CategoryLinkPunct)
	return url + 1, true
}

// lexAutolink claims `<https://…>`, and only that: a bare `<` opens an HTML
// tag far more often than a link.
func (inst *lexer) lexAutolink(p, end int, base CategoryE) (next int, ok bool) {
	close := indexByteIn(inst.src, p+1, end, '>')
	if close < 0 || close == p+1 {
		return p, false
	}
	if !looksLikeUri(inst.src, p+1, close) {
		return p, false
	}
	inst.emit(p, base)
	inst.emit(p+1, CategoryLinkPunct)
	inst.emit(close, CategoryLinkUrl)
	inst.emit(close+1, CategoryLinkPunct)
	return close + 1, true
}

// looksLikeUri requires a scheme followed by `:`, and no spaces.
func looksLikeUri(src []byte, start, stop int) (ok bool) {
	colon := -1
	for i := start; i < stop; i++ {
		ch := src[i]
		if ch == ' ' || ch == '\t' {
			return false
		}
		if ch == ':' && colon < 0 {
			colon = i
		}
	}
	if colon <= start {
		return false
	}
	for i := start; i < colon; i++ {
		ch := src[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case ch == '+', ch == '-', ch == '.':
		default:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Small byte helpers — kept local so the scanner allocates nothing per call.
// ---------------------------------------------------------------------------

func indexByteIn(src []byte, start, stop int, ch byte) (i int) {
	for i = start; i < stop; i++ {
		if src[i] == ch {
			return i
		}
	}
	return -1
}

func indexStrIn(src []byte, start, stop int, s string) (i int) {
	if len(s) == 0 {
		return -1
	}
	for i = start; i+len(s) <= stop; i++ {
		if hasPrefixAt(src, i, stop, s) {
			return i
		}
	}
	return -1
}

func hasPrefixAt(src []byte, p, stop int, s string) (ok bool) {
	if p+len(s) > stop {
		return false
	}
	for i := 0; i < len(s); i++ {
		if src[p+i] != s[i] {
			return false
		}
	}
	return true
}
