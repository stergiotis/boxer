package jackstay

import (
	"strings"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// QuoteIdent renders a ClickHouse identifier in backquotes. Every identifier
// is quoted, whether it needs it or not, so no name can change a statement's
// meaning.
func QuoteIdent(name string) (quoted string) {
	var b strings.Builder
	b.Grow(len(name) + 2)
	b.WriteByte('`')
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch == '`' || ch == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(ch)
	}
	b.WriteByte('`')
	return b.String()
}

func QuoteRef(ref datacatalog.TableRef) (quoted string) {
	return QuoteIdent(ref.Database) + "." + QuoteIdent(ref.Name)
}

// QuoteString renders s as a single-quoted ClickHouse string literal.
func QuoteString(s string) (quoted string) {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' || ch == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(ch)
	}
	b.WriteByte('\'')
	return b.String()
}

func CreateDatabaseDDL(name string) (sql string) {
	return "CREATE DATABASE IF NOT EXISTS " + QuoteIdent(name)
}

// RetargetCreateQuery rewrites a system.tables.create_table_query so it creates
// the same table under target. It replaces the leading `CREATE TABLE db.name`,
// drops a `UUID '…'` clause (the target mints its own), and adds IF NOT EXISTS.
// Everything after the name is kept verbatim. The query is refused when it does
// not start the way the server writes it, rather than being guessed at.
func RetargetCreateQuery(createQuery string, target datacatalog.TableRef) (sql string, err error) {
	var rest string
	rest, err = createBody(createQuery)
	if err != nil {
		return
	}
	sql = "CREATE TABLE IF NOT EXISTS " + QuoteRef(target) + rest
	return
}

// createBody is what follows the table's name in a create_table_query, with
// a `UUID '…'` clause dropped.
func createBody(createQuery string) (rest string, err error) {
	const prefix = "CREATE TABLE "
	if !strings.HasPrefix(createQuery, prefix) {
		err = eb.Build().Str("query", truncate(createQuery)).Errorf("create query does not start with CREATE TABLE")
		return
	}
	rest = createQuery[len(prefix):]
	var n int
	n, err = scanIdent(rest)
	if err != nil {
		err = eb.Build().Str("query", truncate(createQuery)).Errorf("unable to read database name: %w", err)
		return
	}
	if n >= len(rest) || rest[n] != '.' {
		err = eb.Build().Str("query", truncate(createQuery)).Errorf("create query does not name a qualified table")
		return
	}
	rest = rest[n+1:]
	n, err = scanIdent(rest)
	if err != nil {
		err = eb.Build().Str("query", truncate(createQuery)).Errorf("unable to read table name: %w", err)
		return
	}
	rest = rest[n:]
	if strings.HasPrefix(rest, " UUID '") {
		end := strings.IndexByte(rest[len(" UUID '"):], '\'')
		if end < 0 {
			err = eb.Build().Str("query", truncate(createQuery)).Errorf("unterminated UUID clause")
			return
		}
		rest = rest[len(" UUID '")+end+1:]
	}
	return
}

// createParts splits a create_table_query into the elements of its column
// list (column definitions, indices, projections, constraints) and the text
// after the list (the engine and its clauses). ok is false when the query is
// not shaped the way the server writes it.
func createParts(createQuery string) (elements []string, tail string, ok bool) {
	rest, err := createBody(createQuery)
	if err != nil || !strings.HasPrefix(rest, " (") {
		return
	}
	end := closingParen(rest, 1)
	if end < 0 {
		return
	}
	return SplitKeyExprs(rest[2:end]), rest[end+1:], true
}

// engineArgs returns the arguments of the ENGINE clause naming engine, each
// as written; none when the engine takes none. ok is false when the clause
// cannot be found.
func engineArgs(createQuery string, engine string) (args []string, ok bool) {
	_, tail, parsed := createParts(createQuery)
	if !parsed {
		return
	}
	clause := " ENGINE = " + engine
	i := strings.Index(tail, clause)
	if i < 0 {
		return
	}
	after := tail[i+len(clause):]
	switch {
	case after == "" || after[0] == ' ':
		ok = true
		return
	case after[0] != '(':
		return
	}
	end := closingParen(after, 0)
	if end < 0 {
		return
	}
	if inner := strings.TrimSpace(after[1:end]); inner != "" {
		args = SplitKeyExprs(inner)
	}
	ok = true
	return
}

// droppedColumnClauses lists the clauses of column name's definition in
// createQuery that [ColumnDefinition] cannot render, because system.columns
// does not report them: TTL, STATISTICS and SETTINGS.
func droppedColumnClauses(createQuery string, name string) (clauses []string) {
	elements, _, ok := createParts(createQuery)
	if !ok {
		return
	}
	for _, e := range elements {
		if !strings.HasPrefix(e, QuoteIdent(name)+" ") && !strings.HasPrefix(e, name+" ") {
			continue
		}
		blank := blankQuoted(e)
		for _, kw := range []string{"TTL", "STATISTICS", "SETTINGS"} {
			if strings.Contains(blank, " "+kw+" ") || strings.Contains(blank, " "+kw+"(") {
				clauses = append(clauses, kw)
			}
		}
		return
	}
	return
}

// blankQuoted replaces the contents of string literals and quoted
// identifiers in s, so a keyword search cannot match inside them.
func blankQuoted(s string) (blank string) {
	b := []byte(s)
	var quote byte
	for i := 0; i < len(b); i++ {
		if quote == 0 {
			switch b[i] {
			case '\'', '"', '`':
				quote = b[i]
			}
			continue
		}
		switch b[i] {
		case quote:
			quote = 0
		case '\\':
			b[i] = '_'
			if i+1 < len(b) {
				i++
				b[i] = '_'
			}
		default:
			b[i] = '_'
		}
	}
	return string(b)
}

// scanIdent returns the byte length of the identifier at the start of s: bare
// ([A-Za-z_][A-Za-z0-9_]*), or backquoted / double-quoted with backslash escapes.
func scanIdent(s string) (n int, err error) {
	if s == "" {
		err = eb.Build().Errorf("empty identifier")
		return
	}
	switch q := s[0]; q {
	case '`', '"':
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case q:
				n = i + 1
				return
			}
		}
		err = eb.Build().Errorf("unterminated quoted identifier")
		return
	}
	for n < len(s) {
		ch := s[n]
		isAlpha := ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		isDigit := ch >= '0' && ch <= '9'
		if !isAlpha && !(isDigit && n > 0) {
			break
		}
		n++
	}
	if n == 0 {
		err = eb.Build().Errorf("not an identifier")
	}
	return
}

// ColumnDefinition renders a column as it would appear in CREATE or ADD COLUMN:
// name, type, default clause, comment and codec, the parts system.columns
// reports. A column's TTL, STATISTICS and SETTINGS are not among them; only
// the table's CREATE carries them, and [Judge] notes a column that loses
// them.
func ColumnDefinition(c ColumnInfo) (def string) {
	var b strings.Builder
	b.WriteString(QuoteIdent(c.Name))
	b.WriteByte(' ')
	b.WriteString(c.Type)
	if c.DefaultKind != "" {
		b.WriteByte(' ')
		b.WriteString(c.DefaultKind)
		if c.DefaultExpression != "" {
			b.WriteByte(' ')
			b.WriteString(c.DefaultExpression)
		}
	}
	if c.Comment != "" {
		b.WriteString(" COMMENT ")
		b.WriteString(QuoteString(c.Comment))
	}
	if c.CompressionCodec != "" {
		b.WriteByte(' ')
		b.WriteString(c.CompressionCodec)
	}
	return b.String()
}

// AddColumnDDL adds c to target, placed after the column named after, or
// first when after is empty, so the target keeps the source's column order.
func AddColumnDDL(target datacatalog.TableRef, c ColumnInfo, after string) (sql string) {
	sql = "ALTER TABLE " + QuoteRef(target) + " ADD COLUMN IF NOT EXISTS " + ColumnDefinition(c)
	if after == "" {
		sql += " FIRST"
	} else {
		sql += " AFTER " + QuoteIdent(after)
	}
	return
}

func truncate(s string) (t string) {
	const limit = 160
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
