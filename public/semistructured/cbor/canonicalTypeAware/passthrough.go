package canonicalTypeAware

import (
	"hash"
	"time"

	"github.com/stergiotis/boxer/public/semistructured/cbor"
)

type PassthroughEncoder[T cbor.FullEncoderI] struct {
	delegate T
}

func (inst *PassthroughEncoder[T]) EncodeStringRef(val string) (int, error) {
	return inst.delegate.EncodeString(val)
}

func (inst *PassthroughEncoder[T]) EncodeUint(val uint64) (int, error) {
	return inst.delegate.EncodeUint(val)
}

func (inst *PassthroughEncoder[T]) EncodeInt(val int64) (int, error) {
	return inst.delegate.EncodeInt(val)
}

func (inst *PassthroughEncoder[T]) EncodeByteSlice(val []byte) (int, error) {
	return inst.delegate.EncodeByteSlice(val)
}

func (inst *PassthroughEncoder[T]) EncodeCborPayload(val []byte) (int, error) {
	return inst.delegate.EncodeCborPayload(val)
}

func (inst *PassthroughEncoder[T]) EncodeString(val string) (int, error) {
	return inst.delegate.EncodeString(val)
}

func (inst *PassthroughEncoder[T]) EncodeBool(val bool) (int, error) {
	return inst.delegate.EncodeBool(val)
}

func (inst *PassthroughEncoder[T]) EncodeFloat32(val float32) (int, error) {
	return inst.delegate.EncodeFloat32(val)
}

func (inst *PassthroughEncoder[T]) EncodeFloat64(val float64) (int, error) {
	return inst.delegate.EncodeFloat64(val)
}

func (inst *PassthroughEncoder[T]) EncodeTimeUTC(val time.Time) (int, error) {
	return inst.delegate.EncodeTimeUTC(val)
}

func (inst *PassthroughEncoder[T]) EncodeArrayDefinite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeMapDefinite(len uint64) (int, error) {
	return inst.delegate.EncodeMapDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeNil() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) Reset() {
	inst.delegate.Reset()
}

func (inst *PassthroughEncoder[T]) SetWriter(dest cbor.EncoderWriter) {
	inst.delegate.SetWriter(dest)
}

func (inst *PassthroughEncoder[T]) EncodeMapIndefinite() (int, error) {
	return inst.delegate.EncodeMapIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeArrayIndefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeBreak() (int, error) {
	return inst.delegate.EncodeBreak()
}

func (inst *PassthroughEncoder[T]) Hash(sum []byte) ([]byte, error) {
	return inst.delegate.Hash(sum)
}

func (inst *PassthroughEncoder[T]) SetHasher(hasher hash.Hash) {
	inst.delegate.SetHasher(hasher)
}

func (inst *PassthroughEncoder[T]) EncodeUint8(val uint8) (int, error) {
	return inst.delegate.EncodeUint(uint64(val))
}

func (inst *PassthroughEncoder[T]) EncodeUint16(val uint16) (int, error) {
	return inst.delegate.EncodeUint(uint64(val))
}

func (inst *PassthroughEncoder[T]) EncodeUint32(val uint32) (int, error) {
	return inst.delegate.EncodeUint(uint64(val))
}

func (inst *PassthroughEncoder[T]) EncodeUint64(val uint64) (int, error) {
	return inst.delegate.EncodeUint(uint64(val))
}

func (inst *PassthroughEncoder[T]) EncodeInt8(val int8) (int, error) {
	return inst.delegate.EncodeInt(int64(val))
}

func (inst *PassthroughEncoder[T]) EncodeInt16(val int16) (int, error) {
	return inst.delegate.EncodeInt(int64(val))
}

func (inst *PassthroughEncoder[T]) EncodeInt32(val int32) (int, error) {
	return inst.delegate.EncodeInt(int64(val))
}

func (inst *PassthroughEncoder[T]) EncodeInt64(val int64) (int, error) {
	return inst.delegate.EncodeInt(int64(val))
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint8Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint16Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint32Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint64Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt8Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt16Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt32Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt64Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayFloat32Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayFloat64Definite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayTimeDefinite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayBoolDefinite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayStringDefinite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayByteSliceDefinite(len uint64) (int, error) {
	return inst.delegate.EncodeArrayDefinite(len)
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint8Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint16Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint32Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayUint64Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt8Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt16Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt32Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayInt64Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayFloat32Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayFloat64Indefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayTimeIndefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayBoolIndefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayStringIndefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeTypedArrayByteSliceIndefinite() (int, error) {
	return inst.delegate.EncodeArrayIndefinite()
}

func (inst *PassthroughEncoder[T]) EncodeNilUint8() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilUint16() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilUint32() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilUint64() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilInt8() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilInt16() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilInt32() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilInt64() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilBool() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilFloat32() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilFloat64() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilTimeUTC() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilString() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeNilByteSlice() (int, error) {
	return inst.delegate.EncodeNil()
}

func (inst *PassthroughEncoder[T]) EncodeTagSmall(tagSmall cbor.TagSmall) (int, error) {
	return inst.delegate.EncodeTagSmall(tagSmall)
}
func (inst *PassthroughEncoder[T]) EncodeTag8(tagUint8 cbor.TagUint8) (int, error) {
	return inst.delegate.EncodeTag8(tagUint8)
}
func (inst *PassthroughEncoder[T]) EncodeTag16(tagUint16 cbor.TagUint16) (int, error) {
	return inst.delegate.EncodeTag16(tagUint16)
}
func (inst *PassthroughEncoder[T]) EncodeTag32(tagUint32 cbor.TagUint32) (int, error) {
	return inst.delegate.EncodeTag32(tagUint32)
}
func (inst *PassthroughEncoder[T]) EncodeTag64(tagUint64 cbor.TagUint64) (int, error) {
	return inst.delegate.EncodeTag64(tagUint64)
}

var _ EncoderI = (*PassthroughEncoder[*cbor.Encoder])(nil)
var _ FullEncoderI = (*PassthroughEncoder[*cbor.Encoder])(nil)

func NewPassthroughEncoder[T cbor.FullEncoderI](delegate T) *PassthroughEncoder[T] {
	return &PassthroughEncoder[T]{
		delegate: delegate,
	}
}
