package canonicalTypes

const GroupSeparator = "-"
const SignatureSeparator = "_"

const (
	BaseTypeStringNone  BaseTypeStringE = 0
	BaseTypeStringUtf8  BaseTypeStringE = 's'
	BaseTypeStringBytes BaseTypeStringE = 'y'
	BaseTypeStringBool  BaseTypeStringE = 'b'
)

func (inst BaseTypeStringE) String() string {
	switch inst {
	case BaseTypeStringNone:
		return "<none>"
	}
	return string(inst)
}

const (
	BaseTypeMachineNumericNone     BaseTypeMachineNumericE = 0
	BaseTypeMachineNumericUnsigned BaseTypeMachineNumericE = 'u'
	BaseTypeMachineNumericSigned   BaseTypeMachineNumericE = 'i'
	BaseTypeMachineNumericFloat    BaseTypeMachineNumericE = 'f'
)

func (inst BaseTypeMachineNumericE) String() string {
	switch inst {
	case BaseTypeMachineNumericNone:
		return "<none>"
	}
	return string(inst)
}

const (
	BaseTypeTemporalNone          BaseTypeTemporalE = 0
	BaseTypeTemporalUtcDatetime   BaseTypeTemporalE = 'z'
	BaseTypeTemporalZonedDatetime BaseTypeTemporalE = 'd'
	BaseTypeTemporalZonedTime     BaseTypeTemporalE = 't'
)

func (inst BaseTypeTemporalE) String() string {
	switch inst {
	case BaseTypeTemporalNone:
		return "<none>"
	}
	return string(inst)
}

const (
	ScalarModifierNone            ScalarModifierE = 0
	ScalarModifierHomogenousArray ScalarModifierE = 'h'
	ScalarModifierSet             ScalarModifierE = 'm'
)

func (inst ScalarModifierE) String() string {
	switch inst {
	case ScalarModifierNone:
		return "<none>"
	}
	return string(inst)
}

const (
	ByteOrderModifierNone         ByteOrderModifierE = 0
	ByteOrderModifierLittleEndian ByteOrderModifierE = 'l'
	ByteOrderModifierBigEndian    ByteOrderModifierE = 'n'
)

func (inst ByteOrderModifierE) String() string {
	switch inst {
	case ByteOrderModifierNone:
		return "<none>"
	}
	return string(inst)
}

const (
	WidthModifierNone  WidthModifierE = 0
	WidthModifierFixed WidthModifierE = 'x'
)

func (inst WidthModifierE) String() string {
	switch inst {
	case WidthModifierNone:
		return "<none>"
	}
	return string(inst)
}
