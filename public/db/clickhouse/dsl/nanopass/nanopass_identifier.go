package nanopass

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DecodeIdentifier converts identifier token text to the raw name it
// denotes:
//
//	bare_ident   → bare_ident
//	"dq""uoted"  → dq"uoted
//	`bt``icked`  → bt`icked
//	"esc\"aped"  → esc"aped
//	"a\nb"       → a<LF>b
//
// Backslash escapes decode as the server decodes them ([UnescapeQuoted]),
// so two spellings compare equal after decoding exactly when ClickHouse
// resolves them to the same name. A quoted token with a malformed \x
// escape, which the server rejects, is returned undecoded. Re-encode with
// [QuoteIdentifier].
func DecodeIdentifier(s string) string {
	if len(s) < 2 {
		return s
	}
	q := s[0]
	if (q != '"' && q != '`') || s[len(s)-1] != q {
		return s
	}
	inner := s[1 : len(s)-1]
	if !strings.ContainsAny(inner, "\\\"`") {
		return inner
	}
	name, err := UnescapeQuoted(inner, q)
	if err != nil {
		return s
	}
	return name
}

// QuoteIdentifier encodes a raw name as a double-quoted identifier token.
// Backslashes and double quotes are escaped (the lexer treats BACKSLASH
// followed by any char as an escape, so a literal backslash must be
// doubled). Inverse of [DecodeIdentifier] for double-quoted output.
func QuoteIdentifier(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`""`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// UnescapeQuoted decodes the body of a quoted token — a string literal or
// a quoted identifier, without its enclosing quotes — the way the
// ClickHouse server does (parseComplexEscapeSequence in ReadHelpers.cpp):
//
//   - \a \b \e \f \n \r \t \v \0 decode to their control byte;
//   - \xHH decodes to one byte;
//   - \N decodes to nothing;
//   - \\ \' \" \` \/ \= and a backslash before a control byte decode to
//     the following byte alone;
//   - any other backslash sequence keeps its backslash, so '\d+' stays the
//     regex \d+ and 'a\_b' stays the LIKE pattern a\_b. There is no \u or
//     \U escape: '\u0041' is six bytes of text.
//
// A doubled quote byte decodes to one. The only error is a malformed \x.
func UnescapeQuoted(inner string, quote byte) (result string, err error) {
	var buf strings.Builder
	buf.Grow(len(inner))
	i := 0
	for i < len(inner) {
		ch := inner[i]
		switch {
		case ch == '\\' && i+1 < len(inner):
			next := inner[i+1]
			switch next {
			case 'x':
				if i+3 >= len(inner) {
					err = eb.Build().Int("position", i).Errorf("truncated \\x escape")
					return
				}
				val, parseErr := strconv.ParseUint(inner[i+2:i+4], 16, 8)
				if parseErr != nil {
					err = eb.Build().Int("position", i).Errorf("invalid \\x escape: %w", parseErr)
					return
				}
				buf.WriteByte(byte(val))
				i += 4
				continue
			case 'N':
				i += 2
				continue
			}
			decoded := decodeEscapeByte(next)
			switch {
			case decoded == '\\', decoded == '\'', decoded == '"', decoded == '`',
				decoded == '/', decoded == '=', decoded <= 31:
			default:
				buf.WriteByte('\\')
			}
			buf.WriteByte(decoded)
			i += 2
		case ch == quote && i+1 < len(inner) && inner[i+1] == quote:
			buf.WriteByte(quote)
			i += 2
		default:
			buf.WriteByte(ch)
			i++
		}
	}
	result = buf.String()
	return
}

// decodeEscapeByte is the server's single-character escape table
// (parseEscapeSequence); an unnamed byte maps to itself.
func decodeEscapeByte(c byte) byte {
	switch c {
	case 'a':
		return '\a'
	case 'b':
		return '\b'
	case 'e':
		return 0x1b
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'v':
		return '\v'
	case '0':
		return 0
	default:
		return c
	}
}
