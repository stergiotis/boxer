package cbor

import (
	"hash"
	"time"
)

type PrimitiveEncoder interface {
	EncodeUint(val uint64) (int, error)
	EncodeInt(val int64) (int, error)
	EncodeByteSlice(val []byte) (int, error)
	EncodeString(val string) (int, error)
	EncodeBool(val bool) (int, error)
	EncodeFloat32(val float32) (int, error)
	EncodeFloat64(val float64) (int, error)
	EncodeTimeUTC(val time.Time) (int, error)
	EncodeNil() (int, error)
}
type DefiniteContainerEncoder interface {
	EncodeArrayDefinite(length uint64) (int, error)
	EncodeMapDefinite(length uint64) (int, error)
}
type ResetableEncoder interface {
	Reset()
	SetWriter(dest EncoderWriter)
}
type CborPayloadEncoder interface {
	EncodeCborPayload(val []byte) (int, error)
}
type BasicEncoder interface {
	PrimitiveEncoder
	DefiniteContainerEncoder
	ResetableEncoder
	CborPayloadEncoder
}
type IndefiniteContainerEncoder interface {
	EncodeMapIndefinite() (int, error)
	EncodeArrayIndefinite() (int, error)
	EncodeBreak() (int, error)
}
type TagEncoder interface {
	EncodeTagSmall(tagSmall TagSmall) (int, error)
	EncodeTag8(tagUint8 TagUint8) (int, error)
	EncodeTag16(tagUint16 TagUint16) (int, error)
	EncodeTag32(tagUint32 TagUint32) (int, error)
	EncodeTag64(tagUint64 TagUint64) (int, error)
}
type HashingEncoder interface {
	Hash(sum []byte) ([]byte, error)
	SetHasher(hasher hash.Hash)
}
type FullEncoder interface {
	BasicEncoder
	IndefiniteContainerEncoder
	HashingEncoder
	TagEncoder
}
type PositionerI interface {
	GetPosition() uint64
}
type CloneTemporaryI interface {
	CloneTemporary(temporary []byte) []byte
}
type CurrentBufferI interface {
	PositionerI
	GetTemporaryData(posBeginIncl uint64, posEndExcl uint64) (temporary []byte)
	InvalidateTemporary()
}
