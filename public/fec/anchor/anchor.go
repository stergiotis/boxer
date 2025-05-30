package anchor

import (
	"io"
	"math/bits"
)

const MagicByteAnchor = 0b10101010

func MakeAnchor(nBytes int) []byte {
	v := make([]byte, 0, nBytes)
	for i := 0; i < nBytes; i++ {
		v = append(v, MagicByteAnchor)
	}
	return v
}

func SkipPastAnchorConsecutive(reader io.ByteReader, nAnchorBytes int, maxHammingDistPerByteIncl int) (nBytesRead uint64, dist int, err error) {
	nBytesRead = 0
	var b byte
	b, err = reader.ReadByte()
	if err != nil {
		return
	}
	nBytesRead++
	dist = bits.OnesCount8(b ^ MagicByteAnchor)
	if dist <= maxHammingDistPerByteIncl {
		return
	}
	var n uint64
	n, dist, err = SkipPastAnchorInitial(reader, nAnchorBytes, maxHammingDistPerByteIncl)
	nBytesRead += n
	return
}

func SkipPastAnchorInitial(reader io.ByteReader, nAnchorBytes int, maxHammingDistPerByteIncl int) (nBytesRead uint64, dist int, err error) {
	consecutiveSuccesses := 0

	nBytesRead = 0
	b0, err := reader.ReadByte()
	if err != nil {
		return nBytesRead, 0, err
	}
	nBytesRead++
	d0 := bits.OnesCount8(b0 ^ MagicByteAnchor)
	dist = 0
	for {
		if d0 <= maxHammingDistPerByteIncl {
			consecutiveSuccesses++
			dist += d0
			if nAnchorBytes == consecutiveSuccesses {
				return nBytesRead, dist, nil
			}
		} else {
			consecutiveSuccesses = 0
			dist = 0
		}
		b0, err = reader.ReadByte()
		if err != nil {
			return nBytesRead, 0, err
		}
		nBytesRead++
		d0 = bits.OnesCount8(b0 ^ MagicByteAnchor)
	}
}
