//go:build llm_generated_opus47

package watchevent_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchevent"
)

func TestBuscodecAutoRegistersWatchEvent(t *testing.T) {
	got := buscodec.Lookup[watchevent.WatchEvent]()
	want := "watchEvent-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := watchevent.WatchEvent{
		FactId: 1,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Kind:   "create",
		Name:   "sub/file.txt",
		Cookie: 42,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[watchevent.WatchEvent](wire)
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

func TestBuscodecRoundTripKindVariants(t *testing.T) {
	// Pin every canonical WatchEventKindE rendering survives the
	// symbol section round-trip — including the empty/unspecified
	// case (zero-value sentinel).
	kinds := []string{"create", "delete", "modify", "attrib", "renameFrom", "renameTo", "overflow", "closed", ""}
	for _, k := range kinds {
		t.Run(k, func(t *testing.T) {
			orig := watchevent.WatchEvent{
				FactId: 1,
				At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
				Kind:   k,
			}
			wire, err := buscodec.Encode(orig)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := buscodec.Decode[watchevent.WatchEvent](wire)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.Kind != k {
				t.Errorf("Kind: got %q, want %q", got.Kind, k)
			}
		})
	}
}

func TestBuscodecRoundTripRootEvent(t *testing.T) {
	// Events addressing the watched root carry an empty Name; pin
	// the zero-value sentinel survives.
	orig := watchevent.WatchEvent{
		FactId: 2,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Kind:   "closed",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[watchevent.WatchEvent](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Name != "" {
		t.Errorf("Name: got %q, want empty", got.Name)
	}
	if got.Cookie != 0 {
		t.Errorf("Cookie: got %v, want 0", got.Cookie)
	}
}
