package passes

import (
	"testing"
	"time"
)

// TestCachingSchemaProvider_FirstCallSurfacesColumns guards a bug where the
// cache-miss path fetched and cached the delegate's columns but returned the
// zero-valued named returns — so the first lookup of any table reported
// not-found, and the value only surfaced on the second call.
func TestCachingSchemaProvider_FirstCallSurfacesColumns(t *testing.T) {
	delegate := NewStaticSchemaProvider(map[string][]string{"t": {"a", "b", "c"}})
	c := NewCachingSchemaProvider(time.Minute, delegate, 16)

	cols, n, found := c.GetColumns("", "t")
	if !found || n != 3 {
		t.Fatalf("first (cache-miss) call: found=%v n=%d, want found=true n=3", found, n)
	}
	got := 0
	for range cols {
		got++
	}
	if got != 3 {
		t.Fatalf("first call yielded %d columns, want 3", got)
	}
	if _, n2, found2 := c.GetColumns("", "t"); !found2 || n2 != 3 {
		t.Fatalf("second (cache-hit) call: found=%v n=%d", found2, n2)
	}
}

// TestCachingSchemaProvider_KeysByDatabase guards a bug where the cache was
// keyed by table name alone: two same-named tables in different databases
// shared one entry, so whichever was probed first served both for the rest of
// the session and a column handle resolved against the wrong schema.
func TestCachingSchemaProvider_KeysByDatabase(t *testing.T) {
	delegate := NewStaticSchemaProvider(map[string][]string{
		"a.facts": {"x"},
		"b.facts": {"y", "z"},
	})
	c := NewCachingSchemaProvider(time.Minute, delegate, 16)

	first := func(db string) (string, int) {
		t.Helper()
		cols, n, found := c.GetColumns(db, "facts")
		if !found {
			t.Fatalf("%s.facts not found", db)
		}
		for v := range cols {
			return v, n
		}
		t.Fatalf("%s.facts yielded no column", db)
		return "", 0
	}

	// Probe a.facts first, so a table-name-only cache would answer b.facts
	// with a's single column.
	if got, n := first("a"); got != "x" || n != 1 {
		t.Fatalf("a.facts: got %q n=%d, want \"x\" n=1", got, n)
	}
	if got, n := first("b"); got != "y" || n != 2 {
		t.Fatalf("b.facts served a.facts's cache entry: got %q n=%d, want \"y\" n=2", got, n)
	}
	// And both stay right on the cache-hit path.
	if got, n := first("a"); got != "x" || n != 1 {
		t.Fatalf("a.facts on cache hit: got %q n=%d, want \"x\" n=1", got, n)
	}
	if got, n := first("b"); got != "y" || n != 2 {
		t.Fatalf("b.facts on cache hit: got %q n=%d, want \"y\" n=2", got, n)
	}
}

// TestExpandColumns_DeclinesWhenASourceHasNoSchema guards a bug where bare
// `*` and COLUMNS() skipped CTE, subquery and table-function sources, so
// `SELECT * FROM t, numbers(3)` expanded to t's columns alone and silently
// dropped `number`.
func TestExpandColumns_DeclinesWhenASourceHasNoSchema(t *testing.T) {
	pass := ExpandColumns(NewStaticSchemaProvider(map[string][]string{"t": {"a1", "a2", "b"}}), "")
	for _, sql := range []string{
		"SELECT * FROM t, numbers(3)",
		"SELECT * FROM t AS x JOIN (SELECT 1 AS z) AS s ON 1",
		"WITH c AS (SELECT 1 AS a3) SELECT * FROM t, c",
		"SELECT COLUMNS('a') FROM t, numbers(3)",
		"SELECT COLUMNS('a') FROM t JOIN unknown AS u ON 1",
	} {
		got, err := pass.Run(sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if got != sql {
			t.Errorf("%s: expanded to %s, want it left unexpanded", sql, got)
		}
	}
}

// TestExpandColumns_DynamicOperandLeftAlone guards a bug where the
// COLUMNS() expansion replaced whatever node was its parent, so
// `SELECT COLUMNS('a') + 1 FROM t` became `SELECT t.a1, t.a2 FROM t` and
// lost the `+ 1` ClickHouse applies to every matched column.
func TestExpandColumns_DynamicOperandLeftAlone(t *testing.T) {
	pass := ExpandColumns(NewStaticSchemaProvider(map[string][]string{"t": {"a1", "a2", "b"}}), "")
	for _, sql := range []string{
		"SELECT COLUMNS('a') + 1 FROM t",
		"SELECT toString(COLUMNS('a')) FROM t",
	} {
		got, err := pass.Run(sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if got != sql {
			t.Errorf("%s: rewritten to %s, want it left alone", sql, got)
		}
	}
	got, err := pass.Run("SELECT COLUMNS('a'), b FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if want := "SELECT t.a1, t.a2, b FROM t"; got != want {
		t.Errorf("projection item: got %s, want %s", got, want)
	}
}
