//go:build llm_generated_opus47

package inflightsnapshotreply_test

import (
	"reflect"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/inflightsnapshotreply"
)

func sampleReply() inflightsnapshotreply.InflightSnapshotReply {
	return inflightsnapshotreply.InflightSnapshotReply{
		FactId:       1,
		AtNs:         1_700_000_000_000_000_000,
		Ids:          []string{"task-a", "task-b", "task-c"},
		Kinds:        []string{"ch.export", "ch.import", "ch.export"},
		Titles:       []string{"Export A", "Import B", "Export C"},
		OwnerAppIds:  []string{"app.x", "app.y", "app.x"},
		States:       []string{"running", "cancelling", "abandoned"},
		CreatedAtMss: []int64{1_700_000_000_000, 1_700_000_001_000, 1_700_000_002_000},
		LastEmitMss:  []int64{1_700_000_005_000, 1_700_000_006_000, 1_700_000_007_000},
		Currents:     []uint64{10, 50, 90},
		Totals:       []uint64{100, 200, 300},
		Units:        []string{"items", "bytes", "steps"},
		EtaMss:       []int64{500, 1500, 200},
	}
}

func TestBuscodecAutoRegistersInflightSnapshotReply(t *testing.T) {
	got := buscodec.Lookup[inflightsnapshotreply.InflightSnapshotReply]()
	want := "inflightSnapshotReply-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleReply()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[inflightsnapshotreply.InflightSnapshotReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Parallel-array order preservation: each entry-field column must
	// reconstruct in the same slice-index order so the entries zip
	// correctly. Pin every column independently — a regression that
	// reorders Ids relative to (say) States would corrupt the
	// supervisor's inflight snapshot.
	if !reflect.DeepEqual(got.Ids, orig.Ids) {
		t.Errorf("Ids: got %v, want %v", got.Ids, orig.Ids)
	}
	if !reflect.DeepEqual(got.Kinds, orig.Kinds) {
		t.Errorf("Kinds: got %v, want %v", got.Kinds, orig.Kinds)
	}
	if !reflect.DeepEqual(got.Titles, orig.Titles) {
		t.Errorf("Titles: got %v, want %v", got.Titles, orig.Titles)
	}
	if !reflect.DeepEqual(got.OwnerAppIds, orig.OwnerAppIds) {
		t.Errorf("OwnerAppIds: got %v, want %v", got.OwnerAppIds, orig.OwnerAppIds)
	}
	if !reflect.DeepEqual(got.States, orig.States) {
		t.Errorf("States: got %v, want %v", got.States, orig.States)
	}
	if !reflect.DeepEqual(got.CreatedAtMss, orig.CreatedAtMss) {
		t.Errorf("CreatedAtMss: got %v, want %v", got.CreatedAtMss, orig.CreatedAtMss)
	}
	if !reflect.DeepEqual(got.LastEmitMss, orig.LastEmitMss) {
		t.Errorf("LastEmitMss: got %v, want %v", got.LastEmitMss, orig.LastEmitMss)
	}
	if !reflect.DeepEqual(got.Currents, orig.Currents) {
		t.Errorf("Currents: got %v, want %v", got.Currents, orig.Currents)
	}
	if !reflect.DeepEqual(got.Totals, orig.Totals) {
		t.Errorf("Totals: got %v, want %v", got.Totals, orig.Totals)
	}
	if !reflect.DeepEqual(got.Units, orig.Units) {
		t.Errorf("Units: got %v, want %v", got.Units, orig.Units)
	}
	if !reflect.DeepEqual(got.EtaMss, orig.EtaMss) {
		t.Errorf("EtaMss: got %v, want %v", got.EtaMss, orig.EtaMss)
	}
}

func TestBuscodecRoundTripEmptySnapshot(t *testing.T) {
	// Empty snapshot — supervisor with no in-flight tasks. Every
	// parallel column is len-0; the wire must encode the empty
	// state and decode back as zero entries.
	orig := inflightsnapshotreply.InflightSnapshotReply{
		FactId: 1,
		AtNs:   1_700_000_000_000_000_000,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[inflightsnapshotreply.InflightSnapshotReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got.Ids) != 0 || len(got.Kinds) != 0 || len(got.States) != 0 {
		t.Errorf("expected empty parallel columns, got Ids=%v Kinds=%v States=%v", got.Ids, got.Kinds, got.States)
	}
}

func TestBuscodecRoundTripSingleEntry(t *testing.T) {
	// Boundary case: exactly one in-flight task. Pins that the
	// parallel-array pattern doesn't require N≥2 to round-trip.
	orig := inflightsnapshotreply.InflightSnapshotReply{
		FactId:       1,
		AtNs:         1_700_000_000_000_000_000,
		Ids:          []string{"task-solo"},
		Kinds:        []string{"ch.export"},
		Titles:       []string{"Solo"},
		OwnerAppIds:  []string{"app.x"},
		States:       []string{"running"},
		CreatedAtMss: []int64{1_700_000_000_000},
		LastEmitMss:  []int64{1_700_000_005_000},
		Currents:     []uint64{42},
		Totals:       []uint64{100},
		Units:        []string{"items"},
		EtaMss:       []int64{500},
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[inflightsnapshotreply.InflightSnapshotReply](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(got.Ids, orig.Ids) {
		t.Errorf("Ids: got %v, want %v", got.Ids, orig.Ids)
	}
	if got.Currents[0] != 42 {
		t.Errorf("Currents[0]: got %d, want 42", got.Currents[0])
	}
}
