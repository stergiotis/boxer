package play

import (
	"testing"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func TestDateTimePairWidgetMatchesAdjacentFromTo(t *testing.T) {
	w := newDateTimePairWidget()
	slots := []paramSlot{
		{Name: "from", Type: "DateTime"},
		{Name: "to", Type: "DateTime"},
	}
	idx, ok := w.Matches(slots)
	if !ok {
		t.Fatal("expected match on adjacent from/to DateTime")
	}
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Errorf("idx = %v, want [0 1]", idx)
	}
}

func TestDateTimePairWidgetRejectsNonAdjacent(t *testing.T) {
	w := newDateTimePairWidget()
	slots := []paramSlot{
		{Name: "from", Type: "DateTime"},
		{Name: "x", Type: "UInt64"},
		{Name: "to", Type: "DateTime"},
	}
	if _, ok := w.Matches(slots); ok {
		t.Error("should not match non-adjacent from/to")
	}
}

func TestDateTimePairWidgetRejectsNonDateTimeTypes(t *testing.T) {
	w := newDateTimePairWidget()
	cases := [][]paramSlot{
		{{Name: "from", Type: "UInt64"}, {Name: "to", Type: "DateTime"}},
		{{Name: "from", Type: "DateTime"}, {Name: "to", Type: "String"}},
	}
	for i, slots := range cases {
		if _, ok := w.Matches(slots); ok {
			t.Errorf("case %d: should not match non-DateTime types: %v", i, slots)
		}
	}
}

func TestIsDateTimeType(t *testing.T) {
	yes := []string{
		"DateTime", "datetime", "DateTime('UTC')", "DateTime64",
		"DateTime64(3)", "DateTime64(3, 'Europe/Berlin')",
		"Nullable(DateTime)", "Nullable(DateTime64(3))",
	}
	no := []string{
		"UInt64", "String", "Date", "Array(DateTime)",
		"Tuple(DateTime, DateTime)",
	}
	for _, s := range yes {
		if !isDateTimeType(s) {
			t.Errorf("isDateTimeType(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isDateTimeType(s) {
			t.Errorf("isDateTimeType(%q) = true, want false", s)
		}
	}
}

func TestScalarTextWidgetMatchesFirst(t *testing.T) {
	w := newScalarTextWidget()
	slots := []paramSlot{
		{Name: "a", Type: "UInt64"},
		{Name: "b", Type: "String"},
	}
	idx, ok := w.Matches(slots)
	if !ok {
		t.Fatal("expected match on any non-empty slot list")
	}
	if len(idx) != 1 || idx[0] != 0 {
		t.Errorf("idx = %v, want [0]", idx)
	}
}

func TestScalarTextWidgetEmptyRejects(t *testing.T) {
	w := newScalarTextWidget()
	if _, ok := w.Matches(nil); ok {
		t.Error("empty slot list should not match")
	}
}

func TestScanUngroupHint(t *testing.T) {
	cases := map[string]bool{
		"-- play: ungroup\nSELECT 1":      true,
		"--play: ungroup\nSELECT 1":       true,
		"  --  play: ungroup  \nSELECT 1": true,
		"-- PLAY: UNGROUP\nSELECT 1":      true,
		"SELECT 1 -- play: ungroup":       false, // not a line comment
		"-- some other comment\nSELECT 1": false,
		"SELECT 1":                        false,
	}
	for sql, want := range cases {
		if got := scanUngroupHint(sql); got != want {
			t.Errorf("scanUngroupHint(%q) = %v, want %v", sql, got, want)
		}
	}
}

func TestUpdatePackedFromDraftFreshSeedToNow(t *testing.T) {
	// Fresh slot, empty draft → parse fails, packed was 0, seed to now.
	st := &dateTimePairSlotState{}
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	updatePackedFromDraft(st, "", now)
	if !st.seeded {
		t.Error("seeded should flip true after first call")
	}
	if !st.parseOK {
		t.Error("parseOK should be true after seed-to-now")
	}
	got := c.UnpackDateTimeUtc(st.packed)
	if !got.Equal(now) {
		t.Errorf("packed = %v, want %v", got, now)
	}
}

func TestUpdatePackedFromDraftFreshWithValidDraft(t *testing.T) {
	// Fresh slot, parseable draft → parse succeeds, packed = parsed.
	st := &dateTimePairSlotState{}
	updatePackedFromDraft(st, "2026-06-01 09:30:00", time.Now())
	if !st.parseOK {
		t.Error("parseOK should be true after successful parse")
	}
	got := c.UnpackDateTimeUtc(st.packed)
	want := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("packed = %v, want %v", got, want)
	}
}

func TestUpdatePackedFromDraftNoChange(t *testing.T) {
	// Seeded, draft unchanged → no-op.
	st := &dateTimePairSlotState{seeded: true, lastDraft: "2026-06-01 09:30:00", packed: 99, parseOK: false}
	before := *st
	updatePackedFromDraft(st, "2026-06-01 09:30:00", time.Now())
	if *st != before {
		t.Errorf("state mutated on no-op: before=%+v after=%+v", before, *st)
	}
}

func TestUpdatePackedFromDraftUnparseableKeepsPacked(t *testing.T) {
	// Seeded, packed valid, user typed garbage → packed preserved,
	// parseOK=false signals the mirror writeback to skip.
	const validPacked uint64 = 42
	st := &dateTimePairSlotState{seeded: true, lastDraft: "2026-06-01 09:30:00", packed: validPacked, parseOK: true}
	updatePackedFromDraft(st, "garbage", time.Now())
	if st.packed != validPacked {
		t.Errorf("packed = %d, want %d (preserved across unparseable input)", st.packed, validPacked)
	}
	if st.parseOK {
		t.Error("parseOK should flip to false on unparseable input")
	}
}

func TestUpdatePackedFromDraftRecoverFromUnparseable(t *testing.T) {
	// User fixes their typo → parseOK flips back true.
	st := &dateTimePairSlotState{seeded: true, lastDraft: "garbage", packed: 1, parseOK: false}
	updatePackedFromDraft(st, "2026-06-01 09:30:00", time.Now())
	if !st.parseOK {
		t.Error("parseOK should recover when draft becomes parseable")
	}
	got := c.UnpackDateTimeUtc(st.packed)
	want := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("packed = %v, want %v", got, want)
	}
}

func TestParseFormatChDateTimeRoundTrip(t *testing.T) {
	const canonical = "2026-05-24 12:34:56"
	t1, ok := parseChDateTime(canonical)
	if !ok {
		t.Fatalf("parseChDateTime(%q) failed", canonical)
	}
	if got := formatChDateTime(t1); got != canonical {
		t.Errorf("round-trip = %q, want %q", got, canonical)
	}
	// RFC3339 input also lands back in canonical form.
	t2, ok := parseChDateTime("2026-05-24T12:34:56Z")
	if !ok {
		t.Fatal("RFC3339 parse failed")
	}
	if got := formatChDateTime(t2); got != canonical {
		t.Errorf("RFC3339 round-trip = %q, want %q", got, canonical)
	}
}
