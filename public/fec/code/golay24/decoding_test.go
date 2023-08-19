package golay24

import (
	"github.com/stergiotis/boxer/public/unittest"
	"math/rand"
	"testing"

	"github.com/rs/zerolog/log"
	progressbar "github.com/schollz/progressbar/v3"
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
	unittest.NewProgressBar(int64(len(Encoding)))
	bar := progressbar.NewOptions(len(Encoding),
		progressbar.OptionSetDescription("exhaustive test with 0,1,2,3 error bits"))
	for o, g24 := range Encoding {
		assert.Equal(t, uint16(o), DecodeSingle(g24))
		assert.Equal(t, uint8(0), NumberOfBitErrors(g24))
		_ = bar.Add(1)
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
