//go:build llm_generated_opus47

package inflightsnapshotreply

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/cborarrow"
)

// TestUnmarshal_RoundTrip_Batch confirms the parallel-array list
// pattern survives multi-row batches through the Marshal →
// cborarrow.Convert → ipc.Reader → Unmarshal pipeline.
//
// Two failure modes this gates against:
//
//   - Multi-row entityIdx stepping on the ra accel. Each wrapper row
//     carries its own parallel arrays; the read side must walk
//     entityIdx 0..N-1 correctly to keep the per-row Entries
//     reconstructions independent. This is the same surface as
//     m1fixture's TestUnmarshal_RoundTrip_Batch but on a parallel-
//     array shape rather than a mixed-membership shape.
//   - Cross-membership separation inside a single row's section
//     when multiple memberships share storage (stringArray for Ids
//   - OwnerAppIds; symbol for Kinds + States + Units; i64Array
//     for CreatedAtMss + LastEmitMss + EtaMss; u64Array for
//     Currents + Totals). The classifier must split the parallel
//     streams by membership-id; a regression that mis-attributes
//     would corrupt the per-entry zip.
func TestUnmarshal_RoundTrip_Batch(t *testing.T) {
	rows := []InflightSnapshotReply{
		{
			FactId:       1,
			At:           time.Unix(0, 1_700_000_000_000_000_000).UTC(),
			Ids:          []string{"task-a", "task-b"},
			Kinds:        []string{"ch.export", "ch.import"},
			Titles:       []string{"Export A", "Import B"},
			OwnerAppIds:  []string{"app.x", "app.y"},
			States:       []string{"running", "cancelling"},
			CreatedAtMss: []int64{1_700_000_000_000, 1_700_000_001_000},
			LastEmitMss:  []int64{1_700_000_005_000, 1_700_000_006_000},
			Currents:     []uint64{10, 50},
			Totals:       []uint64{100, 200},
			Units:        []string{"items", "bytes"},
			EtaMss:       []int64{500, 1500},
		},
		{
			FactId:       2,
			At:           time.Unix(0, 1_700_000_010_000_000_000).UTC(),
			Ids:          []string{"task-c", "task-d", "task-e"},
			Kinds:        []string{"ch.export", "ch.import", "ch.export"},
			Titles:       []string{"Export C", "Import D", "Export E"},
			OwnerAppIds:  []string{"app.z", "app.x", "app.y"},
			States:       []string{"running", "running", "abandoned"},
			CreatedAtMss: []int64{1_700_000_002_000, 1_700_000_003_000, 1_700_000_004_000},
			LastEmitMss:  []int64{1_700_000_007_000, 1_700_000_008_000, 1_700_000_009_000},
			Currents:     []uint64{20, 60, 99},
			Totals:       []uint64{200, 300, 100},
			Units:        []string{"steps", "items", "bytes"},
			EtaMss:       []int64{800, 1200, 0},
		},
		{
			// Empty third row (zero entries) — exercises the boundary
			// where one wrapper row in a batch has no parallel-array
			// content even though sibling rows do.
			FactId: 3,
			At:     time.Unix(0, 1_700_000_020_000_000_000).UTC(),
		},
	}

	cols := &InflightSnapshotReplyColumns{}
	for _, r := range rows {
		cols.Append(r)
	}

	var wire bytes.Buffer
	if err := cols.Marshal(&wire); err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var arrowBuf bytes.Buffer
	if err := cborarrow.Convert(&wire, &arrowBuf); err != nil {
		t.Fatalf("cborarrow.Convert: %v", err)
	}
	rd, err := ipc.NewReader(&arrowBuf)
	if err != nil {
		t.Fatalf("ipc.NewReader: %v", err)
	}
	defer rd.Release()
	if !rd.Next() {
		t.Fatalf("expected one Arrow record, got none: %v", rd.Err())
	}
	rec := rd.RecordBatch()
	defer rec.Release()

	got := &InflightSnapshotReplyColumns{}
	if err = got.Unmarshal(rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Len() != len(rows) {
		t.Fatalf("Len: got %d, want %d", got.Len(), len(rows))
	}

	for i, want := range rows {
		if !reflect.DeepEqual(got.Ids[i], want.Ids) {
			t.Errorf("row %d Ids: got %v, want %v", i, got.Ids[i], want.Ids)
		}
		if !reflect.DeepEqual(got.Kinds[i], want.Kinds) {
			t.Errorf("row %d Kinds: got %v, want %v", i, got.Kinds[i], want.Kinds)
		}
		if !reflect.DeepEqual(got.Titles[i], want.Titles) {
			t.Errorf("row %d Titles: got %v, want %v", i, got.Titles[i], want.Titles)
		}
		if !reflect.DeepEqual(got.OwnerAppIds[i], want.OwnerAppIds) {
			t.Errorf("row %d OwnerAppIds: got %v, want %v", i, got.OwnerAppIds[i], want.OwnerAppIds)
		}
		if !reflect.DeepEqual(got.States[i], want.States) {
			t.Errorf("row %d States: got %v, want %v", i, got.States[i], want.States)
		}
		if !reflect.DeepEqual(got.CreatedAtMss[i], want.CreatedAtMss) {
			t.Errorf("row %d CreatedAtMss: got %v, want %v", i, got.CreatedAtMss[i], want.CreatedAtMss)
		}
		if !reflect.DeepEqual(got.LastEmitMss[i], want.LastEmitMss) {
			t.Errorf("row %d LastEmitMss: got %v, want %v", i, got.LastEmitMss[i], want.LastEmitMss)
		}
		if !reflect.DeepEqual(got.Currents[i], want.Currents) {
			t.Errorf("row %d Currents: got %v, want %v", i, got.Currents[i], want.Currents)
		}
		if !reflect.DeepEqual(got.Totals[i], want.Totals) {
			t.Errorf("row %d Totals: got %v, want %v", i, got.Totals[i], want.Totals)
		}
		if !reflect.DeepEqual(got.Units[i], want.Units) {
			t.Errorf("row %d Units: got %v, want %v", i, got.Units[i], want.Units)
		}
		if !reflect.DeepEqual(got.EtaMss[i], want.EtaMss) {
			t.Errorf("row %d EtaMss: got %v, want %v", i, got.EtaMss[i], want.EtaMss)
		}
	}
}
