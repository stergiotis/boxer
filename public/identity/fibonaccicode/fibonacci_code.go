package fibonaccicode

import (
	"math/bits"

	"github.com/rs/zerolog/log"
)

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

// MaxRepresentableFibonacciCodeNumberByNBits Tabulated values of `MaxFibonacciCodeRepresentableByWidth(nBits)` function
// Values are inclusive i.e. with 5 Bits the numbers up to and including 7 can be represented
var MaxRepresentableFibonacciCodeNumberByNBits = [64]uint64{
	0, 0, 0, 2, 4, 7, 12, 20, 33, 54, 88, 143, 232, 376, 609, 986, 1596, 2583, 4180, 6764, 10945, 17710, 28656,
	46367, 75024, 121392, 196417, 317810, 514228, 832039, 1346268, 2178308, 3524577, 5702886, 9227464,
	14930351, 24157816, 39088168, 63245985, 102334154, 165580140, 267914295, 433494436, 701408732,
	1134903169, 1836311902, 2971215072, /* uint32 representable */
	4807526975, 7778742048, 12586269024, 20365011073, 32951280098,
	53316291172, 86267571271, 139583862444, 225851433716, 365435296161, 591286729878, 956722026040,
	1548008755919, 2504730781960, 4052739537880, 6557470319841, 10610209857722,
}

func MaxFibonacciCodeRepresentableByWidth(nBits int) (maxRepresentableNumber uint64) {
	switch nBits {
	case 0, 1:
		return 0
	case 2:
		return 0
	}
	rep := (uint64(1) << 63) | (uint64(0b11) << (64 - nBits - 1))
	maxRepresentableNumber = DecodeFibonacciCode(rep) - 1
	return
}

func FindFibonacciCodeCommaLsb(f uint64) (nBits int) {
	for i := 63; i >= 1; i-- {
		if f&0b11 == 0b11 {
			return i
		}
		f >>= 1
	}
	return -1
}

func FindFibonacciCodeCommaMsb(f uint64) (nBits int) {
	for i := 0; i <= 62; i++ {
		t := uint64(0b11) << (64 - 2 - i)
		if f&t == t {
			return i + 1
		}
	}
	return -1
}

func DecodeZeckendorfV(z uint64) (n uint64) {
	for i := 0; i < 64; i++ {
		n += fibNumbers[i] * ((z >> i) & 0b1)
	}
	return n
}

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

var MaxFibonacciCodeRepresentable = fibNumbers[64-1] - 1 // -1: bias to be able to represent 0
func encodeFibonacciCodeNaive(n uint64) (f uint64, nBits int) {
	if n >= MaxFibonacciCodeRepresentable {
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

func EncodeFibonacciCode(n uint64) (r uint64, nBits int) {
	return encodeFibonacciCodeNaive(n)
}

func DecodeFibonacciCode(f uint64) (n uint64) {
	nBits := FindFibonacciCodeCommaMsb(f)
	f >>= 64 - nBits
	for i := 0; i < nBits; i++ {
		n += fibNumbers[nBits-i-1] * (f & 0b1)
		f >>= 1
	}
	n-- // remove bias (0 is not representable in Fibonacci code)
	return
}
