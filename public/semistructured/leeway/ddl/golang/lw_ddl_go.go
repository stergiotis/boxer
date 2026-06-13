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
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	ddl2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// TechnologySpecificCodeGenerator is the Go-struct DDL backend. Its GenerateType
// path (the Go type for a single column) is consumed by the dml / readaccess /
// cli generators and is exercised there. Its GenerateColumnCode path (emitting a
// full struct field per physical column) is an incomplete prototype with no
// in-repo consumers or goldens: it now produces valid identifiers and a valid
// struct tag (review D-1/D-2), but it still does NOT demote non-scalar value
// columns, so a set/array column emits [][]T where ClickHouse/Arrow flatten to a
// single Array level plus a length column. Treat GenerateColumnCode output as
// not production-ready until that demotion is implemented.
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
func (inst *TechnologySpecificCodeGenerator) CheckTypeCompatibility(canonicalType canonicaltypes2.PrimitiveAstNodeI) (compatible bool, msg string) {
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

func (inst *TechnologySpecificCodeGenerator) ResolveMembership(s common.MembershipSpecE) (ct1 canonicaltypes2.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicaltypes2.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, cardRole common.ColumnRoleE, err error) {
	return inst.membershipRepresentation.ResolveMembership(s)
}

func (inst *TechnologySpecificCodeGenerator) GenerateType(canonicalType canonicaltypes2.PrimitiveAstNodeI) (err error) {
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
func (inst *TechnologySpecificCodeGenerator) generateTypeAndCodec(canonicalType canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet) (err error) {
	for _, hint := range encodingaspects2.IterateAspects(hints) {
		switch hint {
		case encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality:
		}
	}
	err = inst.GenerateType(canonicalType)
	if err != nil {
		err = eh.Errorf("unable to generate type: %w", err)
		return
	}

	return
}

type LeewayGoStructTag struct {
	Comment                         string   `json:"comment,omitempty"`
	ColumnNameComponents            []string `json:"columnNameComponents,omitempty"`
	ColumnNameComponentsExplanation []string `json:"columnNameComponentsExplanation,omitempty"`
}

func (inst LeewayGoStructTag) Marshall(w io.Writer) (err error) {
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
	// phy.String() joins the name components with the structural separator
	// (":") so the raw value is not a valid Go identifier (review D-1). Build an
	// exported camel-case identifier from the meaningful components only,
	// skipping the separators (identified via NameComponentsExplanation).
	// Note: this backend still does not demote non-scalar value columns, so a
	// set/array column emits [][]T where ClickHouse/Arrow flatten to one Array
	// level + a length column — see the package-level note.
	meaningful := make([]string, 0, len(phy.NameComponents))
	for i, c := range phy.NameComponents {
		if c == "" {
			continue
		}
		if i < len(phy.NameComponentsExplanation) && phy.NameComponentsExplanation[i] == ddl2.SeparatorExplanation {
			continue
		}
		meaningful = append(meaningful, c)
	}
	name := strcase.ToPascal(strings.Join(meaningful, "_"))
	_, err = b.WriteString(name)
	if err != nil {
		return
	}
	_, err = b.WriteRune(' ')
	if err != nil {
		return
	}
	var ct canonicaltypes2.PrimitiveAstNodeI
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
	{
		// Struct tags are space-separated `key:"value"` pairs. The previous
		// emission used a comma after the cbor value and wrote the leeway value
		// unquoted, producing a malformed (and tag-swallowing, because the
		// comment was emitted first) struct tag (review D-2). Emit a valid,
		// space-separated, quoted tag; the trailing comment comes after it.
		_, err = b.WriteString(" `cbor:\"")
		if err != nil {
			return
		}
		_, err = b.WriteString(strcase.ToCamel(name))
		if err != nil {
			return
		}
		_, err = b.WriteRune('"')
		if err != nil {
			return
		}
		tag := LeewayGoStructTag{
			ColumnNameComponents:            phy.NameComponents,
			ColumnNameComponentsExplanation: phy.NameComponentsExplanation,
			Comment:                         phy.Comment,
		}
		tagBuf := bytes.NewBuffer(make([]byte, 0, 256))
		err = tag.Marshall(tagBuf)
		if err != nil {
			return
		}
		// tag.Marshall already emits a JSON-quoted string (a valid Go struct-tag
		// value); just trim the encoder's trailing newline so it stays on one
		// line.
		_, err = b.WriteString(" leeway:" + strings.TrimSpace(tagBuf.String()))
		if err != nil {
			return
		}
		_, err = b.WriteRune('`')
		if err != nil {
			return
		}
	}

	if phy.Comment != "" {
		// Strip newlines so the comment stays on the field's line.
		_, err = b.WriteString(" // " + strings.ReplaceAll(phy.Comment, "\n", " "))
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
