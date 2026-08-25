package passes

import (
	"strings"
	"testing"
)

// Physical names quoted as nanopass.QuoteIdentifier emits them (double-quoted
// canonical form, valid ClickHouse identifier quoting).
const (
	qSymbol = `"tv:symbol:value:val:s:124::I:0::data"`
	qLat    = `"tv:geoPoint:pointLat:val:f32:g:0:0:0::geo"`
	qLng    = `"tv:geoPoint:pointLng:val:f32:g:0:0:0::geo"`
	qId     = `"id:id:u64:47::0:"`
)

// fakeResolver returns canned verdicts keyed by lower-cased handle per table.
// It stands in for the leeway resolver so these tests exercise only the pass's
// SQL-rewriting logic — colon handles, `:*` expansion, qualified refs, and
// diagnostics.
type fakeResolver struct {
	byTable map[string]map[string]ResolveResult
}

func (f *fakeResolver) Resolve(dbName string, tableName string, handle string) ResolveResult {
	t, ok := f.byTable[tableName]
	if !ok {
		return ResolveResult{Kind: ResolveNotAHandle}
	}
	if r, ok := t[strings.ToLower(handle)]; ok {
		return r
	}
	return ResolveResult{Kind: ResolveNotAHandle}
}

// IsHandleSyntax mirrors the leeway resolver's colon-always rule, so the pass's
// no-catalog-source diagnostic is exercised the way a real host wires it.
func (f *fakeResolver) IsHandleSyntax(name string) bool {
	return strings.Count(name, ":") == 1
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{byTable: map[string]map[string]ResolveResult{
		"facts": {
			"symbol:value":      {Kind: ResolveOK, Physical: []string{"tv:symbol:value:val:s:124::I:0::data"}},
			"geopoint:pointlat": {Kind: ResolveOK, Physical: []string{"tv:geoPoint:pointLat:val:f32:g:0:0:0::geo"}},
			"geopoint:*":        {Kind: ResolveOK, Physical: []string{"tv:geoPoint:pointLat:val:f32:g:0:0:0::geo", "tv:geoPoint:pointLng:val:f32:g:0:0:0::geo"}},
			"id:id":             {Kind: ResolveOK, Physical: []string{"id:id:u64:47::0:"}},
			"geopoint:lat":      {Kind: ResolveUnknownColumn, Section: "geoPoint", Column: "lat", Candidates: []string{"pointLat", "pointLng", "h3"}},
			"nope:x":            {Kind: ResolveUnknownSection, Section: "nope"},
		},
		"other": {
			"symbol:value": {Kind: ResolveOK, Physical: []string{"tv:symbol:value:val:s:0:0:0:0::x"}},
		},
	}}
}

func runResolve(t *testing.T, sql string) string {
	t.Helper()
	out, err := ResolveColumnNames(newFakeResolver(), "", nil).Run(sql)
	if err != nil {
		t.Fatalf("ResolveColumnNames failed on %q: %v", sql, err)
	}
	return out
}

func runResolveDiag(t *testing.T, sql string) (string, []ColumnDiagnostic) {
	t.Helper()
	var diags []ColumnDiagnostic
	out, err := ResolveColumnNames(newFakeResolver(), "", func(d ColumnDiagnostic) { diags = append(diags, d) }).Run(sql)
	if err != nil {
		t.Fatalf("ResolveColumnNames failed on %q: %v", sql, err)
	}
	return out, diags
}

func TestResolve_ColonHandlesInProjection(t *testing.T) {
	out := runResolve(t, "SELECT `symbol:value`, `id:id` FROM facts")
	if !strings.Contains(out, qSymbol) || !strings.Contains(out, qId) {
		t.Errorf("handles not resolved: %s", out)
	}
}

func TestResolve_BareIdentifierUntouched(t *testing.T) {
	// No colon → never a handle. `symbol` and `other_column` pass through.
	out := runResolve(t, "SELECT symbol, other_column FROM facts")
	if strings.Contains(out, qSymbol) {
		t.Errorf("bare symbol should NOT resolve under colon-always: %s", out)
	}
	if !strings.Contains(out, "symbol") || !strings.Contains(out, "other_column") {
		t.Errorf("bare identifiers should survive verbatim: %s", out)
	}
}

func TestResolve_EverywhereNotJustProjection(t *testing.T) {
	sql := "SELECT `symbol:value` FROM facts WHERE `symbol:value` = 'x' GROUP BY `symbol:value` ORDER BY `symbol:value`"
	out := runResolve(t, sql)
	if n := strings.Count(out, qSymbol); n != 4 {
		t.Errorf("expected 4 resolutions, got %d: %s", n, out)
	}
}

func TestResolve_StarExpandsInProjection(t *testing.T) {
	out := runResolve(t, "SELECT `geoPoint:*` FROM facts")
	if !strings.Contains(out, qLat) || !strings.Contains(out, qLng) {
		t.Errorf("`geoPoint:*` should expand to all value columns: %s", out)
	}
}

func TestResolve_StarExpandsInArrayJoin(t *testing.T) {
	out := runResolve(t, "SELECT 1 FROM facts ARRAY JOIN `geoPoint:*`")
	if !strings.Contains(out, qLat) || !strings.Contains(out, qLng) {
		t.Errorf("`geoPoint:*` should expand inside ARRAY JOIN: %s", out)
	}
}

func TestResolve_QualifiedKeepsAlias(t *testing.T) {
	out := runResolve(t, "SELECT f.`symbol:value` FROM facts f")
	if !strings.Contains(out, "f."+qSymbol) {
		t.Errorf("qualified handle should keep the alias: %s", out)
	}
}

func TestResolve_QualifiedStarAliasesEachColumn(t *testing.T) {
	out := runResolve(t, "SELECT f.`geoPoint:*` FROM facts f")
	if !strings.Contains(out, "f."+qLat) || !strings.Contains(out, "f."+qLng) {
		t.Errorf("qualified `:*` should prefix each expanded column with the alias: %s", out)
	}
}

func TestResolve_UnresolvedLeftUntouchedWithoutSink(t *testing.T) {
	// Without a sink, an unresolved colon handle is left as-is (server reports).
	out := runResolve(t, "SELECT `geoPoint:lat` FROM facts")
	if !strings.Contains(out, "geoPoint:lat") {
		t.Errorf("unresolved handle should survive verbatim: %s", out)
	}
}

func TestResolve_DiagnosticsUnknownColumnAndSection(t *testing.T) {
	_, diags := runResolveDiag(t, "SELECT `geoPoint:lat`, `nope:x` FROM facts")
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %+v", len(diags), diags)
	}
	byHandle := map[string]ColumnDiagnostic{}
	for _, d := range diags {
		byHandle[d.Handle] = d
	}
	if d, ok := byHandle["geoPoint:lat"]; !ok {
		t.Errorf("missing diagnostic for geoPoint:lat")
	} else if len(d.Candidates) == 0 || !strings.Contains(strings.Join(d.Candidates, ","), "pointLat") {
		t.Errorf("unknown-column diagnostic should suggest candidates: %+v", d)
	}
	if d, ok := byHandle["nope:x"]; !ok || !strings.Contains(d.Message, "nope") {
		t.Errorf("missing/incorrect unknown-section diagnostic: %+v", byHandle["nope:x"])
	}
}

func TestResolve_AmbiguousAcrossJoinUntouched(t *testing.T) {
	// `symbol:value` resolves in both tables → ambiguous → left untouched.
	out := runResolve(t, "SELECT `symbol:value` FROM facts, other")
	if strings.Contains(out, `"tv:symbol`) {
		t.Errorf("ambiguous handle should be left untouched: %s", out)
	}
}

func TestResolve_Idempotent(t *testing.T) {
	sql := "SELECT `symbol:value`, `geoPoint:*` FROM facts WHERE `id:id` = 1"
	once := runResolve(t, sql)
	twice := runResolve(t, once)
	if once != twice {
		t.Errorf("not idempotent:\n once: %s\n twice: %s", once, twice)
	}
}

// A handle bound by a WITH-expression alias must resolve. `WITH <expr> AS name`
// parses as the query-level `ctes` rule, which is a *sibling* of selectStmt
// rather than a descendant, so a walk anchored at the SELECT never reaches it.
// Filed by the jsonbench-on-facts trial (README §7b row 13), whose queries had
// to inline every handle to work around this.
func TestResolve_HandleBoundByWithExpressionAlias(t *testing.T) {
	out := runResolve(t, "WITH `symbol:value` AS sv SELECT sv FROM facts")
	if !strings.Contains(out, qSymbol) {
		t.Errorf("handle in a WITH-expression alias should resolve: %s", out)
	}
}

func TestResolve_WithExpressionAliasStarExpands(t *testing.T) {
	out := runResolve(t, "WITH `geoPoint:*` AS g SELECT 1 FROM facts")
	if !strings.Contains(out, qLat) || !strings.Contains(out, qLng) {
		t.Errorf("`:*` in a WITH-expression alias should expand: %s", out)
	}
}

// The CTE form of WITH must keep working, and a CTE's *name* is not a handle.
func TestResolve_WithCTEStillResolvesInsideAndOut(t *testing.T) {
	out := runResolve(t, "WITH c AS (SELECT `symbol:value` FROM facts) SELECT `id:id` FROM facts")
	if !strings.Contains(out, qSymbol) {
		t.Errorf("handle inside a CTE body should resolve: %s", out)
	}
	if !strings.Contains(out, qId) {
		t.Errorf("handle in the outer SELECT should resolve: %s", out)
	}
}

func TestResolve_UnparseableBareColonIsError(t *testing.T) {
	_, err := ResolveColumnNames(newFakeResolver(), "", nil).Run("SELECT geoPoint:lat FROM facts")
	if err == nil {
		t.Errorf("expected a parse error for a bare (unquoted) colon")
	}
}

// TestResolve_NoCatalogSourceDiagnostic covers the gap where a handle whose
// only source is a subquery, a CTE or a table function was left untouched AND
// unreported: the statement shipped the handle verbatim and the server answered
// UNKNOWN_IDENTIFIER about a name the user never typed. The rewrite still does
// not happen — there is no schema to resolve against — but the Diagnostics sink
// now says so before the round-trip.
func TestResolve_NoCatalogSourceDiagnostic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sql    string
		expect string
	}{
		{"from subquery", "SELECT `symbol:value` FROM (SELECT * FROM facts)", "(subquery)"},
		{"from aliased subquery", "SELECT `symbol:value` FROM (SELECT * FROM facts) AS v", "v (subquery)"},
		{"qualified through subquery", "SELECT v.`symbol:value` FROM (SELECT * FROM facts) AS v", "v (subquery)"},
		{"from cte", "WITH c AS (SELECT * FROM facts) SELECT `symbol:value` FROM c", "c (CTE)"},
		{"from table function", "SELECT `symbol:value` FROM numbers(10)", "numbers (table function)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, diags := runResolveDiag(t, tc.sql)
			if out != tc.sql {
				t.Errorf("statement should ship unchanged, got %q", out)
			}
			if len(diags) != 1 {
				t.Fatalf("want exactly 1 diagnostic, got %d: %+v", len(diags), diags)
			}
			if diags[0].Handle != "symbol:value" {
				t.Errorf("diagnostic handle = %q, want symbol:value", diags[0].Handle)
			}
			if !strings.Contains(diags[0].Message, tc.expect) {
				t.Errorf("message %q should name the source %q", diags[0].Message, tc.expect)
			}
		})
	}
}

// TestResolve_NoCatalogSourceDoesNotFlagOrdinarySQL is the other half, and the
// reason the diagnostic is gated on handle SPELLING rather than on failure to
// resolve: a subquery-sourced SELECT is full of ordinary column references, and
// warning about those would make the pane useless.
func TestResolve_NoCatalogSourceDoesNotFlagOrdinarySQL(t *testing.T) {
	for _, sql := range []string{
		// Bare identifiers — no colon, never handles.
		"SELECT a, b, c FROM (SELECT * FROM facts)",
		"WITH c AS (SELECT * FROM facts) SELECT x FROM c WHERE y = 1",
		// A physical name typed verbatim has many colons and is not a handle.
		"SELECT `tv:symbol:value:val:s:124::I:0::data` FROM (SELECT * FROM facts)",
		// A real table in scope: resolution happens, nothing to report.
		"SELECT `symbol:value` FROM facts",
		// A table the resolver knows nothing about is not a no-catalog-source
		// case — it has a schema, the resolver just cannot judge it.
		"SELECT `symbol:value` FROM unknown_table",
	} {
		_, diags := runResolveDiag(t, sql)
		if len(diags) != 0 {
			t.Errorf("%q should produce no diagnostic, got %+v", sql, diags)
		}
	}
}

// TestResolve_NoCatalogSourceNeedsHandleSyntax pins the optional-capability
// contract: a resolver that does not implement HandleSyntaxI cannot tell a
// handle from plain SQL, so the pass stays silent rather than guessing.
func TestResolve_NoCatalogSourceNeedsHandleSyntax(t *testing.T) {
	var diags []ColumnDiagnostic
	plain := plainResolver{newFakeResolver()}
	_, err := ResolveColumnNames(plain, "", func(d ColumnDiagnostic) { diags = append(diags, d) }).
		Run("SELECT `symbol:value` FROM (SELECT * FROM facts)")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("a resolver without HandleSyntaxI should get no diagnostic, got %+v", diags)
	}
}

// plainResolver exposes only Resolve, hiding the fake's IsHandleSyntax.
type plainResolver struct{ inner *fakeResolver }

func (p plainResolver) Resolve(dbName string, tableName string, handle string) ResolveResult {
	return p.inner.Resolve(dbName, tableName, handle)
}
