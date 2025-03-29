package golay24

import (
	"math/rand"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestDecodeSingle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping exhaustive search test in short mode")
	}

	for o, e := range Encoding {
		assert.Equal(t, uint16(o), DecodeSingle(e))
		assert.Equal(t, uint8(0), NumberOfBitErrors(e))
	}
	for o, g24 := range Encoding {
		assert.Equal(t, uint16(o), DecodeSingle(g24))
		assert.Equal(t, uint8(0), NumberOfBitErrors(g24))
		GenerateBitErrors(g24, 1, 3, func(code uint32, nErrors uint8) {
			assert.Equal(t, uint16(o), DecodeSingle(code))
			assert.Equal(t, nErrors, NumberOfBitErrors(code))
		})
	}
}

func BenchmarkDecodeSingle(b *testing.B) {
	b.StopTimer()
	var randomEncoded []uint32
	var randomDecoded []uint16
	s := uint16(0)
	const n = 40 * 1024 * 1024 / 4
	randomEncoded = make([]uint32, 0, n)
	m := uint32(1<<12 - 1)
	for i := 0; i < n; i++ {
		p := rand.Uint32() & m
		randomDecoded = append(randomDecoded, uint16(p))
		randomEncoded = append(randomEncoded, Encoding[p])
	}
	b.SetBytes(int64(len(randomEncoded) * 4))
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range randomEncoded {
			s += DecodeSingle(c)
		}
	}
	b.StopTimer()
	_ = randomDecoded
	log.Debug().Uint16("dummy", s).Msg("finished")
}
