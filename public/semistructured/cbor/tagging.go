package cbor

const MaxTagSmallIncl uint8 = 23

type TagSmall uint8
type TagUint8 uint8
type TagUint16 uint16
type TagUint32 uint32
type TagUint64 uint64

const (
	TagStandardDateTimeString      TagSmall = 0
	TagEpochDateTimeNumber         TagSmall = 1
	TagExpectConversionToBase64Url TagSmall = 21
	TagExpectConversionToBase64Std TagSmall = 22
	TagExpectConversionToHex       TagSmall = 23

	TagEncodedCBORDataItem        TagUint8 = 24
	TagReferenceNthPreviousString TagUint8 = 25
	TagURIValue                   TagUint8 = 32
	TagBase64Url                  TagUint8 = 33
	TagBase64Std                  TagUint8 = 34
	TagRegexp                     TagUint8 = 35
	TagBinaryUUID                 TagUint8 = 37
	TagMultiDimArrayRowMajor      TagUint8 = 40
	TagHomogenousArray            TagUint8 = 41
	TagIPv4                       TagUint8 = 52
	TagIPv6                       TagUint8 = 54
	TagEncodedCBORSequence        TagUint8 = 63
	TagTypedArrayUint8            TagUint8 = 64
	TagTypedArrayUint16BE         TagUint8 = 65
	TagTypedArrayUint32BE         TagUint8 = 66
	TagTypedArrayUint64BE         TagUint8 = 67
	TagTypedArrayUint8Clamped     TagUint8 = 68
	TagTypedArrayUint16LE         TagUint8 = 69
	TagTypedArrayUint32LE         TagUint8 = 70
	TagTypedArrayUint64LE         TagUint8 = 71
	TagTypedArraySint8            TagUint8 = 72
	TagTypedArraySint16BE         TagUint8 = 73
	TagTypedArraySint32BE         TagUint8 = 74
	TagTypedArraySint64BE         TagUint8 = 75
	TagTypedArraySint16LE         TagUint8 = 77
	TagTypedArraySint32LE         TagUint8 = 78
	TagTypedArraySint64LE         TagUint8 = 79
	TagTypedArrayFloat16BE        TagUint8 = 80
	TagTypedArrayFloat32BE        TagUint8 = 81
	TagTypedArrayFloat64BE        TagUint8 = 82
	TagTypedArrayFloat128BE       TagUint8 = 83
	TagTypedArrayFloat16LE        TagUint8 = 84
	TagTypedArrayFloat32LE        TagUint8 = 85
	TagTypedArrayFloat64LE        TagUint8 = 86
	TagTypedArrayFloat128LE       TagUint8 = 87
	TagTextMimeMessage            TagUint8 = 36

	TagBinaryMimeMessage        TagUint16 = 257
	TagMathematicalFiniteSet    TagUint16 = 258
	TagEmbeddedJSON             TagUint16 = 262
	TagHexString                TagUint16 = 263
	TagMapStringKeysOnly        TagUint16 = 275
	TagMultiDimArrayColumnMajor TagUint16 = 1040
	TagSelfDescribedCBOR        TagUint16 = 55799
	TagFileContainsCBORSeq      TagUint16 = 55800
)
