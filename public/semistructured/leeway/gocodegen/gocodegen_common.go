package gocodegen

import (
	"fmt"
	"strings"

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
