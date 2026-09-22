package keelsonquery

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
)

// Gate holds a statement to the table its subject grants (ADR-0253 §SD3).
// It returns the statement with keelson('x') macros expanded to bare names
// — the engine's input — or a reason to refuse.
//
// Default-deny throughout, the R5 discipline play's dispatcher applies: a
// statement that cannot be parsed, cannot be proven read-only, or names
// anything the grant does not cover is refused with the reason, never run
// on the chance that clickhouse-local would have caught it. The engine
// snapshots every registered table for an unparseable statement
// (ADR-0094 §SD4's conservative fallback), which is exactly the width a
// per-table grant exists to prevent, so the gate refuses before the engine
// can widen.
//
// What is held to the table: every relation the statement reads, resolved
// through scopes so a CTE or a subquery is not mistaken for a table. A
// qualified name is refused too — the introspection tables have no
// database, and `system.x` on the worker is not what the grant covers. A
// table function is refused by kind: url(), file() and remote() would let
// a grant on one table read anywhere clickhouse-local can reach.
func Gate(reg *introspect.Registry, sql string, table string) (bare string, reason string) {
	bare, err := keelsonsql.RewriteToBare(reg, sql)
	if err != nil {
		return "", err.Error()
	}
	if kind := analysis.ClassifyStatementKind(bare); kind != analysis.KindReadOnly {
		return "", "not provably read-only (" + kind.String() + ")"
	}
	pr, err := nanopass.Parse(bare)
	if err != nil {
		return "", "does not parse: " + err.Error()
	}
	scopes, err := nanopass.BuildScopes(pr, "")
	if err != nil {
		return "", "cannot resolve the tables it names: " + err.Error()
	}
	for _, scope := range nanopass.FlattenScopes(scopes) {
		for _, ts := range scope.Tables {
			switch {
			case ts.IsCTE || ts.IsSubquery:
				continue
			case ts.IsFunction:
				return "", "names a table function (" + ts.Table + "), which a grant on " + table + " does not cover"
			case ts.Database != "":
				return "", "names " + ts.Database + "." + ts.Table + ", and the subject grants only " + table
			case ts.Table != table:
				return "", "names " + ts.Table + ", and the subject grants only " + table
			}
		}
	}
	return bare, ""
}
