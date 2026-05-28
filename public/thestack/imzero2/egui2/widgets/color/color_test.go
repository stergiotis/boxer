//go:build llm_generated_opus47

package color

import (
	"testing"
)

func TestHex(t *testing.T) {
	c := Hex(0x12345678)
	if c.Kind() != ColorKindLiteral {
		t.Fatalf("kind: got %d want %d", c.Kind(), ColorKindLiteral)
	}
	if c.Literal() != 0x12345678 {
		t.Fatalf("literal: got %08x", c.Literal())
	}
}

func TestRGBPacksAlphaFF(t *testing.T) {
	c := RGB(0x11, 0x22, 0x33)
	if c.Literal() != 0x112233ff {
		t.Fatalf("got %08x", c.Literal())
	}
}

func TestRGBA(t *testing.T) {
	c := RGBA(0x11, 0x22, 0x33, 0x44)
	if c.Literal() != 0x11223344 {
		t.Fatalf("got %08x", c.Literal())
	}
}

func TestGray(t *testing.T) {
	c := Gray(0x80)
	if c.Literal() != 0x808080ff {
		t.Fatalf("got %08x", c.Literal())
	}
}

func TestKeepRetainsLiteral(t *testing.T) {
	c := Hex(0xdeadbeef)
	k := c.Keep()
	if k.Kind() != ColorKindRetained {
		t.Fatalf("kind after Keep: got %d want %d", k.Kind(), ColorKindRetained)
	}
	if k.Literal() != 0xdeadbeef {
		t.Fatalf("literal stash lost after Keep: got %08x", k.Literal())
	}
}

func TestKeepOnRetainedIsIdempotent(t *testing.T) {
	c := Hex(0xdeadbeef).Keep()
	c2 := c.Keep()
	if c2.Kind() != ColorKindRetained {
		t.Fatalf("idempotency broken")
	}
	if c2.Literal() != 0xdeadbeef {
		t.Fatalf("literal stash lost on re-Keep")
	}
}

func TestKeepOnZeroPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on Keep() of zero-value Color")
		}
	}()
	var c Color
	_ = c.Keep()
}

func TestNewColorsEmptyIsNonNil(t *testing.T) {
	cs := NewColors(0)
	if cs == nil {
		t.Fatalf("NewColors(0) must be non-nil (SD9 nil-slice sentinel guard)")
	}
	if len(cs) != 0 {
		t.Fatalf("len")
	}
}

func TestNewColorsSized(t *testing.T) {
	cs := NewColors(4)
	if len(cs) != 4 {
		t.Fatalf("len")
	}
	cs.SetHex(2, 0xaabbccdd)
	if cs[2] != 0xaabbccdd {
		t.Fatalf("SetHex")
	}
}

func TestColorsFromU32NilCoercesToNonNil(t *testing.T) {
	cs := ColorsFromU32(nil)
	if cs == nil {
		t.Fatalf("ColorsFromU32(nil) must coerce to non-nil (SD9 nil-slice sentinel guard)")
	}
	if len(cs) != 0 {
		t.Fatalf("len")
	}
}

func TestColorsFromU32ZeroCopyBorrow(t *testing.T) {
	src := []uint32{1, 2, 3}
	cs := ColorsFromU32(src)
	if len(cs) != len(src) || &cs[0] != &src[0] {
		t.Fatalf("expected zero-copy borrow")
	}
}

func TestColorsFromSliceLiterals(t *testing.T) {
	cs := ColorsFromSlice([]Color{Hex(0x1), RGB(0x01, 0x02, 0x03), Gray(0x04)})
	if len(cs) != 3 {
		t.Fatalf("len")
	}
	if cs[0] != 0x1 {
		t.Fatalf("idx 0: got %08x", cs[0])
	}
	if cs[1] != 0x010203ff {
		t.Fatalf("idx 1: got %08x", cs[1])
	}
	if cs[2] != 0x040404ff {
		t.Fatalf("idx 2: got %08x", cs[2])
	}
}

func TestColorsFromSlicePanicsOnRetained(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on retained element (SD9)")
		}
	}()
	_ = ColorsFromSlice([]Color{Hex(0x1).Keep()})
}

func TestColorsSetters(t *testing.T) {
	cs := NewColors(4)
	cs.SetHex(0, 0x11223344)
	cs.SetRGB(1, 0x10, 0x20, 0x30)
	cs.SetRGBA(2, 0x10, 0x20, 0x30, 0x40)
	cs.SetGray(3, 0x80)
	if cs[0] != 0x11223344 {
		t.Errorf("SetHex got %08x", cs[0])
	}
	if cs[1] != 0x102030ff {
		t.Errorf("SetRGB got %08x", cs[1])
	}
	if cs[2] != 0x10203040 {
		t.Errorf("SetRGBA got %08x", cs[2])
	}
	if cs[3] != 0x808080ff {
		t.Errorf("SetGray got %08x", cs[3])
	}
}

func TestColorsAsU32Identity(t *testing.T) {
	cs := NewColors(2)
	cs.SetHex(0, 0xaa)
	cs.SetHex(1, 0xbb)
	u := cs.AsU32()
	if len(u) != 2 || u[0] != 0xaa || u[1] != 0xbb {
		t.Fatalf("AsU32 identity broken: %v", u)
	}
}
