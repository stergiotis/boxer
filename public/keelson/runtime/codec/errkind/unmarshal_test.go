//go:build llm_generated_opus47

package errkind

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/cborarrow"
)

// TestUnmarshal_RoundTrip exercises Marshal → cborarrow.Convert →
// ipc.Reader → Unmarshal on a 4-fact pre-shredded Error. The wire is
// multi-Arbitrary across four sections (string + symbol + u64 each
// carry 2 kinds; u32 and blob carry 1 kind), plus the [][]byte shape
// for blob's Data field.
func TestUnmarshal_RoundTrip(t *testing.T) {
	orig := sampleError()
	cols := &ErrorColumns{}
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

	got := &ErrorColumns{}
	if err := got.Unmarshal(rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Len() != 1 {
		t.Fatalf("decoded Len = %d, want 1", got.Len())
	}

	if got.Id[0] != orig.Id {
		t.Errorf("Id: got %v, want %v", got.Id[0], orig.Id)
	}
	if !bytes.Equal(got.NaturalKey[0], orig.NaturalKey) {
		t.Errorf("NaturalKey: got %x, want %x", got.NaturalKey[0], orig.NaturalKey)
	}
	if !got.CapturedTs[0].Equal(orig.CapturedTs) {
		t.Errorf("CapturedTs: got %v, want %v", got.CapturedTs[0], orig.CapturedTs)
	}

	if !reflect.DeepEqual(got.Messages[0], orig.Messages) {
		t.Errorf("Messages: got %v, want %v", got.Messages[0], orig.Messages)
	}
	if !reflect.DeepEqual(got.Sources[0], orig.Sources) {
		t.Errorf("Sources: got %v, want %v", got.Sources[0], orig.Sources)
	}
	if !reflect.DeepEqual(got.Funcs[0], orig.Funcs) {
		t.Errorf("Funcs: got %v, want %v", got.Funcs[0], orig.Funcs)
	}
	if !reflect.DeepEqual(got.StreamNames[0], orig.StreamNames) {
		t.Errorf("StreamNames: got %v, want %v", got.StreamNames[0], orig.StreamNames)
	}
	if !reflect.DeepEqual(got.Lines[0], orig.Lines) {
		t.Errorf("Lines: got %v, want %v", got.Lines[0], orig.Lines)
	}
	if !reflect.DeepEqual(got.FactIds[0], orig.FactIds) {
		t.Errorf("FactIds: got %v, want %v", got.FactIds[0], orig.FactIds)
	}
	if !reflect.DeepEqual(got.ParentIds[0], orig.ParentIds) {
		t.Errorf("ParentIds: got %v, want %v", got.ParentIds[0], orig.ParentIds)
	}
	// [][]byte: deep-equal works for the byte-slice contents.
	if len(got.Data[0]) != len(orig.Data) {
		t.Errorf("Data: got %d entries, want %d", len(got.Data[0]), len(orig.Data))
	} else {
		for i := range orig.Data {
			if !bytes.Equal(got.Data[0][i], orig.Data[i]) {
				t.Errorf("Data[%d]: got %x, want %x", i, got.Data[0][i], orig.Data[i])
			}
		}
	}
}
