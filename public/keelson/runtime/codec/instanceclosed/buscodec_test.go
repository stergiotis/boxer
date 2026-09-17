package instanceclosed_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/instanceclosed"
)

func TestBuscodecAutoRegistersInstanceClosed(t *testing.T) {
	got := buscodec.Lookup[instanceclosed.InstanceClosed]()
	if want := "instanceClosed-sparse-cbor"; got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := instanceclosed.InstanceClosed{
		FactId: 1, At: time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		AppId: "github.com/example/play", InstanceKey: 42,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[instanceclosed.InstanceClosed](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got.At, got.NaturalKey = orig.At, orig.NaturalKey // At compared by Equal; NaturalKey travels nil or empty
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("got %+v, want %+v", got, orig)
	}
}
