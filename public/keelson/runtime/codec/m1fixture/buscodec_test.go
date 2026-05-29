//go:build llm_generated_opus47

package m1fixture_test

import (
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/m1fixture"
)

func sampleForBuscodec() m1fixture.M1Sample {
	return m1fixture.M1Sample{
		Id:           0x0011223344556677,
		Ts:           time.Unix(1700000000, 0).UTC(),
		Source:       "m1-bus",
		Severity:     7,
		MajorVer:     42,
		Sequence:     0xCAFEBABE,
		LatencyNanos: 1_234_567_890_123,
		CpuPct:       3.14,
		LoadAvg1:     2.71828,
		Healthy:      true,
		PeerV4:       [4]byte{10, 0, 0, 42},
		PeerV6:       [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01},
		LastSuccess:  option.Some(time.Unix(1699999000, 0).UTC()),
		OperatorName: option.Some("alice"),
		Tags:         []string{"a", "b"},
		CapBits:      roaring.BitmapOf(1, 2, 3),
	}
}

// TestBuscodecAutoRegistersSparseCBOR confirms the codegen-emitted init()
// installs the kind's CodecI with buscodec — Lookup[M1Sample] returns
// the sparse-CBOR codec, not the fxamacker-cbor fallback.
func TestBuscodecAutoRegistersSparseCBOR(t *testing.T) {
	got := buscodec.Lookup[m1fixture.M1Sample]()
	if got.Name() != "m1Sample-sparse-cbor" {
		t.Fatalf("Lookup[M1Sample].Name() = %q, want %q", got.Name(), "m1Sample-sparse-cbor")
	}
}

// TestBuscodecRoundTrip validates buscodec.Encode[M1Sample] +
// Decode[M1Sample] round-trip through the auto-registered
// sparse-CBOR codec ("m1Sample-sparse-cbor"; ADR-0042 Phase B).
func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleForBuscodec()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(wire) == 0 {
		t.Fatalf("Encode produced empty wire")
	}

	got, err := buscodec.Decode[m1fixture.M1Sample](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Id != orig.Id {
		t.Errorf("Id: got %v, want %v", got.Id, orig.Id)
	}
	if !got.Ts.Equal(orig.Ts) {
		t.Errorf("Ts: got %v, want %v", got.Ts, orig.Ts)
	}
	if got.Source != orig.Source {
		t.Errorf("Source: got %q, want %q", got.Source, orig.Source)
	}
	if got.Severity != orig.Severity {
		t.Errorf("Severity: got %d, want %d", got.Severity, orig.Severity)
	}
	if got.Sequence != orig.Sequence {
		t.Errorf("Sequence: got %d, want %d", got.Sequence, orig.Sequence)
	}
	if got.CpuPct != orig.CpuPct {
		t.Errorf("CpuPct: got %v, want %v", got.CpuPct, orig.CpuPct)
	}
	if got.LoadAvg1 != orig.LoadAvg1 {
		t.Errorf("LoadAvg1: got %v, want %v", got.LoadAvg1, orig.LoadAvg1)
	}
	if got.Healthy != orig.Healthy {
		t.Errorf("Healthy: got %v, want %v", got.Healthy, orig.Healthy)
	}
	if got.PeerV4 != orig.PeerV4 {
		t.Errorf("PeerV4: got %x, want %x", got.PeerV4, orig.PeerV4)
	}
	if !got.LastSuccess.Has || !got.LastSuccess.Val.Equal(orig.LastSuccess.Val) {
		t.Errorf("LastSuccess: got %+v, want %+v", got.LastSuccess, orig.LastSuccess)
	}
	if !got.OperatorName.Has || got.OperatorName.Val != orig.OperatorName.Val {
		t.Errorf("OperatorName: got %+v, want %+v", got.OperatorName, orig.OperatorName)
	}
	if len(got.Tags) != len(orig.Tags) {
		t.Errorf("Tags len: got %d, want %d", len(got.Tags), len(orig.Tags))
	}
	if got.CapBits == nil || !got.CapBits.Equals(orig.CapBits) {
		t.Errorf("CapBits: got %v, want %v", got.CapBits, orig.CapBits)
	}
}

// TestBuscodecEncodePointerAccepted confirms the codec accepts a
// *M1Sample as well as M1Sample.
func TestBuscodecEncodePointerAccepted(t *testing.T) {
	orig := sampleForBuscodec()
	codec := buscodec.Lookup[m1fixture.M1Sample]()
	wire, err := codec.Encode(&orig)
	if err != nil {
		t.Fatalf("Encode(*M1Sample): %v", err)
	}
	if len(wire) == 0 {
		t.Fatalf("Encode produced empty wire")
	}
}

// TestBuscodecEncodeWrongTypeIsError exercises the type-assertion
// guard.
func TestBuscodecEncodeWrongTypeIsError(t *testing.T) {
	codec := buscodec.Lookup[m1fixture.M1Sample]()
	_, err := codec.Encode("not an M1Sample")
	if err == nil {
		t.Fatalf("Encode(string) should fail")
	}
}
