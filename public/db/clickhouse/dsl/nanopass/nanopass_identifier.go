package nanopass

import "strings"

// DecodeIdentifier converts identifier token text to the raw name it
// denotes:
//
//	bare_ident   → bare_ident
//	"dq""uoted"  → dq"uoted
//	`bt``icked`  → bt`icked
//	"esc\"aped"  → esc"aped   (lexer escape: BACKSLASH followed by any char)
//
// Backslash escapes decode as the raw following character. ClickHouse
// additionally interprets control sequences (\n, \t, …) inside quoted
// identifiers; this decoder deliberately does not — it exists so that two
// spellings of the same name compare equal after decoding, and both sides
// of every comparison in this package go through it. Re-encode with
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
	var b strings.Builder
	b.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c == '\\' && i+1 < len(inner) {
			b.WriteByte(inner[i+1])
			i++
			continue
		}
		if c == q && i+1 < len(inner) && inner[i+1] == q {
			b.WriteByte(q)
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
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
