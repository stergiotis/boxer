package play

import (
	"strings"
	"testing"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func TestDateTimePairWidgetMatchesFromTo(t *testing.T) {
	w := newDateTimePairWidget()
	slots := []paramSlot{
		{Name: "from", Type: "DateTime"},
		{Name: "to", Type: "DateTime"},
	}
	idx, ok := w.Matches(slots)
	if !ok {
		t.Fatal("expected match on from/to DateTime")
	}
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Errorf("idx = %v, want [0 1]", idx)
	}
}

// The inverse of the retired TestDateTimePairWidgetRejectsNonAdjacent, which
// pinned the adjacency rule ADR-0124 §SD5 removed. Kept as its mirror image so
// the record shows adjacency was retired deliberately rather than lost: an
// unrelated placeholder between the bounds must no longer cost the user a
// picker.
func TestDateTimePairWidgetMatchesAcrossInterleavedSlot(t *testing.T) {
	w := newDateTimePairWidget()
	slots := []paramSlot{
		{Name: "from", Type: "DateTime"},
		{Name: "x", Type: "UInt64"},
		{Name: "to", Type: "DateTime"},
	}
	idx, ok := w.Matches(slots)
	if !ok {
		t.Fatal("expected match on from/to separated by an unrelated slot")
	}
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 2 {
		t.Errorf("idx = %v, want [0 2]", idx)
	}
}

func TestMatchRangePairStems(t *testing.T) {
	cases := []struct {
		name  string
		slots []paramSlot
		want  []int // nil means "must not fold"
	}{{
		name: "timeline contract folds",
		slots: []paramSlot{
			{Name: "tl_min", Type: "DateTime64(3, 'UTC')"},
			{Name: "tl_max", Type: "DateTime64(3, 'UTC')"},
		},
		want: []int{0, 1},
	}, {
		name: "stemmed from/to folds",
		slots: []paramSlot{
			{Name: "a_from", Type: "DateTime"},
			{Name: "a_to", Type: "DateTime"},
		},
		want: []int{0, 1},
	}, {
		// Reversed editor order still yields lo-then-hi, which is the order
		// both widgets' Render assumes for its two bounds.
		name: "hi before lo folds, lo-then-hi",
		slots: []paramSlot{
			{Name: "to", Type: "DateTime"},
			{Name: "from", Type: "DateTime"},
		},
		want: []int{1, 0},
	}, {
		name: "nullable unwraps",
		slots: []paramSlot{
			{Name: "since", Type: "Nullable(DateTime64(3))"},
			{Name: "until", Type: "DateTime"},
		},
		want: []int{0, 1},
	}, {
		name: "case insensitive",
		slots: []paramSlot{
			{Name: "Start", Type: "DateTime"},
			{Name: "END", Type: "DateTime"},
		},
		want: []int{0, 1},
	}, {
		name: "distinct stems do not cross-pair",
		slots: []paramSlot{
			{Name: "a_min", Type: "DateTime"},
			{Name: "b_max", Type: "DateTime"},
		},
		want: nil,
	}, {
		name: "half a pair does not fold",
		slots: []paramSlot{
			{Name: "x_from", Type: "DateTime"},
			{Name: "unrelated", Type: "DateTime"},
		},
		want: nil,
	}, {
		name: "mixed vocabularies do not cross-pair",
		slots: []paramSlot{
			{Name: "x_from", Type: "DateTime"},
			{Name: "x_max", Type: "DateTime"},
		},
		want: nil,
	}, {
		// `photo` ends in "to" but carries no `_to` suffix; the separator is
		// what makes a suffix a suffix.
		name: "suffix needs its separator",
		slots: []paramSlot{
			{Name: "from", Type: "DateTime"},
			{Name: "photo", Type: "DateTime"},
		},
		want: nil,
	}, {
		name: "type mismatch does not fold",
		slots: []paramSlot{
			{Name: "x_from", Type: "DateTime"},
			{Name: "x_to", Type: "String"},
		},
		want: nil,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, ok := matchRangePair(tc.slots)
			if tc.want == nil {
				if ok {
					t.Fatalf("matchRangePair folded %v, want no fold (idx=%v)", tc.slots, idx)
				}
				return
			}
			if !ok {
				t.Fatalf("matchRangePair declined %v, want fold at %v", tc.slots, tc.want)
			}
			if len(idx) != len(tc.want) || idx[0] != tc.want[0] || idx[1] != tc.want[1] {
				t.Errorf("idx = %v, want %v", idx, tc.want)
			}
		})
	}
}

// Two stems in one query fold into two pairs. Before §SD5 this was
// unreachable: dedup-by-name meant only one `from` could exist, so the
// dispatcher's re-dispatch loop never had a second group match to make.
func TestMatchRangePairTwoStems(t *testing.T) {
	slots := []paramSlot{
		{Name: "a_from", Type: "DateTime"},
		{Name: "b_from", Type: "DateTime"},
		{Name: "a_to", Type: "DateTime"},
		{Name: "b_to", Type: "DateTime"},
	}
	idx, ok := matchRangePair(slots)
	if !ok || idx[0] != 0 || idx[1] != 2 {
		t.Fatalf("first pair = %v (ok=%v), want [0 2]", idx, ok)
	}
	rest := []paramSlot{slots[1], slots[3]}
	idx, ok = matchRangePair(rest)
	if !ok || idx[0] != 0 || idx[1] != 1 {
		t.Fatalf("second pair = %v (ok=%v), want [0 1]", idx, ok)
	}
}

func TestSplitRangeSuffix(t *testing.T) {
	cases := []struct {
		name       string
		stem, suff string
		ok         bool
	}{
		{"from", "", "from", true},
		{"tl_min", "tl", "min", true},
		{"TL_MAX", "tl", "max", true},
		{"a_b_since", "a_b", "since", true},
		{"photo", "", "", false},
		{"minimum", "", "", false},
		{"x", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		stem, suff, ok := splitRangeSuffix(tc.name)
		if ok != tc.ok || stem != tc.stem || suff != tc.suff {
			t.Errorf("splitRangeSuffix(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.name, stem, suff, ok, tc.stem, tc.suff, tc.ok)
		}
	}
}

func TestNearMissNote(t *testing.T) {
	cases := []struct {
		name     string
		unfolded []paramSlot
		ungroup  bool
		wantSub  string // "" means "must stay quiet"
	}{{
		name: "two datetimes that do not pair",
		unfolded: []paramSlot{
			{Name: "created", Type: "DateTime"},
			{Name: "deleted", Type: "DateTime"},
		},
		wantSub: "do not pair",
	}, {
		name: "type mismatch names both types",
		unfolded: []paramSlot{
			{Name: "x_from", Type: "DateTime"},
			{Name: "x_to", Type: "String"},
		},
		wantSub: "needs both DateTime",
	}, {
		// Bounds that agree on a non-DateTime type are somebody's string
		// range; a picker was never plausible, so saying so would be noise.
		name: "agreeing non-datetime bounds stay quiet",
		unfolded: []paramSlot{
			{Name: "x_from", Type: "String"},
			{Name: "x_to", Type: "String"},
		},
		wantSub: "",
	}, {
		name: "ungroup explains itself",
		unfolded: []paramSlot{
			{Name: "from", Type: "DateTime"},
			{Name: "to", Type: "DateTime"},
		},
		ungroup: true,
		wantSub: "ungroup",
	}, {
		// Nothing would have folded anyway, so the comment is not the reason
		// for anything and mentioning it would mislead.
		name: "ungroup with nothing foldable stays quiet",
		unfolded: []paramSlot{
			{Name: "a", Type: "UInt64"},
		},
		ungroup: true,
		wantSub: "",
	}, {
		name:     "one datetime stays quiet",
		unfolded: []paramSlot{{Name: "at", Type: "DateTime"}},
		wantSub:  "",
	}, {
		name:     "no datetimes stay quiet",
		unfolded: []paramSlot{{Name: "a", Type: "UInt64"}, {Name: "b", Type: "String"}},
		wantSub:  "",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nearMissNote(tc.unfolded, tc.ungroup)
			if tc.wantSub == "" {
				if got != "" {
					t.Fatalf("nearMissNote = %q, want silence", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("nearMissNote = %q, want a mention of %q", got, tc.wantSub)
			}
		})
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
