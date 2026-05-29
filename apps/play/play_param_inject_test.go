//go:build llm_generated_opus47

package play

import (
	"strings"
	"testing"
)

// slotsOf is a tiny test helper: build a []paramSlot from
// (name, type) pairs. Keeps the test bodies readable.
func slotsOf(pairs ...string) []paramSlot {
	if len(pairs)%2 != 0 {
		panic("slotsOf: odd argument count (need name, type pairs)")
	}
	out := make([]paramSlot, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, paramSlot{Name: pairs[i], Type: pairs[i+1]})
	}
	return out
}

func TestSyncParamPreludeInsertsLeadingSet(t *testing.T) {
	sql := `SELECT {a : UInt64}`
	out, changed := SyncParamPrelude(sql, slotsOf("a", "UInt64"), map[string]string{"a": "42"})
	if !changed {
		t.Fatalf("expected changed=true; out=%q", out)
	}
	want := "SET param_a = 42;\nSELECT {a : UInt64}"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestSyncParamPreludeIdempotent(t *testing.T) {
	sql := "SET param_a = 42;\nSELECT {a : UInt64}"
	out, changed := SyncParamPrelude(sql, slotsOf("a", "UInt64"), map[string]string{"a": "42"})
	if changed {
		t.Fatalf("expected changed=false on identity rewrite; out=%q", out)
	}
	if out != sql {
		t.Errorf("out = %q, want unchanged %q", out, sql)
	}
}

func TestSyncParamPreludeUpdatesValue(t *testing.T) {
	sql := "SET param_a = 1;\nSELECT {a : UInt64}"
	out, changed := SyncParamPrelude(sql, slotsOf("a", "UInt64"), map[string]string{"a": "99"})
	if !changed {
		t.Fatalf("expected changed=true")
	}
	want := "SET param_a = 99;\nSELECT {a : UInt64}"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestSyncParamPreludeDropsStaleSet(t *testing.T) {
	sql := "SET param_a = 1;\nSET param_b = 2;\nSELECT {a : UInt64}"
	out, _ := SyncParamPrelude(sql, slotsOf("a", "UInt64"), map[string]string{"a": "1"})
	if strings.Contains(out, "param_b") {
		t.Errorf("stale param_b not dropped: %q", out)
	}
	if !strings.Contains(out, "SET param_a = 1;") {
		t.Errorf("param_a survived but lost format: %q", out)
	}
}

func TestSyncParamPreludeStringValueQuoted(t *testing.T) {
	out, changed := SyncParamPrelude(
		`SELECT {x : String}`,
		slotsOf("x", "String"),
		map[string]string{"x": "hello world"})
	if !changed {
		t.Fatal("expected changed=true")
	}
	if !strings.Contains(out, "SET param_x = 'hello world';") {
		t.Errorf("string not quoted: %q", out)
	}
}

func TestSyncParamPreludeStringEscaped(t *testing.T) {
	out, _ := SyncParamPrelude(
		`SELECT {x : String}`,
		slotsOf("x", "String"),
		map[string]string{"x": "it's\nhello"})
	if !strings.Contains(out, `SET param_x = 'it\'s\nhello';`) {
		t.Errorf("escapes not applied: %q", out)
	}
}

func TestSyncParamPreludeArrayPassthrough(t *testing.T) {
	out, _ := SyncParamPrelude(
		`SELECT {a : Array(UInt64)}`,
		slotsOf("a", "Array(UInt64)"),
		map[string]string{"a": "[1, 2, 3]"})
	if !strings.Contains(out, "SET param_a = [1, 2, 3];") {
		t.Errorf("array literal mis-encoded: %q", out)
	}
}

func TestSyncParamPreludePreservesOrder(t *testing.T) {
	sql := `SELECT * FROM t WHERE ts BETWEEN {from : DateTime} AND {to : DateTime}`
	out, _ := SyncParamPrelude(sql,
		slotsOf("from", "DateTime", "to", "DateTime"),
		map[string]string{
			"from": "2026-01-01 00:00:00",
			"to":   "2026-01-02 00:00:00",
		})
	fromIdx := strings.Index(out, "param_from")
	toIdx := strings.Index(out, "param_to")
	if fromIdx < 0 || toIdx < 0 || fromIdx >= toIdx {
		t.Errorf("order not preserved: from=%d to=%d in %q", fromIdx, toIdx, out)
	}
}

func TestSyncParamPreludeDateTimeQuoted(t *testing.T) {
	out, _ := SyncParamPrelude(
		`SELECT {ts : DateTime}`,
		slotsOf("ts", "DateTime"),
		map[string]string{"ts": "2026-05-24 12:00:00"})
	if !strings.Contains(out, "SET param_ts = '2026-05-24 12:00:00';") {
		t.Errorf("DateTime not quoted: %q", out)
	}
}

func TestSyncParamPreludeNumericTypeRejectsParens(t *testing.T) {
	// User typed (42) into a UInt64 widget: shape isn't numeric, so
	// encoder falls through to quoted-string. CH will reject the
	// type mismatch at execute time, surfacing a typed error rather
	// than a silent corruption.
	out, _ := SyncParamPrelude(
		`SELECT {n : UInt64}`,
		slotsOf("n", "UInt64"),
		map[string]string{"n": "(42)"})
	if !strings.Contains(out, "SET param_n = '(42)';") {
		t.Errorf("non-numeric value for numeric type not quoted: %q", out)
	}
}

func TestSyncParamPreludeNumericNullable(t *testing.T) {
	out, _ := SyncParamPrelude(
		`SELECT {n : Nullable(UInt64)}`,
		slotsOf("n", "Nullable(UInt64)"),
		map[string]string{"n": "7"})
	if !strings.Contains(out, "SET param_n = 7;") {
		t.Errorf("Nullable(UInt64) numeric not verbatim: %q", out)
	}
}

func TestSyncParamPreludeParseErrorReturnsInputUnchanged(t *testing.T) {
	sql := `THIS IS NOT SQL`
	out, changed := SyncParamPrelude(sql, slotsOf("a", "UInt64"), map[string]string{"a": "1"})
	if changed {
		t.Errorf("expected changed=false on parse error")
	}
	if out != sql {
		t.Errorf("out = %q, want unchanged %q", out, sql)
	}
}

func TestRecomposeMirrorSteadyState(t *testing.T) {
	canonical := "SET param_a = 1;\nSELECT {a : UInt64}"
	mirror := "SELECT {a : UInt64}"
	syncedFrom := mirror
	out := recomposeMirror(canonical, mirror, syncedFrom)
	if !out.OK {
		t.Fatal("OK should be true on steady state")
	}
	if out.Canonical != canonical {
		t.Errorf("Canonical changed unexpectedly: %q", out.Canonical)
	}
	if out.Mirror != mirror {
		t.Errorf("Mirror changed unexpectedly: %q", out.Mirror)
	}
	if out.SyncedFrom != syncedFrom {
		t.Errorf("SyncedFrom changed unexpectedly: %q", out.SyncedFrom)
	}
	if out.Prelude != "SET param_a = 1;\n" {
		t.Errorf("Prelude = %q, want SET-block", out.Prelude)
	}
}

func TestRecomposeMirrorCanonicalChangedRefreshesMirror(t *testing.T) {
	// Widget added a SET line — canonical now has two SETs, mirror
	// only saw one before. syncedFrom is stale; canonical wins.
	canonical := "SET param_a = 1;\nSET param_b = 2;\nSELECT {a : UInt64}+{b : UInt64}"
	mirror := "SELECT {a : UInt64}"
	syncedFrom := mirror
	out := recomposeMirror(canonical, mirror, syncedFrom)
	if !out.OK {
		t.Fatal("OK should be true")
	}
	if out.Canonical != canonical {
		t.Errorf("Canonical should be unchanged, got %q", out.Canonical)
	}
	want := "SELECT {a : UInt64}+{b : UInt64}"
	if out.Mirror != want {
		t.Errorf("Mirror = %q, want %q", out.Mirror, want)
	}
	if out.SyncedFrom != want {
		t.Errorf("SyncedFrom should follow Mirror, got %q", out.SyncedFrom)
	}
}

func TestRecomposeMirrorMirrorChangedRecomposesCanonical(t *testing.T) {
	// User typed in the residual editor — mirror has "+1" appended.
	canonical := "SET param_a = 1;\nSELECT {a : UInt64}"
	syncedFrom := "SELECT {a : UInt64}"
	mirror := "SELECT {a : UInt64}+1"
	out := recomposeMirror(canonical, mirror, syncedFrom)
	if !out.OK {
		t.Fatal("OK should be true")
	}
	wantCanon := "SET param_a = 1;\nSELECT {a : UInt64}+1"
	if out.Canonical != wantCanon {
		t.Errorf("Canonical = %q, want %q", out.Canonical, wantCanon)
	}
	if out.Mirror != mirror {
		t.Errorf("Mirror should pass through, got %q", out.Mirror)
	}
	if out.SyncedFrom != mirror {
		t.Errorf("SyncedFrom should follow Mirror, got %q", out.SyncedFrom)
	}
}

func TestRecomposeMirrorCanonicalWinsOverMirror(t *testing.T) {
	// Both changed in the same frame — canonical (parser) wins.
	canonical := "SET param_a = 9;\nSELECT new_residual"
	syncedFrom := "SELECT old_residual"
	mirror := "SELECT mirror_local_edit"
	out := recomposeMirror(canonical, mirror, syncedFrom)
	if out.Mirror != "SELECT new_residual" {
		t.Errorf("Mirror = %q, want refreshed from canonical", out.Mirror)
	}
	if out.SyncedFrom != "SELECT new_residual" {
		t.Errorf("SyncedFrom = %q, want refreshed from canonical", out.SyncedFrom)
	}
}

func TestRecomposeMirrorParseErrorReturnsNotOK(t *testing.T) {
	out := recomposeMirror("THIS IS NOT SQL", "anything", "")
	if out.OK {
		t.Error("OK should be false on parse error")
	}
	if out.Canonical != "THIS IS NOT SQL" {
		t.Errorf("Canonical should pass through on error, got %q", out.Canonical)
	}
}

func TestSyncParamPreludeRoundTripsThroughExtractParams(t *testing.T) {
	out, _ := SyncParamPrelude(
		`SELECT {x : String} + {n : UInt64}`,
		slotsOf("x", "String", "n", "UInt64"),
		map[string]string{"x": "hello world", "n": "7"})
	residual, params, err := ExtractParams(out)
	if err != nil {
		t.Fatalf("ExtractParams: %v", err)
	}
	if got, want := params["param_x"], "hello world"; got != want {
		t.Errorf("param_x = %q, want %q", got, want)
	}
	if got, want := params["param_n"], "7"; got != want {
		t.Errorf("param_n = %q, want %q", got, want)
	}
	if !strings.Contains(residual, "SELECT") {
		t.Errorf("residual lost SELECT: %q", residual)
	}
}
