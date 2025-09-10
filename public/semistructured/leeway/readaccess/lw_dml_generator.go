package readaccess

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

var CodeGeneratorName = "Leeway readaccess (" + vcs.ModuleInfo() + ")"

type GoClassBuilder struct {
	builder *strings.Builder
	tech    *golang.TechnologySpecificCodeGenerator
}

func NewGoClassBuilder() *GoClassBuilder {
	return &GoClassBuilder{
		builder: nil,
		tech:    golang.NewTechnologySpecificCodeGenerator(),
	}
}

func (inst *GoClassBuilder) SetCodeBuilder(s *strings.Builder) {
	inst.builder = s
	inst.tech.SetCodeBuilder(s)
}

func (inst *GoClassBuilder) GetCode() (code string, err error) {
	b := inst.builder
	if b == nil {
		err = common.ErrNoCodebuilder
		return
	}
	code = b.String()
	return
}

func (inst *GoClassBuilder) ResetCodeBuilder() {
	b := inst.builder
	if b != nil {
		b.Reset()
	}
}
func (inst *GoClassBuilder) ComposeNamingConventionDependentCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer gocodegen.GoClassNamerI) (err error) {
	b := inst.builder
	err = gocodegen.GenerateArrowSchemaFactory(b, tableName, ir, namingConvention, tableRowConfig, clsNamer)
	if err != nil {
		err = eh.Errorf("unable to generate schema factory: %w", err)
	}
	return
}

func (inst *GoClassBuilder) ComposeAttributeClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}

func (inst *GoClassBuilder) ComposeSectionClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}

func (inst *GoClassBuilder) ComposeAttributeCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}

func (inst *GoClassBuilder) ComposeSectionCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}
func (inst *GoClassBuilder) ComposeEntityClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	b := inst.builder
	var clsNames gocodegen.ClassNames
	clsNames, err = gocodegen.NewClassNamesEntityOnly(clsNamer, tableName)
	if err != nil {
		err = eh.Errorf("unable to generate class names: %w", err)
		return
	}

	plainIRH := entityIRH.DeriveSubHolder(deriveSubHolderSelectPlainValues)
	for _, pt := range common.AllPlainItemTypes {
		if pt == common.PlainItemTypeNone {
			continue
		}
		irh := plainIRH.DeriveSubHolder(func(cc common.IntermediateColumnContext) (keep bool) {
			return cc.PlainItemType == pt
		})
		if irh.Length() == 0 {
			continue
		}
		clsName := clsNames.InEntityClassName + "Plain" + naming.MustBeValidStylableName(pt.String()).Convert(naming.UpperCamelCase).String()
		_, err = fmt.Fprintf(b, `type %s struct {
`, clsName)
		if err != nil {
			return
		}
		for cc, cp := range irh.IterateColumnProps() {
			if cc.SubType == common.IntermediateColumnsSubTypeScalar {
				for i, name := range cp.Names {
					ct := cp.CanonicalType[i]
					var typeName string
					typeName, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct, cp.EncodingHints[i], common.UseArrowDictionaryEncoding)
					if err != nil {
						err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
						return
					}
					fieldName := fmt.Sprintf("%sValues", name.Convert(naming.UpperCamelCase).String())
					_, err = fmt.Fprintf(b, "\t%s *array.%s\n\t%sColumnIndex uint32\n",
						fieldName,
						typeName,
						fieldName,
					)
					if err != nil {
						return
					}
				}
			}
		}
		_, err = fmt.Fprintf(b, `
}
`)
		if err != nil {
			return
		}

		{ // factory
			_, err = fmt.Fprintf(b, `func New%s() *%s {
	return &%s{
`, clsName, clsName, clsName)
			if err != nil {
				return
			}
			for cc, cp := range irh.IterateColumnProps() {
				if cc.SubType == common.IntermediateColumnsSubTypeScalar {
					for i, name := range cp.Names {
						fieldName := fmt.Sprintf("%sValues", name.Convert(naming.UpperCamelCase).String())
						_, err = fmt.Fprintf(b, "\t\t%sColumnIndex: %d,\n",
							fieldName,
							cc.IndexOffset+uint32(i),
						)
						if err != nil {
							return
						}
					}
				}
			}
			_, err = fmt.Fprintf(b, `
	}
}
`)
			if err != nil {
				return
			}

		}
		{ // Reset
			_, err = fmt.Fprintf(b, `func (inst *%s) Reset() {
`, clsName)
			if err != nil {
				return
			}
			for cc, cp := range irh.IterateColumnProps() {
				if cc.SubType == common.IntermediateColumnsSubTypeScalar {
					for _, name := range cp.Names {
						fieldName := fmt.Sprintf("%sValues", name.Convert(naming.UpperCamelCase).String())
						_, err = fmt.Fprintf(b, "\tinst.%s = nil\n", fieldName)
						if err != nil {
							return
						}
					}
				}
			}
			_, err = fmt.Fprintf(b, `
}
`)
			if err != nil {
				return
			}
		}
		{ // LoadFromRecord
			_, err = fmt.Fprintf(b, `func (inst *%s) LoadFromRecord(rec arrow.Record) (err error) {
`, clsName)
			if err != nil {
				return
			}
			for cc, cp := range irh.IterateColumnProps() {
				if cc.SubType == common.IntermediateColumnsSubTypeScalar {
					for i, name := range cp.Names {
						ct := cp.CanonicalType[i]
						var typeName string
						typeName, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct, cp.EncodingHints[i], common.UseArrowDictionaryEncoding)
						if err != nil {
							err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
							return
						}
						typeId := naming.MustBeValidStylableName(typeName).Convert(naming.UpperSnakeCase)
						fieldName := fmt.Sprintf("%sValues", name.Convert(naming.UpperCamelCase).String())
						_, err = fmt.Fprintf(b, `	{
		c := rec.Column(int(inst.%sColumnIndex))
		if c.DataType().ID() != arrow.%s {
			err = runtime.ErrUnexpectedArrowDataType
			return
		}
		inst.%s = array.New%sData(c.Data())
	}`,
							fieldName,
							typeId,
							fieldName,
							typeName,
						)
					}
				}
			}

			_, err = fmt.Fprintf(b, `
	return
}
`)
			if err != nil {
				return
			}
		}
	}

	return
}
func (inst *GoClassBuilder) ComposeEntityCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	return
}
func (inst *GoClassBuilder) ComposeGoImports(ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE) (err error) {
	b := inst.builder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	imports := containers.NewHashSet[string](32)

	for _, cp := range ir.IterateColumnProps() {
		for i, ct := range cp.CanonicalType {
			var imp []string
			_, _, imp, err = codegen.GenerateGoCode(ct, cp.EncodingHints[i])
			if err != nil {
				err = eb.Build().Stringer("canonicalType", ct).Errorf("unable to generate go code for canonical type: %w", err)
				return
			}
			for _, im := range imp {
				if imports.Has(im) {
					continue
				}
				imports.Add(im)
				_, err = fmt.Fprintf(b, "\t%q\n", im)
				if err != nil {
					return
				}
			}
		}
	}
	for _, im := range []string{"slices", "github.com/stergiotis/boxer/public/observability/eh"} {
		_, err = fmt.Fprintf(b, "\t%q\n", im)
		if err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, "\t%q\n", "errors")
	if err != nil {
		return
	}
	return
}

var _ gocodegen.CodeComposerI = (*GoClassBuilder)(nil)
var _ common.CodeBuilderHolderI = (*GoClassBuilder)(nil)

func deriveSubHolderSelectNonScalar(cc common.IntermediateColumnContext) (keep bool) {
	switch cc.SubType {
	case common.IntermediateColumnsSubTypeHomogenousArray,
		common.IntermediateColumnsSubTypeSet:
		return true
	}
	return false
}
func deriveSubHolderSelectNonScalarSupport(cc common.IntermediateColumnContext) (keep bool) {
	switch cc.SubType {
	case common.IntermediateColumnsSubTypeHomogenousArraySupport,
		common.IntermediateColumnsSubTypeSetSupport:
		return true
	}
	return false
}
func deriveSubHolderSelectMembership(cc common.IntermediateColumnContext) (keep bool) {
	return cc.SubType == common.IntermediateColumnsSubTypeMembership
}
func deriveSubHolderSelectMembershipSupport(cc common.IntermediateColumnContext) (keep bool) {
	return cc.SubType == common.IntermediateColumnsSubTypeMembershipSupport
}
func deriveSubHolderSelectScalar(cc common.IntermediateColumnContext) (keep bool) {
	return cc.SubType == common.IntermediateColumnsSubTypeScalar
}
func deriveSubHolderSelectTaggedValues(cc common.IntermediateColumnContext) (keep bool) {
	return cc.PlainItemType == common.PlainItemTypeNone
}
func deriveSubHolderSelectPlainValues(cc common.IntermediateColumnContext) (keep bool) {
	return cc.PlainItemType != common.PlainItemTypeNone
}
