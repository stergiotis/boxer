package golang

import (
	"bytes"
	"io"
	"strings"

	"github.com/ettle/strcase"
	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	ddl2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

type TechnologySpecificCodeGenerator struct {
	codeBuilder              *strings.Builder
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
func (inst *TechnologySpecificCodeGenerator) CheckTypeCompatibility(canonicalType canonicalTypes2.PrimitiveAstNodeI) (compatible bool, msg string) {
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

func (inst *TechnologySpecificCodeGenerator) GetMembershipSetCanonicalType(s common.MembershipSpecE) (ct1 canonicalTypes2.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicalTypes2.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, err error) {
	return inst.membershipRepresentation.GetMembershipSetCanonicalType(s)
}

func (inst *TechnologySpecificCodeGenerator) GenerateType(canonicalType canonicalTypes2.PrimitiveAstNodeI) (err error) {
	b := inst.codeBuilder
	if b == nil {
		err = common.ErrNoCodebuilder
		return
	}
	var typeCode string
	typeCode, _, _, err = codegen.GenerateGoCode(canonicalType, encodingaspects2.EmptyAspectSet) // TODO pass encoding aspects (only needed for imports, but you never know...)
	if err != nil {
		err = eb.Build().Stringer("canonicalType", canonicalType).Str("technology", inst.GetTechnology().Name).Type("canonicalType", canonicalType).Errorf("unable to generate ddl code: %w", err)
		return
	}
	_, err = b.WriteString(typeCode)
	return
}
func (inst *TechnologySpecificCodeGenerator) generateTypeAndCodec(canonicalType canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet) (err error) {
	for _, hint := range encodingaspects2.IterateAspects(hints) {
		switch hint {
		case encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality:
			break
		}
	}
	err = inst.GenerateType(canonicalType)
	if err != nil {
		err = eh.Errorf("unable to generate type: %w", err)
		return
	}

	return
}

type Et7GoStructTag struct {
	ColumnNameComponents            []string `json:"columnNameComponents,omitempty"`
	ColumnNameComponentsExplanation []string `json:"columnNameComponentsExplanation,omitempty"`
	Comment                         string   `json:"comment,omitempty"`
}

func (inst Et7GoStructTag) Marshall(w io.Writer) (err error) {
	s := bytes.NewBuffer(make([]byte, 0, 4096))
	enc1 := jsontext.NewEncoder(s,
		jsontext.EscapeForHTML(false),
		jsontext.EscapeForJS(false))
	err = json.MarshalEncode(enc1,
		&inst,
		json.DefaultOptionsV2())
	if err != nil {
		err = eh.Errorf("unable to encode instance to json: %w", err)
		return
	}
	enc := jsontext.NewEncoder(w,
		jsontext.EscapeForHTML(false),
		jsontext.EscapeForJS(false))
	err = json.MarshalEncode(enc,
		s.String(),
		json.DefaultOptionsV2())
	if err != nil {
		err = eh.Errorf("unable to escape json string: %w", err)
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
	_, err = b.WriteRune('\t')
	if err != nil {
		return
	}
	name := strcase.ToPascal(phy.GetName())
	_, err = b.WriteString(name)
	if err != nil {
		return
	}
	_, err = b.WriteRune(' ')
	if err != nil {
		return
	}
	var ct canonicalTypes2.PrimitiveAstNodeI
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
		list = true
		break
	default:
		err = eb.Build().Stringer("tableRowConfig", tableRowConfig).Errorf("unhandled table row config")
		return
	}
	if list {
		_, err = b.WriteString("[]")
		if err != nil {
			return
		}
	}
	err = inst.generateTypeAndCodec(ct, hints)
	if err != nil {
		err = eh.Errorf("unable to generate type: %w", err)
		return
	}
	if phy.Comment != "" {
		_, err = b.WriteString("// ")
		if err != nil {
			return
		}
		_, err = b.WriteString(phy.Comment) // FIXME escaping
		if err != nil {
			return
		}
	}
	{
		_, err = b.WriteString(" `cbor:\"")
		if err != nil {
			return
		}
		_, err = b.WriteString(strcase.ToCamel(name))
		if err != nil {
			return
		}
		_, err = b.WriteString("\",et7:")
		if err != nil {
			return
		}
		tag := Et7GoStructTag{
			ColumnNameComponents:            phy.NameComponents,
			ColumnNameComponentsExplanation: phy.NameComponentsExplanation,
			Comment:                         phy.Comment,
		}
		err = tag.Marshall(b)
		if err != nil {
			return
		}
		_, err = b.WriteRune('`')
		if err != nil {
			return
		}
	}

	_, err = b.WriteRune('\n')
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
		Id:   "golang",
		Name: "Golang",
	}
}

func NewTechnologySpecificCodeGenerator() (inst *TechnologySpecificCodeGenerator) {
	inst = &TechnologySpecificCodeGenerator{
		codeBuilder:              nil,
		membershipRepresentation: nil,
	}
	inst.membershipRepresentation = ddl2.NewCanonicalColumnarRepresentation(ddl2.EncodingAspectFilterFuncFromTechnology(inst, common.ImplementationStatusPartial))
	return
}

func (inst *TechnologySpecificCodeGenerator) SetCodeBuilder(s *strings.Builder) {
	inst.codeBuilder = s
}

var _ common.TechnologySpecificGeneratorI = (*TechnologySpecificCodeGenerator)(nil)
