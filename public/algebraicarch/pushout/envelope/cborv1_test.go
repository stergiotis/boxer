package envelope

import (
	"bytes"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// The conformance suite lives in envelope/codectest, which imports this
// package; the checks that need no import cycle are exercised here
// against the properties codectest's fixture cannot reach.

// codectest.CanonicalEnvelope's timestamp has zero nanoseconds, so the
// suite cannot catch a codec that truncates sub-second precision — which
// is exactly what CBOR's default integer-seconds time encoding does.
func TestCBORV1_PreservesSubSecondTimestamps(tt *testing.T) {
	c := CBORV1{}
	want := time.Date(2026, 6, 12, 1, 2, 3, 123456789, time.UTC)
	env := sampleEnvelope(tt)
	env.Timestamp = want

	payload, err := c.Encode(env)
	if err != nil {
		tt.Fatal(err)
	}
	got, err := c.Decode(payload)
	if err != nil {
		tt.Fatal(err)
	}
	if !got.Timestamp.Equal(want) {
		tt.Fatalf("timestamp lost precision: got %v want %v", got.Timestamp.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

// RFC 3339 renders the zone, so the same instant in two locations would
// encode to different bytes unless the codec normalises. Determinism is
// a codec obligation; carrying the zone is not.
func TestCBORV1_ZoneDoesNotMoveTheBytes(tt *testing.T) {
	c := CBORV1{}
	instant := time.Date(2026, 6, 12, 1, 2, 3, 400000000, time.UTC)
	env := sampleEnvelope(tt)

	env.Timestamp = instant
	utc, err := c.Encode(env)
	if err != nil {
		tt.Fatal(err)
	}
	env.Timestamp = instant.In(time.FixedZone("plus-one", 3600))
	shifted, err := c.Encode(env)
	if err != nil {
		tt.Fatal(err)
	}
	if !bytes.Equal(utc, shifted) {
		tt.Fatal("the same instant in two zones encodes to different bytes")
	}
}

// Encode takes its envelope by value and must leave the caller's copy —
// including the zone it chose — untouched.
func TestCBORV1_EncodeDoesNotMutateCaller(tt *testing.T) {
	loc := time.FixedZone("plus-one", 3600)
	env := sampleEnvelope(tt)
	env.Timestamp = time.Date(2026, 6, 12, 1, 2, 3, 0, loc)
	before := env.Timestamp

	if _, err := (CBORV1{}).Encode(env); err != nil {
		tt.Fatal(err)
	}
	if !env.Timestamp.Equal(before) || env.Timestamp.Location() != loc {
		tt.Fatalf("Encode mutated the caller's envelope: %v", env.Timestamp)
	}
}

// A payload binding the same field twice has no single reading. This is
// the untrusted-bytes path, so the codec rejects it rather than picking.
func TestCBORV1_RejectsDuplicateMapKeys(tt *testing.T) {
	// {"producer": "alice", "producer": "mallory"} — hand-built, since a
	// conforming encoder cannot emit it.
	dup := []byte{0xA2}
	dup = append(dup, mustMarshal(tt, "producer")...)
	dup = append(dup, mustMarshal(tt, "alice")...)
	dup = append(dup, mustMarshal(tt, "producer")...)
	dup = append(dup, mustMarshal(tt, "mallory")...)

	if _, err := (CBORV1{}).Decode(dup); err == nil {
		tt.Fatal("expected a duplicate-key rejection")
	}
}

func mustMarshal(tt *testing.T, v any) []byte {
	tt.Helper()
	b, err := cbor.Marshal(v)
	if err != nil {
		tt.Fatal(err)
	}
	return b
}
