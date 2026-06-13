package watchreply_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchreply"
)

func TestBuscodecAutoRegistersWatchReply(t *testing.T) {
	got := buscodec.Lookup[watchreply.WatchReply]()
	want := "watchReply-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTripStarted(t *testing.T) {
	orig := watchreply.WatchReply{
		FactId:       1,
		At:           time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Started:      true,
		EventSubject: "fs.handle.deadbeef.event",
		Backend:      "inotify",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[watchreply.WatchReply](wire)
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

func TestBuscodecRoundTripFailed(t *testing.T) {
	orig := watchreply.WatchReply{
		FactId:  2,
		At:      time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Started: false,
		Reason:  "watch already active",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[watchreply.WatchReply](wire)
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
