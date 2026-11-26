package applink

import (
	"crypto/rand"
	"encoding/hex"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// HandleBytes is the length of a handle: BLAKE3 in its keyed mode, 256
// bits (ADR-0409 §SD7).
const HandleBytes = 32

// Handle names a record the host keeps. It is the keyed digest of the
// record's canonical CBOR, so the same record gets the same handle and
// nobody without the process's key can compute one.
type Handle [HandleBytes]byte

// String is the handle as a link spells it: lower-case hex.
func (inst Handle) String() string {
	return hex.EncodeToString(inst[:])
}

// IsZero says the handle is unset.
func (inst Handle) IsZero() bool {
	return inst == Handle{}
}

// ParseHandle reads a handle's lower-case hex.
func ParseHandle(s string) (h Handle, err error) {
	if len(s) != 2*HandleBytes {
		err = eb.Build().Int("chars", len(s)).Int("want", 2*HandleBytes).Errorf("applink: a handle is 64 hex characters")
		return
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			err = eb.Build().Errorf("applink: a handle is lower-case hex")
			return
		}
	}
	_, err = hex.Decode(h[:], []byte(s))
	return
}

// Key is the key handles are minted under: drawn once per process, never
// stored, so a handle means nothing in another process.
type Key [32]byte

// NewKey draws a key.
func NewKey() (k Key, err error) {
	_, err = rand.Read(k[:])
	return
}

// Mint is the handle of a record's canonical bytes under key.
func Mint(key Key, record []byte) (h Handle) {
	hs := blake3.New(HandleBytes, key[:])
	_, _ = hs.Write(record)
	copy(h[:], hs.Sum(nil))
	return
}
