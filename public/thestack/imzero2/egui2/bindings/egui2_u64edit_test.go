package bindings

import (
	"math"
	"testing"
)

// criticalU64s are the values that matter: the ones an f64-backed DragValue /
// Slider cannot represent. 2^53 is the last integer f64 holds exactly; above it
// the mantissa runs out. 2^63 and up additionally saturate DragValue's
// `n as i64` hex formatter to 0x7FFFFFFFFFFFFFFF.
var criticalU64s = []uint64{
	0,
	1,
	255,
	1 << 52,
	1 << 53,       // last exactly-representable in f64
	(1 << 53) + 1, // first f64 rounds away — DragValue loses this bit
	1 << 62,
	0x7FFFFFFFFFFFFFFF, // i64::MAX — the value DragValue's hex clamp emits
	1 << 63,            // 0x8000000000000000 — f64→i64 saturates here
	0xDEADBEEFCAFEF00D, // a representative tagged-id / hash bit pattern
	math.MaxUint64,     // 0xFFFFFFFFFFFFFFFF
}

func TestU64Edit_RoundTripExact(t *testing.T) {
	for _, v := range criticalU64s {
		for _, hex := range []bool{false, true} {
			s := formatU64(v, hex)
			got, ok := parseU64(s)
			if !ok {
				t.Errorf("parseU64(formatU64(%d, hex=%v)=%q) returned ok=false", v, hex, s)
				continue
			}
			if got != v {
				t.Errorf("round-trip lost precision: %d -> %q -> %d (hex=%v)", v, s, got, hex)
			}
		}
	}
}

func TestU64Edit_ParseAccepts(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"0", 0},
		{"42", 42},
		{"  42  ", 42},                             // trimmed
		{"010", 10},                                // leading zero is decimal, not octal
		{"18446744073709551615", math.MaxUint64},   // decimal u64 max
		{"9007199254740993", (1 << 53) + 1},        // 2^53+1: exact here, lost by f64
		{"0xff", 255},                              // lowercase hex
		{"0XFF", 255},                              // uppercase prefix + digits
		{"0xffffffffffffffff", math.MaxUint64},     // full-width hex, top bit set
		{"0x8000000000000000", 1 << 63},            // the bit DragValue's i64 clamp drops
		{"0xdeadbeefcafef00d", 0xDEADBEEFCAFEF00D}, // tagged-id pattern
	}
	for _, c := range cases {
		got, ok := parseU64(c.in)
		if !ok || got != c.want {
			t.Errorf("parseU64(%q) = (%d, %v), want (%d, true)", c.in, got, ok, c.want)
		}
	}
}

func TestU64Edit_ParseRejects(t *testing.T) {
	for _, in := range []string{
		"",                     // empty
		"   ",                  // whitespace only
		"0x",                   // prefix with no digits
		"xyz",                  // not a number
		"-1",                   // no sign
		"3.14",                 // not an integer
		"0b101",                // binary not supported (dec-or-0xhex only)
		"18446744073709551616", // u64 max + 1, overflow
		"0x10000000000000000",  // 2^64 in hex, overflow
	} {
		if v, ok := parseU64(in); ok {
			t.Errorf("parseU64(%q) = (%d, true), want ok=false", in, v)
		}
	}
}

func TestU64Edit_FormatShape(t *testing.T) {
	cases := []struct {
		v    uint64
		hex  bool
		want string
	}{
		{255, false, "255"},
		{255, true, "0xff"},
		{0, true, "0x0"},
		{math.MaxUint64, false, "18446744073709551615"},
		{math.MaxUint64, true, "0xffffffffffffffff"},
	}
	for _, c := range cases {
		if got := formatU64(c.v, c.hex); got != c.want {
			t.Errorf("formatU64(%d, hex=%v) = %q, want %q", c.v, c.hex, got, c.want)
		}
	}
}
