package m1fixture

import (
	"bytes"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/cborarrow"
)

// TestUnmarshal_RoundTrip drives Marshal → cborarrow.Convert →
// ipc.Reader → Unmarshal end-to-end. CBOR is self-delimiting so
// Convert needs no external column list or rowCount.
func TestUnmarshal_RoundTrip(t *testing.T) {
	orig := sampleM1Sample()
	cols := &M1SampleColumns{}
	cols.Append(orig)

	var wire bytes.Buffer
	if err := cols.Marshal(&wire); err != nil {
		t.Fatalf("marshal: %v", err)
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

	got := &M1SampleColumns{}
	if err := got.Unmarshal(rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Len() != 1 {
		t.Fatalf("decoded Len = %d, want 1", got.Len())
	}

	// Plain.
	if got.Id[0] != orig.Id {
		t.Errorf("Id: got %v, want %v", got.Id[0], orig.Id)
	}
	if !got.Ts[0].Equal(orig.Ts) {
		t.Errorf("Ts: got %v, want %v", got.Ts[0], orig.Ts)
	}

	// ExactlyOne scalars across every type section the kind populates.
	if got.Source[0] != orig.Source {
		t.Errorf("Source: got %q, want %q", got.Source[0], orig.Source)
	}
	if got.Severity[0] != orig.Severity {
		t.Errorf("Severity: got %d, want %d", got.Severity[0], orig.Severity)
	}
	if got.MajorVer[0] != orig.MajorVer {
		t.Errorf("MajorVer: got %d, want %d", got.MajorVer[0], orig.MajorVer)
	}
	if got.Sequence[0] != orig.Sequence {
		t.Errorf("Sequence: got %d, want %d", got.Sequence[0], orig.Sequence)
	}
	if got.LatencyNanos[0] != orig.LatencyNanos {
		t.Errorf("LatencyNanos: got %d, want %d", got.LatencyNanos[0], orig.LatencyNanos)
	}
	if got.CpuPct[0] != orig.CpuPct {
		t.Errorf("CpuPct: got %v, want %v", got.CpuPct[0], orig.CpuPct)
	}
	if got.LoadAvg1[0] != orig.LoadAvg1 {
		t.Errorf("LoadAvg1: got %v, want %v", got.LoadAvg1[0], orig.LoadAvg1)
	}
	if got.Healthy[0] != orig.Healthy {
		t.Errorf("Healthy: got %v, want %v", got.Healthy[0], orig.Healthy)
	}

	// Fixed-byte arrays via the blob section.
	if got.PeerV4[0] != orig.PeerV4 {
		t.Errorf("PeerV4: got %x, want %x", got.PeerV4[0], orig.PeerV4)
	}
	if got.PeerV6[0] != orig.PeerV6 {
		t.Errorf("PeerV6: got %x, want %x", got.PeerV6[0], orig.PeerV6)
	}

	// Option[time.Time], Option[string].
	if !got.LastSuccessHas[0] || !got.LastSuccessVal[0].Equal(orig.LastSuccess.Val) {
		t.Errorf("LastSuccess: got (has=%v, val=%v), want (has=true, val=%v)",
			got.LastSuccessHas[0], got.LastSuccessVal[0], orig.LastSuccess.Val)
	}
	if !got.OperatorNameHas[0] || got.OperatorNameVal[0] != orig.OperatorName.Val {
		t.Errorf("OperatorName: got (has=%v, val=%q), want (has=true, val=%q)",
			got.OperatorNameHas[0], got.OperatorNameVal[0], orig.OperatorName.Val)
	}

	// Slice of strings via the text section.
	if len(got.Tags[0]) != len(orig.Tags) {
		t.Fatalf("Tags: got %v, want %v", got.Tags[0], orig.Tags)
	}
	for i, want := range orig.Tags {
		if got.Tags[0][i] != want {
			t.Errorf("Tags[%d]: got %q, want %q", i, got.Tags[0][i], want)
		}
	}

	// Roaring bitmap.
	if got.CapBits[0] == nil {
		t.Fatalf("CapBits: got nil bitmap, want %v", orig.CapBits.ToArray())
	}
	if !got.CapBits[0].Equals(orig.CapBits) {
		t.Errorf("CapBits: got %v, want %v", got.CapBits[0].ToArray(), orig.CapBits.ToArray())
	}
}

// TestUnmarshal_RoundTrip_OptionAbsent exercises the absent /
// empty-slice / nil-roaring paths through the ra-based Unmarshal.
func TestUnmarshal_RoundTrip_OptionAbsent(t *testing.T) {
	orig := sampleM1Sample()
	orig.LastSuccess = option.None[time.Time]()
	orig.OperatorName = option.None[string]()
	orig.Tags = nil
	orig.CapBits = nil

	cols := &M1SampleColumns{}
	cols.Append(orig)
	var wire bytes.Buffer
	if err := cols.Marshal(&wire); err != nil {
		t.Fatalf("marshal: %v", err)
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
		t.Fatalf("expected one Arrow record")
	}
	rec := rd.RecordBatch()
	defer rec.Release()

	got := &M1SampleColumns{}
	if err := got.Unmarshal(rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.LastSuccessHas[0] {
		t.Errorf("LastSuccess.Has: got true, want false")
	}
	if got.OperatorNameHas[0] {
		t.Errorf("OperatorName.Has: got true, want false")
	}
	if len(got.Tags[0]) != 0 {
		t.Errorf("Tags: got %v, want empty", got.Tags[0])
	}
	if got.CapBits[0] != nil && !got.CapBits[0].IsEmpty() {
		t.Errorf("CapBits: got %v, want nil/empty", got.CapBits[0].ToArray())
	}
}

// TestUnmarshal_RoundTrip_Batch confirms multi-row batches survive the
// driver Marshal → cborarrow → Unmarshal pipeline.
func TestUnmarshal_RoundTrip_Batch(t *testing.T) {
	const n = 5
	cols := &M1SampleColumns{}
	for i := 0; i < n; i++ {
		s := sampleM1Sample()
		s.Id = uint64(i + 1)
		s.Sequence = uint32(i + 100)
		s.CapBits = roaring.BitmapOf(uint32(i + 10))
		cols.Append(s)
	}
	var wire bytes.Buffer
	if err := cols.Marshal(&wire); err != nil {
		t.Fatalf("marshal: %v", err)
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
		t.Fatalf("expected one Arrow record")
	}
	rec := rd.RecordBatch()
	defer rec.Release()

	got := &M1SampleColumns{}
	if err := got.Unmarshal(rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Len() != n {
		t.Fatalf("decoded Len = %d, want %d", got.Len(), n)
	}
	for i := 0; i < n; i++ {
		if got.Id[i] != uint64(i+1) {
			t.Errorf("row %d Id: got %d, want %d", i, got.Id[i], i+1)
		}
		if got.Sequence[i] != uint32(i+100) {
			t.Errorf("row %d Sequence: got %d, want %d", i, got.Sequence[i], i+100)
		}
		if got.CapBits[i] == nil || !got.CapBits[i].Contains(uint32(i+10)) {
			t.Errorf("row %d CapBits: missing %d (got %v)", i, i+10, got.CapBits[i])
		}
	}
}
