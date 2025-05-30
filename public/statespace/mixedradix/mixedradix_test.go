package mixedradix

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPartitionNumber(t *testing.T) {
	radixii := []uint64{3, 4, 5}
	prod := radixii[0] * radixii[1] * radixii[2]
	f1 := func(should []uint64, offset uint64, n uint64) {
		require.EqualValues(t, should, ToDigits(radixii, offset+n))
		require.EqualValues(t, (offset+n)%prod, FromDigits(radixii, should))
	}
	for i := uint64(0); i < 2; i++ {
		f1([]uint64{0, 0, 0}, i*60, 0)
		f1([]uint64{1, 0, 0}, i*60, 1)
		f1([]uint64{2, 0, 0}, i*60, 2)
		f1([]uint64{0, 1, 0}, i*60, 3)
		f1([]uint64{1, 1, 0}, i*60, 4)
		f1([]uint64{2, 1, 0}, i*60, 5)
		f1([]uint64{0, 2, 0}, i*60, 6)
		f1([]uint64{1, 2, 0}, i*60, 7)
		f1([]uint64{2, 2, 0}, i*60, 8)
		f1([]uint64{0, 3, 0}, i*60, 9)
		f1([]uint64{1, 3, 0}, i*60, 10)
		f1([]uint64{2, 3, 0}, i*60, 11)
		f1([]uint64{0, 0, 1}, i*60, 12)
		f1([]uint64{1, 0, 1}, i*60, 13)
		f1([]uint64{2, 0, 1}, i*60, 14)
		f1([]uint64{0, 1, 1}, i*60, 15)
		f1([]uint64{1, 1, 1}, i*60, 16)
		f1([]uint64{2, 1, 1}, i*60, 17)
		f1([]uint64{0, 2, 1}, i*60, 18)
		f1([]uint64{1, 2, 1}, i*60, 19)
		f1([]uint64{2, 2, 1}, i*60, 20)
		f1([]uint64{0, 3, 1}, i*60, 21)
		f1([]uint64{1, 3, 1}, i*60, 22)
		f1([]uint64{2, 3, 1}, i*60, 23)
		f1([]uint64{0, 0, 2}, i*60, 24)
		f1([]uint64{1, 0, 2}, i*60, 25)
		f1([]uint64{2, 0, 2}, i*60, 26)
		f1([]uint64{0, 1, 2}, i*60, 27)
		f1([]uint64{1, 1, 2}, i*60, 28)
		f1([]uint64{2, 1, 2}, i*60, 29)
		f1([]uint64{0, 2, 2}, i*60, 30)
		f1([]uint64{1, 2, 2}, i*60, 31)
		f1([]uint64{2, 2, 2}, i*60, 32)
		f1([]uint64{0, 3, 2}, i*60, 33)
		f1([]uint64{1, 3, 2}, i*60, 34)
		f1([]uint64{2, 3, 2}, i*60, 35)
		f1([]uint64{0, 0, 3}, i*60, 36)
		f1([]uint64{1, 0, 3}, i*60, 37)
		f1([]uint64{2, 0, 3}, i*60, 38)
		f1([]uint64{0, 1, 3}, i*60, 39)
		f1([]uint64{1, 1, 3}, i*60, 40)
		f1([]uint64{2, 1, 3}, i*60, 41)
		f1([]uint64{0, 2, 3}, i*60, 42)
		f1([]uint64{1, 2, 3}, i*60, 43)
		f1([]uint64{2, 2, 3}, i*60, 44)
		f1([]uint64{0, 3, 3}, i*60, 45)
		f1([]uint64{1, 3, 3}, i*60, 46)
		f1([]uint64{2, 3, 3}, i*60, 47)
		f1([]uint64{0, 0, 4}, i*60, 48)
		f1([]uint64{1, 0, 4}, i*60, 49)
		f1([]uint64{2, 0, 4}, i*60, 50)
		f1([]uint64{0, 1, 4}, i*60, 51)
		f1([]uint64{1, 1, 4}, i*60, 52)
		f1([]uint64{2, 1, 4}, i*60, 53)
		f1([]uint64{0, 2, 4}, i*60, 54)
		f1([]uint64{1, 2, 4}, i*60, 55)
		f1([]uint64{2, 2, 4}, i*60, 56)
		f1([]uint64{0, 3, 4}, i*60, 57)
		f1([]uint64{1, 3, 4}, i*60, 58)
		f1([]uint64{2, 3, 4}, i*60, 59)
		f1([]uint64{0, 0, 0}, i*60, 60)
	}
}
