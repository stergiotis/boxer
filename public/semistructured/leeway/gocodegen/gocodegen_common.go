package gocodegen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func GenerateArrowSchemaFactory(b *strings.Builder, tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error) {
	arrowTech := arrow.NewTechnologySpecificCodeGenerator()
	arrowTech.SetCodeBuilder(b)
	ddlGenerator := ddl.NewGeneratorDriver()
	{ // schema factory
		var factoryName string
		factoryName, err = clsNamer.ComposeSchemaFactoryName(tableName)
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `func %s() (schema *arrow.Schema) {
		schema = arrow.NewSchema([]arrow.Field{
`, factoryName)
		if err != nil {
			return
		}

		err = ddlGenerator.GenerateColumnsCode(ir.IterateColumnProps(),
			tableRowConfig,
			namingConvention,
			arrowTech,
			func(hint encodingaspects2.AspectE) (ok bool, msg string) {
				return true, ""
			})
		if err != nil {
			return
		}
		_, err = b.WriteString(
			`		}, nil)
		return
}
`)
		if err != nil {
			return
		}
	}
	return
}
func ComposeCode(impl CodeComposerI, b *strings.Builder, tableName naming.StylableName, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error) {
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	impl.PrepareCodeComposition()

	if conv != nil {
		err = impl.ComposeNamingConventionDependentCode(tableName, ir, conv, tableRowConfig, clsNamer)
		if err != nil {
			err = eb.Build().Stringer("tableName", tableName).Errorf("unable to compose naming convention dependent code: %w", err)
			return
		}
	}
	entityIRH := common.NewIntermediatePairHolder(ir.TotalLength())
	for cc, cp := range ir.IterateColumnProps() {
		entityIRH.Add(cc, cp)
	}

	sectionNames := make([]naming.StylableName, 0, 32)
	maxColumnsPerSection := 0
	for _, t := range ir.TaggedValueDesc {
		secName := t.SectionName
		sectionNames = append(sectionNames, secName)
		maxColumnsPerSection = max(maxColumnsPerSection, t.Length())
	}
	slices.Sort(sectionNames)
	sectionNames = slices.Compact(sectionNames)

	err = impl.ComposeEntityClassAndFactoryCode(clsNamer, tableName, sectionNames, ir, tableRowConfig, entityIRH)
	if err != nil {
		err = eb.Build().Stringer("tableName", tableName).Errorf("unable to compose entity class and factory code: %w", err)
		return
	}
	err = impl.ComposeEntityCode(clsNamer, tableName, sectionNames, ir, tableRowConfig, entityIRH)
	if err != nil {
		err = eb.Build().Stringer("tableName", tableName).Errorf("unable to compose entity code: %w", err)
		return
	}

	sectionIRH := common.NewIntermediatePairHolder(maxColumnsPerSection)
	totalSections := len(sectionNames)
	for i, sectionName := range sectionNames {
		sectionIRH.Reset()
		for cc, cp := range ir.IterateColumnProps() {
			if cc.SectionName == sectionName && cc.PlainItemType == common.PlainItemTypeNone {
				sectionIRH.Add(cc, cp)
			}
		}

		baseErr := eb.Build().Stringer("sectionName", sectionName).Stringer("tableName", tableName)

		{
			err = impl.ComposeSectionClassAndFactoryCode(clsNamer, tableName, sectionName, i, totalSections,
				sectionIRH, tableRowConfig)
			if err != nil {
				err = baseErr.Errorf("unable to compose section class and factory code: %w", err)
				return
			}
			err = impl.ComposeSectionCode(clsNamer, tableName, sectionName, i, totalSections,
				sectionIRH, tableRowConfig)
			if err != nil {
				err = baseErr.Errorf("unable to compose section code: %w", err)
				return
			}
		}

		{
			err = impl.ComposeAttributeClassAndFactoryCode(clsNamer, tableName, sectionName, i,
				totalSections, sectionIRH, tableRowConfig)
			if err != nil {
				err = baseErr.Errorf("unable to compose attribute class and factoy code: %w", err)
				return
			}
			err = impl.ComposeAttributeCode(clsNamer, tableName, sectionName, i,
				totalSections, sectionIRH, tableRowConfig)
			if err != nil {
				err = baseErr.Errorf("unable to compose attribute code: %w", err)
				return
			}
		}
	}

	return
}
