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
