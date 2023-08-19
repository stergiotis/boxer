package unittest

import (
	"bytes"
	"hash"

	"github.com/rs/zerolog/log"
)

type MockHasher struct {
	Buf *bytes.Buffer
}

func (m *MockHasher) Sum32() uint32 {
	return 0xdeadbeef
}

func (m *MockHasher) Sum64() uint64 {
	return 0xdeadbeeffeeffeef
}

func (m *MockHasher) Write(p []byte) (n int, err error) {
	return m.Buf.Write(p)
}

func (m *MockHasher) Sum(b []byte) []byte {
	_, err := m.Buf.Write(b)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to write to buffer")
	}
	return []byte{0xde, 0xad, 0xbe, 0xef}
}

func (m *MockHasher) Reset() {
	m.Buf.Reset()
}

func (m *MockHasher) Size() int {
	return 4
}

func (m *MockHasher) BlockSize() int {
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
