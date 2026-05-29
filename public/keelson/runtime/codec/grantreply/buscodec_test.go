//go:build llm_generated_opus47

package grantreply_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/grantreply"
)

func sampleApproved() grantreply.GrantReply {
	return grantreply.GrantReply{
		FactId:   3,
		AtNs:     1_700_000_000_000_000_000,
		Approved: true,
		GrantId:  "42",
		Reason:   "auto-approve policy",
	}
}

func sampleDenied() grantreply.GrantReply {
	return grantreply.GrantReply{
		FactId:   4,
		AtNs:     1_700_000_000_000_000_000,
		Approved: false,
		Reason:   "deny-all policy",
	}
}

func TestBuscodecAutoRegistersGrantReply(t *testing.T) {
	got := buscodec.Lookup[grantreply.GrantReply]()
	want := "grantReply-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTripApproved(t *testing.T) {
	orig := sampleApproved()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[grantreply.GrantReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != orig {
		t.Errorf("roundtrip: got %+v, want %+v", got, orig)
	}
}

func TestBuscodecRoundTripDenied(t *testing.T) {
	// Denial path: Approved=false, GrantId="" (zero-value sentinel
	// reads as "no grant id assigned"), Reason carries the rationale.
	orig := sampleDenied()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[grantreply.GrantReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Approved {
		t.Errorf("Approved: got true, want false")
	}
	if got.GrantId != "" {
		t.Errorf("GrantId: got %q, want empty", got.GrantId)
	}
	if got.Reason != "deny-all policy" {
		t.Errorf("Reason: got %q, want %q", got.Reason, "deny-all policy")
	}
}
