package recordstore

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// StatementClassE is what an Exec'd statement does to data, as far as its
// leading keywords say (ADR-0295 §SD4).
type StatementClassE uint8

const (
	StatementClassUnspecified StatementClassE = 0
	// StatementClassDDL is schema and object management. It is not only
	// schema: DROP, TRUNCATE and ALTER … DROP PARTITION remove data.
	StatementClassDDL StatementClassE = 1
	// StatementClassMutation rewrites or removes rows: ALTER … UPDATE,
	// ALTER … DELETE, ALTER … APPLY DELETED MASK, a lightweight DELETE FROM
	// and a lightweight UPDATE … SET.
	StatementClassMutation StatementClassE = 2
	// StatementClassOther is everything else — SELECT, INSERT … SELECT,
	// SYSTEM, OPTIMIZE, SET — and any statement the lexer does not recognise.
	StatementClassOther StatementClassE = 3
)

var AllStatementClasses = []StatementClassE{StatementClassUnspecified, StatementClassDDL, StatementClassMutation, StatementClassOther}

func (inst StatementClassE) String() string {
	switch inst {
	case StatementClassDDL:
		return "ddl"
	case StatementClassMutation:
		return "mutation"
	case StatementClassOther:
		return "other"
	default:
		return "unspecified"
	}
}

// ClassifyStatement classifies sql by its leading keywords and names the table
// it addresses, best effort: an empty table means none was recognised. It is a
// lexer over the statement's head, not a parser — comments and quoting are
// understood, grammar beyond the head is not.
func ClassifyStatement(sql string) (class StatementClassE, table string) {
	toks := lexHead(sql)
	if len(toks) == 0 {
		return StatementClassOther, ""
	}
	switch kw(toks, 0) {
	case "CREATE", "DROP", "RENAME", "ATTACH", "DETACH", "EXCHANGE", "UNDROP":
		return StatementClassDDL, nameAfterObjectKeyword(toks)
	case "TRUNCATE":
		// TRUNCATE DATABASE and TRUNCATE ALL TABLES FROM empty many tables at
		// once and name none.
		if k := kw(toks, 1); k == "DATABASE" || k == "ALL" {
			return StatementClassDDL, ""
		}
		return StatementClassDDL, nameAfterSkipping(toks, 1, "TEMPORARY", "TABLE", "IF", "EXISTS")
	case "ALTER":
		return classifyAlter(toks)
	case "DELETE":
		if i := indexKw(toks, "FROM"); i >= 0 {
			return StatementClassMutation, nameAt(toks, i+1)
		}
		return StatementClassMutation, ""
	case "UPDATE":
		return StatementClassMutation, nameAt(toks, 1)
	case "INSERT":
		if i := indexKw(toks, "INTO"); i >= 0 {
			if kw(toks, i+1) == "FUNCTION" {
				return StatementClassOther, "" // a table function is not a table
			}
			return StatementClassOther, nameAfterSkipping(toks, i+1, "TABLE")
		}
		return StatementClassOther, ""
	case "OPTIMIZE":
		return StatementClassOther, nameAfterSkipping(toks, 1, "TABLE")
	}
	return StatementClassOther, QueryTable(sql)
}

// QueryTable names the first table a statement reads FROM, best effort: the
// first FROM followed by a name rather than a subquery. "" when there is none.
func QueryTable(sql string) (table string) {
	toks := lexHead(sql)
	for i := range toks {
		if kw(toks, i) == "FROM" {
			if n := nameAt(toks, i+1); n != "" {
				return n
			}
		}
	}
	return ""
}

func classifyAlter(toks []token) (class StatementClassE, table string) {
	if kw(toks, 1) != "TABLE" {
		return StatementClassDDL, ""
	}
	i := 2
	for kw(toks, i) == "IF" || kw(toks, i) == "EXISTS" {
		i++
	}
	table, i = nameSpan(toks, i)
	if kw(toks, i) == "ON" && kw(toks, i+1) == "CLUSTER" {
		i += 3 // the cluster name: a bare word, a quoted identifier or a string literal
	}
	// An ALTER may chain commands with commas; any one that rewrites or
	// removes rows makes the statement a mutation.
	depth := 0
	for start := true; i < len(toks); i++ {
		if start {
			switch kw(toks, i) {
			case "UPDATE", "DELETE", "APPLY":
				return StatementClassMutation, table
			}
			start = false
		}
		switch toks[i].text {
		case "(":
			depth++
		case ")":
			depth--
		case ",":
			start = depth == 0 && !toks[i].ident && !toks[i].lit
		}
	}
	return StatementClassDDL, table
}

// nameAfterObjectKeyword is the name following TABLE / VIEW / DICTIONARY,
// past an IF EXISTS or IF NOT EXISTS, or "" for an object with none of those
// — a database, a user, a role.
func nameAfterObjectKeyword(toks []token) string {
	for i := 1; i < len(toks); i++ {
		switch kw(toks, i) {
		case "TABLE", "TABLES", "VIEW", "DICTIONARY":
			return nameAfterSkipping(toks, i+1, "IF", "NOT", "EXISTS")
		case "DATABASE", "USER", "ROLE", "QUOTA", "FUNCTION", "SETTINGS", "POLICY", "PROFILE":
			return ""
		}
	}
	return ""
}

func nameAfterSkipping(toks []token, i int, skip ...string) string {
	for i < len(toks) && toks[i].word && slices.Contains(skip, toks[i].upper) {
		i++
	}
	return nameAt(toks, i)
}

func nameAt(toks []token, i int) string {
	n, _ := nameSpan(toks, i)
	return n
}

// nameSpan reads a possibly qualified name (a.b, `a`.`b`) starting at i and
// returns it unquoted with the index past it.
func nameSpan(toks []token, i int) (name string, next int) {
	if i >= len(toks) || !toks[i].ident {
		return "", i
	}
	var sb strings.Builder
	sb.WriteString(toks[i].text)
	i++
	for i+1 < len(toks) && toks[i].text == "." && toks[i+1].ident {
		sb.WriteByte('.')
		sb.WriteString(toks[i+1].text)
		i += 2
	}
	return sb.String(), i
}

func kw(toks []token, i int) string {
	if i < len(toks) && toks[i].word {
		return toks[i].upper
	}
	return ""
}

func indexKw(toks []token, k string) int {
	for i := range toks {
		if kw(toks, i) == k {
			return i
		}
	}
	return -1
}

type token struct {
	text  string // unquoted
	upper string // upper-cased text, for bare words only
	word  bool   // a bare word, which may be a keyword
	ident bool   // usable as a name: a bare word or a quoted identifier
	lit   bool   // a string literal: never a name, but it occupies a position
}

// lexHeadLimit bounds how far into a statement the lexer reads: the head is
// all classification needs, and a statement may carry a large literal.
const lexHeadLimit = 4096

// lexHead splits the head of sql into words, quoted identifiers, string
// literal placeholders and single punctuation characters, dropping whitespace
// and comments.
func lexHead(sql string) (toks []token) {
	if len(sql) > lexHeadLimit {
		sql = sql[:lexHeadLimit]
	}
	toks = make([]token, 0, 16)
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ';':
			i++
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-', c == '#':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return
			}
			i += end + 4
		case c == '`' || c == '"':
			j := i + 1
			for j < len(sql) && sql[j] != c {
				if sql[j] == '\\' {
					j++
				}
				j++
			}
			if j > len(sql) {
				j = len(sql)
			}
			toks = append(toks, token{text: sql[i+1 : j], ident: true})
			i = j + 1
		case c == '\'':
			j := i + 1
			for j < len(sql) && sql[j] != '\'' {
				if sql[j] == '\\' {
					j++
				}
				j++
			}
			// A string literal is never a name, but it holds its place — a
			// cluster name may be one — so it is kept, without its text.
			toks = append(toks, token{lit: true})
			i = j + 1
		case isWordByte(c):
			j := i
			for j < len(sql) && isWordByte(sql[j]) {
				j++
			}
			w := sql[i:j]
			toks = append(toks, token{text: w, upper: strings.ToUpper(w), word: true, ident: true})
			i = j
		default:
			toks = append(toks, token{text: sql[i : i+1]})
			i++
		}
	}
	return
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c >= 0x80
}

var errCodeRe = regexp.MustCompile(`Code: (\d+)\.`)

// errCode is the ClickHouse error code an error's text carries ("Code: 60.
// DB::Exception: …"), or 0 when it carries none.
func errCode(err error) (code int32) {
	if err == nil {
		return
	}
	m := errCodeRe.FindStringSubmatch(err.Error())
	if m == nil {
		return
	}
	v, perr := strconv.ParseInt(m[1], 10, 32)
	if perr != nil {
		return
	}
	code = int32(v)
	return
}
