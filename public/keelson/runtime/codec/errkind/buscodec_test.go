package errkind_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/errkind"
)

func sampleErrorForBuscodec() errkind.Error {
	return errkind.Error{
		Id:          0xDEADBEEF,
		NaturalKey:  []byte{0x01, 0x02},
		CapturedTs:  time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Messages:    []string{"boom", "kapow"},
		Sources:     []string{"main.go:12", "main.go:34"},
		Funcs:       []string{"Run", "Init"},
		StreamNames: []string{"stream", "stream"},
		Lines:       []uint32{12, 34},
		FactIds:     []uint64{1, 2},
		ParentIds:   []uint64{0, 1},
		Data:        [][]byte{{0xAA}, {0xBB, 0xCC}},
	}
}

func TestBuscodecAutoRegistersSparseCBOR(t *testing.T) {
	got := buscodec.Lookup[errkind.Error]()
	if got.Name() != "error-sparse-cbor" {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), "error-sparse-cbor")
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleErrorForBuscodec()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[errkind.Error](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Id != orig.Id {
		t.Errorf("Id: got %v, want %v", got.Id, orig.Id)
	}
	if !bytes.Equal(got.NaturalKey, orig.NaturalKey) {
		t.Errorf("NaturalKey: got %x, want %x", got.NaturalKey, orig.NaturalKey)
	}
	if !got.CapturedTs.Equal(orig.CapturedTs) {
		t.Errorf("CapturedTs: got %v, want %v", got.CapturedTs, orig.CapturedTs)
	}
	checkStringSlice := func(name string, got, want []string) {
		if len(got) != len(want) {
			t.Errorf("%s len: got %d, want %d", name, len(got), len(want))
			return
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s[%d]: got %q, want %q", name, i, got[i], want[i])
			}
		}
	}
	checkStringSlice("Messages", got.Messages, orig.Messages)
	checkStringSlice("Sources", got.Sources, orig.Sources)
	checkStringSlice("Funcs", got.Funcs, orig.Funcs)
	checkStringSlice("StreamNames", got.StreamNames, orig.StreamNames)
	if len(got.Lines) != len(orig.Lines) {
		t.Errorf("Lines len: got %d, want %d", len(got.Lines), len(orig.Lines))
	}
	if len(got.FactIds) != len(orig.FactIds) {
		t.Errorf("FactIds len: got %d, want %d", len(got.FactIds), len(orig.FactIds))
	}
	if len(got.Data) != len(orig.Data) {
		t.Errorf("Data len: got %d, want %d", len(got.Data), len(orig.Data))
		return
	}
	for i := range orig.Data {
		if !bytes.Equal(got.Data[i], orig.Data[i]) {
			t.Errorf("Data[%d]: got %x, want %x", i, got.Data[i], orig.Data[i])
		}
	}
}
