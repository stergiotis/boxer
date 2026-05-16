package unittest

import (
	"bytes"
	"hash"

	"github.com/rs/zerolog/log"
)

type MockHasher struct {
	Buf *bytes.Buffer
}

func (inst *MockHasher) Sum32() uint32 {
	return 0xdeadbeef
}

func (inst *MockHasher) Sum64() uint64 {
	return 0xdeadbeeffeeffeef
}

func (inst *MockHasher) Write(p []byte) (n int, err error) {
	return inst.Buf.Write(p)
}

func (inst *MockHasher) Sum(b []byte) []byte {
	_, err := inst.Buf.Write(b)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to write to buffer")
	}
	return []byte{0xde, 0xad, 0xbe, 0xef}
}

func (inst *MockHasher) Reset() {
	inst.Buf.Reset()
}

func (inst *MockHasher) Size() int {
	return 4
}

func (inst *MockHasher) BlockSize() int {
	return 64
}

var _ hash.Hash = (*MockHasher)(nil)

var _ hash.Hash32 = (*MockHasher)(nil)

var _ hash.Hash64 = (*MockHasher)(nil)

func NewMockHasher() *MockHasher {
	return &MockHasher{
		Buf: &bytes.Buffer{},
	}
}
