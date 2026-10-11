package agentschema

// The laws (ADR-0313 §SD3, §SD5), each a query that returns the rows that
// break it; with a `withheld` section:
//
//	L11 co-alignment: the withheld section's two lanes have equal length
//	L12 no withheld membership survives on a valued section
//	L13 conservation: base memberships equal kept plus withheld
//	L18 the backstop never fires: through every scoped relation, the agent
//	    principal's count and body hash equal the definer's under the same
//	    scope
//
// Each takes the worker's constant scopes; a relation whose scope is not
// pinned is refused, as the worker refuses it.

// L11 returns the rows whose withheld lanes differ in length.
func L11(r Relation, scopes Scopes) (sql string, err error) {
	sel, err := WorkerSelect(r, scopes)
	if err != nil {
		return
	}
	return "SELECT * FROM (" + sel + ") WHERE length(`withheld_memberships`) != length(`withheld_policy`)", nil
}

// L12 returns the rows where a withheld membership survived in the kept
// memberships.
func L12(r Relation, scopes Scopes) (sql string, err error) {
	sel, err := WorkerSelect(r, scopes)
	if err != nil {
		return
	}
	return "SELECT * FROM (" + sel + ") WHERE hasAny(" + quote(r.Memberships) + ", " + stringArray(r.withheldAll()) + ")", nil
}

// L13 returns the rows where the base memberships are not kept plus
// withheld: the rows of the relation joined with the base by key.
func L13(r Relation, scopes Scopes, key string) (sql string, err error) {
	sel, err := WorkerSelect(r, scopes)
	if err != nil {
		return
	}
	k := quote(key)
	return "SELECT a." + k + " FROM (" + sel + ") AS a INNER JOIN " + quoteRef(r.Over) + " AS b ON a." + k + " = b." + k +
		" WHERE length(b." + quote(r.Memberships) + ") != length(a." + quote(r.Memberships) + ") + length(a.`withheld_memberships`)", nil
}

// L18 is the pair of statements the agent principal and the definer each
// run under one scope; the backstop never fired when the two rows agree.
// body names the column digested, empty for a content-free relation.
func L18(r Relation, scopes Scopes, body string) (agent string, definer string, err error) {
	sel, err := WorkerSelect(r, scopes)
	if err != nil {
		return
	}
	digest := "0"
	if body != "" {
		digest = "sum(cityHash64(" + quote(body) + "))"
	}
	agent = "SELECT count() AS n, " + digest + " AS h FROM " + quote(r.Name)
	definer = "SELECT count() AS n, " + digest + " AS h FROM (" + sel + ")"
	return
}
