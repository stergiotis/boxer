package bindings

import (
	"bytes"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// capture runs fn against a fresh RetainedFffiBuilder and returns the
// emitted bytes.
func capture(fn func(r *typed.RetainedFffiBuilder)) []byte {
	r := typed.NewRetainedFffiBuilder()
	fn(r)
	h := r.BuildRetained()
	out := make([]byte, len(h.Content()))
	copy(out, h.Content())
	return out
}

func TestPutColorAsRetainedColor32_DeterministicForLiteral(t *testing.T) {
	a := capture(func(r *typed.RetainedFffiBuilder) {
		PutColorAsRetainedColor32(r, color.Hex(0x11223344))
	})
	b := capture(func(r *typed.RetainedFffiBuilder) {
		PutColorAsRetainedColor32(r, color.Hex(0x11223344))
	})
	if !bytes.Equal(a, b) {
		t.Fatalf("determinism broken: a=%x b=%x", a, b)
	}
	if len(a) == 0 {
		t.Fatalf("empty splice")
	}
}

func TestPutColorAsRetainedColor32_MatchesLegacyFactoryByteForByte(t *testing.T) {
	// Synthesised path (via PutColorAsRetainedColor32 → inline opcodes)
	synth := capture(func(r *typed.RetainedFffiBuilder) {
		PutColorAsRetainedColor32(r, color.Hex(0xaabbccdd))
	})
	// Legacy path (via the Color() fluent factory + Keep() + splice).
	// Both must produce byte-identical wire bytes so pre-ADR callers and
	// post-ADR callers interoperate without pixel drift.
	legacy := capture(func(r *typed.RetainedFffiBuilder) {
		h := Color().FromRgbaUnmultiplied(0xaa, 0xbb, 0xcc, 0xdd).Keep()
		r.SpliceRetained(h.Untype())
	})
	if !bytes.Equal(synth, legacy) {
		t.Fatalf("inline synth diverges from legacy factory: synth=%x legacy=%x", synth, legacy)
	}
}

func TestPutColorAsRetainedColor32_RetainedVariantUsesStashedLiteral(t *testing.T) {
	// color.Hex().Keep() has no external holder; the encoder must still
	// produce the same bytes as the literal path since literal is stashed.
	lit := capture(func(r *typed.RetainedFffiBuilder) {
		PutColorAsRetainedColor32(r, color.Hex(0x11223344))
	})
	kept := capture(func(r *typed.RetainedFffiBuilder) {
		PutColorAsRetainedColor32(r, color.Hex(0x11223344).Keep())
	})
	if !bytes.Equal(lit, kept) {
		t.Fatalf("literal vs Keep'd literal produce different bytes: lit=%x kept=%x", lit, kept)
	}
}

func TestPutColorAsRetainedColor32_FromRetainedHolderSplices(t *testing.T) {
	// The SD7 escape-hatch path: caller builds via Color().FromRgb(...).Keep()
	// and wraps via FromRetainedHolder. The encoder must splice the pre-built
	// holder's bytes directly.
	built := Color().FromRgb(0x10, 0x20, 0x30).Keep()
	col := color.FromRetainedHolder(built.Untype(), 0x102030ff)

	via := capture(func(r *typed.RetainedFffiBuilder) {
		PutColorAsRetainedColor32(r, col)
	})
	direct := capture(func(r *typed.RetainedFffiBuilder) {
		r.SpliceRetained(built.Untype())
	})
	if !bytes.Equal(via, direct) {
		t.Fatalf("FromRetainedHolder → PutColorAsRetainedColor32 diverges from direct splice: via=%x direct=%x", via, direct)
	}
}
