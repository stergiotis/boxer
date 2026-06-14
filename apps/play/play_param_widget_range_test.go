package play

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubEvaluator is a minimal timeRangeEvaluatorI used in tests.
// Returns the millis the caller pre-seeded; lets us assert that
// Render's eval-on-change side-effect ran without spinning up a
// real chlocalbroker.
type stubEvaluator struct {
	fromMs, toMs int64
	err          error
	calls        int
}

func (s *stubEvaluator) Eval(_ context.Context, _ time.Time, _ uint16, _, _ string) (int64, int64, error) {
	s.calls++
	return s.fromMs, s.toMs, s.err
}

func TestDateTimeRangeWidgetMatchesGatesOnEvaluator(t *testing.T) {
	slots := []paramSlot{
		{Name: "from", Type: "DateTime"},
		{Name: "to", Type: "DateTime"},
	}
	w := newDateTimeRangeWidget()

	if _, ok := w.Matches(slots); ok {
		t.Error("Matches should return false when evaluator is unwired")
	}

	w.SetTimeRangeEvaluator(&stubEvaluator{})
	idx, ok := w.Matches(slots)
	if !ok {
		t.Fatal("Matches should return true after evaluator is wired")
	}
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Errorf("idx = %v, want [0 1]", idx)
	}
}

func TestDateTimeRangeWidgetRejectsNonAdjacent(t *testing.T) {
	w := newDateTimeRangeWidget()
	w.SetTimeRangeEvaluator(&stubEvaluator{})
	slots := []paramSlot{
		{Name: "from", Type: "DateTime"},
		{Name: "x", Type: "UInt64"},
		{Name: "to", Type: "DateTime"},
	}
	if _, ok := w.Matches(slots); ok {
		t.Error("should not match non-adjacent from/to")
	}
}

func TestDateTimeRangeWidgetRejectsNonDateTimeTypes(t *testing.T) {
	w := newDateTimeRangeWidget()
	w.SetTimeRangeEvaluator(&stubEvaluator{})
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

func TestDateTimeRangeWidgetIsGroup(t *testing.T) {
	w := newDateTimeRangeWidget()
	if !w.IsGroup() {
		t.Error("dateTimeRangeWidget.IsGroup() should be true (consumes a from/to pair)")
	}
}

func TestDateTimeRangeStateSeedsFromDrafts(t *testing.T) {
	fromDraft := "2026-05-23 00:00:00"
	toDraft := "2026-05-24 00:00:00"
	drafts := map[string]*string{
		"from": &fromDraft,
		"to":   &toDraft,
	}
	st := newDateTimeRangeState("from", "to", drafts)
	if st.fromExpr != "'2026-05-23 00:00:00'" {
		t.Errorf("fromExpr = %q, want quoted literal", st.fromExpr)
	}
	if st.toExpr != "'2026-05-24 00:00:00'" {
		t.Errorf("toExpr = %q, want quoted literal", st.toExpr)
	}
}

func TestDateTimeRangeStateDefaultsWhenDraftsEmpty(t *testing.T) {
	drafts := map[string]*string{}
	st := newDateTimeRangeState("from", "to", drafts)
	if st.fromExpr != "anchor_now - INTERVAL 1 HOUR" {
		t.Errorf("fromExpr = %q, want anchor_now default", st.fromExpr)
	}
	if st.toExpr != "anchor_now" {
		t.Errorf("toExpr = %q, want anchor_now default", st.toExpr)
	}
}

func TestQuoteForCH(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"2026-05-24 12:00:00":          "'2026-05-24 12:00:00'",
		"'already quoted'":             "'already quoted'",
		"(expression)":                 "(expression)",
		"anchor_now":                   "anchor_now",
		"anchor_now - INTERVAL 1 HOUR": "anchor_now - INTERVAL 1 HOUR",
	}
	for in, want := range cases {
		if got := quoteForCH(in); got != want {
			t.Errorf("quoteForCH(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatChDateTimeMilli(t *testing.T) {
	// 2026-05-24 12:34:56.789 UTC in epoch millis.
	ts := time.Date(2026, 5, 24, 12, 34, 56, 789_000_000, time.UTC)
	if got, want := formatChDateTimeMilli(ts), "2026-05-24 12:34:56.789"; got != want {
		t.Errorf("formatChDateTimeMilli = %q, want %q", got, want)
	}
}

func TestDateTimeRangeWidgetEvalErrorSurfaces(t *testing.T) {
	// runEval with a stub that errors leaves lastEval zero and
	// lastEvalErr populated — the status line would render the
	// message; drafts stay untouched.
	w := newDateTimeRangeWidget()
	stub := &stubEvaluator{err: errors.New("broker offline")}
	w.SetTimeRangeEvaluator(stub)
	w.state = newDateTimeRangeState("from", "to", nil)
	w.runEval()
	if w.state.lastEvalErr == nil {
		t.Fatal("expected lastEvalErr populated")
	}
	// EvaluatedRange{} (struct zero), not epoch-zero: AsFromTime
	// would happily return 1970-01-01 on the latter, which IS a
	// real instant.
	if w.state.lastEval.FromEpochMS != 0 || w.state.lastEval.ToEpochMS != 0 {
		t.Errorf("lastEval should be struct-zero on error, got %+v", w.state.lastEval)
	}
	if stub.calls != 1 {
		t.Errorf("stub called %d times, want 1", stub.calls)
	}
}

func TestDateTimeRangeWidgetEvalSuccessSetsFlag(t *testing.T) {
	w := newDateTimeRangeWidget()
	stub := &stubEvaluator{fromMs: 1_700_000_000_000, toMs: 1_700_003_600_000}
	w.SetTimeRangeEvaluator(stub)
	w.state = newDateTimeRangeState("from", "to", nil)
	w.runEval()
	if w.state.lastEvalErr != nil {
		t.Fatalf("unexpected eval error: %v", w.state.lastEvalErr)
	}
	if !w.state.lastEvalSet {
		t.Error("lastEvalSet should be true after a successful Eval")
	}
	if got := w.state.lastEval.FromEpochMS; got != 1_700_000_000_000 {
		t.Errorf("FromEpochMS = %d, want 1700000000000", got)
	}
}

func TestDateTimeRangeWidgetResyncDetectsExternalDraftChange(t *testing.T) {
	w := newDateTimeRangeWidget()
	w.SetTimeRangeEvaluator(&stubEvaluator{})

	fromDraft := "2026-05-01 00:00:00.000"
	toDraft := "2026-05-02 00:00:00.000"
	drafts := map[string]*string{"from": &fromDraft, "to": &toDraft}
	w.state = newDateTimeRangeState("from", "to", drafts)
	startGen := w.state.idGeneration

	// First call: drafts match what newDateTimeRangeState seeded.
	if w.resyncFromDrafts(drafts) {
		t.Error("resync should be a no-op when drafts equal lastMirrored*")
	}
	if w.state.idGeneration != startGen {
		t.Error("idGeneration should not bump without a change")
	}

	// Simulate external SQL edit: drafts change behind the picker's back.
	fromDraft = "2026-06-01 00:00:00.000"
	if !w.resyncFromDrafts(drafts) {
		t.Fatal("resync should return true on external change")
	}
	if w.state.fromExpr != "'2026-06-01 00:00:00.000'" {
		t.Errorf("fromExpr = %q, want quoted CH literal", w.state.fromExpr)
	}
	if w.state.lastMirroredFrom != fromDraft {
		t.Errorf("lastMirroredFrom = %q, want %q", w.state.lastMirroredFrom, fromDraft)
	}
	if w.state.idGeneration != startGen+1 {
		t.Errorf("idGeneration = %d, want %d (bump on resync)", w.state.idGeneration, startGen+1)
	}
	if w.state.lastEvalSet {
		t.Error("lastEvalSet should be cleared on resync")
	}
	if w.state.packedRange != "" {
		t.Error("packedRange should be cleared on resync")
	}
}

func TestDateTimeRangeWidgetResyncEmptyDraftsNoChange(t *testing.T) {
	// Drafts both empty + lastMirrored both empty → no resync.
	w := newDateTimeRangeWidget()
	w.state = &dateTimeRangeState{fromName: "from", toName: "to"}
	drafts := map[string]*string{}
	if w.resyncFromDrafts(drafts) {
		t.Error("empty drafts should not trigger resync when lastMirrored is also empty")
	}
}

func TestParseChDateTimeAcceptsMillisForm(t *testing.T) {
	cases := []string{
		"2026-05-24 12:34:56",
		"2026-05-24 12:34:56.789",
		"'2026-05-24 12:34:56.000'",
		"2026-05-24T12:34:56Z",
	}
	for _, in := range cases {
		if _, ok := parseChDateTime(in); !ok {
			t.Errorf("parseChDateTime(%q) returned ok=false; want parsed", in)
		}
	}
}

func TestDateTimeRangeWidgetClearStateForAbsent(t *testing.T) {
	w := newDateTimeRangeWidget()
	w.state = &dateTimeRangeState{fromName: "from", toName: "to"}
	w.ClearStateForAbsent(map[string]struct{}{"from": {}, "to": {}})
	if w.state == nil {
		t.Error("state cleared even though both slots still present")
	}
	w.ClearStateForAbsent(map[string]struct{}{"from": {}})
	if w.state != nil {
		t.Error("state should be cleared when one of the pair disappears")
	}
}
