package adhocreply_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocreply"
)

func TestBuscodecAutoRegistersAdhocReply(t *testing.T) {
	got := buscodec.Lookup[adhocreply.AdhocReply]()
	if want := "adhocReply-sparse-cbor"; got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTripResolve(t *testing.T) {
	orig := adhocreply.AdhocReply{
		FactId: 1, At: time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Ok: true, Handle: "adhoc_0123456789abcdef", Revision: 4, Rows: 120, Bytes: 4096,
		CreatedAtUs: 1_700_000_000_000_001, HandleLive: true,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[adhocreply.AdhocReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got.At, got.NaturalKey = orig.At, orig.NaturalKey // At compared by Equal; NaturalKey travels nil or empty
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("got %+v, want %+v", got, orig)
	}
}

func TestBuscodecRoundTripRefusal(t *testing.T) {
	orig := adhocreply.AdhocReply{Reason: "not the dataset's publisher", NoLive: true}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[adhocreply.AdhocReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Ok || got.Reason != orig.Reason || !got.NoLive || got.Handle != "" || got.Revision != 0 {
		t.Errorf("got %+v", got)
	}
}
