//go:build llm_generated_opus47

package taskprogress_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
)

func sampleProgress() taskprogress.TaskProgress {
	return taskprogress.TaskProgress{
		FactId:           42,
		AtNs:             1_700_000_000_000_000_000,
		TaskId:           "task-abc123",
		Current:          2048,
		Total:            10240,
		Unit:             "bytes",
		ThroughputPerSec: 512.5,
		EtaMs:            16000,
		Note:             "uploading slice",
	}
}

func TestBuscodecAutoRegistersTaskProgress(t *testing.T) {
	got := buscodec.Lookup[taskprogress.TaskProgress]()
	want := "taskProgress-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleProgress()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskprogress.TaskProgress](wire)
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
	if got.Current != orig.Current {
		t.Errorf("Current: got %v, want %v", got.Current, orig.Current)
	}
	if got.Total != orig.Total {
		t.Errorf("Total: got %v, want %v", got.Total, orig.Total)
	}
	if got.Unit != orig.Unit {
		t.Errorf("Unit: got %q, want %q", got.Unit, orig.Unit)
	}
	if got.ThroughputPerSec != orig.ThroughputPerSec {
		t.Errorf("ThroughputPerSec: got %v, want %v", got.ThroughputPerSec, orig.ThroughputPerSec)
	}
	if got.EtaMs != orig.EtaMs {
		t.Errorf("EtaMs: got %v, want %v", got.EtaMs, orig.EtaMs)
	}
	if got.Note != orig.Note {
		t.Errorf("Note: got %q, want %q", got.Note, orig.Note)
	}
}

func TestBuscodecAtNsLosslessNanoPrecision(t *testing.T) {
	// The codec's plain `ts` column rides DateTime64(9) on the wire
	// (z64 canonical type). Sub-second nanos round-trip losslessly;
	// this pins that property so a future regression to seconds-only
	// precision is caught locally.
	orig := taskprogress.TaskProgress{
		FactId: 1,
		AtNs:   1_700_000_000_123_456_789, // sub-second nanos present
		TaskId: "task-precision",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskprogress.TaskProgress](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.AtNs != orig.AtNs {
		t.Errorf("AtNs: got %v, want %v (nanos must round-trip lossless)", got.AtNs, orig.AtNs)
	}
}

func TestBuscodecRoundTripZeroValues(t *testing.T) {
	// Zero-value semantics: Total=0 ⇒ indeterminate; EtaMs=0 ⇒
	// not-yet-computed; ThroughputPerSec=0.0 ⇒ first report;
	// Note="" ⇒ no annotation. All round-trip as the literal zero.
	orig := taskprogress.TaskProgress{
		FactId:  1,
		AtNs:    1_700_000_000_000_000_000,
		TaskId:  "task-indet",
		Current: 100,
		Unit:    "items",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskprogress.TaskProgress](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Total != 0 {
		t.Errorf("Total: got %v, want 0 (indeterminate)", got.Total)
	}
	if got.EtaMs != 0 {
		t.Errorf("EtaMs: got %v, want 0", got.EtaMs)
	}
	if got.ThroughputPerSec != 0.0 {
		t.Errorf("ThroughputPerSec: got %v, want 0.0", got.ThroughputPerSec)
	}
	if got.Note != "" {
		t.Errorf("Note: got %q, want empty", got.Note)
	}
}
