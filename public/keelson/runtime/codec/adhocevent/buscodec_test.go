package adhocevent_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocevent"
)

func TestBuscodecAutoRegistersAdhocEvent(t *testing.T) {
	got := buscodec.Lookup[adhocevent.AdhocEvent]()
	if want := "adhocEvent-sparse-cbor"; got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := adhocevent.AdhocEvent{
		FactId: 2, At: time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Op: adhocevent.OpPublished, Handle: "adhoc_0123456789abcdef", Alias: "pprof_cpu",
		Publisher: "github.com/example/imzrt", Revision: 7,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[adhocevent.AdhocEvent](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got.At, got.NaturalKey = orig.At, orig.NaturalKey // At compared by Equal; NaturalKey travels nil or empty
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("got %+v, want %+v", got, orig)
	}
}
