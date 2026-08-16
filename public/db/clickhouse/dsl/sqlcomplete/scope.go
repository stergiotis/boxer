package sqlcomplete

import "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"

// TableRef is one source in the statement's FROM.
type TableRef struct {
	Database string
	Name     string
	Alias    string
}

// Scope is what the statement's own tree adds to the site: the names the
// statement itself introduces (ADR-0190 §SD3).
//
// It arrives one quiescence window behind the buffer, from a sentinel parse on
// a worker, because the parser costs 5–18 ms while its DFA warms (ADR-0084).
// Everything the site alone can answer is answered per frame without it, so a
// nil Scope narrows what the engine knows rather than stopping it.
type Scope struct {
	// Frame is the tree's own view of the caret's call. For a comma-separated
	// call it must agree with the site's; for a keyword-syntax call
	// (`CAST(x AS T)`) it is the only one there is.
	Frame *highlight.CallFrame
	// Aliases maps an alias to the source text of the expression it names, so
	// the typer can recurse into it.
	Aliases map[string]string
	CTEs    []string
	Tables  []TableRef
	Windows []string
	// Clause is the clause the sentinel landed in, spelled as
	// [highlight.CaretSite.Clause] spells it so the two tiers do not disagree
	// about the same word.
	Clause string
}

// AliasOf resolves an alias to its defining expression.
func (inst *Scope) AliasOf(name string) (expr string, ok bool) {
	if inst == nil || inst.Aliases == nil {
		return
	}
	expr, ok = inst.Aliases[name]
	return
}

// LookupTable resolves a table alias, or a table named directly, to its source.
func (inst *Scope) LookupTable(name string) (ref TableRef, ok bool) {
	if inst == nil {
		return
	}
	for i := range inst.Tables {
		if inst.Tables[i].Alias == name {
			return inst.Tables[i], true
		}
	}
	for i := range inst.Tables {
		if inst.Tables[i].Name == name {
			return inst.Tables[i], true
		}
	}
	return
}
