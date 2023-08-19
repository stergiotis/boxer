package golay24

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyErrorGF2(t *testing.T) {
	// single flip 1 --> 0
	assert.Equal(t, uint32(0b1110), ApplyErrorGF2(0b1111, 0b0001))
	assert.Equal(t, uint32(0b1101), ApplyErrorGF2(0b1111, 0b0010))
	assert.Equal(t, uint32(0b1011), ApplyErrorGF2(0b1111, 0b0100))
	assert.Equal(t, uint32(0b0111), ApplyErrorGF2(0b1111, 0b1000))
	// single flip 0uint32( --> 1)
	assert.Equal(t, uint32(0b0001), ApplyErrorGF2(0b0000, 0b0001))
	assert.Equal(t, uint32(0b0010), ApplyErrorGF2(0b0000, 0b0010))
	assert.Equal(t, uint32(0b0100), ApplyErrorGF2(0b0000, 0b0100))
	assert.Equal(t, uint32(0b1000), ApplyErrorGF2(0b0000, 0b1000))
	// mixed
	assert.Equal(t, uint32(0b1101), ApplyErrorGF2(0b0010, 0b1111))
	assert.Equal(t, uint32(0b1100), ApplyErrorGF2(0b0010, 0b1110))
	assert.Equal(t, uint32(0b1111), ApplyErrorGF2(0b0010, 0b1101))
	assert.Equal(t, uint32(0b1001), ApplyErrorGF2(0b0010, 0b1011))
}

func TestNumberOfErrors(t *testing.T) {
	assert.Equal(t, 0, NumberOfPossibleErrorsIn24Bits(0))
	assert.Equal(t, 24, NumberOfPossibleErrorsIn24Bits(1))
}

func TestGenerate24BitNumberWithFixedPopcount(t *testing.T) {
	count := func(popcount uint8) int {
		i := 0
		Generate24BitNumberWithFixedPopcount(popcount, func(uint32) {
			i++
		})
		return i
	}
	assert.Equal(t, NumberOfPossibleErrorsIn24Bits(1), count(1))
	assert.Equal(t, NumberOfPossibleErrorsIn24Bits(2), count(2))
	assert.Equal(t, NumberOfPossibleErrorsIn24Bits(3), count(3))
	assert.Equal(t, NumberOfPossibleErrorsIn24Bits(4), count(4))
}
