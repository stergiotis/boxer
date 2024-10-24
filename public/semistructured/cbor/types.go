package cbor

import (
	"hash"
	"time"
)

type BasicEncoder interface {
	EncodeUint(val uint64) (int, error)
	EncodeInt(val int64) (int, error)
	EncodeByteSlice(val []byte) (int, error)
	EncodeCborPayload(val []byte) (int, error)
	EncodeString(val string) (int, error)
	EncodeBool(val bool) (int, error)
	EncodeFloat32(val float32) (int, error)
	EncodeFloat64(val float64) (int, error)
	EncodeTimeUTC(val time.Time) (int, error)
	EncodeArrayDefinite(len uint64) (int, error)
	EncodeMapDefinite(len uint64) (int, error)
	EncodeNil() (int, error)
	Reset()
	SetWriter(dest EncoderWriter)
}
type IndefiniteContainerEncoder interface {
	EncodeMapIndefinite() (int, error)
	EncodeArrayIndefinite() (int, error)
	EncodeBreak() (int, error)
}
type HashingEncoder interface {
	Hash(sum []byte) ([]byte, error)
	SetHasher(hasher hash.Hash)
}
type FullEncoder interface {
	BasicEncoder
	IndefiniteContainerEncoder
	HashingEncoder
}
