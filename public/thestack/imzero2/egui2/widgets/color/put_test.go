package color

import (
	"bytes"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
)

// captureBytes runs fn against a fresh RetainedFffiBuilder and returns the
// produced opcode bytes. Used by the wire-identity baseline tests to pin
// encoding before the generator is modified.
func captureBytes(t *testing.T, fn func(r *typed.RetainedFffiBuilder)) []byte {
	t.Helper()
	r := typed.NewRetainedFffiBuilder()
	fn(r)
	h := r.BuildRetained()
	out := make([]byte, len(h.Content()))
	copy(out, h.Content())
	return out
}

func TestPutAsU32_LiteralWritesFourBytesLE(t *testing.T) {
	got := captureBytes(t, func(r *typed.RetainedFffiBuilder) {
		PutAsU32(r, Hex(0xdeadbeef))
	})
	want := []byte{0xef, 0xbe, 0xad, 0xde}
	if !bytes.Equal(got, want) {
		t.Fatalf("PutAsU32 literal: got %x want %x", got, want)
	}
}

func TestPutAsU32_RetainedFlattensToLiteral(t *testing.T) {
	lit := captureBytes(t, func(r *typed.RetainedFffiBuilder) {
		PutAsU32(r, Hex(0xaabbccdd))
	})
	ret := captureBytes(t, func(r *typed.RetainedFffiBuilder) {
		PutAsU32(r, Hex(0xaabbccdd).Keep())
	})
	if !bytes.Equal(lit, ret) {
		t.Fatalf("retained-as-u32 != literal-as-u32: lit=%x ret=%x", lit, ret)
	}
}

// Note: retained-Color32 synthesis tests live in the components package's
// test suite (components/egui2_color_splice_test.go), since the synthesis
// helper lives there to avoid the color→components import cycle.

func TestPutColorsSlice_MatchesUint32SliceArg(t *testing.T) {
	cs := ColorsFromU32([]uint32{0x1, 0x2, 0x3})
	got := captureBytes(t, func(r *typed.RetainedFffiBuilder) {
		PutColorsSlice(r, cs)
	})
	// Length prefix is 4 bytes (u32 little-endian) + 3 x 4-byte u32 payloads.
	if len(got) != 4+3*4 {
		t.Fatalf("unexpected length: got %d (%x)", len(got), got)
	}
	// Length field must be 3 in LE.
	if got[0] != 0x03 || got[1] != 0x00 || got[2] != 0x00 || got[3] != 0x00 {
		t.Fatalf("length prefix: got %x", got[:4])
	}
	// First element: 0x1 LE.
	if got[4] != 0x01 || got[5] != 0x00 || got[6] != 0x00 || got[7] != 0x00 {
		t.Fatalf("elem 0: got %x", got[4:8])
	}
}

func TestPutColorsSlice_EmptyIsNotNilSentinel(t *testing.T) {
	// SD9 + nil-slice-sentinel guard: empty Colors must emit a 0-length
	// prefix, never the 0xFFFFFFFF nil sentinel.
	got := captureBytes(t, func(r *typed.RetainedFffiBuilder) {
		PutColorsSlice(r, NewColors(0))
	})
	if len(got) != 4 {
		t.Fatalf("expected 4-byte zero length, got %d (%x)", len(got), got)
	}
	if got[0] != 0 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("expected zero length prefix, got %x", got[:4])
	}
}
