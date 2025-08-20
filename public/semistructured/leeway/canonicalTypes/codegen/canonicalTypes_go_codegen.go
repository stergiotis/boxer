package codegen

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

var CodeGeneratorName = "Leeway CT (" + vcs.ModuleInfo() + ")"

var ErrNotImplemented = eh.Errorf("go code generation not implemtented for given canonical type")

func GenerateGoCode(canonicalType canonicalTypes.PrimitiveAstNodeI, hints encodingaspects.AspectSet) (typeCode string, zeroValueLiteral string, imports []string, err error) {
	switch ct := canonicalType.(type) {
	case canonicalTypes.MachineNumericTypeAstNode:
		typeCode, zeroValueLiteral, imports, err = generateMachineNumericType(ct.BaseType, ct.Width, ct.ByteOrderModifier, ct.ScalarModifier, hints)
		return
	case canonicalTypes.StringAstNode:
		typeCode, zeroValueLiteral, imports, err = generateStringType(ct.BaseType, ct.WidthModifier, ct.Width, ct.ScalarModifier, hints)
		return
	case canonicalTypes.TemporalTypeAstNode:
		typeCode, zeroValueLiteral, imports, err = generateTemporalType(ct.BaseType, ct.Width, ct.ScalarModifier, hints)
		return
	default:
		err = eb.Build().Stringer("canonicalType", canonicalType).Errorf("unable to generate go typeCode for type: %w", ErrNotImplemented)
		return
	}
}

func generateStringType(baseType canonicalTypes.BaseTypeStringE, widthModifier canonicalTypes.WidthModifierE, width canonicalTypes.Width, scalarModifier canonicalTypes.ScalarModifierE, hints encodingaspects.AspectSet) (code string, zeroValueLiteral string, imports []string, err error) {
	switch baseType {
	case canonicalTypes.BaseTypeStringBool:
		code = "bool"
		zeroValueLiteral = "false"
		switch widthModifier {
		case canonicalTypes.WidthModifierNone:
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			switch scalarModifier {
			case canonicalTypes.ScalarModifierNone:
				break
			case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
				code = "[]" + code
				zeroValueLiteral = code + "(nil)"
				break
			default:
				err = common.ErrNotImplemented
			}
		}
		break
	case canonicalTypes.BaseTypeStringBytes, canonicalTypes.BaseTypeStringUtf8:
		if baseType == canonicalTypes.BaseTypeStringUtf8 {
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
		case canonicalTypes.WidthModifierNone:
			break
		case canonicalTypes.WidthModifierFixed:
			if baseType == canonicalTypes.BaseTypeStringBytes {
				code = fmt.Sprintf("[%d]byte", width)
				zeroValueLiteral = fmt.Sprintf("[%d]byte{}", width)
			}
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			switch scalarModifier {
			case canonicalTypes.ScalarModifierNone:
				break
			case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
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

func generateTemporalType(baseTemporal canonicalTypes.BaseTypeTemporalE, width canonicalTypes.Width, scalarModifier canonicalTypes.ScalarModifierE, hints encodingaspects.AspectSet) (code string, zeroValueLiteral string, imports []string, err error) {
	imports = []string{"time"}
	switch baseTemporal {
	case canonicalTypes.BaseTypeTemporalUtcDatetime:
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
	case canonicalTypes.BaseTypeTemporalZonedDatetime:
		err = common.ErrNotImplemented
		break
	case canonicalTypes.BaseTypeTemporalZonedTime:
		err = common.ErrNotImplemented
		break
	default:
		err = common.ErrNotImplemented
	}
	if err == nil {
		switch scalarModifier {
		case canonicalTypes.ScalarModifierNone:
			break
		case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
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

func generateMachineNumericType(baseMachineNumber canonicalTypes.BaseTypeMachineNumericE, width canonicalTypes.Width, byteOrderModifier canonicalTypes.ByteOrderModifierE, scalarModifier canonicalTypes.ScalarModifierE, hints encodingaspects.AspectSet) (code string, zeroValueLiteral string, imports []string, err error) {
	switch baseMachineNumber {
	case canonicalTypes.BaseTypeMachineNumericUnsigned, canonicalTypes.BaseTypeMachineNumericSigned:
		if baseMachineNumber == canonicalTypes.BaseTypeMachineNumericUnsigned {
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
		case canonicalTypes.ScalarModifierNone:
			break
		case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
			code = "[]" + code
			zeroValueLiteral = code + "(nil)"
			break
		default:
			err = common.ErrNotImplemented
		}
		break
	case canonicalTypes.BaseTypeMachineNumericFloat:
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
		case canonicalTypes.ScalarModifierNone:
			break
		case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
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
