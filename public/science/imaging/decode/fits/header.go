package fits

import (
	"strconv"
	"strings"
)

// ValueKindE is what a header card's value is (FITS Standard 4.0 §4.2).
type ValueKindE uint8

const (
	// ValueKindNone is a card without a value: COMMENT, HISTORY, a blank
	// keyword, or a keyword not followed by the value indicator.
	ValueKindNone ValueKindE = iota
	// ValueKindUndefined is a card with the value indicator and an empty
	// value field.
	ValueKindUndefined
	ValueKindLogical
	ValueKindInteger
	ValueKindReal
	ValueKindComplex
	ValueKindString
	// ValueKindOpaque is a card kept as written: a HIERARCH card, or a value
	// this reader could not parse.
	ValueKindOpaque
)

func (inst ValueKindE) String() string {
	switch inst {
	case ValueKindNone:
		return "none"
	case ValueKindUndefined:
		return "undefined"
	case ValueKindLogical:
		return "logical"
	case ValueKindInteger:
		return "integer"
	case ValueKindReal:
		return "real"
	case ValueKindComplex:
		return "complex"
	case ValueKindString:
		return "string"
	case ValueKindOpaque:
		return "opaque"
	}
	return "unknown"
}

// Card is one header keyword record, for a person to read. A string value
// is its content, with the doubled quotes undone, the trailing blanks
// removed and any CONTINUE cards that carry it joined on (§4.2.1.2); a
// logical is "T" or "F"; a number is its text as written. Commentary text
// is in Comment.
type Card struct {
	Keyword string
	Value   string
	Comment string
	Kind    ValueKindE
}

const (
	cardLen     = 80
	blockLen    = 2880
	cardsPerBlk = blockLen / cardLen
)

// header is one HDU's cards and an index from keyword to the first card of
// that name.
type header struct {
	cards []Card
	index map[string]int
}

func newHeader() *header {
	return &header{cards: make([]Card, 0, cardsPerBlk), index: make(map[string]int, cardsPerBlk)}
}

// add appends a card, joining a CONTINUE card onto a string that ends in an
// ampersand.
func (inst *header) add(c Card) {
	if c.Keyword == "CONTINUE" && c.Kind == ValueKindString && len(inst.cards) > 0 {
		prev := &inst.cards[len(inst.cards)-1]
		if prev.Kind == ValueKindString && strings.HasSuffix(prev.Value, "&") {
			prev.Value = prev.Value[:len(prev.Value)-1] + c.Value
			if c.Comment != "" {
				if prev.Comment != "" {
					prev.Comment += " "
				}
				prev.Comment += c.Comment
			}
			return
		}
	}
	if _, seen := inst.index[c.Keyword]; !seen && c.Kind != ValueKindNone {
		inst.index[c.Keyword] = len(inst.cards)
	}
	inst.cards = append(inst.cards, c)
}

func (inst *header) card(kw string) (c Card, ok bool) {
	i, ok := inst.index[kw]
	if !ok {
		return c, false
	}
	return inst.cards[i], true
}

func (inst *header) has(kw string) bool {
	_, ok := inst.index[kw]
	return ok
}

// integer is an integer-valued keyword; present reports whether it is
// there, valid whether its value is an integer.
func (inst *header) integer(kw string) (v int64, present, valid bool) {
	c, ok := inst.card(kw)
	if !ok {
		return 0, false, false
	}
	if c.Kind != ValueKindInteger {
		return 0, true, false
	}
	v, err := strconv.ParseInt(strings.TrimPrefix(c.Value, "+"), 10, 64)
	return v, true, err == nil
}

// real is a number-valued keyword, integer or real.
func (inst *header) real(kw string) (v float64, present, valid bool) {
	c, ok := inst.card(kw)
	if !ok {
		return 0, false, false
	}
	if c.Kind != ValueKindInteger && c.Kind != ValueKindReal {
		return 0, true, false
	}
	v, err := strconv.ParseFloat(strings.Map(func(r rune) rune {
		if r == 'D' || r == 'd' {
			return 'E'
		}
		return r
	}, c.Value), 64)
	return v, true, err == nil && isFinite(v)
}

// str is a string-valued keyword's content.
func (inst *header) str(kw string) (v string, present bool) {
	c, ok := inst.card(kw)
	if !ok || c.Kind != ValueKindString {
		return "", ok
	}
	return c.Value, true
}

// logical is a logical-valued keyword.
func (inst *header) logical(kw string) (v, present, valid bool) {
	c, ok := inst.card(kw)
	if !ok {
		return false, false, false
	}
	if c.Kind != ValueKindLogical {
		return false, true, false
	}
	return c.Value == "T", true, true
}

// parseCard reads one 80-byte keyword record. Bytes outside printable ASCII
// are shown as '?'; the standard forbids them and nothing this reader
// interprets can contain one.
func parseCard(raw []byte) Card {
	var b [cardLen]byte
	for i, ch := range raw[:cardLen] {
		if ch < 0x20 || ch > 0x7e {
			ch = '?'
		}
		b[i] = ch
	}
	s := string(b[:])
	kw := strings.TrimRight(s[:8], " ")
	rest := s[8:]
	switch {
	case kw == "HIERARCH":
		return Card{Keyword: kw, Value: strings.TrimSpace(rest), Kind: ValueKindOpaque}
	case kw == "COMMENT" || kw == "HISTORY" || kw == "":
		return Card{Keyword: kw, Comment: strings.TrimSpace(rest)}
	case kw == "CONTINUE":
		c := parseValue(rest)
		c.Keyword = kw
		return c
	case strings.HasPrefix(rest, "= "):
		c := parseValue(rest[2:])
		c.Keyword = kw
		return c
	}
	return Card{Keyword: kw, Comment: strings.TrimSpace(rest)}
}

// parseValue reads a free-format value field and its comment (§4.2).
func parseValue(s string) (c Card) {
	t := strings.TrimLeft(s, " ")
	if t == "" || t[0] == '/' {
		c.Kind = ValueKindUndefined
		c.Comment = commentOf(t)
		return
	}
	switch t[0] {
	case '\'':
		var v strings.Builder
		i := 1
		closed := false
		for i < len(t) {
			if t[i] == '\'' {
				if i+1 < len(t) && t[i+1] == '\'' {
					v.WriteByte('\'')
					i += 2
					continue
				}
				closed = true
				i++
				break
			}
			v.WriteByte(t[i])
			i++
		}
		if !closed {
			return Card{Value: strings.TrimSpace(s), Kind: ValueKindOpaque}
		}
		c.Value = strings.TrimRight(v.String(), " ")
		c.Kind = ValueKindString
		c.Comment = commentOf(strings.TrimLeft(t[i:], " "))
		return
	case '(':
		end := strings.IndexByte(t, ')')
		if end < 0 {
			return Card{Value: strings.TrimSpace(s), Kind: ValueKindOpaque}
		}
		c.Value = strings.TrimSpace(t[1:end])
		c.Kind = ValueKindComplex
		c.Comment = commentOf(strings.TrimLeft(t[end+1:], " "))
		return
	}
	tok, comment := t, ""
	if i := strings.IndexByte(t, '/'); i >= 0 {
		tok, comment = t[:i], t[i:]
	}
	tok = strings.TrimSpace(tok)
	c.Value = tok
	c.Comment = commentOf(comment)
	switch {
	case tok == "T" || tok == "F":
		c.Kind = ValueKindLogical
	case isInteger(tok):
		c.Kind = ValueKindInteger
	case isReal(tok):
		c.Kind = ValueKindReal
	default:
		c.Kind = ValueKindOpaque
		c.Value = strings.TrimSpace(s)
		c.Comment = ""
	}
	return
}

func commentOf(t string) string {
	if t == "" {
		return ""
	}
	if t[0] == '/' {
		t = t[1:]
	}
	return strings.TrimSpace(t)
}

func digits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// isInteger is [+-]digits.
func isInteger(s string) bool {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	n := digits(s)
	return n > 0 && n == len(s)
}

// isReal is the standard's fixed or exponential form: [+-] digits with at
// most one decimal point and at least one digit, then optionally E or D, a
// sign and digits.
func isReal(s string) bool {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	n := digits(s)
	s = s[n:]
	m := 0
	if s != "" && s[0] == '.' {
		s = s[1:]
		m = digits(s)
		s = s[m:]
	}
	if n+m == 0 {
		return false
	}
	if s == "" {
		return true
	}
	if s[0] != 'E' && s[0] != 'D' && s[0] != 'e' && s[0] != 'd' {
		return false
	}
	s = s[1:]
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	k := digits(s)
	return k > 0 && k == len(s)
}
