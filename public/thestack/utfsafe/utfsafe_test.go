//go:build llm_generated_opus47

package utfsafe

import (
	"fmt"
	"testing"
	"unicode/utf8"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestEnsureUTF8_validReturnsSamePointer(t *testing.T) {
	// Common case: a valid UTF-8 string is returned unchanged AND
	// shares storage with the input (zero-copy / zero-alloc).
	in := "hello · world"
	out := EnsureUTF8(in)
	assert.Equal(t, in, out)
	// Pointer equality verifies no copy happened. unsafe.StringData
	// is the supported way to compare string backing addresses in
	// modern Go (1.20+).
	assert.Equal(t, unsafe.StringData(in), unsafe.StringData(out),
		"EnsureUTF8 must not copy on the valid path")
}

func TestEnsureUTF8_emptyString(t *testing.T) {
	out := EnsureUTF8("")
	assert.Equal(t, "", out)
}

func TestEnsureUTF8_invalidIsHexEncoded(t *testing.T) {
	// The exact non-UTF-8 prefix from the runtime.facts desync.
	bad := []byte{0x6d, 0x0c, 0xe9, 0xd1, 0x2c, 0x79, 0x8a, 0xff,
		0xdf, 0xf7, 0x5b, 0xd8, 0xa5, 0x38, 0x0b, 0x98}
	out := EnsureUTF8(string(bad))
	assert.Equal(t, "6d0ce9d12c798affdff75bd8a5380b98", out)
	assert.True(t, utf8.ValidString(out), "hex output must be ASCII")
}

func TestEnsureUTF8_lonelyContinuation(t *testing.T) {
	// 0x80 is a lone continuation byte — invalid as the first byte
	// of any UTF-8 sequence.
	out := EnsureUTF8(string([]byte{0x80}))
	assert.Equal(t, "80", out)
}

func TestEnsureUTF8_multibyteStaysValid(t *testing.T) {
	// 4-byte multi-byte (U+1F600 GRINNING FACE).
	in := "\xf0\x9f\x98\x80"
	out := EnsureUTF8(in)
	assert.Equal(t, in, out)
	assert.Equal(t, unsafe.StringData(in), unsafe.StringData(out))
}

func TestAppendEnsureUTF8_validAppends(t *testing.T) {
	dst := []byte("prefix:")
	out := AppendEnsureUTF8(dst, "ok")
	assert.Equal(t, "prefix:ok", string(out))
}

func TestAppendEnsureUTF8_invalidHex(t *testing.T) {
	dst := []byte("prefix:")
	out := AppendEnsureUTF8(dst, string([]byte{0xff, 0xfe}))
	assert.Equal(t, "prefix:fffe", string(out))
}

// Benchmark the hot path (valid input) — should bottom out at the
// utf8.ValidString cost.
func BenchmarkEnsureUTF8_validShort(b *testing.B) {
	s := "Elapsed: 12.345ms"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := EnsureUTF8(s)
		// Prevent the compiler from optimising the call away.
		if len(r) == 0 {
			b.Fatal("unexpected empty")
		}
	}
}

func BenchmarkEnsureUTF8_validLong(b *testing.B) {
	s := "this is a longer label with some · characters and a trailing tail that's quite long"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EnsureUTF8(s)
	}
}

func BenchmarkEnsureUTF8_invalidShort(b *testing.B) {
	s := string([]byte{0x6d, 0x0c, 0xe9, 0xd1, 0x2c, 0x79, 0x8a, 0xff,
		0xdf, 0xf7, 0x5b, 0xd8, 0xa5, 0x38, 0x0b, 0x98})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EnsureUTF8(s)
	}
}

// Reference benchmark: the obvious fmt.Sprintf("%x", []byte(s))
// formulation we replaced. Demonstrates the alloc reduction.
func BenchmarkEnsureUTF8_invalidShort_fmtSprintfReference(b *testing.B) {
	s := string([]byte{0x6d, 0x0c, 0xe9, 0xd1, 0x2c, 0x79, 0x8a, 0xff,
		0xdf, 0xf7, 0x5b, 0xd8, 0xa5, 0x38, 0x0b, 0x98})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%x", []byte(s))
	}
}
