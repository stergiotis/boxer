package codegen

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

var CodeGeneratorName = "Leeway CT (" + vcs.ModuleInfo() + ")"

var ErrNotImplemented = eh.Errorf("go code generation not implemtented for given canonical type")

func GenerateGoCode(canonicalType canonicaltypes.PrimitiveAstNodeI, hints encodingaspects.AspectSet) (typeCode string, zeroValueLiteral string, imports []string, err error) {
	switch ct := canonicalType.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		typeCode, zeroValueLiteral, imports, err = generateMachineNumericType(ct.BaseType, ct.Width, ct.ByteOrderModifier, ct.ScalarModifier, hints)
		return
	case canonicaltypes.StringAstNode:
		typeCode, zeroValueLiteral, imports, err = generateStringType(ct.BaseType, ct.WidthModifier, ct.Width, ct.ScalarModifier, hints)
		return
	case canonicaltypes.TemporalTypeAstNode:
		typeCode, zeroValueLiteral, imports, err = generateTemporalType(ct.BaseType, ct.Width, ct.ScalarModifier, hints)
		return
	default:
		err = eb.Build().Stringer("canonicalType", canonicalType).Errorf("unable to generate go typeCode for type: %w", ErrNotImplemented)
		return
	}
}

func generateStringType(baseType canonicaltypes.BaseTypeStringE, widthModifier canonicaltypes.WidthModifierE, width canonicaltypes.Width, scalarModifier canonicaltypes.ScalarModifierE, hints encodingaspects.AspectSet) (code string, zeroValueLiteral string, imports []string, err error) {
	switch baseType {
	case canonicaltypes.BaseTypeStringBool:
		code = "bool"
		zeroValueLiteral = "false"
		switch widthModifier {
		case canonicaltypes.WidthModifierNone:
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			switch scalarModifier {
			case canonicaltypes.ScalarModifierNone:
				break
			case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
				code = "[]" + code
				zeroValueLiteral = code + "(nil)"
				break
			default:
				err = common.ErrNotImplemented
			}
		}
		break
	case canonicaltypes.BaseTypeStringBytes, canonicaltypes.BaseTypeStringUtf8:
		if baseType == canonicaltypes.BaseTypeStringUtf8 {
			code = "string"
			zeroValueLiteral = "\"\""
		} else {
			code = "[]byte"
			zeroValueLiteral = "[]byte(nil)"
			if common.UseArrowDictionaryEncoding {
				dict := false
				for _, asp := range hints.IterateAspects() {
					dict = dict || asp == encodingaspects.AspectIntraRecordLowCardinality || asp == encodingaspects.AspectInterRecordLowCardinality
				}
				if dict {
					imports = []string{
						"github.com/stergiotis/boxer/public/unsafeperf",
					}
				}
			}
		}
		switch widthModifier {
		case canonicaltypes.WidthModifierNone:
			break
		case canonicaltypes.WidthModifierFixed:
			if baseType == canonicaltypes.BaseTypeStringBytes {
				code = fmt.Sprintf("[%d]byte", width)
				zeroValueLiteral = fmt.Sprintf("[%d]byte{}", width)
			}
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			switch scalarModifier {
			case canonicaltypes.ScalarModifierNone:
				break
			case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
				code = "[]" + code
				zeroValueLiteral = code + "(nil)"
				break
			default:
				err = common.ErrNotImplemented
			}
		}
		break
	default:
		err = common.ErrNotImplemented
	}
	if err != nil {
		err = eb.Build().Stringer("baseType", baseType).Stringer("widthModifier", widthModifier).Stringer("width", width).Stringer("scalarModifier", scalarModifier).Errorf("%w", err)
	}
	return
}

func generateTemporalType(baseTemporal canonicaltypes.BaseTypeTemporalE, width canonicaltypes.Width, scalarModifier canonicaltypes.ScalarModifierE, hints encodingaspects.AspectSet) (code string, zeroValueLiteral string, imports []string, err error) {
	imports = []string{"time"}
	switch baseTemporal {
	case canonicaltypes.BaseTypeTemporalUtcDatetime:
		switch width {
		case 32:
			code = "time.Time"
			zeroValueLiteral = "time.Time{}"
			break
		case 64:
			code = "time.Time"
			zeroValueLiteral = "time.Time{}"
			break
		default:
			err = common.ErrNotImplemented
		}
		break
	case canonicaltypes.BaseTypeTemporalZonedDatetime:
		err = common.ErrNotImplemented
		break
	case canonicaltypes.BaseTypeTemporalZonedTime:
		err = common.ErrNotImplemented
		break
	default:
		err = common.ErrNotImplemented
	}
	if err == nil {
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = "[]" + code
			zeroValueLiteral = code + "(nil)"
			break
		default:
			err = common.ErrNotImplemented
		}
	}
	if err != nil {
		err = eb.Build().Stringer("baseType", baseTemporal).Stringer("width", width).Stringer("scalarModifier", scalarModifier).Errorf("%w", err)
		return
	}
	return
}

func generateMachineNumericType(baseMachineNumber canonicaltypes.BaseTypeMachineNumericE, width canonicaltypes.Width, byteOrderModifier canonicaltypes.ByteOrderModifierE, scalarModifier canonicaltypes.ScalarModifierE, hints encodingaspects.AspectSet) (code string, zeroValueLiteral string, imports []string, err error) {
	switch baseMachineNumber {
	case canonicaltypes.BaseTypeMachineNumericUnsigned, canonicaltypes.BaseTypeMachineNumericSigned:
		if baseMachineNumber == canonicaltypes.BaseTypeMachineNumericUnsigned {
			code = "uint"
		} else {
			code = "int"
		}
		switch width {
		case 8, 16, 32, 64, 128, 256:
			code = fmt.Sprintf("%s%d", code, width)
			zeroValueLiteral = code + "(0)"
			break
		default:
			err = common.ErrNotImplemented
		}
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = "[]" + code
			zeroValueLiteral = code + "(nil)"
			break
		default:
			err = common.ErrNotImplemented
		}
		break
	case canonicaltypes.BaseTypeMachineNumericFloat:
		code = "float"
		switch width {
		case 32, 64:
			code = fmt.Sprintf("%s%d", code, width)
			zeroValueLiteral = code + "(0.0)"
			break
		default:
			err = common.ErrNotImplemented
		}
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = "[]" + code
			zeroValueLiteral = code + "(nil)"
			break
		default:
			err = common.ErrNotImplemented
		}
		break
	default:
		err = common.ErrNotImplemented
	}
	if err != nil {
		err = eb.Build().Stringer("baseType", baseMachineNumber).Stringer("width", width).Stringer("byteOrderModifier", byteOrderModifier).Stringer("scalarModifier", scalarModifier).Errorf("%w", err)
		return
	}
	return
}
