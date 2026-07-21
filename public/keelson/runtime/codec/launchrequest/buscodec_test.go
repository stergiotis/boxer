package launchrequest_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
)

func sampleRequest() launchrequest.LaunchRequest {
	return launchrequest.LaunchRequest{
		FactId:      7,
		At:          time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		TargetAppId: "test.app",
		ConfigKind:  "playLaunch",
		Config:      []byte{0x01, 0x02, 0x03, 0xff},
	}
}

func TestBuscodecAutoRegistersLaunchRequest(t *testing.T) {
	got := buscodec.Lookup[launchrequest.LaunchRequest]()
	want := "launchRequest-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleRequest()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[launchrequest.LaunchRequest](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.FactId != orig.FactId {
		t.Errorf("FactId: got %v, want %v", got.FactId, orig.FactId)
	}
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v", got.At, orig.At)
	}
	if got.TargetAppId != orig.TargetAppId {
		t.Errorf("TargetAppId: got %q, want %q", got.TargetAppId, orig.TargetAppId)
	}
	if got.ConfigKind != orig.ConfigKind {
		t.Errorf("ConfigKind: got %q, want %q", got.ConfigKind, orig.ConfigKind)
	}
	if !bytes.Equal(got.Config, orig.Config) {
		t.Errorf("Config: got %x, want %x", got.Config, orig.Config)
	}
}

func TestBuscodecRoundTripPlainOpen(t *testing.T) {
	// A plain open carries no config: empty kind (the symbol section's
	// zero-value sentinel) and empty bytes must survive the round trip.
	orig := launchrequest.LaunchRequest{
		FactId:      1,
		At:          time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		TargetAppId: "test.app",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[launchrequest.LaunchRequest](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ConfigKind != "" {
		t.Errorf("ConfigKind: got %q, want empty", got.ConfigKind)
	}
	if len(got.Config) != 0 {
		t.Errorf("Config: got %x, want empty", got.Config)
	}
}
