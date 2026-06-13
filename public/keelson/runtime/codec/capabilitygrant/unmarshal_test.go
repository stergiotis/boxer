package capabilitygrant

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/cborarrow"
)

// TestUnmarshal_RoundTrip exercises Marshal → cborarrow.Convert →
// ipc.Reader → Unmarshal. CBOR is self-delimiting so Convert needs
// no external column list.
func TestUnmarshal_RoundTrip(t *testing.T) {
	orig := sampleGrant()
	cols := &CapabilityGrantColumns{}
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

	got := &CapabilityGrantColumns{}
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
	if !bytes.Equal(got.NaturalKey[0], orig.NaturalKey) {
		t.Errorf("NaturalKey: got %x, want %x", got.NaturalKey[0], orig.NaturalKey)
	}
	if !got.Ts[0].Equal(orig.Ts) {
		t.Errorf("Ts: got %v, want %v", got.Ts[0], orig.Ts)
	}
	if !got.ExpiresAt[0].Equal(orig.ExpiresAt) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt[0], orig.ExpiresAt)
	}

	// Tagged scalars.
	if got.Subject[0] != orig.Subject {
		t.Errorf("Subject: got %q, want %q", got.Subject[0], orig.Subject)
	}
	if got.Capability[0] != orig.Capability {
		t.Errorf("Capability: got %q, want %q", got.Capability[0], orig.Capability)
	}

	// Multi-sub-column ExactlyOne (u32Range).
	if got.ValidityBegin[0] != orig.ValidityBegin {
		t.Errorf("ValidityBegin: got %v, want %v", got.ValidityBegin[0], orig.ValidityBegin)
	}
	if got.ValidityEnd[0] != orig.ValidityEnd {
		t.Errorf("ValidityEnd: got %v, want %v", got.ValidityEnd[0], orig.ValidityEnd)
	}

	// bool.
	if got.Active[0] != orig.Active {
		t.Errorf("Active: got %v, want %v", got.Active[0], orig.Active)
	}

	// Option[uint64] present.
	if !got.GranterFactHas[0] {
		t.Errorf("GranterFact.Has: got false, want true")
	} else if got.GranterFactVal[0] != orig.GranterFact.Val {
		t.Errorf("GranterFact.Val: got %v, want %v", got.GranterFactVal[0], orig.GranterFact.Val)
	}
}

// TestUnmarshal_RoundTrip_GranterAbsent covers the Option[T]=None
// path.
func TestUnmarshal_RoundTrip_GranterAbsent(t *testing.T) {
	orig := sampleGrant()
	orig.GranterFact = option.None[uint64]()

	cols := &CapabilityGrantColumns{}
	cols.Append(orig)
	var wire bytes.Buffer
	if err := cols.Marshal(&wire); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var arrowBuf bytes.Buffer
	if err := cborarrow.Convert(&wire, &arrowBuf); err != nil {
		t.Fatalf("cborarrow.Convert: %v", err)
	}

	rd, _ := ipc.NewReader(&arrowBuf)
	defer rd.Release()
	rd.Next()
	rec := rd.RecordBatch()
	defer rec.Release()

	got := &CapabilityGrantColumns{}
	if err := got.Unmarshal(rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.GranterFactHas[0] {
		t.Errorf("GranterFact.Has: got true, want false")
	}
}
