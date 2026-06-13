package eh

import (
	"bytes"
	"errors"
	"testing"
)

func TestWalkStreams_Nil(t *testing.T) {
	got := WalkStreams(nil)
	if got != nil {
		t.Fatalf("expected nil for nil error, got %#v", got)
	}
}

func TestWalkStreams_SimpleErrorfHasOneStackStream(t *testing.T) {
	err := Errorf("boom")
	streams := WalkStreams(err)
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	if streams[0].Name != "stack-0" {
		t.Fatalf("expected stream name 'stack-0', got %q", streams[0].Name)
	}
	// At least: 1 message fact + N frame stubs (>=1). The message fact has
	// Msg set; frame stubs have Source/Func set.
	var (
		msgs, frames int
	)
	for _, f := range streams[0].Facts {
		if f.Msg != "" {
			msgs++
		}
		if f.Source != "" || f.Func != "" {
			frames++
		}
	}
	if msgs != 1 {
		t.Fatalf("expected exactly 1 message fact, got %d (facts=%+v)", msgs, streams[0].Facts)
	}
	if frames == 0 {
		t.Fatalf("expected at least one frame stub, got 0 (facts=%+v)", streams[0].Facts)
	}
}

func TestWalkStreams_WrappedChainProducesPerStackStreams(t *testing.T) {
	// Each wrap captures a distinct stack (different call-site PCs in the
	// test function), so the PC-prefix dedup at stacktrace.go:16-23 does
	// NOT merge them — three nested Errorf calls produce three streams.
	// The test pins this observable behaviour so we notice if the dedup
	// rule (currently strict-prefix-only, identical reps don't merge) is
	// ever relaxed.
	inner := Errorf("inner")
	mid := Errorf("mid: %w", inner)
	outer := Errorf("outer: %w", mid)

	streams := WalkStreams(outer)
	if len(streams) != 3 {
		t.Fatalf("expected 3 distinct streams for nested wraps, got %d", len(streams))
	}
	var allMsgs int
	for _, s := range streams {
		for _, f := range s.Facts {
			if f.Msg != "" {
				allMsgs++
			}
		}
	}
	if allMsgs != 3 {
		t.Fatalf("expected 3 total message facts (inner/mid/outer), got %d", allMsgs)
	}
}

func TestWalkStreams_ErrorfWithoutStackProducesNoStackStream(t *testing.T) {
	err := ErrorfWithDataWithoutStack(nil, "no-stack here")
	streams := WalkStreams(err)
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	if streams[0].Name != "no-stack" {
		t.Fatalf("expected stream name 'no-stack', got %q", streams[0].Name)
	}
	if len(streams[0].Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(streams[0].Facts))
	}
	f := streams[0].Facts[0]
	if f.Msg != "no-stack here" {
		t.Fatalf("unexpected msg %q", f.Msg)
	}
	if f.Source != "" || f.Func != "" || f.Line != 0 {
		t.Fatalf("expected no frame fields on no-stack fact, got %+v", f)
	}
}

func TestWalkStreams_ErrorfWithDataSurfacesData(t *testing.T) {
	payload := []byte{0xa1, 0x63, 'k', 'e', 'y', 0x05} // CBOR: {"key":5}
	err := ErrorfWithData(payload, "with attached data")
	streams := WalkStreams(err)
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	var dataFacts int
	for _, f := range streams[0].Facts {
		if f.Data != nil {
			dataFacts++
			if !bytes.Equal(f.Data, payload) {
				t.Fatalf("data round-trip mismatch: got %x want %x", f.Data, payload)
			}
		}
	}
	if dataFacts != 1 {
		t.Fatalf("expected exactly 1 data fact, got %d", dataFacts)
	}
}

func TestWalkStreams_JoinedErrorsProduceMultipleStreams(t *testing.T) {
	// Two independently-produced stacks should not be merged by findStack's
	// PC-prefix detection (they have disjoint top frames).
	a := func() error { return Errorf("from A") }()
	b := func() error { return Errorf("from B") }()
	err := Errorf("joined: %w", errors.Join(a, b))

	streams := WalkStreams(err)
	if len(streams) < 2 {
		t.Fatalf("expected at least 2 streams for joined errors with distinct stacks, got %d: %+v", len(streams), streams)
	}
}

// TestWalkStreams_MatchesGatherStructEgress asserts the walker produces
// the same stream/fact layout as gatherFactsAndStacks (the same in-memory
// state that drives the zerolog egress). Comparing against the gather
// struct directly avoids coupling to whichever wire encoder zerolog is
// configured for (boxer's binary_log tag flips it to CBOR).
func TestWalkStreams_MatchesGatherStructEgress(t *testing.T) {
	// Build a multi-stream error: two siblings + an outer wrap.
	a := func() error { return Errorf("alpha") }()
	b := func() error { return Errorf("beta") }()
	composed := Errorf("compose: %w", errors.Join(a, b))

	// Reference: independently re-run the gather pipeline.
	g := newGatherFactsAndStacks()
	_ = g.addError(composed, 0)
	g.materialize()

	expectedStreams := 0
	if g.hasStacklessStream() {
		expectedStreams++
	}
	expectedStreams += len(g.perStackFacts[1:])

	walked := WalkStreams(composed)
	if got, want := len(walked), expectedStreams; got != want {
		t.Fatalf("stream count: walker=%d gather=%d", got, want)
	}

	// Index stacks by name for comparison.
	streamIdx := 0
	if g.hasStacklessStream() {
		nls := g.stacklessFacts()
		ws := walked[streamIdx]
		if ws.Name != "no-stack" {
			t.Fatalf("stream %d name: walker=%q expected 'no-stack'", streamIdx, ws.Name)
		}
		if len(ws.Facts) != len(nls) {
			t.Fatalf("no-stack fact count: walker=%d gather=%d", len(ws.Facts), len(nls))
		}
		for j, gf := range nls {
			compareFact(t, "no-stack", j, ws.Facts[j], gf)
		}
		streamIdx++
	}
	for i, perPositionFacts := range g.perStackFacts[1:] {
		ws := walked[streamIdx+i]
		// Flatten gather facts in the same per-position-then-per-fact order
		// as errorFactsLogger.MarshalZerologArray (zerolog.go:208-221).
		var flat []*errorFact
		for _, fs := range perPositionFacts {
			flat = append(flat, fs...)
		}
		if len(ws.Facts) != len(flat) {
			t.Fatalf("stack-%d fact count: walker=%d gather=%d", i, len(ws.Facts), len(flat))
		}
		for j, gf := range flat {
			compareFact(t, ws.Name, j, ws.Facts[j], gf)
		}
	}
}

func compareFact(t *testing.T, streamName string, idx int, got Fact, want *errorFact) {
	t.Helper()
	if got.Msg != want.Msg {
		t.Fatalf("%s fact %d msg: walker=%q gather=%q", streamName, idx, got.Msg, want.Msg)
	}
	if !bytes.Equal(got.Data, want.StructuredData) {
		t.Fatalf("%s fact %d data: walker=%x gather=%x", streamName, idx, got.Data, want.StructuredData)
	}
	if got.Id != want.Id {
		t.Fatalf("%s fact %d id: walker=%d gather=%d", streamName, idx, got.Id, want.Id)
	}
	if got.ParentId != want.ParentId {
		t.Fatalf("%s fact %d parentId: walker=%d gather=%d", streamName, idx, got.ParentId, want.ParentId)
	}
	if want.Frame == nil {
		if got.Source != "" || got.Func != "" || got.Line != 0 {
			t.Fatalf("%s fact %d expected no frame, walker has Source=%q Func=%q Line=%d", streamName, idx, got.Source, got.Func, got.Line)
		}
		return
	}
	if got.Source != want.Frame.File {
		t.Fatalf("%s fact %d source: walker=%q gather=%q", streamName, idx, got.Source, want.Frame.File)
	}
	if got.Func != want.Frame.Function {
		t.Fatalf("%s fact %d func: walker=%q gather=%q", streamName, idx, got.Func, want.Frame.Function)
	}
}
