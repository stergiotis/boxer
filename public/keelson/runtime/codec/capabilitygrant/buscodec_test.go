package capabilitygrant_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/capabilitygrant"
)

func sampleGrantForBuscodec() capabilitygrant.CapabilityGrant {
	return capabilitygrant.CapabilityGrant{
		Id:            12345,
		NaturalKey:    []byte{0xa1, 0xb2, 0xc3, 0xd4},
		Ts:            time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		ExpiresAt:     time.Unix(0, 1_900_000_000_000_000_000).UTC(),
		Subject:       "user/alice/repo/foo",
		Capability:    "read",
		ValidityBegin: 1_700_000_000,
		ValidityEnd:   1_800_000_000,
		Active:        true,
		GranterFact:   option.Some(uint64(9876)),
	}
}

func TestBuscodecAutoRegistersSparseCBOR(t *testing.T) {
	got := buscodec.Lookup[capabilitygrant.CapabilityGrant]()
	if got.Name() != "capabilityGrant-sparse-cbor" {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), "capabilityGrant-sparse-cbor")
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleGrantForBuscodec()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[capabilitygrant.CapabilityGrant](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Id != orig.Id {
		t.Errorf("Id: got %v, want %v", got.Id, orig.Id)
	}
	if !bytes.Equal(got.NaturalKey, orig.NaturalKey) {
		t.Errorf("NaturalKey: got %x, want %x", got.NaturalKey, orig.NaturalKey)
	}
	if !got.Ts.Equal(orig.Ts) {
		t.Errorf("Ts: got %v, want %v", got.Ts, orig.Ts)
	}
	if !got.ExpiresAt.Equal(orig.ExpiresAt) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, orig.ExpiresAt)
	}
	if got.Subject != orig.Subject {
		t.Errorf("Subject: got %q, want %q", got.Subject, orig.Subject)
	}
	if got.Capability != orig.Capability {
		t.Errorf("Capability: got %q, want %q", got.Capability, orig.Capability)
	}
	if got.ValidityBegin != orig.ValidityBegin || got.ValidityEnd != orig.ValidityEnd {
		t.Errorf("Validity range: got %d..%d, want %d..%d",
			got.ValidityBegin, got.ValidityEnd, orig.ValidityBegin, orig.ValidityEnd)
	}
	if got.Active != orig.Active {
		t.Errorf("Active: got %v, want %v", got.Active, orig.Active)
	}
	if !got.GranterFact.Has || got.GranterFact.Val != orig.GranterFact.Val {
		t.Errorf("GranterFact: got %+v, want %+v", got.GranterFact, orig.GranterFact)
	}
}
