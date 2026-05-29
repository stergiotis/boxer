//go:build llm_generated_opus47

package watchrequest_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchrequest"
)

func TestBuscodecAutoRegistersWatchRequest(t *testing.T) {
	got := buscodec.Lookup[watchrequest.WatchRequest]()
	want := "watchRequest-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := watchrequest.WatchRequest{
		FactId:         1,
		AtNs:           1_700_000_000_000_000_000,
		PollFallback:   true,
		PollIntervalMs: 250,
		Recursive:      true,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[watchrequest.WatchRequest](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != orig {
		t.Errorf("roundtrip: got %+v, want %+v", got, orig)
	}
}

func TestBuscodecRoundTripAllDefaults(t *testing.T) {
	// All zero values is the "use broker defaults" wire shape that
	// the legacy UnmarshalWatchRequest tolerated as an empty
	// payload. The migrated codec round-trips it explicitly — the
	// nil-payload back-compat lives in fsbroker.UnmarshalWatchRequest.
	orig := watchrequest.WatchRequest{
		FactId: 1,
		AtNs:   1_700_000_000_000_000_000,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[watchrequest.WatchRequest](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != orig {
		t.Errorf("roundtrip: got %+v, want %+v", got, orig)
	}
}
