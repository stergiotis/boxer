package arrow

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	ddl2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

type TechnologySpecificCodeGenerator struct {
	codeBuilder              *strings.Builder
	typeProlog               string
	typeEpilog               string
	membershipRepresentation common.TechnologySpecificMembershipSetGenI
}

func (inst *TechnologySpecificCodeGenerator) GetEncodingHintImplementationStatus(hint encodingaspects2.AspectE) (status common.ImplementationStatusE, msg string) {
	switch hint {
	case encodingaspects2.AspectInterRecordLowCardinality,
		encodingaspects2.AspectIntraRecordLowCardinality:
		return common.ImplementationStatusFull, ""
	}
	return common.ImplementationStatusNotImplemented, ""
}
func (inst *TechnologySpecificCodeGenerator) CheckTypeCompatibility(canonicalType canonicaltypes.PrimitiveAstNodeI) (compatible bool, msg string) {
	b := inst.codeBuilder
	inst.codeBuilder = &strings.Builder{}
	u := inst.GenerateType(canonicalType)
	compatible = u == nil
	if u != nil {
		msg = u.Error()
	}
	inst.codeBuilder = b
	return
}

func (inst *TechnologySpecificCodeGenerator) ResolveMembership(s common.MembershipSpecE) (ct1 canonicaltypes.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicaltypes.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, cardRole common.ColumnRoleE, err error) {
	return inst.membershipRepresentation.ResolveMembership(s)
}

func (inst *TechnologySpecificCodeGenerator) GenerateType(canonicalType canonicaltypes.PrimitiveAstNodeI) (err error) {
	switch ct := canonicalType.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		err = inst.generateMachineNumericType(ct.BaseType, ct.Width, ct.ByteOrderModifier, ct.ScalarModifier)
		break
	case canonicaltypes.StringAstNode:
		err = inst.generateStringType(ct.BaseType, ct.WidthModifier, ct.Width, ct.ScalarModifier)
		break
	case canonicaltypes.TemporalTypeAstNode:
		err = inst.generateTemporalType(ct.BaseType, ct.Width, ct.ScalarModifier)
		break
	default:
		err = eb.Build().Stringer("canonicalType", canonicalType).Str("technology", inst.GetTechnology().Name).Type("canonicalType", canonicalType).Errorf("unable to generate ddl code: %w", common.ErrNotImplemented)
	}
	return
}
func (inst *TechnologySpecificCodeGenerator) generateTypeAndCodec(canonicalType canonicaltypes.PrimitiveAstNodeI, hints encodingaspects2.AspectSet) (err error) {
	lowCard := false
	for _, hint := range encodingaspects2.IterateAspects(hints) {
		switch hint {
		case encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality:
			lowCard = true
			break
		}
	}
	if lowCard && common.UseArrowDictionaryEncoding {
		inst.typeProlog = "&arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Uint16, ValueType: "
		inst.typeEpilog = "}"
	}
	err = inst.GenerateType(canonicalType)
	inst.typeProlog = ""
	inst.typeEpilog = ""
	if err != nil {
		err = eh.Errorf("unable to generate type: %w", err)
		return
	}

	return
}
func (inst *TechnologySpecificCodeGenerator) GenerateColumnCode(idx int, phy common.PhysicalColumnDesc) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoCodebuilder
		return
	}
	_, err = fmt.Fprintf(b, "\t\t/* %03d */ arrow.Field{Name: %q, Nullable: false, Type: ", idx, phy.String())
	if err != nil {
		return
	}
	var tableRowConfig common.TableRowConfigE
	tableRowConfig, err = phy.GetTableRowConfig()
	if err != nil {
		err = eh.Errorf("unable to get table row config")
		return
	}
	var list bool
	switch tableRowConfig {
	case common.TableRowConfigMultiAttributesPerRow:
		var plainItemType common.PlainItemTypeE
		plainItemType, err = phy.GetPlainItemType()
		if err != nil {
			err = eh.Errorf("unable to get plain item type: %w", err)
			return
		}
		list = plainItemType == common.PlainItemTypeNone
		break
	default:
		err = eb.Build().Stringer("tableRowConfig", tableRowConfig).Errorf("unhandled table row config")
		return
	}
	if list {
		_, err = b.WriteString("arrow.ListOfNonNullable(")
		if err != nil {
			return
		}
	}

	var ct canonicaltypes.PrimitiveAstNodeI
	ct, err = phy.GetCanonicalType()
	if err != nil {
		err = eb.Build().Stringer("column", phy).Errorf("unable to get canonical type from physical column: %w", err)
		return
	}
	var hints encodingaspects2.AspectSet
	hints, err = phy.GetEncodingHints()
	if err != nil {
		err = eb.Build().Stringer("column", phy).Errorf("unable to get encoding hints from physical column: %w", err)
		return
	}
	if list {
		ctScalar := canonicaltypes.DemoteToScalar(ct)
		err = inst.generateTypeAndCodec(ctScalar, hints)
	} else {
		err = inst.generateTypeAndCodec(ct, hints)
	}
	if err != nil {
		err = eh.Errorf("unable to generate type: %w", err)
		return
	}
	if list {
		_, err = b.WriteString(")")
		if err != nil {
			return
		}
	}
	if phy.Comment != "" {
		_, err = b.WriteString(fmt.Sprintf(", Metadata : arrow.NewMetadata([]string{\"comment\"},[]string{%q})", phy.Comment))
		if err != nil {
			return
		}
	}
	_, err = b.WriteString("},\n")
	if err != nil {
		return
	}
	return
}

func (inst *TechnologySpecificCodeGenerator) ResetCodeBuilder() {
	b := inst.codeBuilder
	if b != nil {
		b.Reset()
	}
}

func (inst *TechnologySpecificCodeGenerator) GetCode() (code string, err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	code = b.String()
	return
}

func (inst *TechnologySpecificCodeGenerator) GetTechnology() (tech common.TechnologyDto) {
	return common.TechnologyDto{
		Id:   "Apache_Arrow",
		Name: "Apache Arrow",
	}
}

func NewTechnologySpecificCodeGenerator() (inst *TechnologySpecificCodeGenerator) {
	inst = &TechnologySpecificCodeGenerator{
		codeBuilder:              nil,
		typeProlog:               "",
		typeEpilog:               "",
		membershipRepresentation: nil,
	}
	inst.membershipRepresentation = ddl2.NewCanonicalColumnarRepresentation(ddl2.EncodingAspectFilterFuncFromTechnology(inst, common.ImplementationStatusPartial))
	return
}

func (inst *TechnologySpecificCodeGenerator) SetCodeBuilder(s *strings.Builder) {
	inst.codeBuilder = s
}

func (inst *TechnologySpecificCodeGenerator) generateStringType(baseType canonicaltypes.BaseTypeStringE, widthModifier canonicaltypes.WidthModifierE, width canonicaltypes.Width, scalarModifier canonicaltypes.ScalarModifierE) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}

	switch baseType {
	case canonicaltypes.BaseTypeStringBool:
		code := "&arrow.BooleanType{}"
		switch widthModifier {
		case canonicaltypes.WidthModifierNone:
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			code = inst.typeProlog + code + inst.typeEpilog
			switch scalarModifier {
			case canonicaltypes.ScalarModifierNone:
				break
			case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
				code = fmt.Sprintf("arrow.ListOfNonNullable(%s)", code)
				break
			default:
				err = common.ErrNotImplemented
			}
		}
		if err == nil {
			_, err = b.WriteString(code)
			if err != nil {
				err = eh.Errorf("unable to write to builder: %w", err)
				return
			}
		}
		break
	case canonicaltypes.BaseTypeStringBytes, canonicaltypes.BaseTypeStringUtf8:
		var code string
		if baseType == canonicaltypes.BaseTypeStringUtf8 {
			code = "&arrow.StringType{}"
		} else {
			code = "&arrow.BinaryType{}"
		}
		switch widthModifier {
		case canonicaltypes.WidthModifierNone:
			break
		case canonicaltypes.WidthModifierFixed:
			code = fmt.Sprintf("&arrow.FixedSizeBinaryType{ByteWidth: %d}", width)
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			code = inst.typeProlog + code + inst.typeEpilog
			switch scalarModifier {
			case canonicaltypes.ScalarModifierNone:
				break
			case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
				code = fmt.Sprintf("arrow.ListOfNonNullable(%s)", code)
				break
			default:
				err = common.ErrNotImplemented
			}
		}
		if err == nil {
			_, err = b.WriteString(code)
			if err != nil {
				err = eh.Errorf("unable to write to builder: %w", err)
				return
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

func (inst *TechnologySpecificCodeGenerator) generateTemporalType(baseTemporal canonicaltypes.BaseTypeTemporalE, width canonicaltypes.Width, scalarModifier canonicaltypes.ScalarModifierE) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	var code string
	switch baseTemporal {
	case canonicaltypes.BaseTypeTemporalUtcDatetime:
		switch width {
		case 32:
			code = "&arrow.TimestampType{Unit: arrow.Millisecond}"
			break
		case 64:
			code = "&arrow.TimestampType{Unit: arrow.Nanosecond}"
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
		code = inst.typeProlog + code + inst.typeEpilog
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = fmt.Sprintf("arrow.ListOfNonNullable(%s)", code)
			break
		default:
			err = common.ErrNotImplemented
		}
	}
	if err == nil {
		_, err = b.WriteString(code)
		if err != nil {
			err = eh.Errorf("unable to write to code builder: %w", err)
			return
		}
	} else {
		err = eb.Build().Stringer("baseType", baseTemporal).Stringer("width", width).Stringer("scalarModifier", scalarModifier).Errorf("%w", err)
	}
	return
}

func (inst *TechnologySpecificCodeGenerator) generateMachineNumericType(baseMachineNumber canonicaltypes.BaseTypeMachineNumericE, width canonicaltypes.Width, byteOrderModifier canonicaltypes.ByteOrderModifierE, scalarModifier canonicaltypes.ScalarModifierE) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	var code string
	switch baseMachineNumber {
	case canonicaltypes.BaseTypeMachineNumericUnsigned, canonicaltypes.BaseTypeMachineNumericSigned:
		if baseMachineNumber == canonicaltypes.BaseTypeMachineNumericUnsigned {
			code = "arrow.PrimitiveTypes.Uint"
		} else {
			code = "arrow.PrimitiveTypes.Int"
		}
		switch width {
		case 8, 16, 32, 64, 128, 256:
			code = fmt.Sprintf("%s%d", code, width)
			break
		default:
			err = common.ErrNotImplemented
		}
		code = inst.typeProlog + code + inst.typeEpilog
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = fmt.Sprintf("arrow.ListOfNonNullable(%s)", code)
			break
		default:
			err = common.ErrNotImplemented
		}
		break
	case canonicaltypes.BaseTypeMachineNumericFloat:
		code = "arrow.PrimitiveTypes.Float"
		switch width {
		case 32, 64:
			code = fmt.Sprintf("%s%d", code, width)
			break
		default:
			err = common.ErrNotImplemented
		}
		code = inst.typeProlog + code + inst.typeEpilog
		switch scalarModifier {
		case canonicaltypes.ScalarModifierNone:
			break
		case canonicaltypes.ScalarModifierHomogenousArray, canonicaltypes.ScalarModifierSet:
			code = fmt.Sprintf("arrow.ListOfNonNullable(%s)", code)
			break
		default:
			err = common.ErrNotImplemented
		}
		break
	default:
		err = common.ErrNotImplemented
	}
	if err == nil {
		_, err = b.WriteString(code)
		if err != nil {
			err = eh.Errorf("unable to write to code builder: %w", err)
			return
		}
	} else {
		err = eb.Build().Stringer("baseType", baseMachineNumber).Stringer("width", width).Stringer("byteOrderModifier", byteOrderModifier).Stringer("scalarModifier", scalarModifier).Errorf("%w", err)
	}
	return
}

var _ common.TechnologySpecificGeneratorI = (*TechnologySpecificCodeGenerator)(nil)
