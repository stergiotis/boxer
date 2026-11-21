package eh_test

import (
	"errors"
	"io"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func TestLookupFactFindsAWrappedFact(t *testing.T) {
	inner := eb.Build().Str("path", "/x").Int("tries", 3).Errorf("open: %w", io.EOF)
	outer := eh.Errorf("load: %w", inner)

	path, ok := eh.LookupFact[string](outer, "path")
	if !ok || path != "/x" {
		t.Fatalf("path = %q, %v; want /x, true", path, ok)
	}
	tries, ok := eh.LookupFact[int](outer, "tries")
	if !ok || tries != 3 {
		t.Fatalf("tries = %d, %v; want 3, true", tries, ok)
	}
	if _, ok = eh.LookupFact[string](outer, "absent"); ok {
		t.Fatal("an absent key must not be found")
	}
	if _, ok = eh.LookupFact[int](outer, "path"); ok {
		t.Fatal("a value that does not decode into T must report not ok")
	}
}

func TestLookupFactOuterWinsAndJoinIsSearched(t *testing.T) {
	inner := eb.Build().Str("k", "inner").Errorf("inner")
	outer := eb.Build().Str("k", "outer").Errorf("outer: %w", inner)
	if v, _ := eh.LookupFact[string](outer, "k"); v != "outer" {
		t.Fatalf("k = %q; the outermost carrier wins", v)
	}
	joined := errors.Join(errors.New("plain"), inner)
	if v, ok := eh.LookupFact[string](joined, "k"); !ok || v != "inner" {
		t.Fatalf("k = %q, %v; joined errors are searched", v, ok)
	}
	if _, ok := eh.LookupFact[string](nil, "k"); ok {
		t.Fatal("nil carries nothing")
	}
}
