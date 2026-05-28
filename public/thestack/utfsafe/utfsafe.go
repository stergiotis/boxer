//go:build llm_generated_opus47

// Package utfsafe coerces arbitrary byte streams into UTF-8-valid
// strings for the FFFI wire and other UTF-8-strict consumers.
//
// The boundary problem
//
// Go's `string` type carries arbitrary bytes — there is no UTF-8
// enforcement at the language level. Several producers in this repo
// surface non-UTF-8 bytes through the `string` interface:
//
//   - `*array.LargeBinary` / `*array.LargeString` from Arrow: CH's
//     FORMAT ArrowStream emits binary columns as LargeBinary regardless
//     of leeway DDL declaration; `ValueStr()` returns `string(raw)`.
//   - `*array.String` carrying binary data (Arrow does not validate
//     UTF-8 on writes).
//   - Anything fed through the leeway sink's `WriteString` /
//     `Write([]byte)` interfaces.
//
// Such bytes break Rust's `read_plain_s` (`String::from_utf8`) on the
// FFFI side. The wire is now lossy (substitutes `U+FFFD`) so the
// protocol survives, but the GUI shows replacement characters and the
// operator gets one ERROR log per affected row. Hex-encoding at the
// Go-side boundary avoids both — and gives the operator a stable,
// readable cell value to inspect the bad bytes.
//
// Cost model
//
// The common case (valid UTF-8) returns the input string unchanged
// with ZERO allocations — the `utf8.ValidString` scan is the only cost
// and is a single linear pass already optimised in the standard
// library.
//
// The rare case (invalid UTF-8) performs EXACTLY ONE allocation: a
// `2*len(s)` byte buffer for the hex output. The implementation uses
// `unsafeperf` to avoid the implicit copies that `[]byte(s)` and
// `string([]byte)` would incur with the obvious `fmt.Sprintf("%x",
// []byte(s))` formulation. The output is returned via
// `unsafeperf.UnsafeBytesToString` (the buffer is not retained beyond
// the call, so no aliasing risk).
package utfsafe

import (
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

// hexAlphabet is the lower-case hex digit table — kept as a string so
// the indexing path avoids a bounds check on a backing array.
const hexAlphabet = "0123456789abcdef"

// EnsureUTF8 returns s unchanged when it is valid UTF-8; otherwise
// returns the lower-case hex encoding of s's bytes. See the package
// doc for the allocation guarantees.
func EnsureUTF8(s string) (r string) {
	if utf8.ValidString(s) {
		r = s
		return
	}
	src := unsafeperf.UnsafeStringToBytes(s)
	n := len(src)
	dst := make([]byte, n*2)
	for i := 0; i < n; i++ {
		b := src[i]
		dst[i*2] = hexAlphabet[b>>4]
		dst[i*2+1] = hexAlphabet[b&0x0f]
	}
	r = unsafeperf.UnsafeBytesToString(dst)
	return
}

// AppendEnsureUTF8 appends the UTF-8-safe form of s to dst and returns
// the extended slice. For callers that already maintain a scratch
// buffer pool — avoids the per-call allocation in the rare-case path.
// In the common case (valid UTF-8) this still copies the bytes (since
// the contract is "append"), so the per-call form `EnsureUTF8` is
// cheaper when the input is expected to be valid.
func AppendEnsureUTF8(dst []byte, s string) (r []byte) {
	if utf8.ValidString(s) {
		r = append(dst, s...)
		return
	}
	src := unsafeperf.UnsafeStringToBytes(s)
	n := len(src)
	// Grow dst once to its final size to avoid amortised copying.
	if cap(dst)-len(dst) < n*2 {
		grown := make([]byte, len(dst), len(dst)+n*2)
		copy(grown, dst)
		dst = grown
	}
	for i := 0; i < n; i++ {
		b := src[i]
		dst = append(dst, hexAlphabet[b>>4], hexAlphabet[b&0x0f])
	}
	r = dst
	return
}
