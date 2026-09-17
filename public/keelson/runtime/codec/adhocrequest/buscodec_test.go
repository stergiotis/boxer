package adhocrequest_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocrequest"
)

func TestBuscodecAutoRegistersAdhocRequest(t *testing.T) {
	got := buscodec.Lookup[adhocrequest.AdhocRequest]()
	if want := "adhocRequest-sparse-cbor"; got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTripPublish(t *testing.T) {
	orig := adhocrequest.AdhocRequest{
		FactId: 3, At: time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Op: adhocrequest.OpPublish, Alias: "items", Handle: "adhoc_0123456789abcdef",
		KeepAfterClose: true, ArrowStream: []byte{0xff, 0xff, 0xff, 0xff, 0x00, 0x01},
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[adhocrequest.AdhocRequest](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Op != orig.Op || got.Alias != orig.Alias || got.Handle != orig.Handle || got.KeepAfterClose != orig.KeepAfterClose {
		t.Errorf("fields: got %+v, want %+v", got, orig)
	}
	if !bytes.Equal(got.ArrowStream, orig.ArrowStream) {
		t.Errorf("ArrowStream: got %x, want %x", got.ArrowStream, orig.ArrowStream)
	}
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v", got.At, orig.At)
	}
}

func TestBuscodecRoundTripRetractLeavesTheRestZero(t *testing.T) {
	orig := adhocrequest.AdhocRequest{Op: adhocrequest.OpRetract, Handle: "adhoc_deadbeefdeadbeef"}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[adhocrequest.AdhocRequest](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Op != adhocrequest.OpRetract || got.Handle != orig.Handle || got.Alias != "" || got.KeepAfterClose || len(got.ArrowStream) != 0 {
		t.Errorf("got %+v", got)
	}
}
