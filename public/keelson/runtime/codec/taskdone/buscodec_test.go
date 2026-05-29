//go:build llm_generated_opus47

package taskdone_test

import (
	"bytes"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
)

func sampleDone() taskdone.TaskDone {
	return taskdone.TaskDone{
		FactId: 5,
		AtNs:   1_700_000_000_000_000_000,
		TaskId: "task-abc123",
		Result: []byte{0x01, 0x02, 0xff, 0x00, 0xfe},
	}
}

func TestBuscodecAutoRegistersTaskDone(t *testing.T) {
	got := buscodec.Lookup[taskdone.TaskDone]()
	want := "taskDone-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleDone()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskdone.TaskDone](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.FactId != orig.FactId {
		t.Errorf("FactId: got %v, want %v", got.FactId, orig.FactId)
	}
	if got.AtNs != orig.AtNs {
		t.Errorf("AtNs: got %v, want %v", got.AtNs, orig.AtNs)
	}
	if got.TaskId != orig.TaskId {
		t.Errorf("TaskId: got %q, want %q", got.TaskId, orig.TaskId)
	}
	if !bytes.Equal(got.Result, orig.Result) {
		t.Errorf("Result: got %x, want %x", got.Result, orig.Result)
	}
}

func TestBuscodecRoundTripEmptyResult(t *testing.T) {
	// Tasks may signal success without a payload (most common case).
	// Empty Result must reconstruct as an empty slice on read; the
	// nil vs zero-length distinction is not preserved across the
	// codec — observers should always presence-check via len.
	orig := taskdone.TaskDone{
		FactId: 1,
		AtNs:   1_700_000_000_000_000_000,
		TaskId: "task-no-result",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskdone.TaskDone](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.Result) != 0 {
		t.Errorf("Result: got %x, want empty", got.Result)
	}
}

func TestBuscodecRoundTripBinaryPayload(t *testing.T) {
	// Result is opaque application binary. Pin that the blob section
	// preserves arbitrary bytes including embedded NULs and the
	// full 0..255 byte range — i.e. the scalar-blob grammar
	// extension is wire-faithful, not text-coerced.
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}
	orig := taskdone.TaskDone{
		FactId: 2,
		AtNs:   1_700_000_000_000_000_000,
		TaskId: "task-binary",
		Result: payload,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskdone.TaskDone](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got.Result, payload) {
		t.Errorf("Result: got %x, want %x", got.Result, payload)
	}
	if len(got.Result) != 256 {
		t.Errorf("Result length: got %d, want 256", len(got.Result))
	}
}

func TestBuscodecRoundTripDefensiveCopy(t *testing.T) {
	// The codegen path inserts an explicit `make + copy` on read so
	// the decoded Result detaches from the shared Arrow buffer. Pin
	// the contract by mutating the decoded slice and verifying it
	// doesn't reach back into wire-internal state via a second
	// round-trip — i.e. the second decode is independent of the first.
	orig := taskdone.TaskDone{
		FactId: 1,
		AtNs:   1_700_000_000_000_000_000,
		TaskId: "task-aliased",
		Result: []byte{0x11, 0x22, 0x33},
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	first, err := buscodec.Decode[taskdone.TaskDone](wire)
	if err != nil {
		t.Fatalf("Decode first: %v", err)
	}
	first.Result[0] = 0xff
	second, err := buscodec.Decode[taskdone.TaskDone](wire)
	if err != nil {
		t.Fatalf("Decode second: %v", err)
	}
	if second.Result[0] != 0x11 {
		t.Errorf("Result[0]: got %x, want 0x11 (defensive copy contract violated)", second.Result[0])
	}
}

func TestBuscodecAtNsLosslessNanoPrecision(t *testing.T) {
	// z64 wire → sub-second nanos round-trip losslessly.
	orig := taskdone.TaskDone{
		FactId: 1,
		AtNs:   1_700_000_000_777_888_999,
		TaskId: "task-precision",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskdone.TaskDone](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.AtNs != orig.AtNs {
		t.Errorf("AtNs: got %v, want %v (nanos must round-trip lossless)", got.AtNs, orig.AtNs)
	}
}
