package recordstore

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ColumnSpec is one column a store's generated CREATE TABLE declares: the
// physical (unquoted) name, the ClickHouse type as the DDL spells it, and
// the CODEC argument list ("" when the column declares none). The
// generator parses it out of the DDL it composes, so the list and the
// DDL file cannot disagree.
type ColumnSpec struct {
	Name  string
	Type  string
	Codec string
}

// ColumnNames is the names of cols, in order.
func ColumnNames(cols []ColumnSpec) (names []string) {
	names = make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.Name)
	}
	return
}

// SelectList renders the fields of schema as an explicit SELECT projection
// of double-quoted identifiers. A generated store reads through it rather
// than `SELECT *`, so a table carrying columns the store does not know
// (a newer schema, or an operator's ALTER) is read all the same, and one
// lacking a column the store reads fails naming it.
func SelectList(schema *arrow.Schema) (list string) {
	var b strings.Builder
	for i, f := range schema.Fields() {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(QuoteIdentifier(f.Name))
	}
	return b.String()
}

// QuoteIdentifier renders a ClickHouse identifier in double quotes, the
// quoting the generated DDL uses for leeway physical names.
func QuoteIdentifier(name string) (quoted string) {
	var b strings.Builder
	b.Grow(len(name) + 2)
	b.WriteByte('"')
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch == '"' || ch == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(ch)
	}
	b.WriteByte('"')
	return b.String()
}

// ColumnIndexI is the slice of a generated read-access reader that binds it
// to record columns: the indices it reads, in its own order, and the
// setter taking them back in that order. Every generated reader satisfies
// it (the leeway read-access runtime's ColumnIndexHandlingI).
type ColumnIndexI interface {
	GetColumnIndices() (columnIndices []uint32)
	SetColumnIndices(indices []uint32) (restIndices []uint32)
}

// BindColumnsByName points readers at the columns of got by name. Each
// reader's current indices are positions in want — the generated schema
// the reader was built against, whose order its constructor defaults
// assume — and are replaced by the position of the same name in got.
// Columns of got no reader names are ignored. A name a reader needs and got
// lacks fails, naming every such column.
func BindColumnsByName(want *arrow.Schema, got *arrow.Schema, readers ...ColumnIndexI) (err error) {
	wantFields := want.Fields()
	var missing []string
	seen := map[string]bool{}
	for _, r := range readers {
		idx := r.GetColumnIndices()
		remapped := make([]uint32, len(idx))
		for i, w := range idx {
			if int(w) >= len(wantFields) {
				err = eb.Build().Uint32("index", w).Int("columns", len(wantFields)).Errorf("reader index outside the generated schema")
				return
			}
			name := wantFields[w].Name
			pos := got.FieldIndices(name)
			if len(pos) == 0 {
				if !seen[name] {
					seen[name] = true
					missing = append(missing, name)
				}
				continue
			}
			remapped[i] = uint32(pos[0])
		}
		if len(missing) == 0 {
			_ = r.SetColumnIndices(remapped)
		}
	}
	if len(missing) > 0 {
		err = eb.Build().Strs("columns", missing).Errorf("the result lacks %d column(s) this store reads (%s); the table is older than the store — `boxer schema plan` lists the statements that add them", len(missing), strings.Join(missing, ", "))
	}
	return
}

// ParseCreateTableColumns reads the column list out of a CREATE TABLE
// statement in the shape the leeway DDL composer writes: one element per
// column, `"name" Type [CODEC(...)]`, INDEX / PROJECTION / CONSTRAINT
// elements skipped. A script with a CREATE DATABASE in front is accepted;
// the first CREATE TABLE is the one read. It refuses rather than guesses.
func ParseCreateTableColumns(ddl string) (cols []ColumnSpec, err error) {
	upper := strings.ToUpper(ddl)
	at := strings.Index(upper, "CREATE TABLE")
	if at < 0 {
		at = strings.Index(upper, "CREATE OR REPLACE TABLE")
	}
	if at < 0 {
		err = eh.Errorf("no CREATE TABLE in the statement")
		return
	}
	open := strings.IndexByte(ddl[at:], '(')
	if open < 0 {
		err = eh.Errorf("CREATE TABLE has no column list")
		return
	}
	body, ok := balanced(ddl[at+open:])
	if !ok {
		err = eh.Errorf("CREATE TABLE column list is not closed")
		return
	}
	for _, el := range splitTopLevel(body, ',') {
		el = strings.TrimSpace(el)
		if el == "" {
			continue
		}
		head := strings.ToUpper(el)
		if strings.HasPrefix(head, "INDEX ") || strings.HasPrefix(head, "PROJECTION ") || strings.HasPrefix(head, "CONSTRAINT ") {
			continue
		}
		var c ColumnSpec
		c, err = parseColumnElement(el)
		if err != nil {
			return
		}
		cols = append(cols, c)
	}
	if len(cols) == 0 {
		err = eh.Errorf("CREATE TABLE declares no columns")
	}
	return
}

// columnClauseKeywords end a column's type in a column element.
var columnClauseKeywords = map[string]bool{
	"DEFAULT": true, "MATERIALIZED": true, "ALIAS": true, "EPHEMERAL": true,
	"CODEC": true, "COMMENT": true, "TTL": true, "STATISTICS": true, "SETTINGS": true,
	"NULL": true, "NOT": true, "PRIMARY": true,
}

func parseColumnElement(el string) (c ColumnSpec, err error) {
	rest := el
	switch rest[0] {
	case '"', '`':
		q := rest[0]
		i := 1
		var name strings.Builder
		for ; i < len(rest); i++ {
			if rest[i] == '\\' && i+1 < len(rest) {
				i++
				name.WriteByte(rest[i])
				continue
			}
			if rest[i] == q {
				break
			}
			name.WriteByte(rest[i])
		}
		if i >= len(rest) {
			err = eb.Build().Str("element", el).Errorf("unterminated column name")
			return
		}
		c.Name = name.String()
		rest = rest[i+1:]
	default:
		sp := strings.IndexAny(rest, " \t\n")
		if sp < 0 {
			err = eb.Build().Str("element", el).Errorf("column element has no type")
			return
		}
		c.Name = rest[:sp]
		rest = rest[sp:]
	}
	rest = strings.TrimSpace(rest)
	// The type runs to the first clause keyword outside parentheses and quotes.
	typeEnd := len(rest)
	depth := 0
	var quote byte
	for i := 0; i < len(rest); i++ {
		ch := rest[i]
		switch {
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case depth == 0 && (ch == ' ' || ch == '\t' || ch == '\n'):
			word := rest[i+1:]
			if j := strings.IndexAny(word, " \t\n("); j >= 0 {
				word = word[:j]
			}
			if columnClauseKeywords[strings.ToUpper(word)] {
				typeEnd = i
				i = len(rest)
			}
		}
	}
	c.Type = strings.TrimSpace(rest[:typeEnd])
	if c.Type == "" {
		err = eb.Build().Str("element", el).Errorf("column element has no type")
		return
	}
	clauses := rest[typeEnd:]
	if k := indexTopLevelWord(clauses, "CODEC"); k >= 0 {
		after := strings.TrimSpace(clauses[k+len("CODEC"):])
		inner, ok := balanced(after)
		if !ok {
			err = eb.Build().Str("element", el).Errorf("CODEC is not closed")
			return
		}
		c.Codec = strings.TrimSpace(inner)
	}
	return
}

// indexTopLevelWord finds word as a whole word outside quotes and
// parentheses, case-insensitively; -1 when absent.
func indexTopLevelWord(s string, word string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case depth == 0 && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t' || s[i-1] == '\n'):
			if len(s)-i >= len(word) && strings.EqualFold(s[i:i+len(word)], word) {
				end := i + len(word)
				if end == len(s) || s[end] == '(' || s[end] == ' ' {
					return i
				}
			}
		}
	}
	return -1
}

// balanced takes s starting at '(' and returns what lies inside the
// matching ')', honouring quotes.
func balanced(s string) (inner string, ok bool) {
	if s == "" || s[0] != '(' {
		return
	}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return
}

// splitTopLevel splits s at sep outside quotes and parentheses.
func splitTopLevel(s string, sep byte) (parts []string) {
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case ch == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return
}

// NormalizeType is a ClickHouse type with the whitespace outside string
// literals removed, so the DDL's `DateTime64(9,'UTC')` and the server's
// `DateTime64(9, 'UTC')` compare equal.
func NormalizeType(t string) (n string) {
	var b strings.Builder
	b.Grow(len(t))
	var quote bool
	for i := 0; i < len(t); i++ {
		ch := t[i]
		if quote {
			b.WriteByte(ch)
			if ch == '\\' && i+1 < len(t) {
				i++
				b.WriteByte(t[i])
			} else if ch == '\'' {
				quote = false
			}
			continue
		}
		switch ch {
		case '\'':
			quote = true
			b.WriteByte(ch)
		case ' ', '\t', '\n', '\r':
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// NormalizeCodec is a codec argument list or a `CODEC(...)` clause reduced
// to the bare argument list without whitespace.
func NormalizeCodec(c string) (n string) {
	c = strings.TrimSpace(c)
	if len(c) > len("CODEC(") && strings.EqualFold(c[:len("CODEC(")], "CODEC(") {
		if inner, ok := balanced(c[len("CODEC"):]); ok {
			c = inner
		}
	}
	return NormalizeType(c)
}

// stripLowCardinality removes every LowCardinality(...) wrapper of a
// normalized type at any nesting.
func stripLowCardinality(typ string) (t string) {
	const w = "LowCardinality("
	t = typ
	for {
		i := strings.Index(t, w)
		if i < 0 {
			return
		}
		inner, ok := balanced(t[i+len(w)-1:])
		if !ok {
			return
		}
		t = t[:i] + inner + t[i+len(w)+len(inner)+1:]
	}
}

// AspectFreeKey is a leeway physical column name with its aspect segments
// blanked: the encoding hints and value aspects of a plain column, and the
// encoding hints, use aspects and value aspects of a tagged-value column
// (ADR-0182). Two names with one key differ only in aspects — a codec or
// vocabulary change renamed the column, the data it holds is the same. ok
// is false for a name that is not leeway-shaped.
func AspectFreeKey(name string) (key string, ok bool) {
	segs := strings.Split(name, ":")
	switch {
	case len(segs) == 11 && segs[0] == "tv":
		segs[5], segs[6], segs[7] = "", "", ""
	case len(segs) == 7 && segs[0] != "tv":
		segs[3], segs[4] = "", ""
	default:
		return
	}
	key = strings.Join(segs, ":")
	ok = true
	return
}
