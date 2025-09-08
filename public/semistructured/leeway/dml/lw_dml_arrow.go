package dml

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

func GoTypeToArrowType(ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet) (prefix string, suffix string, err error) {
	switch ctt := ct.(type) {
	case canonicaltypes2.StringAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeStringUtf8:
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
		case canonicaltypes2.BaseTypeStringBytes:
			break
		case canonicaltypes2.BaseTypeStringBool:
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		switch ctt.WidthModifier {
		case canonicaltypes2.WidthModifierNone:
			break
		case canonicaltypes2.WidthModifierFixed:
			suffix += "[:]"
			break
		}
		break
	case canonicaltypes2.MachineNumericTypeAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeMachineNumericUnsigned:
			break
		case canonicaltypes2.BaseTypeMachineNumericSigned:
			break
		case canonicaltypes2.BaseTypeMachineNumericFloat:
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		break
	case canonicaltypes2.TemporalTypeAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeTemporalUtcDatetime:
			prefix = "arrow.Date32FromTime("
			suffix = ")"
			break
		case canonicaltypes2.BaseTypeTemporalZonedDatetime:
			prefix = "arrow.Date32FromTime("
			suffix = ")"
			break
		case canonicaltypes2.BaseTypeTemporalZonedTime:
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
func CanonicalTypeToArrowBuilderClassName(ct canonicaltypes2.PrimitiveAstNodeI, encodingHints encodingaspects.AspectSet) (name string, mayError bool, err error) {
	switch ctt := ct.(type) {
	case canonicaltypes2.StringAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeStringUtf8:
			name = "String"
			break
		case canonicaltypes2.BaseTypeStringBytes:
			name = "Binary"
			break
		case canonicaltypes2.BaseTypeStringBool:
			name = "Boolean"
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		switch ctt.WidthModifier {
		case canonicaltypes2.WidthModifierNone:
			break
		case canonicaltypes2.WidthModifierFixed:
			name = "FixedSize" + name
			break
		}
		break
	case canonicaltypes2.MachineNumericTypeAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeMachineNumericUnsigned:
			name = fmt.Sprintf("Uint%d", ctt.Width)
			break
		case canonicaltypes2.BaseTypeMachineNumericSigned:
			name = fmt.Sprintf("Int%d", ctt.Width)
			break
		case canonicaltypes2.BaseTypeMachineNumericFloat:
			name = fmt.Sprintf("Float%d", ctt.Width)
			break
		default:
			err = eb.Build().Stringer("baseType", ctt.BaseType).Errorf("unhandled base type")
			return
		}
		break
	case canonicaltypes2.TemporalTypeAstNode:
		switch ctt.BaseType {
		case canonicaltypes2.BaseTypeTemporalUtcDatetime:
			name = fmt.Sprintf("Date%d", ctt.Width)
			break
		case canonicaltypes2.BaseTypeTemporalZonedDatetime:
			name = fmt.Sprintf("Date%d", ctt.Width)
			break
		case canonicaltypes2.BaseTypeTemporalZonedTime:
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
