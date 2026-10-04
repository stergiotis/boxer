package trail

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

// textSectionCols are the columns of the text section, the one section a
// message row's text lives on ([LlmMessageBody]). ditch_test.go checks them
// against the table's DDL.
var textSectionCols = []string{
	`"tv:textArray:value:val:sh:5::7:0::data"`,
	`"tv:textArray:hr:hr:u64:47:::0::data"`,
	`"tv:textArray:lr:lr:u64:1247:::0::data"`,
	`"tv:textArray:lmr:lmr:u64:1247:::0::data"`,
	`"tv:textArray:mrhp:mrhp:y:4:::0::data"`,
	`"tv:textArray:len:len:u64:4D:::0::data"`,
	`"tv:textArray:hrcard:hrcard:u64:4E:::0::data"`,
	`"tv:textArray:lrcard:lrcard:u64:4E:::0::data"`,
	`"tv:textArray:lmrcard:lmrcard:u64:4E:::0::data"`,
}

// DitchBodiesSQL is the statement that removes the kept text of model
// conversations (ADR-0264 §SD6, ADR-0277 §SD4): it empties the text section
// of every message row in table, or only of app's when app is not empty.
// The rows stay, so the audit — who said how much to which call, and which
// tools it called — survives it, as does every other kind.
//
// It is a verification device, not a purge: ditch the text, run the app
// again, and if it behaves the same it did not rely on it. table is the
// facts table ("boxer.facts" when empty); app is quoted as a string literal.
// The statement is a mutation and waits for it.
func DitchBodiesSQL(table string, app string) (sql string) {
	if table == "" {
		table = TrailTableName
	}
	sets := make([]string, 0, len(textSectionCols))
	for _, c := range textSectionCols {
		sets = append(sets, c+" = arrayResize("+c+", 0)")
	}
	sql = "ALTER TABLE " + table + " UPDATE " + strings.Join(sets, ", ") + " WHERE " + messagesPredicate(app) + " SETTINGS mutations_sync = 2"
	return
}

// messagesPredicate selects message rows by their kind membership, and by
// the origin's app when app is not empty.
func messagesPredicate(app string) (p string) {
	p = "has(" + symbolLrCol + ", " + strconv.FormatUint(TrailMembershipIds["LlmMessage"]["runtimeKindLlmMessage"], 10) + ")"
	if app != "" {
		p += " AND " + symbolValueCol + "[indexOf(" + symbolLrCol + ", " + strconv.FormatUint(TrailMembershipIds["Origin"]["runtimeApp"], 10) + ")] = " + quoteString(app)
	}
	return
}

// quoteString renders s as a ClickHouse string literal.
func quoteString(s string) (q string) {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
