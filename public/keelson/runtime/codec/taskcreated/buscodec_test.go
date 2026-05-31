//go:build llm_generated_opus47

package taskcreated_test

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
)

func sampleCreated() taskcreated.TaskCreated {
	return taskcreated.TaskCreated{
		FactId:       7,
		At:           time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		TaskId:       "task-abc123",
		Kind:         "ch.export",
		Title:        "Export rows",
		OwnerAppId:   "test.app",
		OwnerTileKey: 42,
		OwnerRunId:   "run-7e3f",
		CancellableB: true,
		EstimatedMs:  30_000,
	}
}

func TestBuscodecAutoRegistersTaskCreated(t *testing.T) {
	got := buscodec.Lookup[taskcreated.TaskCreated]()
	want := "taskCreated-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleCreated()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskcreated.TaskCreated](wire)
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
	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, orig.Kind)
	}
	if got.Title != orig.Title {
		t.Errorf("Title: got %q, want %q", got.Title, orig.Title)
	}
	if got.OwnerAppId != orig.OwnerAppId {
		t.Errorf("OwnerAppId: got %q, want %q", got.OwnerAppId, orig.OwnerAppId)
	}
	if got.OwnerTileKey != orig.OwnerTileKey {
		t.Errorf("OwnerTileKey: got %v, want %v", got.OwnerTileKey, orig.OwnerTileKey)
	}
	if got.OwnerRunId != orig.OwnerRunId {
		t.Errorf("OwnerRunId: got %q, want %q", got.OwnerRunId, orig.OwnerRunId)
	}
	if got.CancellableB != orig.CancellableB {
		t.Errorf("CancellableB: got %v, want %v", got.CancellableB, orig.CancellableB)
	}
	if got.EstimatedMs != orig.EstimatedMs {
		t.Errorf("EstimatedMs: got %v, want %v", got.EstimatedMs, orig.EstimatedMs)
	}
}

func TestBuscodecRoundTripZeroValues(t *testing.T) {
	// Direct task.Spawn callers that bypass MountContextI.Tasks may
	// leave OwnerAppId / OwnerTileKey / OwnerRunId / Title /
	// EstimatedMs at zero/empty. The wire must reconstruct them as
	// literal zero — the supervisor + observers treat that as
	// best-effort metadata.
	orig := taskcreated.TaskCreated{
		FactId:       1,
		At:           time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		TaskId:       "task-bare",
		Kind:         "ch.import",
		CancellableB: false,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskcreated.TaskCreated](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Title != "" || got.OwnerAppId != "" || got.OwnerRunId != "" {
		t.Errorf("expected empty Title/OwnerAppId/OwnerRunId, got %+v", got)
	}
	if got.OwnerTileKey != 0 || got.EstimatedMs != 0 {
		t.Errorf("expected zero TileKey/EstimatedMs, got %+v", got)
	}
	if got.CancellableB {
		t.Errorf("CancellableB: expected false, got true")
	}
}

func TestBuscodecAtNsLosslessNanoPrecision(t *testing.T) {
	// z64 wire → sub-second nanos round-trip losslessly.
	orig := taskcreated.TaskCreated{
		FactId: 1,
		At:     time.Unix(0, 1_700_000_000_987_654_321).UTC(),
		TaskId: "task-precision",
		Kind:   "ch.export",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskcreated.TaskCreated](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.At.Equal(orig.At) {
		t.Errorf("At: got %v, want %v (nanos must round-trip lossless)", got.At, orig.At)
	}
}
