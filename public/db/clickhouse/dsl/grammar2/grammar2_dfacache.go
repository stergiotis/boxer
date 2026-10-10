package grammar2

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

// SharedDFA is the one bounded DFA cache every seam that parses grammar2 uses
// (ADR-0084 for the bound, ADR-0196 §SD3 for why it is shared and lives here).
// grammar2 has only one seam today — nanopass.ParseCanonical — but it carries
// the same WITH-clause ambiguity as grammar1 and the same reason to keep the
// holder next to the ATN it caches.
//
// Hand-written, co-located with generated code — the package_props.go
// precedent (ADR-0080). generate.sh only sweeps clickhouse*.go and *.out.*, so
// this file survives regeneration.
var SharedDFA antlr4utils.DFACache

func init() {
	SharedDFA.SetLLIslands(LLIsland)
}

// LLIsland names the positions where SLL's lowest-alternative choice is the
// wrong one for the position the parser is in (ADR-0305). Each is a
// rule invocation, identified by its parent:
//
//   - the operands of x BETWEEN a AND b: continuing a with a binary AND looks
//     as good as stopping for BETWEEN's own AND;
//   - the table expression of a FROM or JOIN item: a table name followed by
//     `(` looks as good as a table function, because INSERT INTO t (…) lets a
//     tableIdentifier be followed by a parenthesis elsewhere.
//
// Over every statement the test suite parses, these are the only
// positions where SLL rejected input LL accepts.
func LLIsland(parent antlr.Tree) bool {
	switch parent.(type) {
	case *ColumnExprBetweenContext, *JoinExprTableContext:
		return true
	}
	return false
}
