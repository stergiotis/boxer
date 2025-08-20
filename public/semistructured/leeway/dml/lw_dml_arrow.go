package dml

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

func NewArrowValueAdder() *ArrowValueAdder {
	return &ArrowValueAdder{
		s: nil,
	}
}
func GoTypeToArrowType(ct canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet) (prefix string, suffix string, err error) {
	switch ctt := ct.(type) {
	case canonicalTypes2.StringAstNode:
		switch ctt.BaseType {
		case canonicalTypes2.BaseTypeStringUtf8:
			var builderCls string
			builderCls, _, err = CanonicalTypeToArrowBuilderClassName(ct, hints)
			if err != nil {
				err = eh.Errorf("unable to get arrow builder class name: %w", err)
				return
			}
			if builderCls == "BinaryDictionary" {
				prefix = "unsafeperf.UnsafeStringToByte("
				suffix = ")"
			}
			break
		case canonicalTypes2.BaseTypeStringBytes:
			break
		case canonicalTypes2.BaseTypeStringBool:
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		switch ctt.WidthModifier {
		case canonicalTypes2.WidthModifierNone:
			break
		case canonicalTypes2.WidthModifierFixed:
			suffix += "[:]"
			break
		}
		break
	case canonicalTypes2.MachineNumericTypeAstNode:
		switch ctt.BaseType {
		case canonicalTypes2.BaseTypeMachineNumericUnsigned:
			break
		case canonicalTypes2.BaseTypeMachineNumericSigned:
			break
		case canonicalTypes2.BaseTypeMachineNumericFloat:
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		break
	case canonicalTypes2.TemporalTypeAstNode:
		switch ctt.BaseType {
		case canonicalTypes2.BaseTypeTemporalUtcDatetime:
			prefix = "arrow.Date32FromTime("
			suffix = ")"
			break
		case canonicalTypes2.BaseTypeTemporalZonedDatetime:
			prefix = "arrow.Date32FromTime("
			suffix = ")"
			break
		case canonicalTypes2.BaseTypeTemporalZonedTime:
			err = common.ErrNotImplemented
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		break
	default:
		err = eb.Build().Type("type", ct).Errorf("unhandled canonical type")
		return
	}
	return
}
func CanonicalTypeToArrowBuilderClassName(ct canonicalTypes2.PrimitiveAstNodeI, encodingHints encodingaspects.AspectSet) (name string, mayError bool, err error) {
	switch ctt := ct.(type) {
	case canonicalTypes2.StringAstNode:
		switch ctt.BaseType {
		case canonicalTypes2.BaseTypeStringUtf8:
			name = "String"
			break
		case canonicalTypes2.BaseTypeStringBytes:
			name = "Binary"
			break
		case canonicalTypes2.BaseTypeStringBool:
			name = "Boolean"
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		switch ctt.WidthModifier {
		case canonicalTypes2.WidthModifierNone:
			break
		case canonicalTypes2.WidthModifierFixed:
			name = "FixedSize" + name
			break
		}
		break
	case canonicalTypes2.MachineNumericTypeAstNode:
		switch ctt.BaseType {
		case canonicalTypes2.BaseTypeMachineNumericUnsigned:
			name = fmt.Sprintf("Uint%d", ctt.Width)
			break
		case canonicalTypes2.BaseTypeMachineNumericSigned:
			name = fmt.Sprintf("Int%d", ctt.Width)
			break
		case canonicalTypes2.BaseTypeMachineNumericFloat:
			name = fmt.Sprintf("Float%d", ctt.Width)
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		break
	case canonicalTypes2.TemporalTypeAstNode:
		switch ctt.BaseType {
		case canonicalTypes2.BaseTypeTemporalUtcDatetime:
			name = fmt.Sprintf("Date%d", ctt.Width)
			break
		case canonicalTypes2.BaseTypeTemporalZonedDatetime:
			name = fmt.Sprintf("Date%d", ctt.Width)
			break
		case canonicalTypes2.BaseTypeTemporalZonedTime:
			name = fmt.Sprintf("Time%d", ctt.Width)
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		break
	default:
		err = eb.Build().Type("type", ct).Errorf("unhandled canonical type")
		return
	}
	dictEncoding := false
	for _, asp := range encodingHints.IterateAspects() {
		switch asp {
		case encodingaspects.AspectIntraRecordLowCardinality,
			encodingaspects.AspectInterRecordLowCardinality:
			dictEncoding = true
			break
		}
	}
	if dictEncoding && common.UseArrowDictionaryEncoding {
		mayError = true
		switch name {
		case "String":
			name = "BinaryDictionary"
			break
		default:
			name += "Dictionary"
		}
	}
	return
}

func (inst *ArrowValueAdder) SetCodeBuilder(s *strings.Builder) {
	inst.s = s
}

func (inst *ArrowValueAdder) GetCode() (code string, err error) {
	s := inst.s
	if s != nil {
		code = s.String()
	} else {
		err = common.ErrNoCodebuilder
	}
	return
}

func (inst *ArrowValueAdder) ResetCodeBuilder() {
	s := inst.s
	if s != nil {
		s.Reset()
	}
}
