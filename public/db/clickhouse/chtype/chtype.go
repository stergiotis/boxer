package chtype

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Type is a parsed ClickHouse type: a family name and its arguments.
//
// The zero value is not a type; [Parse] never returns it without an error.
type Type struct {
	// Name is the family name as written — `UInt8`, `Nullable`, `Tuple`. It
	// keeps the server's capitalisation, which ClickHouse treats as
	// significant for type names.
	Name string
	// Args is empty for a plain name.
	Args []Arg
}

// Arg is one entry of a type's argument list. Exactly one shape is populated:
//
//   - a named element — Name and Type, from `Tuple(a UInt8)` / `Nested(…)`;
//   - a nested type — Type alone, from `Array(String)` / `Map(K, V)`;
//   - a literal — Literal alone, from `FixedString(16)` / `DateTime64(9,'UTC')`;
//   - an enum member — Name (still quoted, as written) and Literal, from
//     `Enum8('a' = 1)`.
//
// The enum member keeps its quotes in Name because that is what round-trips;
// [Type.EnumValues] is the accessor that strips them.
type Arg struct {
	Name    string
	Type    *Type
	Literal string
}

// Parse reads one ClickHouse type string.
//
// Whitespace between tokens is insignificant, so a `DESCRIBE` answer
// pretty-printed across lines parses the same as the one-line form. Trailing
// text after a complete type is an error rather than a silent truncation: a
// caller handing over a column definition instead of a type should learn so.
func Parse(s string) (t Type, err error) {
	p := parser{src: s}
	t, err = p.parseType()
	if err != nil {
		return
	}
	p.skipSpace()
	if p.pos < len(p.src) {
		err = eb.Build().Str("type", s).Int("at", p.pos).
			Errorf("trailing text after a complete type")
		return
	}
	return
}

// String renders the canonical one-line spelling. Parse and String round-trip:
// re-parsing String's output yields an equal Type.
func (inst Type) String() (s string) {
	var b strings.Builder
	inst.writeTo(&b)
	s = b.String()
	return
}

func (inst Type) writeTo(b *strings.Builder) {
	b.WriteString(inst.Name)
	if len(inst.Args) == 0 {
		return
	}
	b.WriteByte('(')
	for i := range inst.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		inst.Args[i].writeTo(b)
	}
	b.WriteByte(')')
}

func (inst Arg) writeTo(b *strings.Builder) {
	switch {
	case inst.Name != "" && inst.Type != nil:
		b.WriteString(inst.Name)
		b.WriteByte(' ')
		inst.Type.writeTo(b)
	case inst.Name != "":
		b.WriteString(inst.Name)
		b.WriteString(" = ")
		b.WriteString(inst.Literal)
	case inst.Type != nil:
		inst.Type.writeTo(b)
	default:
		b.WriteString(inst.Literal)
	}
}

// Unwrap strips the wrappers that do not change what the value *is*:
// `Nullable` and `LowCardinality`, in any nesting order. `Array` is not one of
// them — an array of tuples is not a tuple.
func (inst Type) Unwrap() (t Type) {
	t = inst
	for {
		if len(t.Args) != 1 || t.Args[0].Type == nil {
			return
		}
		switch t.Name {
		case "Nullable", "LowCardinality":
			t = *t.Args[0].Type
		default:
			return
		}
	}
}

// Elements returns the named elements of a Tuple or Nested, after [Type.Unwrap].
//
// ok is false for anything else, and for a positional tuple
// (`Tuple(UInt8, String)`) whose elements have no names to offer. A tuple that
// names only some of its elements is not a shape ClickHouse produces; it is
// reported as unnamed rather than half-answered.
func (inst Type) Elements() (elems []Arg, ok bool) {
	u := inst.Unwrap()
	switch u.Name {
	case "Tuple", "Nested":
	default:
		return
	}
	if len(u.Args) == 0 {
		return
	}
	for i := range u.Args {
		if u.Args[i].Name == "" || u.Args[i].Type == nil {
			return
		}
	}
	elems = u.Args
	ok = true
	return
}

// EnumValues returns an Enum8/Enum16's member names, unquoted, in declaration
// order. ok is false for any other type.
func (inst Type) EnumValues() (vals []string, ok bool) {
	u := inst.Unwrap()
	switch u.Name {
	case "Enum", "Enum8", "Enum16":
	default:
		return
	}
	vals = make([]string, 0, len(u.Args))
	for i := range u.Args {
		n := u.Args[i].Name
		if n == "" || u.Args[i].Type != nil {
			vals = nil
			return
		}
		vals = append(vals, unquote(n))
	}
	ok = true
	return
}

// ElementNames is Elements reduced to the names, for a caller that offers them
// as completion candidates.
func (inst Type) ElementNames() (names []string, ok bool) {
	elems, ok := inst.Elements()
	if !ok {
		return
	}
	names = make([]string, len(elems))
	for i := range elems {
		names[i] = unquoteIdent(elems[i].Name)
	}
	return
}

// Element looks one named element up by name. Backtick-quoted element names
// match on their unquoted spelling.
func (inst Type) Element(name string) (a Arg, ok bool) {
	elems, ok := inst.Elements()
	if !ok {
		return
	}
	for i := range elems {
		if unquoteIdent(elems[i].Name) == name {
			a = elems[i]
			ok = true
			return
		}
	}
	a = Arg{}
	ok = false
	return
}

type parser struct {
	src string
	pos int
}

func (inst *parser) skipSpace() {
	for inst.pos < len(inst.src) {
		switch inst.src[inst.pos] {
		case ' ', '\t', '\n', '\r':
			inst.pos++
		default:
			return
		}
	}
}

func (inst *parser) parseType() (t Type, err error) {
	inst.skipSpace()
	name, ok := inst.readIdent()
	if !ok {
		err = eb.Build().Str("type", inst.src).Int("at", inst.pos).
			Errorf("expected a type name")
		return
	}
	t.Name = name
	inst.skipSpace()
	if inst.pos >= len(inst.src) || inst.src[inst.pos] != '(' {
		return
	}
	inst.pos++
	t.Args = make([]Arg, 0, 4)
	pendingComma := false
	for {
		inst.skipSpace()
		if inst.pos >= len(inst.src) {
			err = eb.Build().Str("type", inst.src).Errorf("unterminated argument list")
			return
		}
		if inst.src[inst.pos] == ')' {
			inst.pos++
			if len(t.Args) == 0 {
				// `Tuple()` is not a type ClickHouse writes; refusing it here
				// keeps Args non-empty whenever the parens were present.
				err = eb.Build().Str("type", inst.src).Errorf("empty argument list")
				return
			}
			if pendingComma {
				err = eb.Build().Str("type", inst.src).Int("at", inst.pos).
					Errorf("trailing comma in an argument list")
			}
			return
		}
		var a Arg
		a, err = inst.parseArg()
		if err != nil {
			return
		}
		t.Args = append(t.Args, a)
		pendingComma = false
		inst.skipSpace()
		if inst.pos >= len(inst.src) {
			err = eb.Build().Str("type", inst.src).Errorf("unterminated argument list")
			return
		}
		switch inst.src[inst.pos] {
		case ',':
			inst.pos++
			pendingComma = true
		case ')':
		default:
			err = eb.Build().Str("type", inst.src).Int("at", inst.pos).
				Errorf("expected a comma or a closing paren")
			return
		}
	}
}

func (inst *parser) parseArg() (a Arg, err error) {
	inst.skipSpace()
	if inst.pos >= len(inst.src) {
		err = eb.Build().Str("type", inst.src).Errorf("expected an argument")
		return
	}
	c := inst.src[inst.pos]
	if c == '\'' {
		// A quoted name is either an enum member (`'a' = 1`) or a plain
		// literal argument (`Object('json')`); the `=` is what tells them
		// apart.
		lit, ok := inst.readString()
		if !ok {
			err = eb.Build().Str("type", inst.src).Int("at", inst.pos).
				Errorf("unterminated string literal")
			return
		}
		save := inst.pos
		inst.skipSpace()
		if inst.pos < len(inst.src) && inst.src[inst.pos] == '=' {
			inst.pos++
			inst.skipSpace()
			num, okNum := inst.readNumber()
			if !okNum {
				err = eb.Build().Str("type", inst.src).Int("at", inst.pos).
					Errorf("expected an enum member value")
				return
			}
			a.Name = lit
			a.Literal = num
			return
		}
		inst.pos = save
		a.Literal = lit
		return
	}
	if c == '-' || c == '+' || (c >= '0' && c <= '9') {
		num, ok := inst.readNumber()
		if !ok {
			err = eb.Build().Str("type", inst.src).Int("at", inst.pos).
				Errorf("expected a numeric literal")
			return
		}
		a.Literal = num
		return
	}

	start := inst.pos
	name, ok := inst.readIdent()
	if !ok {
		err = eb.Build().Str("type", inst.src).Int("at", inst.pos).
			Errorf("expected a type name, an element name or a literal")
		return
	}
	inst.skipSpace()
	// `a UInt8` is a named element; `UInt8`, `Array(…)` and `sum` are types.
	// An identifier followed by another identifier is the only case where the
	// first one is a name rather than a type.
	if inst.pos < len(inst.src) && isIdentStart(inst.src[inst.pos]) {
		var elem Type
		elem, err = inst.parseType()
		if err != nil {
			return
		}
		a.Name = name
		a.Type = &elem
		return
	}
	inst.pos = start
	var t Type
	t, err = inst.parseType()
	if err != nil {
		return
	}
	a.Type = &t
	return
}

func (inst *parser) readIdent() (s string, ok bool) {
	if inst.pos >= len(inst.src) {
		return
	}
	if inst.src[inst.pos] == '`' {
		start := inst.pos
		inst.pos++
		for inst.pos < len(inst.src) {
			if inst.src[inst.pos] == '\\' && inst.pos+1 < len(inst.src) {
				inst.pos += 2
				continue
			}
			if inst.src[inst.pos] == '`' {
				inst.pos++
				s = inst.src[start:inst.pos]
				ok = true
				return
			}
			inst.pos++
		}
		inst.pos = start
		return
	}
	if !isIdentStart(inst.src[inst.pos]) {
		return
	}
	start := inst.pos
	for inst.pos < len(inst.src) && isIdentPart(inst.src[inst.pos]) {
		inst.pos++
	}
	s = inst.src[start:inst.pos]
	ok = true
	return
}

func (inst *parser) readString() (s string, ok bool) {
	start := inst.pos
	inst.pos++ // the opening quote
	for inst.pos < len(inst.src) {
		switch inst.src[inst.pos] {
		case '\\':
			inst.pos += 2
		case '\'':
			if inst.pos+1 < len(inst.src) && inst.src[inst.pos+1] == '\'' {
				inst.pos += 2
				continue
			}
			inst.pos++
			s = inst.src[start:inst.pos]
			ok = true
			return
		default:
			inst.pos++
		}
	}
	inst.pos = start
	return
}

func (inst *parser) readNumber() (s string, ok bool) {
	start := inst.pos
	if inst.pos < len(inst.src) && (inst.src[inst.pos] == '-' || inst.src[inst.pos] == '+') {
		inst.pos++
	}
	digits := inst.pos
	for inst.pos < len(inst.src) {
		c := inst.src[inst.pos]
		if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' {
			inst.pos++
			continue
		}
		if (c == '-' || c == '+') && inst.pos > digits {
			p := inst.src[inst.pos-1]
			if p == 'e' || p == 'E' {
				inst.pos++
				continue
			}
		}
		break
	}
	if inst.pos == digits {
		inst.pos = start
		return
	}
	s = inst.src[start:inst.pos]
	ok = true
	return
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '`' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// unquote strips the surrounding single quotes of a SQL string literal and
// resolves the escapes inside it.
func unquote(s string) (v string) {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		v = s
		return
	}
	return Unescape(s[1 : len(s)-1])
}

func unquoteIdent(s string) (v string) {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		v = strings.ReplaceAll(s[1:len(s)-1], "\\`", "`")
		return
	}
	v = s
	return
}

// Unescape resolves the escapes of a ClickHouse single-quoted string literal's
// *body* — the text between the quotes. It is exported because a caller holding
// a type string that arrived inside another literal (a generated Projection's
// CAST argument, ADR-0190 §SD6) must undo that quoting before parsing.
func Unescape(body string) (v string) {
	if !strings.ContainsAny(body, "\\'") {
		v = body
		return
	}
	var b strings.Builder
	b.Grow(len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == '\\' && i+1 < len(body) {
			i++
			switch body[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			default:
				b.WriteByte(body[i])
			}
			continue
		}
		if c == '\'' && i+1 < len(body) && body[i+1] == '\'' {
			b.WriteByte('\'')
			i++
			continue
		}
		b.WriteByte(c)
	}
	v = b.String()
	return
}
