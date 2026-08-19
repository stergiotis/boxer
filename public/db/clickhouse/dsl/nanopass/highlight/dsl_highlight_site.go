package highlight

// The caret's *site*: everything about where the caret is that a completion
// engine can learn without a parse (ADR-0190 §SD2).
//
// [EntityAt] answers "what name is the caret on, and what calls enclose it",
// which is what a documentation lookup needs. Completion needs more: which
// argument of the enclosing call the caret is in, what the completed siblings
// are, whether the caret is inside a string literal and what has been typed in
// it, whether it follows a `.` and what the receiver is, and which brackets are
// still open so a repair can close them.
//
// It is one forward scan over the same lex-tier span stream, for the same
// reason: it has to answer on a buffer that does not parse, which is every
// moment somebody is typing. The scope-aware half — aliases, CTEs, the FROM
// clause — comes from a sentinel parse on a worker (§SD3) and is a separate
// tier; this one runs every frame.

import "strings"

// Range is a half-open byte range into the buffer the spans cover.
type Range struct {
	Start int
	Stop  int
}

// Len is the range's width; zero for an absent range.
func (inst Range) Len() int {
	if inst.Stop <= inst.Start {
		return 0
	}
	return inst.Stop - inst.Start
}

// CallFrame is one argument list the caret is inside.
type CallFrame struct {
	// Callee is the function name, unquoted. Empty for a grouping paren, a
	// subquery, or an array subscript — all of which are still traversed, so
	// an enclosing call outside them is still reported.
	Callee string
	// Open is the byte offset of the bracket that opened the frame.
	Open int
	// Ordinal is which argument the caret is in, counting top-level commas
	// from the bracket.
	//
	// It is -1 for a keyword-syntax call — `CAST(x AS T)`,
	// `EXTRACT(HOUR FROM ts)` — where commas are not what separates the
	// arguments. A consumer wanting the ordinal there needs the tree (§SD3),
	// which is why this says "unknown" rather than guessing zero.
	Ordinal int
	// Args are the byte ranges of the arguments, indexed by ordinal, with
	// surrounding whitespace trimmed. It covers the arguments after the caret
	// too when the frame is closed later in the buffer, because a domain may
	// depend on a sibling either side. An argument that is empty (the caret's
	// own, usually) has a zero-length range.
	Args []Range
	// Bracket is the opening bracket character.
	Bracket byte
}

// LiteralSite is the string literal the caret is inside.
//
// It covers both halves of what §SD9 needs: an *unterminated* literal is the
// one being typed, and a *terminated* one is a literal the caret has been moved
// back into — where the whole content is what should resolve, while the text
// before the caret is still what a prefix match filters on.
type LiteralSite struct {
	// Quote is the opening quote character.
	Quote byte
	// Start is the offset of the opening quote.
	Start int
	// Stop is the offset just past the closing quote, or -1 when the literal
	// is unterminated — which is also what tells a repair it must close it.
	Stop int
	// Text is the literal's whole content, quotes excluded, escapes as
	// written.
	Text string
	// Prefix is the content from the opening quote to the caret.
	Prefix string
}

// Terminated reports whether the literal has a closing quote.
func (inst LiteralSite) Terminated() bool { return inst.Stop >= 0 }

// ReceiverKindE says what a member access is reaching into.
//
//codelint:enum-prefix=Receiver
type ReceiverKindE uint8

const (
	// ReceiverNone is the zero value: no member access at the caret.
	ReceiverNone ReceiverKindE = iota
	// ReceiverIdent is a dotted identifier chain — an alias, a table, a
	// database, or a column of one.
	ReceiverIdent
	// ReceiverCall is a call whose result is being reached into:
	// `LW_COMPONENT('SysMem').`
	ReceiverCall
	// ReceiverParen is a parenthesised expression: `(x + y).`
	ReceiverParen
)

func (inst ReceiverKindE) String() string {
	switch inst {
	case ReceiverNone:
		return "none"
	case ReceiverIdent:
		return "identifier"
	case ReceiverCall:
		return "call"
	case ReceiverParen:
		return "parenthesised"
	}
	return "unknown"
}

// MemberAccess is the `X.` the caret sits after.
type MemberAccess struct {
	// Receiver is X's byte range.
	Receiver Range
	Kind     ReceiverKindE
	// Chain is the receiver's dotted identifier chain, outermost first —
	// `a.b.c.` yields ["a","b","c"]. Empty for a call or parenthesised
	// receiver, whose type comes from the typer instead.
	Chain []string
	// Callee is the receiver's function name when Kind is ReceiverCall.
	Callee string
	// Dot is the offset of the `.` itself.
	Dot int
}

// CaretSite is one frame's answer about where the caret is.
type CaretSite struct {
	// Entity is [EntityAt]'s answer, embedded rather than recomputed: the two
	// walks read the same spans and a consumer often wants both.
	Entity CaretEntity
	// Frames are the argument lists enclosing the caret, innermost first.
	Frames []CallFrame
	// Literal is the string literal the caret is inside, or nil.
	Literal *LiteralSite
	// Member is the member access the caret is completing, or nil.
	Member *MemberAccess
	// Partial is the byte range of the token completion would replace: the
	// literal's content when the caret is in one, the identifier under the
	// caret otherwise. Zero-length when the caret is on nothing replaceable.
	Partial Range
	// PartialText is what has been typed *before* the caret within Partial —
	// what a prefix match filters on.
	PartialText string
	// PartialFull is Partial's whole text, which equals PartialText when the
	// caret is at its end. What an *exact* match compares against, so that a
	// caret moved back inside a complete literal still reports that it
	// resolves (§SD9).
	PartialFull string
	// Open are the brackets still unclosed at the caret, outermost first — what
	// a repair must close before handing the statement to a parser (§SD3).
	// An unterminated literal is not in here; [CaretSite.Literal] carries it.
	Open []byte
	// Clause is the top-level keyword the caret sits under, upper-cased —
	// SELECT, FROM, WHERE, SETTINGS — or empty at depth where none was seen.
	// The coarse fallback for a position no call frame explains.
	Clause string
}

// InnerFrame is the innermost enclosing call, ok=false at statement level.
func (inst CaretSite) InnerFrame() (f CallFrame, ok bool) {
	if len(inst.Frames) == 0 {
		return
	}
	f = inst.Frames[0]
	ok = true
	return
}

// CaretAtPartialEnd reports whether the caret sits at the end of the token
// completion would extend — the condition ADR-0190 §SD10's suffix insert is
// valid under.
func (inst CaretSite) CaretAtPartialEnd() bool {
	return inst.PartialText == inst.PartialFull
}

// SiteAtIn is [SiteAt] over a buffer it lexes itself.
func SiteAtIn(sql string, off int) (site CaretSite) {
	return SiteAt(HighlightLex(sql), off)
}

// clauseKeywords are the top-level keywords the coarse clause rule reports.
// Deliberately short: it is the fallback for a caret no call frame explains,
// and a longer list would claim precision the rule does not have.
var clauseKeywords = map[string]struct{}{
	"SELECT": {}, "FROM": {}, "WHERE": {}, "GROUP": {}, "HAVING": {},
	"ORDER": {}, "LIMIT": {}, "SETTINGS": {}, "JOIN": {}, "ON": {},
	"USING": {}, "WITH": {}, "PREWHERE": {}, "FORMAT": {}, "INTO": {},
	"VALUES": {}, "SET": {}, "BY": {},
}

// keywordSyntaxSeparators are the in-call keywords that mean commas are not
// what separates this call's arguments.
var keywordSyntaxSeparators = map[string]struct{}{
	"AS": {}, "FROM": {},
}

// SiteAt resolves the caret at byte offset off against a lexed buffer.
//
// spans must be [HighlightLex]'s output for the buffer off indexes; the spans
// cover it contiguously, which is what lets the raw text of a literal be
// recovered from them.
//
// The scan is forward and single-pass. Everything from an unterminated
// literal's opening quote onwards is opaque to it — a comma or a bracket
// inside a literal being typed is text, not structure, and the lexer, which
// gap-fills what it cannot tokenise, hands them over as ordinary tokens.
func SiteAt(spans []Span, off int) (site CaretSite) {
	site.Partial = Range{Start: off, Stop: off}
	if len(spans) == 0 {
		return
	}
	site.Entity, _ = EntityAt(spans, off)

	lit := literalAt(spans, off)
	site.Literal = lit

	// Structure stops where an unterminated literal starts.
	limit := spans[len(spans)-1].Stop
	if lit != nil && !lit.Terminated() {
		limit = lit.Start
	}

	frames, open, clause, closed := scanFrames(spans, off, limit)
	site.Frames = frames
	site.Open = open
	site.Clause = clause

	setPartial(&site, spans, off, lit)
	site.Member = memberAt(spans, off, site.Partial.Start, lit, closed)
	return
}

// literalAt finds the string literal covering the caret.
//
// A terminated literal is one CatStringLit span, so it is found by coverage. An
// unterminated one is not a token at all: the lexer errors on the opening quote
// and gap-fills it as a CatPlain span, then tokenises the rest of the buffer
// ordinarily (the probes page §P3). Since it never closes, the LAST such quote
// at or before the caret is the one the caret is inside.
func literalAt(spans []Span, off int) (lit *LiteralSite) {
	for i := range spans {
		s := spans[i]
		if s.Start > off {
			break
		}
		if s.Category == CatStringLit {
			// The caret must be strictly inside, not on either quote: sitting
			// after the closing quote is being outside it.
			if off > s.Start && off < s.Stop && len(s.Text) >= 2 {
				body := s.Text[1 : len(s.Text)-1]
				lit = &LiteralSite{
					Quote: s.Text[0], Start: s.Start, Stop: s.Stop,
					Text: body, Prefix: body[:min(off-s.Start-1, len(body))],
				}
			}
			continue
		}
		if s.Category != CatPlain {
			continue
		}
		q := strings.LastIndexAny(s.Text, "'")
		if q < 0 {
			continue
		}
		qOff := s.Start + q
		if qOff >= off {
			continue
		}
		lit = &LiteralSite{Quote: '\'', Start: qOff, Stop: -1}
	}
	if lit != nil && !lit.Terminated() {
		lit.Prefix = textBetween(spans, lit.Start+1, off)
		lit.Text = lit.Prefix
	}
	return
}

// textBetween reconstructs the buffer's raw text over a byte range from the
// spans, which cover it contiguously.
func textBetween(spans []Span, start int, stop int) (s string) {
	if stop <= start {
		return
	}
	var b strings.Builder
	b.Grow(stop - start)
	for i := range spans {
		sp := spans[i]
		if sp.Stop <= start {
			continue
		}
		if sp.Start >= stop {
			break
		}
		lo := max(start-sp.Start, 0)
		hi := min(stop-sp.Start, len(sp.Text))
		if hi > lo {
			b.WriteString(sp.Text[lo:hi])
		}
	}
	s = b.String()
	return
}

// frameBuild is a frame under construction during the scan.
type frameBuild struct {
	CallFrame
	calleeStart int
	argStart    int
	argOpen     bool
	keywordSyn  bool
	closedAt    int
	caretArg    int
}

// scanFrames walks the spans once, maintaining a bracket stack.
//
// The frames open at the caret are the stack at the moment the scan reaches it;
// the scan continues past the caret so those frames also collect the arguments
// that follow it, since a domain may depend on a sibling on either side.
func scanFrames(spans []Span, off int, limit int) (frames []CallFrame, open []byte, clause string, closed []frameBuild) {
	built := make([]frameBuild, 0, 8)
	stack := make([]int, 0, 8)
	var snapshot []int
	snapped := false
	lastName := -1

	closeArg := func(fi int, stop int) {
		f := &built[fi]
		if !f.argOpen {
			return
		}
		f.Args = append(f.Args, trimRange(spans, Range{Start: f.argStart, Stop: stop}))
		f.argOpen = false
	}

	for i := range spans {
		s := spans[i]
		if s.Start >= limit {
			break
		}
		if !significant(s) {
			continue
		}
		if !snapped && s.Start >= off {
			snapshot = append([]int(nil), stack...)
			for _, fi := range stack {
				built[fi].caretArg = len(built[fi].Args)
			}
			snapped = true
		}
		switch s.Text {
		case "(", "[":
			f := frameBuild{calleeStart: -1, closedAt: -1, caretArg: -1}
			f.Open = s.Start
			f.Bracket = s.Text[0]
			f.argStart = s.Stop
			f.argOpen = true
			// A callee is any nameable token butted straight against the `(`.
			// Wider than enclosingCallees's CatFunctionName test on purpose:
			// the keyword-syntax calls (CAST, EXTRACT) lex their name as a
			// keyword, and they are exactly the frames whose ordinal rule
			// differs, so a site that could not name them could not report
			// that either. A false positive — `IN(1,2)`, `BY(a)` — costs
			// nothing: no roster declares those, so no domain resolves.
			if s.Text == "(" && lastName >= 0 && spans[lastName].Stop == s.Start {
				f.Callee = unquoteIdent(spans[lastName].Text)
				f.calleeStart = spans[lastName].Start
			}
			built = append(built, f)
			stack = append(stack, len(built)-1)
		case ")", "]":
			if len(stack) == 0 {
				break
			}
			fi := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			closeArg(fi, s.Start)
			built[fi].closedAt = s.Stop
		case ",":
			if len(stack) == 0 {
				break
			}
			fi := stack[len(stack)-1]
			closeArg(fi, s.Start)
			built[fi].argStart = s.Stop
			built[fi].argOpen = true
		default:
			if s.Category == CatKeyword {
				up := strings.ToUpper(s.Text)
				if len(stack) == 0 {
					if _, ok := clauseKeywords[up]; ok && s.Start < off {
						clause = up
					}
				} else if _, ok := keywordSyntaxSeparators[up]; ok && s.Start < off {
					built[stack[len(stack)-1]].keywordSyn = true
				}
			}
		}
		if nameable(s) {
			lastName = i
		}
	}
	if !snapped {
		snapshot = append([]int(nil), stack...)
		for _, fi := range stack {
			built[fi].caretArg = len(built[fi].Args)
		}
	}
	// Whatever is still open at the limit closes the last argument, so a
	// caret-side frame reports the argument it is in even when the buffer ends
	// mid-expression.
	for _, fi := range stack {
		closeArg(fi, limit)
	}

	open = make([]byte, 0, len(stack))
	for _, fi := range stack {
		open = append(open, built[fi].Bracket)
	}

	frames = make([]CallFrame, 0, len(snapshot))
	for i := len(snapshot) - 1; i >= 0; i-- {
		f := built[snapshot[i]]
		cf := f.CallFrame
		cf.Ordinal = f.caretArg
		if f.keywordSyn {
			cf.Ordinal = -1
		}
		frames = append(frames, cf)
	}
	// The closed frames go back to the caller for the member-access walk,
	// which needs to find the call a `)` before a `.` closes.
	closed = make([]frameBuild, 0, len(built))
	for i := range built {
		if built[i].closedAt >= 0 {
			closed = append(closed, built[i])
		}
	}
	return
}

// trimRange narrows a range to its significant content, so an argument's range
// is the expression and not the whitespace around it.
func trimRange(spans []Span, r Range) (out Range) {
	out = r
	for i := range spans {
		s := spans[i]
		if s.Stop <= r.Start || s.Start >= r.Stop {
			continue
		}
		if !significant(s) {
			continue
		}
		if out.Start == r.Start || s.Start < out.Start {
			out.Start = max(s.Start, r.Start)
		}
		break
	}
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		if s.Stop <= r.Start || s.Start >= r.Stop {
			continue
		}
		if !significant(s) {
			continue
		}
		out.Stop = min(s.Stop, r.Stop)
		break
	}
	if out.Stop < out.Start {
		out.Stop = out.Start
	}
	return
}

func setPartial(site *CaretSite, spans []Span, off int, lit *LiteralSite) {
	if lit != nil {
		start := lit.Start + 1
		stop := start + len(lit.Text)
		site.Partial = Range{Start: start, Stop: stop}
		site.PartialText = lit.Prefix
		site.PartialFull = lit.Text
		return
	}
	idx := spanIndexAt(spans, off)
	if idx < 0 {
		return
	}
	pick := -1
	if idx > 0 && spans[idx].Start == off && nameable(spans[idx-1]) {
		pick = idx - 1
	} else if nameable(spans[idx]) && off > spans[idx].Start {
		pick = idx
	}
	if pick < 0 {
		return
	}
	s := spans[pick]
	site.Partial = Range{Start: s.Start, Stop: s.Stop}
	site.PartialFull = s.Text
	site.PartialText = s.Text[:min(max(off-s.Start, 0), len(s.Text))]
}

// memberAt reads the `X.` in front of the partial, when there is one.
func memberAt(spans []Span, off int, partialStart int, lit *LiteralSite, closed []frameBuild) (m *MemberAccess) {
	if lit != nil {
		// `f('a.b` is text inside a literal, not a member access.
		return
	}
	at := min(partialStart, off)
	dot := -1
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		if s.Start >= at {
			continue
		}
		if !significant(s) {
			continue
		}
		if s.Text == "." {
			dot = i
		}
		break
	}
	if dot < 0 {
		return
	}
	prev := -1
	for i := dot - 1; i >= 0; i-- {
		if significant(spans[i]) {
			prev = i
			break
		}
	}
	if prev < 0 {
		return
	}
	m = &MemberAccess{Dot: spans[dot].Start}
	if spans[prev].Text == ")" {
		for i := range closed {
			if closed[i].closedAt == spans[prev].Stop {
				f := closed[i]
				m.Receiver = Range{Start: f.Open, Stop: f.closedAt}
				m.Kind = ReceiverParen
				if f.Callee != "" {
					m.Callee = f.Callee
					m.Kind = ReceiverCall
					m.Receiver.Start = f.calleeStart
				}
				return
			}
		}
		m = nil
		return
	}
	if !nameable(spans[prev]) {
		m = nil
		return
	}
	// Walk the identifier chain backwards: ident (. ident)*.
	chain := []string{unquoteIdent(spans[prev].Text)}
	start := spans[prev].Start
	i := prev - 1
	for i >= 0 {
		for i >= 0 && !significant(spans[i]) {
			i--
		}
		if i < 0 || spans[i].Text != "." {
			break
		}
		j := i - 1
		for j >= 0 && !significant(spans[j]) {
			j--
		}
		if j < 0 || !nameable(spans[j]) {
			break
		}
		chain = append(chain, unquoteIdent(spans[j].Text))
		start = spans[j].Start
		i = j - 1
	}
	for l, r := 0, len(chain)-1; l < r; l, r = l+1, r-1 {
		chain[l], chain[r] = chain[r], chain[l]
	}
	m.Kind = ReceiverIdent
	m.Chain = chain
	m.Receiver = Range{Start: start, Stop: spans[prev].Stop}
	return
}

// Rebase shifts every offset in the site by delta.
//
// For a consumer holding a site resolved against a whole buffer that needs one
// in a sub-range's coordinates — the caret's own statement, which is the unit a
// repair and a parse operate on (ADR-0190 §SD3). The text a rebased site
// describes must be the sub-range itself; nothing here checks that.
func (inst CaretSite) Rebase(delta int) (out CaretSite) {
	out = inst
	out.Partial = Range{Start: inst.Partial.Start + delta, Stop: inst.Partial.Stop + delta}
	if len(inst.Frames) > 0 {
		out.Frames = make([]CallFrame, len(inst.Frames))
		for i := range inst.Frames {
			f := inst.Frames[i]
			f.Open += delta
			if len(inst.Frames[i].Args) > 0 {
				f.Args = make([]Range, len(inst.Frames[i].Args))
				for j := range inst.Frames[i].Args {
					f.Args[j] = Range{
						Start: inst.Frames[i].Args[j].Start + delta,
						Stop:  inst.Frames[i].Args[j].Stop + delta,
					}
				}
			}
			out.Frames[i] = f
		}
	}
	if inst.Literal != nil {
		lit := *inst.Literal
		lit.Start += delta
		if lit.Stop >= 0 {
			lit.Stop += delta
		}
		out.Literal = &lit
	}
	if inst.Member != nil {
		m := *inst.Member
		m.Receiver = Range{Start: m.Receiver.Start + delta, Stop: m.Receiver.Stop + delta}
		m.Dot += delta
		out.Member = &m
	}
	out.Entity.Start += delta
	out.Entity.Stop += delta
	return
}
