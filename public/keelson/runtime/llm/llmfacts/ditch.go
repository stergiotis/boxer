package llmfacts

import (
	"strconv"
	"strings"
)

// The symbol section's membership and value columns: a symbol is one value
// per membership, so the value at a membership's position in the lr column
// is that membership's value.
const (
	symbolLrCol    = `"tv:symbol:lr:lr:u64:1247:::0::data"`
	symbolValueCol = `"tv:symbol:value:val:s:124::I:0::data"`
)

// DitchMessagesSQL is the statement that removes the kept text of retained
// conversations (ADR-0264 §SD6): every llmMessage row in table, or only
// app's when app is not empty. The llmCall rows and every other kind are
// untouched, so the counts and the audit survive it.
//
// It is a verification device, not a purge: ditch the rows, run the app
// again, and if it behaves the same it did not rely on them. table is the
// facts table ("boxer.facts" when empty); app is quoted as a string literal.
func DitchMessagesSQL(table string, app string) (sql string) {
	if table == "" {
		table = CallTableName
	}
	sql = "DELETE FROM " + table + " WHERE " + messagesPredicate(app)
	return
}

// messagesPredicate selects llmMessage rows by their kind membership, and by
// the app's value when app is not empty.
func messagesPredicate(app string) (p string) {
	ids := CallMembershipIds["LlmMessage"]
	p = "has(" + symbolLrCol + ", " + strconv.FormatUint(ids["runtimeKindLlmMessage"], 10) + ")"
	if app != "" {
		p += " AND " + symbolValueCol + "[indexOf(" + symbolLrCol + ", " + strconv.FormatUint(ids["llmMessageApp"], 10) + ")] = " + quoteString(app)
	}
	return
}

// quoteString renders s as a ClickHouse string literal.
func quoteString(s string) (q string) {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
