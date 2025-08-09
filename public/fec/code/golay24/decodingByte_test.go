package golay24

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeLowLevel(t *testing.T) {
	sz := (10 * 1024 / 12) * 12
	input := make([]byte, sz, sz)
	encoded := make([]byte, 0, sz*2)
	decoded := make([]byte, sz, sz)
	prng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for i := 0; i < len(input)/4; i++ {
		binary.BigEndian.PutUint32(input[i*4:], prng.Uint32())
	}
	var err error
	encoded, err = EncodeBytes(encoded, input)
	require.NoError(t, err)
	DecodeLowLevel(decoded, encoded)
	require.Equal(t, input, decoded)
}

func BenchmarkDecodeLowLevel(b *testing.B) {
	b.StopTimer()
	b.ReportAllocs()
	sz := (100 * 1024 * 1024 / 12) * 12
	input := make([]byte, sz, sz)
	encoded := make([]byte, 0, sz*2)
	decoded := make([]byte, sz, sz)
	prng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for i := 0; i < len(input)/4; i++ {
		binary.BigEndian.PutUint32(input[i*4:], prng.Uint32())
	}
	var err error
	encoded, err = EncodeBytes(encoded, input)
	require.NoError(b, err)

	b.SetBytes(int64(sz))
	b.StartTimer()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeLowLevel(decoded, encoded)
	}
}
