package chviews

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// sqlLiteral renders a Go string as a ClickHouse string literal. Every value
// this package splices is a generated enum name or a separator, but escaping
// here rather than trusting that keeps a vocabulary addition from becoming a
// syntax error in a view body.
func sqlLiteral(s string) (lit string) {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('\'')
	return b.String()
}

// sqlStringArray renders a constant Array(String) literal.
func sqlStringArray(ss []string) (lit string) {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, sqlLiteral(s))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// sqlStringTuple renders a constant tuple, the form IN takes. An array
// literal reads as a set in ClickHouse too, but only the tuple form is what
// the system tables' database/table predicates are written against.
func sqlStringTuple(ss []string) (lit string) {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, sqlLiteral(s))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// sqlTransform renders a transform() over two parallel constant arrays —
// the shape every enum decode in this package takes. from and to must be the
// same length; a mismatch is a generator bug and is fatal rather than emitted
// as SQL that fails at CREATE time on every endpoint.
func sqlTransform(expr string, from []string, to []string, defaultExpr string) (sql string) {
	if len(from) != len(to) {
		log.Panic().Int("from", len(from)).Int("to", len(to)).Msg("transform tables differ in length")
	}
	if len(from) == 0 {
		return defaultExpr
	}
	return "transform(" + expr + ", " + sqlStringArray(from) + ", " + sqlStringArray(to) + ", " + defaultExpr + ")"
}

// selectList renders `expr AS alias` lines into a SELECT list. Alias width is
// not padded: a generated body is read in a terminal as often as in a file,
// and a widest-alias indent turns every addition into a whole-body diff.
func selectList(pairs [][2]string) (sql string) {
	lines := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p[0] == p[1] {
			lines = append(lines, "    "+p[0])
			continue
		}
		lines = append(lines, "    "+p[0]+" AS "+p[1])
	}
	return strings.Join(lines, ",\n")
}
