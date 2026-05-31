//go:build llm_generated_opus47

package dialogreply_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/dialogreply"
)

func TestBuscodecAutoRegistersDialogReply(t *testing.T) {
	got := buscodec.Lookup[dialogreply.DialogReply]()
	want := "dialogReply-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTripApproved(t *testing.T) {
	orig := dialogreply.DialogReply{
		FactId:              1,
		At:                  time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Approved:            true,
		HandleSubjectPrefix: "fs.handle.deadbeef",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[dialogreply.DialogReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// NaturalKey is an unused entity-key column; the sparse codec
	// canonicalises its nil default to empty []byte. At is compared by
	// instant (reflect.DeepEqual on time.Time is unreliable).
	orig.NaturalKey = got.NaturalKey
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v", got.At, orig.At)
	}
	got.At, orig.At = time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("roundtrip: got %+v, want %+v", got, orig)
	}
}

func TestBuscodecRoundTripDenied(t *testing.T) {
	orig := dialogreply.DialogReply{
		FactId:   2,
		At:       time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Approved: false,
		Reason:   "user cancelled",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[dialogreply.DialogReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Approved {
		t.Errorf("Approved: got true, want false")
	}
	if got.HandleSubjectPrefix != "" {
		t.Errorf("HandleSubjectPrefix: got %q, want empty", got.HandleSubjectPrefix)
	}
	if got.Reason != "user cancelled" {
		t.Errorf("Reason: got %q, want %q", got.Reason, "user cancelled")
	}
}
