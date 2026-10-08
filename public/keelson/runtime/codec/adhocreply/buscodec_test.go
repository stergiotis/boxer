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
	if !reflect.DeepEqual(normalized(got, orig), orig) {
		t.Errorf("got %+v, want %+v", got, orig)
	}
}

func TestBuscodecRoundTripBundleResolve(t *testing.T) {
	orig := adhocreply.AdhocReply{
		FactId: 2, At: time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Ok: true, Bundle: "sales", Revision: 3, Document: []byte("---\ntabs: [table]\n---\n"),
		LocalNames: []string{"orders", "regions"}, Handles: []string{"adhoc_0123456789abcdef", "adhoc_fedcba9876543210"},
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[adhocreply.AdhocReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(normalized(got, orig), orig) {
		t.Errorf("got %+v, want %+v", got, orig)
	}
}

// normalized copies onto got what the wire does not distinguish: At is
// compared by Equal, and a nil slice travels as an empty one.
func normalized(got adhocreply.AdhocReply, orig adhocreply.AdhocReply) adhocreply.AdhocReply {
	if got.At.Equal(orig.At) {
		got.At = orig.At
	}
	if len(got.NaturalKey) == 0 {
		got.NaturalKey = orig.NaturalKey
	}
	if len(got.Document) == 0 {
		got.Document = orig.Document
	}
	if len(got.LocalNames) == 0 {
		got.LocalNames = orig.LocalNames
	}
	if len(got.Handles) == 0 {
		got.Handles = orig.Handles
	}
	if len(got.ArrowStream) == 0 {
		got.ArrowStream = orig.ArrowStream
	}
	if len(got.ColumnSummaries) == 0 {
		got.ColumnSummaries = orig.ColumnSummaries
	}
	return got
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
