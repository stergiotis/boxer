package passes

import (
	"strings"
	"testing"
)

// Physical names as the fake resolver returns them; the pass quotes them with
// nanopass.QuoteIdentifier, which emits the double-quoted canonical form (valid
// ClickHouse identifier quoting, same as CanonicalizeIdentifiers).
const (
	physSymbol = `"tv:symbol:value:val:s:m:0:24:0::data"`
	physGeoLat = `"tv:geoPoint:lat:val:f64:0:0:0:0::data"`
	physId     = `"id:id:u64:2k:0:0:"`
)

// fakeResolver maps a folded (lower-cased) handle to a physical name per
// table. It stands in for the leeway resolver so these tests exercise only the
// pass's SQL-rewriting logic — scope walking, bare vs qualified refs,
// resolution outside the projection, ambiguity, and passthrough.
type fakeResolver struct {
	byTable map[string]map[string]string
}

func (f *fakeResolver) Resolve(dbName string, tableName string, handle string) (physical string, ok bool) {
	t, ok := f.byTable[tableName]
	if !ok {
		return "", false
	}
	physical, ok = t[strings.ToLower(handle)]
	return
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{byTable: map[string]map[string]string{
		"facts": {
			"symbol":       "tv:symbol:value:val:s:m:0:24:0::data",
			"geopoint:lat": "tv:geoPoint:lat:val:f64:0:0:0:0::data",
			"id":           "id:id:u64:2k:0:0:",
		},
		"other": {
			"symbol": "tv:symbol:value:val:s:0:0:0:0::x",
		},
	}}
}

func runResolve(t *testing.T, sql string) string {
	t.Helper()
	out, err := ResolveColumnNames(newFakeResolver(), "").Run(sql)
	if err != nil {
		t.Fatalf("ResolveColumnNames failed on %q: %v", sql, err)
	}
	return out
}

func TestResolveColumnNames_BareInProjection(t *testing.T) {
	out := runResolve(t, "SELECT symbol, id FROM facts")
	if !strings.Contains(out, physSymbol) {
		t.Errorf("symbol not resolved: %s", out)
	}
	if !strings.Contains(out, physId) {
		t.Errorf("id not resolved: %s", out)
	}
}

func TestResolveColumnNames_QuotedColonComposite(t *testing.T) {
	out := runResolve(t, "SELECT `geoPoint:lat` FROM facts")
	if !strings.Contains(out, physGeoLat) {
		t.Errorf("`geoPoint:lat` not resolved: %s", out)
	}
}

func TestResolveColumnNames_ResolvesOutsideProjection(t *testing.T) {
	// The whole point of substitution over COLUMNS('…'): it works in WHERE,
	// GROUP BY, ORDER BY, HAVING — not just the SELECT list.
	sql := "SELECT symbol FROM facts WHERE symbol = 'x' GROUP BY symbol ORDER BY symbol"
	out := runResolve(t, sql)
	if n := strings.Count(out, physSymbol); n != 4 {
		t.Errorf("expected symbol resolved in all 4 positions, got %d: %s", n, out)
	}
	if strings.Contains(out, "BY symbol") || strings.Contains(out, "symbol =") {
		t.Errorf("a bare symbol token survived: %s", out)
	}
}

func TestResolveColumnNames_QualifiedRefKeepsAlias(t *testing.T) {
	out := runResolve(t, "SELECT f.symbol FROM facts f")
	// Alias prefix is preserved (it disambiguates joins); only the column part
	// is rewritten.
	if !strings.Contains(out, "f."+physSymbol) {
		t.Errorf("qualified ref not resolved with alias kept: %s", out)
	}
}

func TestResolveColumnNames_NonHandleUntouched(t *testing.T) {
	out := runResolve(t, "SELECT other_column, count() AS n FROM facts GROUP BY other_column")
	if !strings.Contains(out, "other_column") {
		t.Errorf("non-handle column should be left untouched: %s", out)
	}
	if strings.Contains(out, `"tv:`) {
		t.Errorf("nothing should have been resolved: %s", out)
	}
}

func TestResolveColumnNames_AmbiguousAcrossJoinUntouched(t *testing.T) {
	// Both tables resolve "symbol" — ambiguous, so leave it for the server to
	// report rather than guess.
	out := runResolve(t, "SELECT symbol FROM facts, other")
	if strings.Contains(out, `"tv:symbol`) {
		t.Errorf("ambiguous bare handle should be left untouched: %s", out)
	}
	if !strings.Contains(out, "symbol") {
		t.Errorf("bare symbol should survive verbatim: %s", out)
	}
}

func TestResolveColumnNames_QualifiedDisambiguatesJoin(t *testing.T) {
	// The same handle, qualified, resolves against exactly one table.
	out := runResolve(t, "SELECT f.symbol FROM facts f, other o")
	if !strings.Contains(out, "f."+physSymbol) {
		t.Errorf("qualified handle should resolve against its table: %s", out)
	}
}

func TestResolveColumnNames_SubqueryScoping(t *testing.T) {
	// Inner select's bare handle resolves against the inner table; the outer
	// alias column is untouched (not a handle for the outer scope).
	out := runResolve(t, "SELECT n FROM (SELECT symbol AS n FROM facts) s")
	if !strings.Contains(out, physSymbol) {
		t.Errorf("inner handle not resolved: %s", out)
	}
	if strings.Count(out, `"tv:`) != 1 {
		t.Errorf("only the inner handle should resolve: %s", out)
	}
}

func TestResolveColumnNames_Idempotent(t *testing.T) {
	sql := "SELECT symbol, `geoPoint:lat` FROM facts WHERE symbol = 'x'"
	once := runResolve(t, sql)
	twice := runResolve(t, once)
	if once != twice {
		t.Errorf("pass is not idempotent:\n once: %s\n twice: %s", once, twice)
	}
}

func TestResolveColumnNames_UnparseableIsError(t *testing.T) {
	// A bare colon does not parse — the pass surfaces the parse error, and the
	// best-effort registry wrapper then ships the pre-pass SQL.
	_, err := ResolveColumnNames(newFakeResolver(), "").Run("SELECT geoPoint:lat FROM facts")
	if err == nil {
		t.Errorf("expected a parse error for a bare colon handle")
	}
}
