//go:build llm_generated_opus47

package persistreply_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/persistreply"
)

func TestBuscodecAutoRegistersPersistReply(t *testing.T) {
	got := buscodec.Lookup[persistreply.PersistReply]()
	want := "persistReply-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTripGetHit(t *testing.T) {
	// Successful Get: Found=true, Value populated, Reason empty.
	orig := persistreply.PersistReply{
		FactId: 1,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Found:  true,
		Value:  []byte{0x01, 0x02, 0xff, 0x00},
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[persistreply.PersistReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.Found {
		t.Errorf("Found: got false, want true")
	}
	if !bytes.Equal(got.Value, orig.Value) {
		t.Errorf("Value: got %x, want %x", got.Value, orig.Value)
	}
	if got.Reason != "" {
		t.Errorf("Reason: got %q, want empty", got.Reason)
	}
}

func TestBuscodecRoundTripGetMiss(t *testing.T) {
	// Get on a missing key: Found=false, Value empty, Reason empty.
	// All three fields collapse to zero values — verifies the
	// success-with-no-value path doesn't confuse the failure path.
	orig := persistreply.PersistReply{
		FactId: 2,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[persistreply.PersistReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Found {
		t.Errorf("Found: got true, want false")
	}
	if len(got.Value) != 0 {
		t.Errorf("Value: got %x, want empty", got.Value)
	}
	if got.Reason != "" {
		t.Errorf("Reason: got %q, want empty", got.Reason)
	}
}

func TestBuscodecRoundTripFailure(t *testing.T) {
	// Failure: Reason populated, Found/Value irrelevant. Reason
	// shares the cross-DTO vocabulary entry so the wire column is
	// queryable alongside TaskCancel.Reason, TaskError.Reason,
	// WatchReply.Reason, etc.
	orig := persistreply.PersistReply{
		FactId: 3,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Reason: "backend: connection refused",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[persistreply.PersistReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Reason != "backend: connection refused" {
		t.Errorf("Reason: got %q, want %q", got.Reason, "backend: connection refused")
	}
	if got.Found {
		t.Errorf("Found: got true, want false")
	}
	if len(got.Value) != 0 {
		t.Errorf("Value: got %x, want empty", got.Value)
	}
}

func TestBuscodecRoundTripBinaryValue(t *testing.T) {
	// Value is opaque app binary. Pin that the scalar-blob grammar
	// extension preserves the full 0..255 byte range — same gate
	// codec/taskdone introduced for the grammar.
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}
	orig := persistreply.PersistReply{
		FactId: 4,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Found:  true,
		Value:  payload,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[persistreply.PersistReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got.Value, payload) {
		t.Errorf("Value: got %x, want %x", got.Value, payload)
	}
}
