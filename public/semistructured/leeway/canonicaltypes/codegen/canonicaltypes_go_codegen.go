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
	case canonicaltypes.NetworkTypeAstNode:
		typeCode, zeroValueLiteral, imports, err = generateNetworkType(ct, hints)
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
			default:
				err = common.ErrNotImplemented
			}
		}
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
			// Fixed-width UTF-8 (sxN): Go has no fixed-width string type, so it
			// maps to the plain `string` set above. This is a deliberate
			// cross-technology divergence — ClickHouse enforces the width via
			// FixedString(N), Go does not (review B-5). u128/256 (no Go builtin)
			// is handled distinctly and errors below.
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
			default:
				err = common.ErrNotImplemented
			}
		}
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
		case 64:
			code = "time.Time"
			zeroValueLiteral = "time.Time{}"
		default:
			err = common.ErrNotImplemented
		}
	case canonicaltypes.BaseTypeTemporalZonedDatetime:
		err = common.ErrNotImplemented
	case canonicaltypes.BaseTypeTemporalZonedTime:
		err = common.ErrNotImplemented
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
		case 8, 16, 32, 64:
			code = fmt.Sprintf("%s%d", code, width)
			zeroValueLiteral = code + "(0)"
		default:
			// 128/256 are valid canonical widths (CH UInt128/256) but Go has no
			// uint128/int256 builtin — emitting them produced uncompilable code
			// with a nil error (review B-2/D-5). Fail loud instead.
			err = common.ErrNotImplemented
		}
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = "[]" + code
			zeroValueLiteral = code + "(nil)"
		default:
			err = common.ErrNotImplemented
		}
	case canonicaltypes.BaseTypeMachineNumericFloat:
		code = "float"
		switch width {
		case 32, 64:
			code = fmt.Sprintf("%s%d", code, width)
			zeroValueLiteral = code + "(0.0)"
		default:
			err = common.ErrNotImplemented
		}
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = "[]" + code
			zeroValueLiteral = code + "(nil)"
		default:
			err = common.ErrNotImplemented
		}
	default:
		err = common.ErrNotImplemented
	}
	if err != nil {
		err = eb.Build().Stringer("baseType", baseMachineNumber).Stringer("width", width).Stringer("byteOrderModifier", byteOrderModifier).Stringer("scalarModifier", scalarModifier).Errorf("%w", err)
		return
	}
	return
}

func generateNetworkType(ct canonicaltypes.NetworkTypeAstNode, hints encodingaspects.AspectSet) (code string, zeroValueLiteral string, imports []string, err error) {
	n := ct.ByteWidth()
	code = fmt.Sprintf("[%d]byte", n)
	zeroValueLiteral = fmt.Sprintf("[%d]byte{}", n)
	switch ct.ScalarModifier {
	case canonicaltypes.ScalarModifierNone:
		break
	case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
		code = "[]" + code
		zeroValueLiteral = code + "(nil)"
	default:
		err = common.ErrNotImplemented
	}
	if err != nil {
		err = eb.Build().Stringer("baseType", ct.BaseType).Stringer("cidrModifier", ct.CIDRModifier).Stringer("scalarModifier", ct.ScalarModifier).Errorf("%w", err)
	}
	return
}
