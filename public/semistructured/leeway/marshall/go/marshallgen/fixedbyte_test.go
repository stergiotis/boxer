package marshallgen_test

import "testing"

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
