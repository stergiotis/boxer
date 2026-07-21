package launchreply_test

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
)

func sampleReply() launchreply.LaunchReply {
	return launchreply.LaunchReply{
		FactId:    9,
		At:        time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		WindowKey: 42,
	}
}

func TestBuscodecAutoRegistersLaunchReply(t *testing.T) {
	got := buscodec.Lookup[launchreply.LaunchReply]()
	want := "launchReply-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleReply()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[launchreply.LaunchReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.FactId != orig.FactId {
		t.Errorf("FactId: got %v, want %v", got.FactId, orig.FactId)
	}
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v", got.At, orig.At)
	}
	if got.WindowKey != orig.WindowKey {
		t.Errorf("WindowKey: got %v, want %v", got.WindowKey, orig.WindowKey)
	}
	if got.Reason != "" {
		t.Errorf("Reason: got %q, want empty", got.Reason)
	}
}

func TestBuscodecRoundTripRefusal(t *testing.T) {
	// A refusal reply: zero WindowKey, non-empty Reason.
	orig := launchreply.LaunchReply{
		FactId: 2,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Reason: "windowhost: app not registered id=nope",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[launchreply.LaunchReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.WindowKey != 0 {
		t.Errorf("WindowKey: got %v, want 0", got.WindowKey)
	}
	if got.Reason != orig.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, orig.Reason)
	}
}
