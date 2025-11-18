package naturalkey

const (
	SerializationFormatCbor SerializationFormatE = 0
	SerializationFormatJson SerializationFormatE = 1
)

var AllSerializationFormats = []SerializationFormatE{SerializationFormatCbor, SerializationFormatJson}

const (
	encoderStateInitial naturalKeyEncoderStateE = 0
	encoderStateBegun   naturalKeyEncoderStateE = 1
	encoderStateEnded   naturalKeyEncoderStateE = 2
)
const JsonSpecialValuePrefix = "3952d183f4183ad6:"
const taggedIdJsonValuePrefix = "TaggedId:0x"
