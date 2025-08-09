package test

import (
	"fmt"
	"strings"
	"testing"

	common2 "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	ddl2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stretchr/testify/require"
)

func TestDdlGenerator(t *testing.T) {
	gen := ddl2.NewGeneratorDriver()
	techs := []common2.TechnologySpecificGeneratorI{
		arrow.NewTechnologySpecificCodeGenerator(),
		clickhouse.NewTechnologySpecificCodeGenerator(),
	}
	ir := common2.NewIntermediateTableRepresentation()
	conv, err := ddl2.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	const tableRowConfig = common2.TableRowConfigMultiAttributesPerRow

	var tbl common2.TableDesc
	tbl, err = mapping.NewJsonMapping()
	require.NoError(t, err)
	b := &strings.Builder{}
	for _, tech := range techs {
		b.Reset()
		tech.SetCodeBuilder(b)
		ir.Reset()
		err = ir.LoadFromTable(&tbl, tech)
		require.NoError(t, err)
		err = gen.GenerateColumnsCode(ir.IterateColumnProps(), tableRowConfig, conv, tech, func(hint encodingaspects.AspectE) (ok bool, msg string) {
			var status common2.ImplementationStatusE
			status, msg = tech.GetEncodingHintImplementationStatus(hint)
			ok = status == common2.ImplementationStatusFull
			ok = true // FIXME arrow dictionary
			return
		})
		require.NoError(t, err)
		fmt.Printf("%s\n%s", tech.GetTechnology().Name, b.String())
	}
}
