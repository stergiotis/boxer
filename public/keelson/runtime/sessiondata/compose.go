package sessiondata

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// MaxDefinitionBytes bounds a data set's defining statement.
const MaxDefinitionBytes = 64 << 10

// checkedRead is a statement proven a plain read once its macros were
// replaced: the text of its one SELECT (or UNION of SELECTs), without a
// FORMAT clause, a SET prelude or a trailing semicolon, and the stratum a
// read of it falls in.
type checkedRead struct {
	inner   string
	stratum analysis.QueryStratumE
}

// checkRead proves sql — a statement whose macros are already replaced by
// table names — a plain read: one query the grammar parses, which both the
// statement-kind classifier the host's fairlead pass uses and the security
// class play requires of an agent's statement place as a read, with no
// FORMAT clause and no SET prelude. Both classifiers refuse a statement the
// server would tokenise differently from boxer's lexer (a nested block
// comment, [analysis.NestedBlockComment]). The text a composed statement
// wraps is the SELECT's tokens with every comment replaced by a space, and
// that text is proven a read again, so what is sent is what was classified
// and carries no comment for a server to read otherwise.
func checkRead(sql string) (cr checkedRead, err error) {
	cr, err = checkReadOnce(sql)
	if err != nil {
		return
	}
	again, err := checkReadOnce(cr.inner)
	switch {
	case err != nil:
		return checkedRead{}, eh.Errorf("the statement without its comments is not a plain read: %w", err)
	case again.inner != cr.inner:
		return checkedRead{}, eh.Errorf("the statement's SELECT does not read the same once its comments are gone")
	}
	return
}

// checkReadOnce is one pass of checkRead: the checks on sql, and its
// SELECT's text without comments.
func checkReadOnce(sql string) (cr checkedRead, err error) {
	pr, err := parseStatement(sql)
	if err != nil {
		return
	}
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok || pr.InsertStmt() != nil || qs.Query() == nil {
		return cr, eh.Errorf("the statement is not a query; a data set is defined by a SELECT")
	}
	if qs.FORMAT() != nil {
		return cr, eh.Errorf("the statement names an output FORMAT; a data set is defined by a SELECT without one")
	}
	q, ok := qs.Query().(*grammar1.QueryContext)
	if !ok || len(q.AllSetStmt()) > 0 {
		return cr, eh.Errorf("the statement has a SET prelude; a data set's definition is replayed without one, so it binds no parameter")
	}
	if kind := analysis.ClassifyStatementKind(sql); kind != analysis.KindReadOnly {
		return cr, eb.Build().Stringer("kind", kind).Errorf("the statement is not a read")
	}
	class, witnesses, cErr := analysis.ClassifyQuerySecurity(pr)
	if cErr != nil || class != analysis.QuerySecurityRead {
		why := "the statement is " + class.String() + ", not a plain read"
		if len(witnesses) > 0 && witnesses[0].Name != "" {
			why += " (" + witnesses[0].Name + ": " + witnesses[0].Describe() + ")"
		}
		return cr, eb.Build().Str("class", class.String()).Errorf("%s", why) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text; the witness is what the agent can act on"
	}
	sel, ok := q.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
	if !ok || sel == nil {
		return cr, eh.Errorf("the statement has no SELECT")
	}
	cr.inner, err = textWithoutComments(pr, sel)
	if err != nil {
		return
	}
	if stratum, sErr := analysis.ClassifyQueryStratum(pr, ""); sErr == nil {
		cr.stratum = stratum
	}
	return
}

// textWithoutComments is the text of node's tokens as the lexer read them,
// whitespace kept and every comment replaced by a space.
func textWithoutComments(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (text string, err error) {
	start, stop := node.GetStart(), node.GetStop()
	if start == nil || stop == nil || pr.TokenStream == nil || stop.GetTokenIndex() < start.GetTokenIndex() {
		return "", eh.Errorf("the statement's SELECT could not be located in its text")
	}
	tokens := pr.TokenStream.GetAllTokens()
	if stop.GetTokenIndex() >= len(tokens) {
		return "", eh.Errorf("the statement's SELECT could not be located in its text")
	}
	var b strings.Builder
	for _, t := range tokens[start.GetTokenIndex() : stop.GetTokenIndex()+1] {
		switch t.GetTokenType() {
		case grammar1.ClickHouseLexerMULTI_LINE_COMMENT, grammar1.ClickHouseLexerSINGLE_LINE_COMMENT:
			b.WriteByte(' ')
		case antlr.TokenEOF:
		default:
			b.WriteString(t.GetText())
		}
	}
	return b.String(), nil
}

// createSpec is what a composed create needs besides the checked read.
type createSpec struct {
	table   string
	orderBy []string
	// limit is one more than the rows the data session may still take; a
	// table that reaches it is over the bound.
	limit uint64
}

// composeCreate is the capability's own statement around a checked read:
// a temporary MergeTree table, so its rows live in the server's temporary
// data on disk rather than in memory, filled from the read bounded by a
// LIMIT. Every token outside the parenthesised read is the capability's.
func composeCreate(spec createSpec, read checkedRead) (sql string) {
	var b strings.Builder
	b.WriteString("CREATE TEMPORARY TABLE ")
	b.WriteString(quoteIdentifier(spec.table))
	b.WriteString(" ENGINE = MergeTree ORDER BY ")
	if len(spec.orderBy) == 0 {
		b.WriteString("tuple()")
	} else {
		b.WriteString("(")
		for i, c := range spec.orderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quoteIdentifier(c))
		}
		b.WriteString(")")
	}
	b.WriteString(" AS SELECT * FROM (\n")
	b.WriteString(read.inner)
	b.WriteString("\n) LIMIT ")
	b.WriteString(strconv.FormatUint(spec.limit, 10))
	return b.String()
}

// composeDrop drops a table the capability created.
func composeDrop(table string) (sql string) {
	return "DROP TEMPORARY TABLE IF EXISTS " + quoteIdentifier(table)
}

// probeStatement lists the session's temporary tables with their sizes.
const probeStatement = "SELECT name, total_rows, total_bytes FROM system.tables WHERE is_temporary AND startsWith(name, '" + tablePrefix + "')"

// describeStatement lists a table's columns.
func describeStatement(table string) (sql string) {
	return "DESCRIBE TABLE " + quoteIdentifier(table)
}

// checkOrderBy refuses a sort key that is not a list of column names.
func checkOrderBy(cols []string) (err error) {
	if len(cols) > 16 {
		return eh.Errorf("a sort key names at most 16 columns")
	}
	for _, c := range cols {
		if !ValidName(c) {
			return eb.Build().Str("column", c).Errorf("a sort key names columns by identifier")
		}
	}
	return
}

// statementIsRead is checkRead's verdict on a statement as a whole, for an
// agent's statement a binding rewrote: a read the grammar parses, its
// FORMAT clause and SET prelude allowed.
func statementIsRead(sql string) (err error) {
	pr, err := parseStatement(sql)
	if err != nil {
		return
	}
	if kind := analysis.ClassifyStatementKind(sql); kind != analysis.KindReadOnly {
		return eb.Build().Stringer("kind", kind).Errorf("the statement is not a read")
	}
	class, _, cErr := analysis.ClassifyQuerySecurity(pr)
	if cErr != nil || class != analysis.QuerySecurityRead {
		return eb.Build().Str("class", class.String()).Errorf("the statement is not a plain read")
	}
	return
}
