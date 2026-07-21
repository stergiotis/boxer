package kindcheck_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
)

func encodeRequest(t *testing.T) (b []byte) {
	t.Helper()
	b, err := buscodec.Encode(launchrequest.LaunchRequest{
		FactId:      7,
		At:          time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		TargetAppId: "test.app",
		ConfigKind:  "playLaunch",
		Config:      []byte{0xde, 0xad},
	})
	if err != nil {
		t.Fatalf("Encode LaunchRequest: %v", err)
	}
	return
}

func encodeReply(t *testing.T) (b []byte) {
	t.Helper()
	b, err := buscodec.Encode(launchreply.LaunchReply{
		FactId:    1,
		At:        time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		WindowKey: 3,
	})
	if err != nil {
		t.Fatalf("Encode LaunchReply: %v", err)
	}
	return
}

func TestPeekKindIdentifiesBothLaunchKinds(t *testing.T) {
	kind, err := kindcheck.PeekKind(encodeRequest(t))
	if err != nil {
		t.Fatalf("PeekKind(request): %v", err)
	}
	if kind != "launchRequest" {
		t.Fatalf("PeekKind(request) = %q, want launchRequest", kind)
	}
	kind, err = kindcheck.PeekKind(encodeReply(t))
	if err != nil {
		t.Fatalf("PeekKind(reply): %v", err)
	}
	if kind != "launchReply" {
		t.Fatalf("PeekKind(reply) = %q, want launchReply", kind)
	}
}

func TestCheckAcceptsMatchingKind(t *testing.T) {
	if err := kindcheck.Check("launchRequest", encodeRequest(t)); err != nil {
		t.Fatalf("Check(launchRequest, request bytes): %v", err)
	}
}

func TestCheckRefusesMismatchedKind(t *testing.T) {
	err := kindcheck.Check("launchReply", encodeRequest(t))
	if err == nil {
		t.Fatal("Check(launchReply, request bytes) accepted a mismatched payload")
	}
}

func TestCheckRefusesUnregisteredKind(t *testing.T) {
	err := kindcheck.Check("noSuchKind", encodeRequest(t))
	if err == nil {
		t.Fatal("Check(noSuchKind, ...) accepted an unregistered kind")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unexpected error shape: %v", err)
	}
}

func TestPeekKindRefusesGarbage(t *testing.T) {
	if _, err := kindcheck.PeekKind([]byte("not cbor at all")); err == nil {
		t.Fatal("PeekKind(garbage) succeeded")
	}
	if _, err := kindcheck.PeekKind(nil); err == nil {
		t.Fatal("PeekKind(nil) succeeded")
	}
}

func TestPeekKindRefusesTruncated(t *testing.T) {
	b := encodeRequest(t)
	if _, err := kindcheck.PeekKind(b[:len(b)/2]); err == nil {
		t.Fatal("PeekKind(truncated) succeeded")
	}
	if err := kindcheck.Check("launchRequest", b[:len(b)/2]); err == nil {
		t.Fatal("Check(truncated) succeeded")
	}
}
