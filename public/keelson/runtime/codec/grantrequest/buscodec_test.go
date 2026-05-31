//go:build llm_generated_opus47

package grantrequest_test

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/grantrequest"
)

func sampleRequest() grantrequest.GrantRequest {
	return grantrequest.GrantRequest{
		FactId:          7,
		At:              time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		AppId:           "test.app",
		FilterPattern:   "task.*.cancel",
		FilterReason:    "user clicked allow",
		FilterDirection: "pub+sub",
		FilterSticky:    true,
	}
}

func TestBuscodecAutoRegistersGrantRequest(t *testing.T) {
	got := buscodec.Lookup[grantrequest.GrantRequest]()
	want := "grantRequest-sparse-cbor"
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
	got, err := buscodec.Decode[grantrequest.GrantRequest](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.FactId != orig.FactId {
		t.Errorf("FactId: got %v, want %v", got.FactId, orig.FactId)
	}
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v", got.At, orig.At)
	}
	if got.AppId != orig.AppId {
		t.Errorf("AppId: got %q, want %q", got.AppId, orig.AppId)
	}
	if got.FilterPattern != orig.FilterPattern {
		t.Errorf("FilterPattern: got %q, want %q", got.FilterPattern, orig.FilterPattern)
	}
	if got.FilterReason != orig.FilterReason {
		t.Errorf("FilterReason: got %q, want %q", got.FilterReason, orig.FilterReason)
	}
	if got.FilterDirection != orig.FilterDirection {
		t.Errorf("FilterDirection: got %q, want %q", got.FilterDirection, orig.FilterDirection)
	}
	if got.FilterSticky != orig.FilterSticky {
		t.Errorf("FilterSticky: got %v, want %v", got.FilterSticky, orig.FilterSticky)
	}
}

func TestBuscodecRoundTripDirectionVariants(t *testing.T) {
	// Pin every canonical CapDirectionE rendering survives the
	// symbol section round-trip — including the empty/unspecified
	// case which the wire-vocabulary stores as the literal empty
	// string (zero-value sentinel).
	dirs := []string{"pub", "sub", "pub+sub", ""}
	for _, d := range dirs {
		t.Run(d, func(t *testing.T) {
			orig := grantrequest.GrantRequest{
				FactId:          1,
				At:              time.Unix(0, 1_700_000_000_000_000_000).UTC(),
				AppId:           "test.app",
				FilterPattern:   "foo.>",
				FilterDirection: d,
			}
			wire, err := buscodec.Encode(orig)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := buscodec.Decode[grantrequest.GrantRequest](wire)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.FilterDirection != d {
				t.Errorf("FilterDirection: got %q, want %q", got.FilterDirection, d)
			}
		})
	}
}
