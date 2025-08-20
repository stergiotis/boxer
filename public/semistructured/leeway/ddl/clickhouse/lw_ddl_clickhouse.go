package clickhouse

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
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
	case encodingaspects2.AspectDeltaEncoding,
		encodingaspects2.AspectDoubleDeltaEncoding,
		encodingaspects2.AspectLightGeneralCompression,
		encodingaspects2.AspectUltraLightGeneralCompression,
		encodingaspects2.AspectHeavyGeneralCompression,
		encodingaspects2.AspectUltraHeavyGeneralCompression,
		encodingaspects2.AspectInterRecordLowCardinality,
		encodingaspects2.AspectIntraRecordLowCardinality,
		encodingaspects2.AspectLightBiasSmallInteger,
		encodingaspects2.AspectHeavyBiasSmallInteger:
		return common.ImplementationStatusFull, ""
	}
	return common.ImplementationStatusNotImplemented, ""
}
func (inst *TechnologySpecificCodeGenerator) CheckTypeCompatibility(canonicalType canonicalTypes.PrimitiveAstNodeI) (compatible bool, msg string) {
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

func (inst *TechnologySpecificCodeGenerator) GetMembershipSetCanonicalType(s common.MembershipSpecE) (ct1 canonicalTypes.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicalTypes.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, err error) {
	return inst.membershipRepresentation.GetMembershipSetCanonicalType(s)
}

func (inst *TechnologySpecificCodeGenerator) GenerateType(canonicalType canonicalTypes.PrimitiveAstNodeI) (err error) {
	switch ct := canonicalType.(type) {
	case canonicalTypes.MachineNumericTypeAstNode:
		err = inst.generateMachineNumericType(ct.BaseType, ct.Width, ct.ByteOrderModifier, ct.ScalarModifier)
		break
	case canonicalTypes.StringAstNode:
		err = inst.generateStringType(ct.BaseType, ct.WidthModifier, ct.Width, ct.ScalarModifier)
		break
	case canonicalTypes.TemporalTypeAstNode:
		err = inst.generateTemporalType(ct.BaseType, ct.Width, ct.ScalarModifier)
		break
	default:
		err = eb.Build().Stringer("canonicalType", canonicalType).Str("technology", inst.GetTechnology().Name).Type("canonicalType", canonicalType).Errorf("unable to generate ddl code: %w", common.ErrNotImplemented)
	}
	return
}
func (inst *TechnologySpecificCodeGenerator) generateTypeAndCodec(canonicalType canonicalTypes.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, list bool) (err error) {
	lowCard := false
	compr := 0
	delta := 0
	floatc := 0
	biased := 0
	for _, hint := range encodingaspects2.IterateAspects(hints) {
		switch hint {
		case encodingaspects2.AspectUltraLightGeneralCompression:
			compr = max(compr, 1)
			break
		case encodingaspects2.AspectLightGeneralCompression:
			compr = max(compr, 2)
			break
		case encodingaspects2.AspectHeavyGeneralCompression:
			compr = max(compr, 3)
			break
		case encodingaspects2.AspectUltraHeavyGeneralCompression:
			compr = 4
			break
		case encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality:
			lowCard = true
			break
		case encodingaspects2.AspectDeltaEncoding:
			delta = max(delta, 1)
			break
		case encodingaspects2.AspectDoubleDeltaEncoding:
			delta = max(delta, 2)
			break
		case encodingaspects2.AspectUltraLightSlowlyChangingFloat:
			floatc = max(floatc, 1)
			break
		case encodingaspects2.AspectLightSlowlyChangingFloat:
			floatc = max(floatc, 2)
			break
		case encodingaspects2.AspectHeavySlowlyChangingFloat:
			floatc = max(floatc, 3)
			break
		case encodingaspects2.AspectUltraHeavySlowlyChangingFloat:
			floatc = 4
			break
		case encodingaspects2.AspectLightBiasSmallInteger:
			biased = 1
			break
		case encodingaspects2.AspectHeavyBiasSmallInteger:
			biased = 2
			break
		}
	}
	b := inst.codeBuilder
	if list {
		inst.typeProlog = "Array("
		inst.typeEpilog = ")"
	}
	if lowCard {
		inst.typeProlog += "LowCardinality("
		inst.typeEpilog += ")"
	}
	err = inst.GenerateType(canonicalType)
	inst.typeProlog = ""
	inst.typeEpilog = ""
	if err != nil {
		err = eh.Errorf("unable to generate type: %w", err)
		return
	}

	codecChain := make([]string, 1, 6)
	codecChain[0] = " CODEC("
	switch delta {
	case 1:
		codecChain = append(codecChain, "Delta")
		break
	case 2:
		codecChain = append(codecChain, "DoubleDelta")
		break
	}
	switch floatc {
	case 1:
		codecChain = append(codecChain, "FPC(4)")
		break
	case 2:
		codecChain = append(codecChain, "FPC(12)")
		break
	case 3:
		codecChain = append(codecChain, "Gorilla")
		break
	case 4:
		codecChain = append(codecChain, "Gorilla")
		break
	}
	switch biased {
	case 1, 2:
		codecChain = append(codecChain, "T64")
		break
	}
	switch compr {
	case 0:
		codecChain = append(codecChain, "NONE")
		break
	case 1:
		codecChain = append(codecChain, "LZ4(4)")
		break
	case 2:
		codecChain = append(codecChain, "ZSTD(3)")
		break
	case 3:
		codecChain = append(codecChain, "ZSTD(12)")
		break
	case 4:
		codecChain = append(codecChain, "ZSTD(19)")
		break
	}

	if len(codecChain) > 1 {
		codecChain = append(codecChain, ")")
		l := len(codecChain)
		for i, c := range codecChain {
			_, err = b.WriteString(c)
			if err != nil {
				err = eh.Errorf("unable to write to code builder: %w", err)
				return
			}
			if i > 0 && i < l-2 {
				_, err = b.WriteRune(',')
				if err != nil {
					err = eh.Errorf("unable to write to code builder: %w", err)
					return
				}
			}
		}
	}

	return
}
func (inst *TechnologySpecificCodeGenerator) GenerateColumnCode(idx int, phy common.PhysicalColumnDesc) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoCodebuilder
		return
	}
	if idx > 0 {
		_, err = b.WriteString(",\n\t")
	} else {
		_, err = b.WriteRune('\t')
	}
	if err != nil {
		return
	}

	_, err = b.WriteRune('"')
	if err != nil {
		return
	}
	_, err = b.WriteString(phy.GetName()) // FIXME escaping
	if err != nil {
		return
	}
	_, err = b.WriteRune('"')
	if err != nil {
		return
	}
	_, err = b.WriteRune(' ')
	if err != nil {
		return
	}
	var ct canonicalTypes.PrimitiveAstNodeI
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
	err = inst.generateTypeAndCodec(ct, hints, list)
	if err != nil {
		err = eh.Errorf("unable to generate type: %w", err)
		return
	}
	if phy.Comment != "" {
		_, err = b.WriteString("COMMENT '")
		if err != nil {
			return
		}
		_, err = b.WriteString(phy.Comment) // FIXME escaping
		if err != nil {
			return
		}
		_, err = b.WriteRune('\'')
		if err != nil {
			return
		}
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
		Id:   "ClickHouse",
		Name: "ClickHouse",
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
	return inst
}

func (inst *TechnologySpecificCodeGenerator) SetCodeBuilder(s *strings.Builder) {
	inst.codeBuilder = s
}

func (inst *TechnologySpecificCodeGenerator) generateStringType(baseType canonicalTypes.BaseTypeStringE, widthModifier canonicalTypes.WidthModifierE, width canonicalTypes.Width, scalarModifier canonicalTypes.ScalarModifierE) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}

	switch baseType {
	case canonicalTypes.BaseTypeStringBool:
		code := "Bool"
		switch widthModifier {
		case canonicalTypes.WidthModifierNone:
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			code = inst.typeProlog + code + inst.typeEpilog
			switch scalarModifier {
			case canonicalTypes.ScalarModifierNone:
				break
			case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
				code = fmt.Sprintf("Array(%s)", code)
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
	case canonicalTypes.BaseTypeStringBytes, canonicalTypes.BaseTypeStringUtf8:
		code := "String"
		switch widthModifier {
		case canonicalTypes.WidthModifierNone:
			break
		case canonicalTypes.WidthModifierFixed:
			code = fmt.Sprintf("FixedString(%d)", width)
			break
		default:
			err = common.ErrNotImplemented
		}
		if err == nil {
			code = inst.typeProlog + code + inst.typeEpilog
			switch scalarModifier {
			case canonicalTypes.ScalarModifierNone:
				break
			case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
				code = fmt.Sprintf("Array(%s)", code)
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

func (inst *TechnologySpecificCodeGenerator) generateTemporalType(baseTemporal canonicalTypes.BaseTypeTemporalE, width canonicalTypes.Width, scalarModifier canonicalTypes.ScalarModifierE) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	var code string
	switch baseTemporal {
	case canonicalTypes.BaseTypeTemporalUtcDatetime:
		switch width {
		case 32:
			code = "DateTime('UTC')"
			break
		case 64:
			code = "DateTime64(9,'UTC')" // 9 = nanosecond precision
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
		code = inst.typeProlog + code + inst.typeEpilog
		switch scalarModifier {
		case canonicalTypes.ScalarModifierNone:
			break
		case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
			code = fmt.Sprintf("Array(%s)", code)
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

func (inst *TechnologySpecificCodeGenerator) generateMachineNumericType(baseMachineNumber canonicalTypes.BaseTypeMachineNumericE, width canonicalTypes.Width, byteOrderModifier canonicalTypes.ByteOrderModifierE, scalarModifier canonicalTypes.ScalarModifierE) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	var code string
	switch baseMachineNumber {
	case canonicalTypes.BaseTypeMachineNumericUnsigned, canonicalTypes.BaseTypeMachineNumericSigned:
		if baseMachineNumber == canonicalTypes.BaseTypeMachineNumericUnsigned {
			code = "UInt"
		} else {
			code = "Int"
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
		case canonicalTypes.ScalarModifierNone:
			break
		case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
			code = fmt.Sprintf("Array(%s)", code)
			break
		default:
			err = common.ErrNotImplemented
		}
		break
	case canonicalTypes.BaseTypeMachineNumericFloat:
		code = "Float"
		switch width {
		case 32, 64:
			code = fmt.Sprintf("%s%d", code, width)
			break
		default:
			err = common.ErrNotImplemented
		}
		code = inst.typeProlog + code + inst.typeEpilog
		switch scalarModifier {
		case canonicalTypes.ScalarModifierNone:
			break
		case canonicalTypes.ScalarModifierHomogenousArray, canonicalTypes.ScalarModifierSet:
			code = fmt.Sprintf("Array(%s)", code)
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
