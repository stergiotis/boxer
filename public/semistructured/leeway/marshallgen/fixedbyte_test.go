package marshallgen_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
)

// TestFixedByteArrayLen pins the single recogniser used by every
// fixed-byte site in both packages: any decimal [N]byte is accepted; the
// variable-length blob []byte and non-byte / malformed names are not.
func TestFixedByteArrayLen(t *testing.T) {
	cases := []struct {
		in     string
		wantN  int
		wantOk bool
	}{
		{"[4]byte", 4, true},
		{"[8]byte", 8, true}, // not a historically special-cased size
		{"[16]byte", 16, true},
		{"[32]byte", 32, true},
		{"[1]byte", 1, true},
		{"[0]byte", 0, true},     // degenerate but well-formed
		{"[]byte", 0, false},     // variable-length blob, not a fixed array
		{"[4]uint8", 0, false},   // classifier never emits this spelling
		{"[4]byte ", 0, false},   // trailing space
		{"[-4]byte", 0, false},   // negative length
		{"[0x10]byte", 0, false}, // non-decimal length not recognised
		{"uint64", 0, false},
		{"byte", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := marshallgen.FixedByteArrayLen(c.in)
		if n != c.wantN || ok != c.wantOk {
			t.Errorf("FixedByteArrayLen(%q) = (%d, %v), want (%d, %v)", c.in, n, ok, c.wantN, c.wantOk)
		}
		if got := marshallgen.IsFixedByteArray(c.in); got != c.wantOk {
			t.Errorf("IsFixedByteArray(%q) = %v, want %v", c.in, got, c.wantOk)
		}
	}
}

// TestEmit_FixedByteArray_ArbitraryLen confirms a [8]byte field — neither
// of the historically special-cased sizes — is carried as a []byte blob:
// resliced on write and copied back into the fixed array on read, exactly
// like [4]/[16]byte. Previously such a field parsed but emitted a
// [8]byte value into a []byte BeginAttribute, which would not compile.
func TestEmit_FixedByteArray_ArbitraryLen(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	Sig [8]byte   `+"`lw:\"sig,blob\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "BeginAttribute(value []byte) Attr")   // wired as a []byte blob
	mustContain(t, out, "blobSec.BeginAttribute(c.Sig[i][:])") // resliced on write
	mustContain(t, out, "var blobSigVal [8]byte")              // read accumulator is the array
	mustContain(t, out, "copy(blobSigVal[:], val)")            // copied back on read
	mustNotContain(t, out, "BeginAttribute(value [8]byte)")    // never passed as the raw array
}
