package taskcancel_test

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
)

func sampleCancel() taskcancel.TaskCancel {
	return taskcancel.TaskCancel{
		FactId: 9,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		TaskId: "task-abc123",
		Reason: "user clicked cancel",
	}
}

func TestBuscodecAutoRegistersTaskCancel(t *testing.T) {
	got := buscodec.Lookup[taskcancel.TaskCancel]()
	want := "taskCancel-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleCancel()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskcancel.TaskCancel](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.FactId != orig.FactId {
		t.Errorf("FactId: got %v, want %v", got.FactId, orig.FactId)
	}
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v", got.At, orig.At)
	}
	if got.TaskId != orig.TaskId {
		t.Errorf("TaskId: got %q, want %q", got.TaskId, orig.TaskId)
	}
	if got.Reason != orig.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, orig.Reason)
	}
}

func TestBuscodecRoundTripEmptyReason(t *testing.T) {
	// Cancel-with-no-reason is the common case (programmatic
	// deadline cancels, supervisor cleanup). Reason="" must
	// reconstruct as the literal empty string.
	orig := taskcancel.TaskCancel{
		FactId: 1,
		At:     time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		TaskId: "task-no-reason",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskcancel.TaskCancel](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Reason != "" {
		t.Errorf("Reason: got %q, want empty", got.Reason)
	}
}

func TestBuscodecAtNsLosslessNanoPrecision(t *testing.T) {
	// z64 wire → sub-second nanos round-trip losslessly.
	orig := taskcancel.TaskCancel{
		FactId: 1,
		At:     time.Unix(0, 1_700_000_000_555_555_555).UTC(),
		TaskId: "task-precision",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskcancel.TaskCancel](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v (nanos must round-trip lossless)", got.At, orig.At)
	}
}
