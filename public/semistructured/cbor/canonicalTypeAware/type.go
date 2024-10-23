package canonicalTypeAware

import "github.com/stergiotis/boxer/public/semistructured/cbor"

type CanonicalTypeAwareEncoder interface {
	EncodeUint8(val uint8) (int, error)
	EncodeUint16(val uint16) (int, error)
	EncodeUint32(val uint32) (int, error)
	EncodeUint64(val uint64) (int, error)
	EncodeInt8(val int8) (int, error)
	EncodeInt16(val int16) (int, error)
	EncodeInt32(val int32) (int, error)
	EncodeInt64(val int64) (int, error)
	EncodeTypedArrayUint8Definite(len uint64) (int, error)
	EncodeTypedArrayUint16Definite(len uint64) (int, error)
	EncodeTypedArrayUint32Definite(len uint64) (int, error)
	EncodeTypedArrayUint64Definite(len uint64) (int, error)
	EncodeTypedArrayInt8Definite(len uint64) (int, error)
	EncodeTypedArrayInt16Definite(len uint64) (int, error)
	EncodeTypedArrayInt32Definite(len uint64) (int, error)
	EncodeTypedArrayInt64Definite(len uint64) (int, error)
	EncodeTypedArrayFloat32Definite(len uint64) (int, error)
	EncodeTypedArrayFloat64Definite(len uint64) (int, error)
	EncodeTypedArrayTimeDefinite(len uint64) (int, error)
	EncodeTypedArrayBoolDefinite(len uint64) (int, error)
	EncodeTypedArrayStringDefinite(len uint64) (int, error)
	EncodeTypedArrayByteSliceDefinite(len uint64) (int, error)
	EncodeTypedArrayUint8Indefinite() (int, error)
	EncodeTypedArrayUint16Indefinite() (int, error)
	EncodeTypedArrayUint32Indefinite() (int, error)
	EncodeTypedArrayUint64Indefinite() (int, error)
	EncodeTypedArrayInt8Indefinite() (int, error)
	EncodeTypedArrayInt16Indefinite() (int, error)
	EncodeTypedArrayInt32Indefinite() (int, error)
	EncodeTypedArrayInt64Indefinite() (int, error)
	EncodeTypedArrayFloat32Indefinite() (int, error)
	EncodeTypedArrayFloat64Indefinite() (int, error)
	EncodeTypedArrayTimeIndefinite() (int, error)
	EncodeTypedArrayBoolIndefinite() (int, error)
	EncodeTypedArrayStringIndefinite() (int, error)
	EncodeTypedArrayByteSliceIndefinite() (int, error)
	EncodeNilUint8() (int, error)
	EncodeNilUint16() (int, error)
	EncodeNilUint32() (int, error)
	EncodeNilUint64() (int, error)
	EncodeNilInt8() (int, error)
	EncodeNilInt16() (int, error)
	EncodeNilInt32() (int, error)
	EncodeNilInt64() (int, error)
	EncodeNilBool() (int, error)
	EncodeNilFloat32() (int, error)
	EncodeNilFloat64() (int, error)
	EncodeNilTimeUTC() (int, error)
	EncodeNilString() (int, error)
	EncodeNilByteSlice() (int, error)
}
type FullEncoder interface {
	cbor.FullEncoder
	CanonicalTypeAwareEncoder
}
