//go:build llm_generated_opus47

package watchreply_test

import (
	"testing"

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
		AtNs:         1_700_000_000_000_000_000,
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
	if got != orig {
		t.Errorf("roundtrip: got %+v, want %+v", got, orig)
	}
}

func TestBuscodecRoundTripFailed(t *testing.T) {
	orig := watchreply.WatchReply{
		FactId:  2,
		AtNs:    1_700_000_000_000_000_000,
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
	if got != orig {
		t.Errorf("roundtrip: got %+v, want %+v", got, orig)
	}
}
