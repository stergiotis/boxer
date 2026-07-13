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
