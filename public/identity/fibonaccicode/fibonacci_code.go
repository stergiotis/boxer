// Package fibonaccicode encodes and decodes Fibonacci codes (Zeckendorf
// representations terminated by a "11" comma) in a uint64, MSB-aligned.
//
// Width conventions (ADR-0106 SD9): a code's *full width* includes the
// trailing comma-completing 1 bit; EncodeFibonacciCode returns the full
// width. FindFibonacciCodeCommaMsb returns the bit count up to and including
// the comma's UPPER bit, which is the full width minus one. Values are
// biased by one on encode (0 is not representable in a bare Fibonacci code),
// so EncodeFibonacciCode(n) emits the code of the Zeckendorf sum n+1 and
// DecodeFibonacciCode undoes the bias.
package fibonaccicode

import (
	"math/bits"

	"github.com/rs/zerolog/log"
)

// fibNumbers[i] is the Fibonacci number F(i+2): 1, 2, 3, 5, 8, …
// fibNumbers[64] is used only as the EncodeZeckendorf range bound.
var fibNumbers = [65]uint64{
	1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987, 1597,
	2584, 4181, 6765, 10946, 17711, 28657, 46368, 75025, 121393, 196418,
	317811, 514229, 832040, 1346269, 2178309, 3524578, 5702887, 9227465,
	14930352, 24157817, 39088169, 63245986, 102334155, 165580141,
	267914296, 433494437, 701408733, 1134903170, 1836311903,
	2971215073, /* largest fibonacci number representable as uint32 */
	4807526976, 7778742049, 12586269025,
	20365011074, 32951280099, 53316291173, 86267571272,
	139583862445, 225851433717, 365435296162, 591286729879,
	956722026041, 1548008755920, 2504730781961, 4052739537881,
	6557470319842, 10610209857723, 17167680177565,
	27777890035288,
}

// MaxRepresentableExcl is the EXCLUSIVE upper bound of encodable values:
// EncodeFibonacciCode accepts n in [0, MaxRepresentableExcl) and panics at or
// above it (the code of MaxRepresentableExcl-1 fills all 64 bits). It equals
// the count of values encodable in a uint64.
var MaxRepresentableExcl = fibNumbers[64-1] - 1 // -1: bias to be able to represent 0

// MaxRepresentableExclByWidth is the EXCLUSIVE upper bound of the values
// whose full code width (including the trailing comma bit) is at most nBits:
// exactly the values in [0, MaxRepresentableExclByWidth(nBits)) fit in nBits
// bits. Equivalently it is the first value that needs a wider code, so the
// values whose width is exactly w form the half-open interval
// [MaxRepresentableExclByWidth(w-1), MaxRepresentableExclByWidth(w)).
// Defined for every int: nBits < 3 yields 0 (only the width-2 code "11",
// value 0, exists below width 3) and nBits >= 64 saturates to
// MaxRepresentableExcl.
func MaxRepresentableExclByWidth(nBits int) (maxRepresentableNumberExcl uint64) {
	if nBits < 3 {
		if nBits == 2 {
			return 1 // width 2 is the bare comma "11": exactly the value 0
		}
		return 0
	}
	if nBits >= 64 {
		return MaxRepresentableExcl
	}
	rep := (uint64(1) << 63) | (uint64(0b11) << (64 - nBits - 1))
	n, ok := DecodeFibonacciCode(rep)
	if !ok {
		// unreachable: rep always carries a comma
		log.Panic().Int("nBits", nBits).Msg("constructed width probe did not decode")
	}
	// The probe decodes to fibNumbers[nBits-1]; the values encodable within
	// nBits full bits are exactly [0, fibNumbers[nBits-1]-1), so the
	// exclusive bound is one below the probe's value.
	maxRepresentableNumberExcl = n - 1
	return
}

// FindFibonacciCodeCommaMsb scans from the MSB for the first adjacent "11"
// pair and returns the number of bits up to and including the pair's upper
// bit — the code's full width minus one. It returns -1 when no comma exists
// (f is not an MSB-aligned Fibonacci code).
func FindFibonacciCodeCommaMsb(f uint64) (nBits int) {
	for i := 0; i <= 62; i++ {
		t := uint64(0b11) << (64 - 2 - i)
		if f&t == t {
			return i + 1
		}
	}
	return -1
}

// DecodeZeckendorfV sums fibNumbers[i] over the set bits of the LSB-aligned
// Zeckendorf representation z. It does not validate the no-consecutive-ones
// property.
func DecodeZeckendorfV(z uint64) (n uint64) {
	for i := 0; i < 64; i++ {
		n += fibNumbers[i] * ((z >> i) & 0b1)
	}
	return n
}

// EncodeZeckendorf writes n as an LSB-aligned Zeckendorf representation and
// returns it with its width in bits. It panics when n >= fibNumbers[64].
func EncodeZeckendorf(n uint64) (z uint64, nBits int) {
	if n >= fibNumbers[64] {
		log.Panic().Uint64("n", n).Msg("number is out of range representable range")
	}
	i := 63
	for ; i >= 0; i-- {
		f := fibNumbers[i]
		if n >= f {
			nBits = i + 1
			z |= 1 << i
			n -= f
			i-- // no two consecutive fibonacci numbers --> skip next
			break
		}
	}
	for ; i >= 0 && n != 0; i-- {
		f := fibNumbers[i]
		if n >= f {
			z |= 1 << i
			n -= f
			i-- // no two consecutive fibonacci numbers --> skip next
		}
	}
	return
}

func encodeFibonacciCodeNaive(n uint64) (f uint64, nBits int) {
	if n >= MaxRepresentableExcl {
		log.Panic().Uint64("n", n).Msg("number is out of range representable range")
	}
	var z uint64
	z, nBits = EncodeZeckendorf(n + 1) //+1: 0 can not be encoded --> bias by one
	f = z
	f = bits.Reverse64(f)
	nBits++
	f |= uint64(1) << uint64(64-nBits)
	return
}

// EncodeFibonacciCode returns the MSB-aligned Fibonacci code of n and its
// full width in bits, including the trailing comma-completing 1. n must be
// below MaxRepresentableExcl or the function panics.
func EncodeFibonacciCode(n uint64) (r uint64, nBits int) {
	return encodeFibonacciCodeNaive(n)
}

// DecodeFibonacciCode reverses EncodeFibonacciCode. ok is false when f
// carries no "11" comma and is therefore not a Fibonacci code; n is 0 then.
// Bits below the comma are ignored, so a tagged id (code in the high bits,
// payload below) decodes to its tag's value directly.
func DecodeFibonacciCode(f uint64) (n uint64, ok bool) {
	nBits := FindFibonacciCodeCommaMsb(f)
	if nBits < 0 {
		return 0, false
	}
	f >>= 64 - nBits
	for i := 0; i < nBits; i++ {
		n += fibNumbers[nBits-i-1] * (f & 0b1)
		f >>= 1
	}
	n-- // remove bias (0 is not representable in Fibonacci code)
	return n, true
}
