package golay24

import (
	"math/big"
	"math/bits"
)

func ApplyErrorGF2(code uint32, errorBits uint32) uint32 {
	// flip bits where errorBits is 1
	c := code ^ errorBits
	// merge
	return (code & (^errorBits)) | (c & errorBits)
}

func Generate24BitNumberWithFixedPopcount(popcount uint8, handler func(combination uint32)) {
	const u = uint32(1) << 24
	for i := uint32(0); i < u; i++ {
		p := bits.OnesCount32(i)
		if uint8(p) == popcount {
			handler(i)
		}
	}
}

func GenerateBitErrors(code uint32, minErrorsIncl uint8, maxErrorsIncl uint8, handler func(codeWithErrors uint32, nErrors uint8)) {
	for i := minErrorsIncl; i <= maxErrorsIncl; i++ {
		Generate24BitNumberWithFixedPopcount(i, func(combination uint32) {
			handler(ApplyErrorGF2(code, combination), i)
		})
	}
}

// NumberOfPossibleErrorsIn24Bits binomialCoefficient(24,numberOfBitsToChange)
func NumberOfPossibleErrorsIn24Bits(numberOfBitsToChange uint8) int {
	if numberOfBitsToChange == 0 {
		return 0
	}
	return int(big.NewInt(0).Binomial(24, int64(numberOfBitsToChange)).Int64())
}

func NumberOfPossibleRepresentationsInMultiCodewordSeq(numberOfCodewords int64, numberOfBitsToChangeMax uint8) *big.Int {
	r := big.NewInt(1)
	for i := numberOfBitsToChangeMax; i > 0; i-- {
		r = r.Add(r, big.NewInt(int64(NumberOfPossibleErrorsIn24Bits(i))))
	}
	return r.Exp(r, big.NewInt(numberOfCodewords), nil)
}
